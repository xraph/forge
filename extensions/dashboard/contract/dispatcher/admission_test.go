package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// Observe every generic cache operation, including lookup on a claiming store.
type admissionStore struct {
	IdempotencyStore

	activity [4]atomic.Int64
}

func (s *admissionStore) Lookup(ctx context.Context, key, identity string) (*IdempotencyCached, bool) {
	s.activity[0].Add(1)

	return s.IdempotencyStore.Lookup(ctx, key, identity)
}

func (s *admissionStore) Store(ctx context.Context, key, identity string, c IdempotencyCached) error {
	s.activity[1].Add(1)

	return s.IdempotencyStore.Store(ctx, key, identity, c)
}

func (s *admissionStore) counts() [4]int64 {
	return [4]int64{s.activity[0].Load(), s.activity[1].Load(), s.activity[2].Load(), s.activity[3].Load()}
}

type admissionClaimer struct{ *admissionStore }

func (s *admissionClaimer) Claim(ctx context.Context, key, identity string) (IdempotencyClaim, error) {
	s.activity[2].Add(1)

	claim, err := s.IdempotencyStore.(IdempotencyClaimer).Claim(ctx, key, identity)
	if end := claim.End; end != nil {
		claim.End = func(ctx context.Context, c *IdempotencyCached) error {
			s.activity[3].Add(1)

			return end(ctx, c)
		}
	}

	return claim, err
}

func observedAdmissionStore(claiming bool) (IdempotencyStore, *admissionStore) {
	s := &admissionStore{IdempotencyStore: newStubStore()}
	if claiming {
		s.IdempotencyStore = newClaimStore()

		return &admissionClaimer{s}, s
	}

	return s, s
}

func requireAdmissionCode(t *testing.T, err error, code contract.ErrorCode) {
	t.Helper()

	var ce *contract.Error
	if !errors.As(err, &ce) || ce.Code != code {
		t.Fatalf("error=%v, want %s", err, code)
	}
}

func TestAdmission_RegistrationKinds(t *testing.T) {
	for _, registration := range []string{"raw", "required", "command", "query"} {
		for _, kind := range []contract.Kind{contract.KindQuery, contract.KindCommand, contract.KindSubscribe, ""} {
			t.Run(registration+"/"+string(kind), func(t *testing.T) {
				store, observed := observedAdmissionStore(true)
				d := NewWithOptions(nil, WithIdempotencyStore(store))
				calls, admissions := 0, 0

				typed := func(context.Context, mintIn, contract.Principal) (mintOut, error) {
					calls++

					return mintOut{}, nil
				}

				admission := BeforeDispatch(func(context.Context, contract.Request, contract.Principal) error {
					admissions++

					return nil
				})

				var err error

				required := contract.Kind("")

				switch registration {
				case "raw", "required":
					opts := []RegisterOption{admission}

					if registration == "required" {
						required = contract.KindCommand
						opts = append(opts, RequireKind(contract.KindQuery), RequireKind(required))
					}

					err = d.Register("keysmith", "keys.create", 1, func(context.Context, json.RawMessage, map[string]any, contract.Principal) (*Result, error) {
						calls++

						return &Result{}, nil
					}, opts...)
				case "command":
					required = contract.KindCommand
					err = RegisterCommand(d, "keysmith", "keys.create", 1, typed, admission, RequireKind(contract.KindQuery))
				case "query":
					required = contract.KindQuery
					err = RegisterQuery(d, "keysmith", "keys.create", 1, typed, admission, RequireKind(contract.KindCommand))
				}

				if err != nil {
					t.Fatal(err)
				}

				req := mintRequest()
				req.Kind = kind

				_, _, err = d.Dispatch(context.Background(), req, alice())
				if required != "" && required != kind {
					requireAdmissionCode(t, err, contract.CodeBadRequest)

					if calls != 0 || admissions != 0 || observed.counts() != [4]int64{} {
						t.Fatalf("wrong kind ran handler, admission or cache: %d %d %v", calls, admissions, observed.counts())
					}
				} else if err != nil || calls != 1 || admissions < 1 {
					t.Fatalf("accepted kind: calls=%d admission=%d err=%v", calls, admissions, err)
				}
			})
		}
	}
}

func TestAdmission_InvalidRegistrationOptions(t *testing.T) {
	for name, opts := range map[string][]RegisterOption{
		"nil option": {nil}, "nil callback": {BeforeDispatch(nil)},
		"empty kind": {RequireKind("")}, "subscription": {RequireKind(contract.KindSubscribe)},
		"invalid overwritten":      {RequireKind("bogus"), RequireKind(contract.KindCommand)},
		"nil callback overwritten": {BeforeDispatch(nil), BeforeDispatch(func(context.Context, contract.Request, contract.Principal) error { return nil })},
	} {
		t.Run(name, func(t *testing.T) {
			d := New(nil)
			if err := RegisterCommand(d, "keysmith", "keys.create", 1, func(context.Context, mintIn, contract.Principal) (mintOut, error) { return mintOut{}, nil }, opts...); err == nil || !strings.Contains(err.Error(), "dispatcher:") {
				t.Fatalf("registration error=%v", err)
			}

			_, _, err := d.Dispatch(context.Background(), mintRequest(), alice())
			requireAdmissionCode(t, err, contract.CodeNotFound)
		})
	}
}

