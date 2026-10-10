package state

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"slices"

	"time"
)

type Event struct {
	Time           time.Time `json:"time"`
	Op             string    `json:"op"`
	Status         Status    `json:"status"`
	Message        string    `json:"message"`
	ProviderID     string    `json:"provider_id,omitempty"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
}

type Journal interface {
	Record(ev Event) error
	Events() ([]Event, error)
	Completed(idempotencyKey string) (Event, bool)
}

type fileJournal struct {
	root *os.Root
	path string
}

func (s *Store) Journal() Journal {
	if s.db != nil {
		return &sqlJournal{store: s}
	}

	return &fileJournal{root: s.root, path: "journal.jsonl"}
}

type sqlJournal struct{ store *Store }

func (j *sqlJournal) Record(ev Event) error {
	if err := j.store.checkAuthority(); err != nil {
		return err
	}

	if j.store.lease == nil {
		return ErrLocked
	}

	raw, err := json.Marshal(ev)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return j.store.lease.Append(ctx, "journal.jsonl", append(raw, '\n'))
}
func (j *sqlJournal) Events() ([]Event, error) {
	raw, err := j.store.ReadFile("journal.jsonl")
	if os.IsNotExist(err) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return decodeEvents(bytes.NewReader(raw))
}
func (j *sqlJournal) Completed(key string) (Event, bool) {
	events, _ := j.Events()
	for _, event := range slices.Backward(events) {
		if event.IdempotencyKey == key && (event.Status == StatusAccepted || event.Status == StatusHealthy) {
			return event, true
		}
	}

	return Event{}, false
}

func (j *fileJournal) Record(ev Event) error {
	f, err := j.root.OpenFile(j.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}

	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}

	return f.Sync()
}

func (j *fileJournal) Events() ([]Event, error) {
	f, err := j.root.Open(j.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, err
	}
	defer f.Close()

	return decodeEvents(f)
}
func decodeEvents(reader io.Reader) ([]Event, error) {
	var out []Event

	sc := bufio.NewScanner(reader)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for sc.Scan() {
		var ev Event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			return nil, err
		}

		out = append(out, ev)
	}

	return out, sc.Err()
}

func (j *fileJournal) Completed(key string) (Event, bool) {
	evs, _ := j.Events()
	for _, v := range slices.Backward(evs) {
		if v.IdempotencyKey == key && (v.Status == StatusAccepted || v.Status == StatusHealthy) {
			return v, true
		}
	}

	return Event{}, false
}
