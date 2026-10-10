package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/idempotency"
	"github.com/xraph/forge/middleware"
)

func requireCacheReason(t *testing.T, err error, reason string) {
	t.Helper()

	var ce *contract.Error
	if !errors.As(err, &ce) || ce.Details[dispatcher.ReasonDetail] != reason {
		t.Fatalf("err=%v; want %s", err, reason)
	}
}

func bindingRequest() contract.Request {
	return contract.Request{Envelope: "v1", Kind: contract.KindCommand, Contributor: "a", Intent: "run.cancel", IntentVersion: 1, IdempotencyKey: "same-key", Payload: json.RawMessage(`{"value":1}`), Params: map[string]any{"tenant": "tenant-a", "target": "run-a"}}
}
func bindingPrincipal() contract.Principal {
	return contract.Principal{User: &dashauth.UserInfo{Subject: "operator", Roles: []string{"admin", "reader"}, Scopes: []string{"read", "write"}, Claims: map[string]any{"tenant": "tenant-a"}, Metadata: map[string]any{"region": "west"}}, Claims: map[string]any{"tenant": "tenant-a", "principal_kind": "user"}}
}

func TestCacheBinding_ProductionAdapterCollisions(t *testing.T) {
	changes := map[string]func(*contract.Request, *contract.Principal, *string){
		"contributor": func(r *contract.Request, _ *contract.Principal, _ *string) { r.Contributor = "b" },
		"version":     func(r *contract.Request, _ *contract.Principal, _ *string) { r.IntentVersion = 2 },
		"envelope":    func(r *contract.Request, _ *contract.Principal, _ *string) { r.Envelope = "v2" },
		"target":      func(r *contract.Request, _ *contract.Principal, _ *string) { r.Params["target"] = "run-b" },
		"tenant": func(r *contract.Request, p *contract.Principal, _ *string) {
			r.Params["tenant"] = "tenant-b"
			p.Claims["tenant"] = "tenant-b"
		},
		"principal-kind": func(_ *contract.Request, p *contract.Principal, _ *string) {
			p.Claims["principal_kind"] = "service_acct"
		},
		"provider":     func(_ *contract.Request, p *contract.Principal, _ *string) { p.User.ProviderName = "other" },
		"display-name": func(_ *contract.Request, p *contract.Principal, _ *string) { p.User.DisplayName = "changed" },
		"email":        func(_ *contract.Request, p *contract.Principal, _ *string) { p.User.Email = "new@example.com" },
		"avatar":       func(_ *contract.Request, p *contract.Principal, _ *string) { p.User.AvatarURL = "avatar" },
		"roles": func(_ *contract.Request, p *contract.Principal, _ *string) {
			p.User.Roles = []string{"reader", "admin"}
		},
		"scopes":      func(_ *contract.Request, p *contract.Principal, _ *string) { p.User.Scopes = []string{"write", "read"} },
		"user-claims": func(_ *contract.Request, p *contract.Principal, _ *string) { p.User.Claims["exp"] = 123 },
		"metadata":    func(_ *contract.Request, p *contract.Principal, _ *string) { p.User.Metadata["region"] = "east" },
		"nested-claim": func(_ *contract.Request, p *contract.Principal, _ *string) {
			p.Claims["nested"] = map[string]any{"level": []any{1, 2}}
		},
		"payload-bytes": func(r *contract.Request, _ *contract.Principal, _ *string) {
			r.Payload = json.RawMessage(`{ "value":1}`)
		},
		"trusted-installation":  func(_ *contract.Request, _ *contract.Principal, s *string) { *s = "v1/installation-b/tenant-a" },
		"trusted-policy-tenant": func(_ *contract.Request, _ *contract.Principal, s *string) { *s = "v1/installation-a/tenant-b" },
		"colon-identity": func(r *contract.Request, p *contract.Principal, _ *string) {
			p.User.Subject = "operator:run"
			r.Intent = "cancel"
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			store := idempotency.NewInMemoryStore()
			d := dispatcher.NewWithOptions(nil, dispatcher.WithIdempotencyStore(AdaptIdempotencyStore(store)))
			calls := 0
			scope := "v1/installation-a/tenant-a"

			for _, route := range []struct {
				contributor, intent string
				version             int
			}{{"a", "run.cancel", 1}, {"b", "run.cancel", 1}, {"a", "run.cancel", 2}, {"a", "cancel", 1}} {
				err := d.Register(route.contributor, route.intent, route.version, func(_ context.Context, _ json.RawMessage, params map[string]any, p contract.Principal) (*dispatcher.Result, error) {
					calls++
					data, err := json.Marshal(map[string]any{"target": params["target"], "tenant": params["tenant"], "principal-kind": p.Claims["principal_kind"]})

					return &dispatcher.Result{Data: data, ExtraInvalidates: []string{"runs"}}, err
				}, dispatcher.BeforeDispatch(func(_ context.Context, r contract.Request, p contract.Principal) error {
					if p.Claims["tenant"] != r.Params["tenant"] {
						return &contract.Error{Code: contract.CodePermissionDenied}
					}

					return nil
				}), dispatcher.IdempotencyScope(func(context.Context, contract.Request, contract.Principal) ([]byte, error) { return []byte(scope), nil }))
				if err != nil {
					t.Fatal(err)
				}
			}

			req, p := bindingRequest(), bindingPrincipal()

			if name == "colon-identity" { // use an explicit colliding pair with a colon in the intent
				req.Intent = "run:cancel"
				p.User.Subject = "operator"

				if err := d.Register("a", "run:cancel", 1, func(context.Context, json.RawMessage, map[string]any, contract.Principal) (*dispatcher.Result, error) {
					calls++

					return &dispatcher.Result{Data: json.RawMessage(`{"private":true}`)}, nil
				}, dispatcher.IdempotencyScope(func(context.Context, contract.Request, contract.Principal) ([]byte, error) { return []byte(scope), nil })); err != nil {
					t.Fatal(err)
				}
			}

			if _, _, err := d.Dispatch(context.Background(), req, p); err != nil {
				t.Fatal(err)
			}

			identity := p.User.Subject + ":" + req.Intent

			before, hit := store.Lookup(context.Background(), req.IdempotencyKey, identity)
			if !hit {
				t.Fatal("missing first record")
			}

			change(&req, &p, &scope)
			data, meta, err := d.Dispatch(context.Background(), req, p)
			requireCacheReason(t, err, dispatcher.ReasonBindingConflict)

			after, _ := store.Lookup(context.Background(), req.IdempotencyKey, identity)
			if calls != 1 || data != nil || !reflect.DeepEqual(meta, contract.ResponseMeta{}) || !reflect.DeepEqual(before, after) {
				t.Fatal("collision executed, disclosed or rewrote")
			}
		})
	}
}

