package dispatcher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// These sequential fixtures also retain the old counters used by tombstone tests.
type sequentialClaimStore struct{ *stubStore }

func newSequentialClaimStore() *sequentialClaimStore { return &sequentialClaimStore{newStubStore()} }
func (s *sequentialClaimStore) Claim(ctx context.Context, key, identity string) (IdempotencyClaim, error) {
	if cached, hit := s.Lookup(ctx, key, identity); hit {
		return IdempotencyClaim{Cached: cached}, nil
	}

	return IdempotencyClaim{End: func(ctx context.Context, c *IdempotencyCached) error {
		if c == nil {
			return nil
		}

		return s.Store(ctx, key, identity, *c)
	}}, nil
}

func bindingRecord(t *testing.T, req contract.Request, p contract.Principal) IdempotencyCached {
	t.Helper()

	digest, err := requestBinding(context.Background(), req, p, handlerEntry{})
	if err != nil {
		t.Fatal(err)
	}

	body, err := json.Marshal(boundResponse{Format: bindingFormat, Version: bindingVersion, Binding: digest, Response: contract.Response{OK: true, Envelope: req.Envelope, Kind: req.Kind, Data: json.RawMessage(`{"cached":true}`), Meta: contract.ResponseMeta{IntentVersion: req.IntentVersion}}})
	if err != nil {
		t.Fatal(err)
	}

	return IdempotencyCached{Status: TombstoneStatus, WireBody: body, StoredAt: time.Now(), TTL: time.Hour}
}

func requireBindingReason(t *testing.T, err error, reason string) {
	t.Helper()

	var ce *contract.Error
	if !errors.As(err, &ce) || ce.Details[ReasonDetail] != reason {
		t.Fatalf("error=%v; want reason %s", err, reason)
	}
}

type namedBindingString string
type bindingMarshaler struct{ called *bool }

func (m bindingMarshaler) MarshalJSON() ([]byte, error) {
	*m.called = true

	return []byte(`null`), nil
}

func encodeBindingValue(value any) ([]byte, error) {
	c := bindingEncoder{}
	c.value(value, 0)

	return c.buf.Bytes(), c.err
}

func TestBinding_CanonicalDomain(t *testing.T) {
	values := []any{nil, false, true, "", "1", int(1), int8(1), int16(1), int32(1), int64(1), uint(1), uint8(1), uint16(1), uint32(1), uint64(1), float32(1), float64(1), json.Number("1"), json.Number("1.0"), json.Number("1e0"), json.Number("9007199254740993"), int64(9007199254740993), float64(9007199254740992), math.Copysign(0, -1), float64(0), float32(math.Copysign(0, -1)), float32(0), json.RawMessage(nil), json.RawMessage{}, json.RawMessage("1"), []byte(nil), []byte{}, []byte("1"), []string(nil), []string{}, []string{"a", "b"}, []string{"b", "a"}, []any(nil), []any{}, []any{"a", "b"}, map[string]string(nil), map[string]string{}, map[string]any(nil), map[string]any{}, []byte{255}, json.RawMessage{255}}
	seen := map[string]any{}

	for _, value := range values {
		encoded, err := encodeBindingValue(value)
		if err != nil {
			t.Fatalf("%T: %v", value, err)
		}

		if previous, exists := seen[string(encoded)]; exists {
			t.Fatalf("collision %T(%v) / %T(%v)", previous, previous, value, value)
		}

		seen[string(encoded)] = value
	}

	first, _ := encodeBindingValue(map[string]any{"b": []any{2, "z"}, "a": map[string]string{"y": "2", "x": "1"}})
	secondMap := map[string]any{}
	secondMap["a"] = map[string]string{"x": "1", "y": "2"}
	secondMap["b"] = []any{2, "z"}

	second, _ := encodeBindingValue(secondMap)
	if !bytes.Equal(first, second) {
		t.Fatal("map order changed binding")
	}

	called := false
	cyclic := map[string]any{}
	cyclic["self"] = cyclic

	invalid := []any{namedBindingString("a"), bindingMarshaler{&called}, struct{}{}, new(int), func() {}, make(chan int), []int{1}, map[int]string{}, uintptr(1), math.NaN(), math.Inf(1), float32(math.Inf(-1)), json.Number(""), json.Number("01"), json.Number("NaN"), string([]byte{255}), map[string]any{string([]byte{255}): true}, cyclic, strings.Repeat("a", bindingMaxBytes), make([]any, bindingMaxNodes)}
	for _, value := range invalid {
		if _, err := encodeBindingValue(value); err == nil {
			t.Fatalf("accepted %T", value)
		}
	}

	if called {
		t.Fatal("called custom marshaler")
	}

	var nested any = true
	for range bindingMaxDepth {
		nested = []any{nested}
	}

	if _, err := encodeBindingValue(nested); err != nil {
		t.Fatal("rejected 64 containers", err)
	}

	if _, err := encodeBindingValue([]any{nested}); err == nil {
		t.Fatal("accepted 65 containers")
	}
}