func TestAdmission_CachedPermissionRevocation(t *testing.T) {
	for _, claiming := range []bool{false, true} {
		t.Run(map[bool]string{false: "lookup", true: "claim"}[claiming], func(t *testing.T) {
			store, observed := observedAdmissionStore(claiming)
			allowed := true
			checks := 0

			d, calls := mintCommand(t, store, BeforeDispatch(func(context.Context, contract.Request, contract.Principal) error {
				checks++

				if !allowed {
					return &contract.Error{Code: contract.CodePermissionDenied}
				}

				return nil
			}))
			if !claiming {
				observed.IdempotencyStore.(*stubStore).hits["k1|alice:keys.create"] = bindingRecord(t, mintRequest(), alice())
			}

			for range 2 {
				if _, _, err := d.Dispatch(context.Background(), mintRequest(), alice()); err != nil {
					t.Fatal(err)
				}
			}

			expectedChecks := 2
			if claiming {
				expectedChecks = 4
			}

			expectedCalls := int64(0)
			if claiming {
				expectedCalls = 1
			}

			if *calls != expectedCalls || checks != expectedChecks {
				t.Fatalf("handler=%d admission=%d", *calls, checks)
			}

			before := observed.counts()
			allowed = false
			data, _, err := d.Dispatch(context.Background(), mintRequest(), alice())
			requireAdmissionCode(t, err, contract.CodePermissionDenied)

			if data != nil || *calls != expectedCalls || observed.counts() != before {
				t.Fatalf("denial accessed data, handler or cache: %s %d %v", data, *calls, observed.counts())
			}
		})
	}
}

func TestAdmission_TrueBypass(t *testing.T) {
	for _, claiming := range []bool{false, true} {
		for _, secret := range []bool{false, true} {
			store, observed := observedAdmissionStore(claiming)
			// Even an existing tombstone belongs to the bypassing handler's policy.
			if err := store.Store(context.Background(), "k1", "alice:keys.create", IdempotencyCached{Status: TombstoneStatus}); err != nil {
				t.Fatal(err)
			}

			before := observed.counts()
			checks := 0

			opts := []RegisterOption{BypassIdempotency(), BeforeDispatch(func(context.Context, contract.Request, contract.Principal) error {
				checks++

				return nil
			})}
			if secret {
				opts = append(opts, SecretResponse())
			}

			d, calls := mintCommand(t, store, opts...)
			for range 2 {
				if _, _, err := d.Dispatch(context.Background(), mintRequest(), alice()); err != nil {
					t.Fatal(err)
				}
			}

			if *calls != 2 || checks != 2 || observed.counts() != before {
				t.Fatalf("bypass claiming=%v secret=%v: handler=%d admission=%d cache=%v", claiming, secret, *calls, checks, observed.counts())
			}
		}
	}
}

func TestAdmission_DenialMapsErrorsAndObservability(t *testing.T) {
	public := &contract.Error{Code: contract.CodePermissionDenied, Message: "denied", Details: map[string]any{"permission": "write"}}
	for name, admissionErr := range map[string]error{"public": public, "unknown": errors.New("private backend credential"), "cancelled": context.Canceled} {
		for _, claiming := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "/lookup", true: "/claim"}[claiming], func(t *testing.T) {
				store, observed := observedAdmissionStore(claiming)
				exporter := tracetest.NewInMemoryExporter()

				provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
				defer func() { _ = provider.Shutdown(context.Background()) }()

				metrics := &recordingMetrics{}
				d := NewWithOptions(metrics, WithIdempotencyStore(store), WithTracer(provider.Tracer("admission")))
				calls := 0

				if err := d.Register("keysmith", "keys.create", 1, func(context.Context, json.RawMessage, map[string]any, contract.Principal) (*Result, error) {
					calls++

					return &Result{}, nil
				}, BeforeDispatch(func(context.Context, contract.Request, contract.Principal) error { return admissionErr })); err != nil {
					t.Fatal(err)
				}

				data, _, err := d.Dispatch(context.Background(), mintRequest(), alice())
				want := contract.CodeInternal

				switch name {
				case "public":
					want = contract.CodePermissionDenied
				case "cancelled":
					want = contract.CodeUnavailable
				}

				requireAdmissionCode(t, err, want)

				if name == "public" && !errors.Is(err, public) {
					t.Fatal("canonical error was not preserved")
				}

				if name == "unknown" && strings.Contains(err.Error(), "credential") {
					t.Fatal("private error leaked")
				}

				var canonical *contract.Error
				if name == "cancelled" && errors.As(err, &canonical) && !canonical.Retryable {
					t.Fatal("cancellation must be retryable")
				}

				if data != nil || calls != 0 || observed.counts() != [4]int64{} {
					t.Fatalf("denial touched handler or cache: %d %v", calls, observed.counts())
				}

				if len(metrics.records) != 1 || metrics.records[0].ErrCode != want {
					t.Fatalf("metrics=%v", metrics.records)
				}

				spans := exporter.GetSpans()
				if len(spans) != 1 || spans[0].Status.Code != codes.Error {
					t.Fatalf("spans=%v", spans)
				}

				found := false

				for _, attr := range spans[0].Attributes {
					if string(attr.Key) == "forge.contract.error_code" && attr.Value.AsString() == string(want) {
						found = true
					}
				}

				if !found {
					t.Fatal("span lacks canonical error code")
				}
			})
		}
	}
}

