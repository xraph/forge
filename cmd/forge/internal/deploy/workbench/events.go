package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
)

type Event struct {
	ID          uint64          `json:"id"`
	Time        time.Time       `json:"time"`
	Run         string          `json:"run,omitempty"`
	Target      string          `json:"target,omitempty"`
	Environment string          `json:"environment,omitempty"`
	Type        string          `json:"type"`
	Operation   *provider.Event `json:"operation,omitempty"`
	Error       *APIError       `json:"error,omitempty"`
}
type eventBuffer struct {
	mu      sync.Mutex
	next    uint64
	items   []Event
	changed chan struct{}
}

func newEvents() *eventBuffer { return &eventBuffer{changed: make(chan struct{})} }
func (b *eventBuffer) append(event Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.next++
	event.ID = b.next
	event.Time = time.Now().UTC()

	b.items = append(b.items, event)
	if len(b.items) > 256 {
		b.items = b.items[len(b.items)-256:]
	}

	close(b.changed)
	b.changed = make(chan struct{})
}
func (b *eventBuffer) after(id uint64) ([]Event, <-chan struct{}, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	items := []Event{}

	gap := id > b.next || (len(b.items) > 0 && id > 0 && id < b.items[0].ID-1)
	for _, event := range b.items {
		if event.ID > id {
			items = append(items, event)
		}
	}

	return items, b.changed, gap
}
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	after := r.URL.Query().Get("after")
	if after == "" {
		after = r.Header.Get("Last-Event-ID")
	}

	var (
		id  uint64
		err error
	)
	if after != "" {
		id, err = strconv.ParseUint(after, 10, 64)
		if err != nil {
			apiFailure(w, 400, "invalid event cursor", nil)

			return
		}
	}

	flush, ok := w.(http.Flusher)
	if !ok {
		apiFailure(w, 500, "streaming unavailable", nil)

		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flush.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		items, changed, gap := s.events.after(id)
		if gap {
			id = 0
			items, changed, _ = s.events.after(0)

			if _, err := fmt.Fprint(w, "event: reset\ndata: {\"reason\":\"event history expired; reload status\"}\n\n"); err != nil {
				return
			}

			flush.Flush()
		}

		for _, event := range items {
			raw, err := json.Marshal(event)
			if err != nil {
				return
			}

			if _, err := fmt.Fprintf(w, "id: %d\nevent: progress\ndata: %s\n\n", event.ID, raw); err != nil {
				return
			}

			id = event.ID

			flush.Flush()
		}

		select {
		case <-r.Context().Done():
			return
		case <-s.done:
			return
		case <-changed:
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}

			flush.Flush()
		}
	}
}

type Run struct {
	ID          string     `json:"id"`
	Action      string     `json:"action"`
	Target      string     `json:"target"`
	Environment string     `json:"environment"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	Error       *APIError  `json:"error,omitempty"`
}

func (s *Server) beginRun(action, target, env string, fn func(context.Context, chan<- provider.Event) error) (Run, error) {
	s.operations.Lock()
	defer s.operations.Unlock()

	select {
	case <-s.done:
		return Run{}, errors.New("workbench is closed")
	default:
	}

	if s.active != nil {
		return Run{}, errBusy
	}

	id, err := randomToken()
	if err != nil {
		return Run{}, err
	}

	run := Run{ID: id, Action: action, Target: target, Environment: env, Status: "running", StartedAt: time.Now().UTC()}
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	s.active = &run
	s.cancel = cancel
	s.runs[id] = run

	s.runOrder = append(s.runOrder, id)
	if len(s.runOrder) > 32 {
		delete(s.runs, s.runOrder[0])
		s.runOrder = s.runOrder[1:]
	}

	s.wg.Add(1)
	go func(run Run) {
		defer s.wg.Done()
		defer cancel()

		channel := make(chan provider.Event, 64)

		pumped := make(chan struct{})
		go func() {
			defer close(pumped)

			for operation := range channel {
				item := operation
				s.events.append(Event{Run: id, Target: target, Environment: env, Type: "operation", Operation: &item})
			}
		}()

		s.events.append(Event{Run: id, Target: target, Environment: env, Type: "started"})

		err := fn(ctx, channel)
		close(channel)
		<-pumped

		finished := time.Now().UTC()
		run.FinishedAt = &finished

		run.Status = "completed"
		if err != nil {
			run.Status = "failed"
			if errors.Is(ctx.Err(), context.Canceled) {
				run.Status = "cancelled"
			}

			run.Error = errorData(err)
		}

		s.operations.Lock()
		s.runs[id] = run
		s.active = nil
		s.cancel = nil
		s.operations.Unlock()
		s.events.append(Event{Run: id, Target: target, Environment: env, Type: run.Status, Error: run.Error})
	}(run)

	return run, nil
}
func (s *Server) currentRuns() []Run {
	s.operations.Lock()
	defer s.operations.Unlock()

	runs := []Run{}
	for _, id := range s.runOrder {
		runs = append(runs, s.runs[id])
	}

	return runs
}
func (s *Server) cancelRun(id string) error {
	s.operations.Lock()
	defer s.operations.Unlock()

	if s.active == nil || s.active.ID != id {
		return errors.New("active deployment run was not found")
	}

	s.cancel()

	return nil
}
