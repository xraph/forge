// stream.go
package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/google/uuid"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

// SubscriptionSource is the upstream events feeder. Slice (c) implements one
// for each contributor's subscription intents.
type SubscriptionSource interface {
	Subscribe(ctx context.Context, p contract.Principal, contributor string, intent contract.Intent, params map[string]contract.ParamSource) (<-chan contract.StreamEvent, func(), error)
}

// StreamBroker manages active SSE connections + their subscriptions.
type StreamBroker struct {
	reg    contract.Registry
	wreg   contract.WardenRegistry
	source SubscriptionSource

	mu      sync.Mutex
	streams map[string]*streamConn
}

type streamConn struct {
	id      string
	w       http.ResponseWriter
	flusher http.Flusher
	user    *dashauth.UserInfo
	subs    map[string]subscription // keyed by subscriptionID
	mu      sync.Mutex
	wg      sync.WaitGroup // tracks per-event goroutines so ServeStream can drain on close
}

type subscription struct {
	cancel func()
}

// NewStreamBroker returns a broker bound to a registry, warden registry, and source.
func NewStreamBroker(reg contract.Registry, wreg contract.WardenRegistry, source SubscriptionSource) *StreamBroker {
	return &StreamBroker{
		reg:     reg,
		wreg:    wreg,
		source:  source,
		streams: map[string]*streamConn{},
	}
}

// ServeStream implements GET /api/dashboard/v1/stream.
func (b *StreamBroker) ServeStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	id := uuid.NewString()
	conn := &streamConn{
		id: id, w: w, flusher: flusher,
		user: dashauth.UserFromContext(r.Context()),
		subs: map[string]subscription{},
	}
	b.mu.Lock()
	b.streams[id] = conn
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.streams, id)
		b.mu.Unlock()
		conn.mu.Lock()
		for _, s := range conn.subs {
			s.cancel()
		}
		conn.mu.Unlock()
		// Wait for all per-event goroutines to drain so callers (and tests)
		// observing ServeStream's return know no further writes will occur.
		conn.wg.Wait()
	}()

	// Send a hello event so the client learns its streamID
	conn.mu.Lock()
	fmt.Fprintf(w, "event: hello\ndata: {\"streamID\":%q}\n\n", id)
	flusher.Flush()
	conn.mu.Unlock()

	<-r.Context().Done()
}

// SnapshotIDs returns currently-active stream IDs (test helper / introspection).
func (b *StreamBroker) SnapshotIDs() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, 0, len(b.streams))
	for id := range b.streams {
		out = append(out, id)
	}
	return out
}

func (b *StreamBroker) writeEvent(conn *streamConn, subID string, ev contract.StreamEvent) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if _, err := fmt.Fprintf(conn.w, "event: %s\ndata: %s\n\n", subID, payload); err != nil {
		return err
	}
	conn.flusher.Flush()
	return nil
}