func TestCacheBinding_MatchingReplayAndAnonymousDistinctions(t *testing.T) {
	for _, change := range []string{"metadata-only", "nil-user", "empty-claims"} {
		t.Run(change, func(t *testing.T) {
			d := dispatcher.NewWithOptions(nil, dispatcher.WithIdempotencyStore(AdaptIdempotencyStore(idempotency.NewInMemoryStore())))
			calls := 0
			req := bindingRequest()
			p := contract.Principal{}

			if err := d.Register(req.Contributor, req.Intent, 1, func(context.Context, json.RawMessage, map[string]any, contract.Principal) (*dispatcher.Result, error) {
				calls++

				return &dispatcher.Result{Data: json.RawMessage(`{"private":true}`), ExtraInvalidates: []string{"runs"}}, nil
			}); err != nil {
				t.Fatal(err)
			}

			first, firstMeta, err := d.Dispatch(context.Background(), req, p)
			if err != nil {
				t.Fatal(err)
			}

			switch change {
			case "nil-user":
				p.User = &dashauth.UserInfo{}
			case "empty-claims":
				p.Claims = map[string]any{}
			default:
				req.CSRF = "fresh"
				req.Context = contract.RequestContext{Route: "untrusted-tenant", CorrelationID: "fresh"}
			}

			data, meta, err := d.Dispatch(context.Background(), req, p)
			if change == "metadata-only" {
				if err != nil || !bytes.Equal(data, first) || !reflect.DeepEqual(meta, firstMeta) {
					t.Fatal("matching replay changed", err)
				}
			} else {
				requireCacheReason(t, err, dispatcher.ReasonBindingConflict)
			}

			if calls != 1 {
				t.Fatal("executed twice")
			}
		})
	}
}

// This is the inspected previous reader, retained as an executable compatibility
// check. Old records remain unsafe when an old reader is still serving traffic.
func previousCacheReader(c *idempotency.Cached) (json.RawMessage, bool) {
	if c.Status == dispatcher.TombstoneStatus {
		return nil, true
	}

	var resp contract.Response
	if json.Unmarshal(c.WireBody, &resp) == nil && resp.OK {
		return resp.Data, true
	}

	return nil, false
}