func TestBinding_LookupRefusalsNeverMutateOrRewrite(t *testing.T) {
	req, p := mintRequest(), alice()
	valid := bindingRecord(t, req, p)
	body := string(valid.WireBody)
	cases := map[string]IdempotencyCached{
		"legacy":       {Status: 200, WireBody: json.RawMessage(`{"ok":true,"data":{"private":true}}`)},
		"malformed":    {Status: TombstoneStatus, WireBody: json.RawMessage(`{`)},
		"wrong-status": {Status: 200, WireBody: valid.WireBody},
	}

	replacements := map[string][2]string{
		"unknown-version":      {`"version":1`, `"version":2`},
		"missing-digest":       {`"binding":"sha256:`, `"binding":"`},
		"uppercase-digest":     {`"binding":"sha256:`, `"binding":"SHA256:`},
		"wrong-kind":           {`"kind":"command"`, `"kind":"query"`},
		"wrong-intent-version": {`"intentVersion":1`, `"intentVersion":2`},
		"not-success":          {`"ok":true`, `"ok":false`},
		"null-success":         {`"ok":true`, `"ok":null`},
		"duplicate-format":     {`"format":`, `"format":"other","format":`},
		"duplicate-ok":         {`"ok":true`, `"ok":false,"ok":true`},
		"duplicate-meta":       {`"intentVersion":1`, `"intentVersion":2,"intentVersion":1`},
		"case-alias":           {`"ok":true`, `"OK":true`},
		"unknown-field":        {`"ok":true`, `"ok":true,"error":{}`},
		"null-meta":            {`"meta":{"intentVersion":1}`, `"meta":null`},
	}
	for name, replacement := range replacements {
		cases[name] = IdempotencyCached{Status: TombstoneStatus, WireBody: json.RawMessage(strings.Replace(body, replacement[0], replacement[1], 1))}
	}

	cases["trailing"] = IdempotencyCached{Status: TombstoneStatus, WireBody: append(append([]byte{}, valid.WireBody...), []byte(` {}`)...)}
	for name, record := range cases {
		t.Run(name, func(t *testing.T) {
			store := newStubStore()
			store.hits["k1|alice:keys.create"] = record
			d, calls := mintCommand(t, store)
			data, meta, err := d.Dispatch(context.Background(), req, p)
			requireBindingReason(t, err, ReasonBindingConflict)

			if data != nil || !reflect.DeepEqual(meta, contract.ResponseMeta{}) || *calls != 0 || store.puts != 0 || !reflect.DeepEqual(store.hits["k1|alice:keys.create"], record) {
				t.Fatal("refusal disclosed, executed or rewrote")
			}
		})
	}

	store := newStubStore()
	store.hits["k1|alice:keys.create"] = valid

	d, calls := mintCommand(t, store)
	if data, _, err := d.Dispatch(context.Background(), req, p); err != nil || string(data) != `{"cached":true}` {
		t.Fatalf("bound lookup %s %v", data, err)
	}

	delete(store.hits, "k1|alice:keys.create")

	_, _, err := d.Dispatch(context.Background(), req, p)
	requireBindingReason(t, err, ReasonClaimRequired)

	if *calls != 0 || store.puts != 0 {
		t.Fatal("lookup miss mutated")
	}
}

