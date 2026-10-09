// Package state keeps the journal, lock and release history for one target
// and environment under .forge/state.
package state

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
)

type Status string

const (
	StatusPlanned   Status = "planned"
	StatusApplying  Status = "applying"
	StatusAccepted  Status = "accepted"
	StatusHealthy   Status = "healthy"
	StatusFailed    Status = "failed"
	StatusUnknown   Status = "unknown"
	StatusCancelled Status = "cancelled"
	StatusPartial   Status = "partial"
)

type Release struct {
	ID        string        `json:"id"`
	PlanHash  string        `json:"plan_hash"`
	Images    []model.Image `json:"images"`
	AppliedAt time.Time     `json:"applied_at"`
	Status    Status        `json:"status"`
	// Migrations lists migration ids this release ran; reversible is per id.
	Migrations map[string]bool `json:"migrations,omitempty"`
}

type ResourceState struct {
	Name       string             `json:"name"`
	Type       model.ResourceType `json:"type"`
	Lifecycle  model.Lifecycle    `json:"lifecycle"`
	ProviderID string             `json:"provider_id"`
	CreatedAt  time.Time          `json:"created_at"`
}

type WorkloadState struct {
	PlanHash  string      `json:"plan_hash"`
	Resources []string    `json:"resources"`
	Image     model.Image `json:"image"`
}

type Snapshot struct {
	Revision        uint64                   `json:"revision,omitempty"`
	Workloads       map[string]WorkloadState `json:"workloads,omitempty"`
	Identities      map[string][]string      `json:"identities,omitempty"`
	FailedOperation string                   `json:"failed_operation,omitempty"`
	ActivePlanHash  string                   `json:"active_plan_hash,omitempty"`
	Releases        []Release                `json:"releases"`
	Resources       map[string]ResourceState `json:"resources"`
	Status          Status                   `json:"status"`
}

var ErrLocked = errors.New("another apply holds the lock")

type Store struct {
	dir  string
	root *os.Root
}

func Open(projectRoot, target, env string) (*Store, error) {
	if !regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`).MatchString(target) || !regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`).MatchString(env) {
		return nil, errors.New("invalid target or environment name")
	}

	project, err := os.OpenRoot(projectRoot)
	if err != nil {
		return nil, err
	}
	defer project.Close()

	rel := filepath.Join(".forge", "state", target, env)
	if err := project.MkdirAll(rel, 0700); err != nil {
		return nil, err
	}

	anchored, err := project.OpenRoot(rel)
	if err != nil {
		return nil, err
	}

	_ = project.Chmod(".forge", 0700)

	return &Store{dir: filepath.Join(projectRoot, rel), root: anchored}, nil
}

func (s *Store) Dir() string         { return s.dir }
func (s *Store) SecretsPath() string { return filepath.Join(s.dir, "generated.env") }

func (s *Store) Lock(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	f, err := s.root.OpenFile("lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}

	return fileLock(f)
}

func (s *Store) Snapshot() (Snapshot, error) {
	snap := Snapshot{Resources: map[string]ResourceState{}, Status: StatusUnknown}

	data, err := s.root.ReadFile("snapshot.json")
	if err != nil {
		if os.IsNotExist(err) {
			return snap, nil
		}

		return snap, err
	}

	return snap, json.Unmarshal(data, &snap)
}

func (s *Store) SaveSnapshot(snap Snapshot) error {
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}

	return s.WriteFile("snapshot.json", data)
}

func (s *Store) RecordRelease(r Release) error {
	snap, err := s.Snapshot()
	if err != nil {
		return err
	}

	for i, old := range snap.Releases {
		if old.ID == r.ID {
			snap.Releases[i] = r

			return s.SaveSnapshot(snap)
		}
	}

	snap.Releases = append(snap.Releases, r)
	if len(snap.Releases) > 20 {
		snap.Releases = snap.Releases[len(snap.Releases)-20:]
	}

	return s.SaveSnapshot(snap)
}

func (s *Store) Close() error                         { return s.root.Close() }
func (s *Store) ReadFile(path string) ([]byte, error) { return s.root.ReadFile(path) }
func (s *Store) WriteFile(path string, data []byte) error {
	if !filepath.IsLocal(path) {
		return errors.New("invalid state path")
	}

	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return err
	}

	temp := ".state-" + hex.EncodeToString(token)

	f, err := s.root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = s.root.Remove(temp) }()

	if _, err := f.Write(data); err != nil {
		f.Close()

		return err
	}

	if err := f.Sync(); err != nil {
		f.Close()

		return err
	}

	if err := f.Close(); err != nil {
		return err
	}

	return s.root.Rename(temp, path)
}

func (s *Store) MkdirAll(path string) error {
	if !filepath.IsLocal(path) {
		return errors.New("invalid state path")
	}

	return s.root.MkdirAll(path, 0700)
}