func TestCacheBinding_CompatibilityAndSharedReopen(t *testing.T) {
	ctx := context.Background()
	backend := middleware.NewMemoryIdempotencyStore()
	store := idempotency.NewSharedStore(backend)
	req, p := bindingRequest(), bindingPrincipal()

	d := dispatcher.NewWithOptions(nil, dispatcher.WithIdempotencyStore(AdaptIdempotencyStore(store)))
	if err := d.Register(req.Contributor, req.Intent, 1, func(context.Context, json.RawMessage, map[string]any, contract.Principal) (*dispatcher.Result, error) {
		return &dispatcher.Result{Data: json.RawMessage(`{"private":true}`)}, nil
	}); err != nil {
		t.Fatal(err)
	}

	if _, _, err := d.Dispatch(ctx, req, p); err != nil {
		t.Fatal(err)
	}

	bound, hit := store.Lookup(ctx, req.IdempotencyKey, "operator:run.cancel")
	if !hit {
		t.Fatal("missing bound record")
	}

	if data, answered := previousCacheReader(bound); len(data) != 0 || !answered {
		t.Fatal("old reader did not refuse new record")
	}

	legacy := idempotency.Cached{Status: 200, WireBody: json.RawMessage(`{"ok":true,"data":{"other-tenant":true}}`), StoredAt: time.Now(), TTL: time.Hour}
	if err := store.Store(ctx, "legacy", "operator:run.cancel", legacy); err != nil {
		t.Fatal(err)
	}

	if data, answered := previousCacheReader(&legacy); !answered || !strings.Contains(string(data), "other-tenant") {
		t.Fatal("retained old writer/reader negative no longer demonstrates disclosure")
	}
	// A late old writer can overwrite a protected entry. This remains an unsafe
	// mixed-deployment condition, not a migration performed by the new reader.
	if err := store.Store(ctx, req.IdempotencyKey, "operator:run.cancel", legacy); err != nil {
		t.Fatal(err)
	}

	overwritten, _ := store.Lookup(ctx, req.IdempotencyKey, "operator:run.cancel")
	if data, _ := previousCacheReader(overwritten); len(data) == 0 {
		t.Fatal("old write negative missing")
	}

	_, _, err := d.Dispatch(ctx, req, p)
	requireCacheReason(t, err, dispatcher.ReasonBindingConflict)

	tombstone := idempotency.Cached{Status: dispatcher.TombstoneStatus, StoredAt: time.Now(), TTL: time.Hour}
	if err := store.Store(ctx, "tombstone", "operator:run.cancel", tombstone); err != nil {
		t.Fatal(err)
	}

	held, err := store.Claim(ctx, "held", "operator:run.cancel")
	if err != nil {
		t.Fatal(err)
	}

	reopened := idempotency.NewSharedStore(backend)
	for key, want := range map[string]idempotency.Cached{"legacy": legacy, "tombstone": tombstone} {
		got, hit := reopened.Lookup(ctx, key, "operator:run.cancel")
		if !hit || !reflect.DeepEqual(*got, want) {
			t.Fatalf("reopen %s: %+v", key, got)
		}
	}

	wait, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()

	if _, err := reopened.Claim(wait, "held", "operator:run.cancel"); !errors.Is(err, idempotency.ErrClaimHeld) {
		t.Fatal("reopen lost live claim", err)
	}

	if err := held.End(ctx, nil); err != nil {
		t.Fatal(err)
	}
}

func TestCacheBinding_StaleCompletionPreservesSuccessor(t *testing.T) {
	for _, kind := range []string{"bound", "tombstone", "live"} {
		t.Run(kind, func(t *testing.T) {
			var mu sync.Mutex

			now := time.Now()
			clock := func() time.Time {
				mu.Lock()
				defer mu.Unlock()

				return now
			}
			shared := middleware.NewMemoryIdempotencyStore(middleware.MemoryIdempotencyClock(clock))
			store := idempotency.NewSharedStore(shared)
			rig := newMintRig(t, store, nil)
			first := rig.dispatch()
			await(t, rig.started, "first handler")
			mu.Lock()
			now = now.Add(2 * time.Minute)
			mu.Unlock()

			successor, err := store.Claim(context.Background(), "k1", "alice:keys.create")
			if err != nil || successor.End == nil {
				t.Fatal("successor did not acquire", err)
			}

			record := idempotency.Cached{Status: dispatcher.TombstoneStatus, StoredAt: now, TTL: time.Hour}

			if kind == "bound" {
				// Obtain real bound bytes through the same production adapter.
				seedStore := idempotency.NewInMemoryStore()
				seed := newMintRig(t, seedStore, nil)
				close(seed.gate)

				if result := await(t, seed.dispatch(), "seed"); result.err != nil {
					t.Fatal(result.err)
				}

				seeded, hit := seedStore.Lookup(context.Background(), "k1", "alice:keys.create")
				if !hit {
					t.Fatal("seed did not publish")
				}

				record.WireBody = seeded.WireBody
			}

			if kind != "live" {
				if err := successor.End(context.Background(), &record); err != nil {
					t.Fatal(err)
				}
			}

			close(rig.gate)

			if a := await(t, first, "stale handler"); a.err != nil {
				t.Fatal(a.err)
			}

			if kind == "live" {
				wait, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
				defer cancel()

				if _, err := store.Claim(wait, "k1", "alice:keys.create"); !errors.Is(err, idempotency.ErrClaimHeld) {
					t.Fatal("stale holder disturbed successor", err)
				}

				if err := successor.End(context.Background(), nil); err != nil {
					t.Fatal(err)
				}
			} else {
				got, hit := store.Lookup(context.Background(), "k1", "alice:keys.create")
				if !hit || !reflect.DeepEqual(*got, record) {
					t.Fatal("stale completion overwrote successor")
				}
			}
		})
	}
}