func TestBinding_SnapshotAndScope(t *testing.T) {
	req, p := mintRequest(), alice()
	req.Params = map[string]any{"target": "a"}
	store := newClaimStore()
	d := NewWithOptions(nil, WithIdempotencyStore(store))
	scopeCalls := 0
	scope := IdempotencyScope(func(context.Context, contract.Request, contract.Principal) ([]byte, error) {
		scopeCalls++

		return []byte("trusted-v1/installation/tenant"), nil
	})
	calls := 0

	if err := d.Register(req.Contributor, req.Intent, req.IntentVersion, func(_ context.Context, _ json.RawMessage, params map[string]any, principal contract.Principal) (*Result, error) {
		calls++
		params["target"] = "changed"
		principal.User.Metadata = map[string]any{"changed": true}

		return &Result{Data: json.RawMessage(`{"done":true}`)}, nil
	}, scope); err != nil {
		t.Fatal(err)
	}

	if _, _, err := d.Dispatch(context.Background(), req, p); err != nil {
		t.Fatal(err)
	}

	req.Params["target"] = "a"

	p.User.Metadata = nil
	if _, _, err := d.Dispatch(context.Background(), req, p); err != nil {
		t.Fatal(err)
	}

	if calls != 1 || scopeCalls != 4 {
		t.Fatalf("calls=%d scope=%d", calls, scopeCalls)
	}

	if err := New(nil).Register("c", "i", 1, func(context.Context, json.RawMessage, map[string]any, contract.Principal) (*Result, error) {
		return &Result{}, nil
	}, IdempotencyScope(nil)); err == nil {
		t.Fatal("accepted nil scope")
	}

	entry := handlerEntry{}
	IdempotencyScope(func(context.Context, contract.Request, contract.Principal) ([]byte, error) { return []byte("a"), nil })(&entry)
	IdempotencyScope(func(context.Context, contract.Request, contract.Principal) ([]byte, error) { return []byte("bc"), nil })(&entry)

	one, err := requestBinding(context.Background(), req, p, entry)
	if err != nil {
		t.Fatal(err)
	}

	other := handlerEntry{}
	IdempotencyScope(func(context.Context, contract.Request, contract.Principal) ([]byte, error) { return []byte("ab"), nil })(&other)
	IdempotencyScope(func(context.Context, contract.Request, contract.Principal) ([]byte, error) { return []byte("c"), nil })(&other)

	two, err := requestBinding(context.Background(), req, p, other)
	if err != nil || one == two {
		t.Fatal("scope framing collision", err)
	}
}

type invalidClaimStore struct {
	*stubStore

	endCalls int
}

func (s *invalidClaimStore) Claim(context.Context, string, string) (IdempotencyClaim, error) {
	return IdempotencyClaim{Cached: &IdempotencyCached{}, End: func(context.Context, *IdempotencyCached) error {
		s.endCalls++

		return nil
	}}, nil
}

type nilHitStore struct{ *stubStore }

func (s nilHitStore) Lookup(context.Context, string, string) (*IdempotencyCached, bool) {
	return nil, true
}

func TestBinding_InvalidBackendOwnership(t *testing.T) {
	store := &invalidClaimStore{stubStore: newStubStore()}
	for _, backend := range []IdempotencyStore{store, nilHitStore{newStubStore()}} {
		d, calls := mintCommand(t, backend)
		_, _, err := d.Dispatch(context.Background(), mintRequest(), alice())
		requireBindingReason(t, err, ReasonClaimFailed)

		if *calls != 0 || store.endCalls != 0 {
			t.Fatal("assumed ownership or executed invalid union")
		}
	}
}