func TestAdmission_OriginalInputsOrderAndUnlockedCallbacks(t *testing.T) {
	d := New(nil)
	req, principal := mintRequest(), alice()
	req.Params = map[string]any{"target": "run-1"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sequence := []int{}

	admission := func(n int) RegisterOption {
		return BeforeDispatch(func(gotCtx context.Context, gotReq contract.Request, gotPrincipal contract.Principal) error {
			if gotCtx.Err() != context.Canceled || !reflect.DeepEqual(gotReq, req) || !reflect.DeepEqual(gotPrincipal, principal) {
				t.Error("admission inputs changed")
			}

			d.SetRemoteDispatcher(nil) // Acquires the write lock; callback must not hold it.

			sequence = append(sequence, n)

			return nil
		})
	}
	if err := d.Register("keysmith", "keys.create", 1, func(context.Context, json.RawMessage, map[string]any, contract.Principal) (*Result, error) {
		sequence = append(sequence, 3)

		return &Result{}, nil
	}, admission(1), admission(2)); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)

	go func() { _, _, err := d.Dispatch(ctx, req, principal); done <- err }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("admission callback blocked under dispatcher lock")
	}

	if !reflect.DeepEqual(sequence, []int{1, 2, 3}) {
		t.Fatalf("sequence=%v", sequence)
	}
}

func TestAdmission_RevokedWhileClaimWaits(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(map[bool]string{false: "acquires", true: "replays"}[cached], func(t *testing.T) {
			store := newClaimStore()

			held, err := store.Claim(context.Background(), "k1", "alice:keys.create")
			if err != nil {
				t.Fatal(err)
			}

			var allowed atomic.Bool
			allowed.Store(true)

			var checks atomic.Int64

			d, calls := mintCommand(t, store, BeforeDispatch(func(context.Context, contract.Request, contract.Principal) error {
				checks.Add(1)

				if !allowed.Load() {
					return &contract.Error{Code: contract.CodePermissionDenied}
				}

				return nil
			}))
			done := make(chan error, 1)

			go func() {
				data, _, err := d.Dispatch(context.Background(), mintRequest(), alice())
				if data != nil {
					done <- errors.New("denial returned cached data")

					return
				}

				done <- err
			}()

			select {
			case <-store.waiting:
			case <-time.After(time.Second):
				t.Fatal("claim did not wait")
			}

			allowed.Store(false)

			var entry *IdempotencyCached
			if cached {
				entry = &IdempotencyCached{Status: 200, WireBody: json.RawMessage(`{"ok":true,"data":{"private":true}}`)}
			}

			if err := held.End(context.Background(), entry); err != nil {
				t.Fatal(err)
			}

			select {
			case err := <-done:
				requireAdmissionCode(t, err, contract.CodePermissionDenied)
			case <-time.After(time.Second):
				t.Fatal("dispatch stayed blocked")
			}

			_, _, releases, heldCount := store.counts()

			wantReleases := 2
			if cached {
				wantReleases = 0
			}

			if *calls != 0 || checks.Load() != 2 || releases != wantReleases || heldCount != 0 {
				t.Fatalf("handler=%d checks=%d releases=%d held=%d", *calls, checks.Load(), releases, heldCount)
			}
		})
	}
}

func TestAdmission_UnknownAndRemoteIgnoreLocalCache(t *testing.T) {
	for _, claiming := range []bool{false, true} {
		for _, remote := range []bool{false, true} {
			store, observed := observedAdmissionStore(claiming)
			if err := store.Store(context.Background(), "k1", "alice:keys.create", IdempotencyCached{Status: 200, WireBody: json.RawMessage(`{"ok":true,"data":{"private":true}}`)}); err != nil {
				t.Fatal(err)
			}

			before := observed.counts()
			d := NewWithOptions(nil, WithIdempotencyStore(store))

			forward := &countingRemote{}
			if remote {
				d.SetRemoteDispatcher(forward)
			}

			data, _, err := d.Dispatch(context.Background(), mintRequest(), alice())
			if remote {
				if err != nil || forward.calls != 1 || string(data) != `{"raw":"remote"}` {
					t.Fatalf("forwarded data=%s err=%v calls=%d", data, err, forward.calls)
				}
			} else {
				requireAdmissionCode(t, err, contract.CodeNotFound)

				if data != nil {
					t.Fatal("unknown handler returned cache")
				}
			}

			if observed.counts() != before {
				t.Fatalf("remote/unknown accessed local cache: %v", observed.counts())
			}
		}
	}
}
