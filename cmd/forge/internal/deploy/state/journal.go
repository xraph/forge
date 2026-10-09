package state

import (
	"bufio"
	"encoding/json"
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

func (s *Store) Journal() Journal { return &fileJournal{root: s.root, path: "journal.jsonl"} }

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

	var out []Event

	sc := bufio.NewScanner(f)
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