func TestBinding_UnencodableSuccessConsumesClaim(t *testing.T) {
	store := newClaimStore()
	d := NewWithOptions(nil, WithIdempotencyStore(store))

	req := mintRequest()
	if err := d.Register(req.Contributor, req.Intent, 1, func(context.Context, json.RawMessage, map[string]any, contract.Principal) (*Result, error) {
		return &Result{Data: json.RawMessage(`invalid`)}, nil
	}); err != nil {
		t.Fatal(err)
	}

	if _, _, err := d.Dispatch(context.Background(), req, alice()); err != nil {
		t.Fatal(err)
	}

	record, ok := store.Lookup(context.Background(), "k1", "alice:keys.create")
	if !ok || record.Status != TombstoneStatus || len(record.WireBody) != 0 {
		t.Fatalf("entry=%+v", record)
	}

	_, _, err := d.Dispatch(context.Background(), req, alice())
	requireBindingReason(t, err, ReasonAlreadyRan)

	_, stores, releases, held := store.counts()
	if stores != 1 || releases != 0 || held != 0 {
		t.Fatalf("ownership %d %d %d", stores, releases, held)
	}
}

func TestBinding_AdmissionPrecedesBindingAndBypassSkipsIt(t *testing.T) {
	for _, mode := range []string{"denied", "invalid", "scope-error", "scope-limit", "bypass"} {
		t.Run(mode, func(t *testing.T) {
			backend, observed := observedAdmissionStore(true)
			scopeCalls := 0

			opts := []RegisterOption{BeforeDispatch(func(context.Context, contract.Request, contract.Principal) error {
				if mode == "denied" {
					return &contract.Error{Code: contract.CodePermissionDenied}
				}

				return nil
			}), IdempotencyScope(func(context.Context, contract.Request, contract.Principal) ([]byte, error) {
				scopeCalls++

				switch mode {
				case "scope-error":
					return nil, errors.New("private credential")
				case "scope-limit":
					return make([]byte, bindingMaxScopeBytes+1), nil
				}

				return nil, nil
			})}
			if mode == "bypass" {
				opts = append(opts, BypassIdempotency())
			}

			d, calls := mintCommand(t, backend, opts...)

			req := mintRequest()
			if mode == "denied" || mode == "invalid" {
				req.Params = map[string]any{"unsupported": make(chan int)}
			}

			principal := alice()
			if mode == "bypass" {
				principal.Claims["unsupported"] = make(chan int)
			}

			_, _, err := d.Dispatch(context.Background(), req, principal)

			switch mode {
			case "denied":
				requireAdmissionCode(t, err, contract.CodePermissionDenied)
			case "scope-error":
				requireAdmissionCode(t, err, contract.CodeInternal)

				if strings.Contains(err.Error(), "credential") {
					t.Fatal("leaked callback error")
				}
			case "bypass":
				if err != nil || *calls != 1 {
					t.Fatal(err)
				}
			default:
				requireAdmissionCode(t, err, contract.CodeBadRequest)
			}

			if observed.counts() != [4]int64{} {
				t.Fatal("binding failure or bypass accessed cache", observed.counts())
			}

			if mode != "bypass" && *calls != 0 {
				t.Fatal("binding failure executed")
			}

			if (mode == "denied" || mode == "bypass") && scopeCalls != 0 {
				t.Fatal("scope ran before admission or on bypass")
			}
		})
	}
}

