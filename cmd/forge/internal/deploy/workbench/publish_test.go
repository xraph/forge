package workbench

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
)

func TestPublicationRequiresApproval(t *testing.T) {
	s, cookie := serverFixture(t)

	response := request(s, cookie, "POST", "/api/publish", `{"hash":"bad","approval":"wrong"}`)
	if response.Code != 409 {
		t.Fatal(response.Code, response.Body.String())
	}
}

type publicationEngine struct{ engineAPI }

func (publicationEngine) LoadPlan(_ context.Context, hash string) (*plan.Plan, error) {
	return &plan.Plan{Hash: hash, TargetName: "local", Environment: "dev"}, nil
}
func (publicationEngine) PublishImages(_ context.Context, _ *plan.Plan, _ string, _ chan<- provider.Event) (map[string]model.Image, error) {
	return map[string]model.Image{"api": {Repository: "ghcr.io/acme/api", Digest: "sha256:" + strings.Repeat("a", 64)}}, nil
}
func TestPublicationReturnsOnlyImmutableResult(t *testing.T) {
	s, cookie := serverFixture(t)
	s.engine = publicationEngine{engineAPI: s.engine}

	response := request(s, cookie, "POST", "/api/publish", `{"hash":"approved","approval":"approved"}`)
	if response.Code != 202 {
		t.Fatal(response.Code, response.Body.String())
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		runs := s.currentRuns()
		if len(runs) > 0 && runs[0].Status == "completed" {
			raw, err := json.Marshal(runs[0])
			if err != nil {
				t.Fatal(err)
			}

			if !strings.Contains(string(raw), "publication") || !strings.Contains(string(raw), "approved") || !strings.Contains(string(raw), "sha256:") {
				t.Fatal("missing immutable publication result", string(raw))
			}

			return
		}

		time.Sleep(time.Millisecond)
	}

	t.Fatal("publication did not complete")
}
