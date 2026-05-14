// control.go
package transport

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

// ControlMessage is one client request on POST /stream/control.
type ControlMessage struct {
	StreamID       string                          `json:"streamID"`
	Op             string                          `json:"op"` // "subscribe" | "unsubscribe"
	SubscriptionID string                          `json:"subscriptionID"`
	Contributor    string                          `json:"contributor,omitempty"`
	Intent         string                          `json:"intent,omitempty"`
	Params         map[string]contract.ParamSource `json:"params,omitempty"`
}

// ServeControl handles POST /api/dashboard/v1/stream/control.
func (b *StreamBroker) ServeControl(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var msg ControlMessage
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, "invalid control message", http.StatusBadRequest)
		return
	}
	b.mu.Lock()
	conn, ok := b.streams[msg.StreamID]
	b.mu.Unlock()
	if !ok {
		http.Error(w, "unknown streamID", http.StatusNotFound)
		return
	}
	switch msg.Op {
	case "subscribe":
		in, ok := b.reg.Intent(msg.Contributor, msg.Intent, intentVersionForSubscribe(b.reg, msg))
		if !ok || in.Kind != contract.IntentKindSubscription {
			http.Error(w, "intent not a subscription", http.StatusBadRequest)
			return
		}
		p := contract.PrincipalFor(conn.user)
		if !in.Requires.Allow(conn.user, nil) {
			http.Error(w, "permission denied", http.StatusForbidden)
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		ch, stop, err := b.source.Subscribe(ctx, p, msg.Contributor, in, msg.Params)
		if err != nil {
			cancel()
			http.Error(w, "subscribe failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		conn.mu.Lock()
		conn.subs[msg.SubscriptionID] = subscription{cancel: func() { stop(); cancel() }}
		conn.mu.Unlock()
		conn.wg.Add(1)
		go func() {
			defer conn.wg.Done()
			defer cancel()
			for ev := range ch {
				if !b.allowsEvent(conn.user, in) {
					continue
				}
				if ev.Mode != "" && in.Mode != "" && ev.Mode != in.Mode {
					log.Printf("contract/stream: %s/%s mode mismatch declared=%s emitted=%s", msg.Contributor, msg.Intent, in.Mode, ev.Mode)
				}
				if err := b.writeEvent(conn, msg.SubscriptionID, ev); err != nil {
					return
				}
			}
		}()
		w.WriteHeader(http.StatusOK)
	case "unsubscribe":
		conn.mu.Lock()
		s, ok := conn.subs[msg.SubscriptionID]
		if ok {
			s.cancel()
			delete(conn.subs, msg.SubscriptionID)
		}
		conn.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "unknown op", http.StatusBadRequest)
	}
}

// allowsEvent re-checks the user's predicate. Real per-event Warden invocation
// with a TTL cache lives in slice (b); slice (a) re-evaluates the YAML predicate
// against the current connection's UserInfo.
func (b *StreamBroker) allowsEvent(user *dashauth.UserInfo, in contract.Intent) bool {
	return in.Requires.Allow(user, nil)
}

func intentVersionForSubscribe(reg contract.Registry, msg ControlMessage) int {
	v, _ := reg.HighestVersion(msg.Contributor, msg.Intent)
	return v
}
