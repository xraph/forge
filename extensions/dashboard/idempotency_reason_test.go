package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/idempotency"
	"github.com/xraph/forge/extensions/dashboard/contract/transport"
)

// wireRig is keysmith's keys.create behind the real HTTP transport, on a
// dispatcher over an adapted idempotency.Store. The handler signals started
// and then waits for gate.
type wireRig struct {
	h       http.Handler
	started chan struct{}
	gate    chan struct{}
}

func newWireRig(t *testing.T, store idempotency.Store, dispOpts ...dispatcher.Option) *wireRig {
	t.Helper()

	reg := contract.NewRegistry()

	var m contract.ContractManifest
	if err := contract.UnmarshalManifestForTest([]byte(`
schemaVersion: 1
contributor: { name: keysmith, envelope: { supports: [v1], preferred: v1 } }
intents:
  - { name: keys.create, kind: command, version: 1, capability: write }
`), &m); err != nil {
		t.Fatal(err)
	}

	if err := reg.Register(&m); err != nil {
		t.Fatal(err)
	}

	opts := append([]dispatcher.Option{dispatcher.WithIdempotencyStore(AdaptIdempotencyStore(store))}, dispOpts...)
	d := dispatcher.NewWithOptions(dispatcher.NoopMetricsEmitter{}, opts...)

	r := &wireRig{
		h:       transport.NewHandler(reg, contract.NewWardenRegistry(), d, nil),
		started: make(chan struct{}, 8),
		gate:    make(chan struct{}),
	}

	type out struct {
		Raw string `json:"raw"`
	}

	err := dispatcher.RegisterCommand(d, "keysmith", "keys.create", 1, func(ctx context.Context, _ struct{}, _ contract.Principal) (out, error) {
		r.started <- struct{}{}

		select {
		case <-r.gate:
		case <-ctx.Done():
			return out{}, ctx.Err()
		}

		return out{Raw: claimRaw}, nil
	}, dispatcher.SecretResponse())
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	return r
}

// post sends keys.create with idempotency key k1 as alice and decodes the
// envelope that comes back.
func (r *wireRig) post(t *testing.T) contract.ErrorResponse {
	t.Helper()

	env, err := r.send()
	if err != nil {
		t.Fatal(err)
	}

	return env
}

// postAsync is post on its own goroutine. A body that does not decode comes
// back as an envelope with no error, which every caller treats as a failure.
func (r *wireRig) postAsync() <-chan contract.ErrorResponse {
	out := make(chan contract.ErrorResponse, 1)

	go func() {
		env, err := r.send()
		if err != nil {
			env = contract.ErrorResponse{Error: &contract.Error{Code: contract.CodeInternal, Message: err.Error()}}
		}

		out <- env
	}()

	return out
}

func (r *wireRig) send() (contract.ErrorResponse, error) {
	body := `{"envelope":"v1","kind":"command","contributor":"keysmith","intent":"keys.create","intentVersion":1,"csrf":"c","idempotencyKey":"k1","payload":{}}`
	ctx := dashauth.WithUser(context.Background(), &dashauth.UserInfo{Subject: "alice"})
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/dashboard/v1", strings.NewReader(body))

	w := httptest.NewRecorder()
	r.h.ServeHTTP(w, req)

	var env contract.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		return env, fmt.Errorf("decode %s: %w", w.Body, err)
	}

	return env, nil
}

// wantReason checks that env is a failed envelope whose error carries reason
// in details, with the code and retryable flag the dispatcher gave it.
func wantReason(t *testing.T, env contract.ErrorResponse, code contract.ErrorCode, retryable bool, reason string) {
	t.Helper()

	if env.OK || env.Error == nil {
		t.Fatalf("envelope = %+v, want a failure", env)
	}

	if env.Error.Code != code || env.Error.Retryable != retryable {
		t.Errorf("error = %s retryable=%v, want %s retryable=%v", env.Error.Code, env.Error.Retryable, code, retryable)
	}

	if got := env.Error.Details[dispatcher.ReasonDetail]; got != reason {
		t.Errorf("details = %v, want %s=%q", env.Error.Details, dispatcher.ReasonDetail, reason)
	}
}

func TestIdempotencyReason_AlreadyRanOnTheWire(t *testing.T) {
	r := newWireRig(t, idempotency.NewInMemoryStore())
	close(r.gate)

	if env := r.post(t); env.Error != nil {
		t.Fatalf("first post failed: %+v", env.Error)
	}

	wantReason(t, r.post(t), contract.CodeConflict, false, dispatcher.ReasonAlreadyRan)
}

func TestIdempotencyReason_StillRunningOnTheWire(t *testing.T) {
	r := newWireRig(t, idempotency.NewInMemoryStore(), dispatcher.WithIdempotencyWait(30*time.Millisecond))

	first := r.postAsync()
	await(t, r.started, "the first handler to start")

	wantReason(t, r.post(t), contract.CodeConflict, true, dispatcher.ReasonStillRunning)

	close(r.gate)

	if env := await(t, first, "the first post"); env.Error != nil {
		t.Fatalf("first post failed: %+v", env.Error)
	}
}

func TestIdempotencyReason_ClaimFailedOnTheWire(t *testing.T) {
	r := newWireRig(t, brokenClaimStore{})
	close(r.gate)

	wantReason(t, r.post(t), contract.CodeUnavailable, true, dispatcher.ReasonClaimFailed)

	select {
	case <-r.started:
		t.Fatal("the handler ran although the claim failed")
	default:
	}
}

// brokenClaimStore is an idempotency.Claimer whose Claim always fails for a
// reason other than a held key.
type brokenClaimStore struct{ lookupOnlyStore }

func (brokenClaimStore) Claim(context.Context, string, string) (idempotency.Claim, error) {
	return idempotency.Claim{}, errors.New("store unreachable")
}