func TestBinding_ScopeChangesDuringClaimWait(t *testing.T) {
	for _, cached := range []bool{false, true} {
		for _, fails := range []bool{false, true} {
			t.Run(fmt.Sprintf("cached=%t/error=%t", cached, fails), func(t *testing.T) {
				store := newClaimStore()

				held, err := store.Claim(context.Background(), "k1", "alice:keys.create")
				if err != nil {
					t.Fatal(err)
				}

				var changed atomic.Bool

				d, calls := mintCommand(t, store, IdempotencyScope(func(context.Context, contract.Request, contract.Principal) ([]byte, error) {
					if changed.Load() {
						if fails {
							return nil, errors.New("scope unavailable")
						}

						return []byte("new"), nil
					}

					return []byte("old"), nil
				}))
				done := make(chan error, 1)

				go func() {
					data, meta, err := d.Dispatch(context.Background(), mintRequest(), alice())
					if data != nil || !reflect.DeepEqual(meta, contract.ResponseMeta{}) {
						done <- errors.New("scope change disclosed cache")

						return
					}

					done <- err
				}()

				recv(t, store.waiting, "waiter")
				changed.Store(true)

				var record *IdempotencyCached

				if cached {
					entry := bindingRecord(t, mintRequest(), alice())
					record = &entry
				}

				if err := held.End(context.Background(), record); err != nil {
					t.Fatal(err)
				}

				err = recv(t, done, "result")
				if fails {
					requireAdmissionCode(t, err, contract.CodeInternal)
				} else {
					requireBindingReason(t, err, ReasonBindingConflict)
				}

				_, _, releases, heldCount := store.counts()

				wantReleases := 2
				if cached {
					wantReleases = 0
				}

				if *calls != 0 || releases != wantReleases || heldCount != 0 {
					t.Fatalf("ownership calls=%d releases=%d held=%d", *calls, releases, heldCount)
				}
			})
		}
	}
}

func TestBinding_CompetingRequestWaitsThenConflicts(t *testing.T) {
	store := newClaimStore()
	g := newGatedCommand(t, store, nil)
	first := g.dispatch(context.Background())
	recv(t, g.started, "first handler")

	done := make(chan error, 1)

	go func() {
		req := mintRequest()
		req.Payload = json.RawMessage(`{"name":"different"}`)

		_, _, err := g.d.Dispatch(context.Background(), req, alice())
		done <- err
	}()

	recv(t, store.waiting, "competitor")
	close(g.gate)

	if result := recv(t, first, "first"); result.err != nil {
		t.Fatal(result.err)
	}

	requireBindingReason(t, recv(t, done, "competitor"), ReasonBindingConflict)

	if g.calls.Load() != 1 {
		t.Fatal("competing operation executed")
	}
}

func TestBinding_ExactLimitsAndOpaqueData(t *testing.T) {
	encoded, err := encodeBindingValue(strings.Repeat("a", bindingMaxBytes-len("string")-9))
	if err != nil || len(encoded) != bindingMaxBytes {
		t.Fatalf("exact byte limit: size=%d err=%v", len(encoded), err)
	}

	entry := handlerEntry{}
	IdempotencyScope(func(context.Context, contract.Request, contract.Principal) ([]byte, error) {
		return make([]byte, bindingMaxScopeBytes), nil
	})(&entry)

	if _, err := requestBinding(context.Background(), mintRequest(), alice(), entry); err != nil {
		t.Fatal("exact scope limit", err)
	}

	for _, version := range []int{0, 1} {
		req := mintRequest()
		req.IntentVersion = version

		digest, err := requestBinding(context.Background(), req, alice(), handlerEntry{})
		if err != nil {
			t.Fatal(err)
		}

		record := boundResponse{Format: bindingFormat, Version: bindingVersion, Binding: digest, Response: contract.Response{OK: true, Envelope: req.Envelope, Kind: req.Kind, Data: json.RawMessage(`{"x":1,"x":2}`), Meta: contract.ResponseMeta{IntentVersion: version}}}

		wire, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}

		decoded, err := decodeBoundResponse(wire, req, digest)
		if err != nil || string(decoded.Data) != `{"x":1,"x":2}` {
			t.Fatal("protocol parser changed opaque application data", err)
		}
	}
}
