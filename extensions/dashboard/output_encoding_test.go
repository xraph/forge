package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/idempotency"
	"github.com/xraph/forge/middleware"
)

// Observe ownership while preserving the real default adapter and claim store.
type outputEncodingStore struct {
	dispatcher.IdempotencyStore

	claims, completions, releases, stores int
	completionContextError                error
}

func (s *outputEncodingStore) Store(ctx context.Context, key, identity string, record dispatcher.IdempotencyCached) error {
	s.stores++

	return s.IdempotencyStore.Store(ctx, key, identity, record)
}

func (s *outputEncodingStore) Claim(ctx context.Context, key, identity string) (dispatcher.IdempotencyClaim, error) {
	s.claims++

	claim, err := s.IdempotencyStore.(dispatcher.IdempotencyClaimer).Claim(ctx, key, identity)
	if claim.End != nil {
		end := claim.End
		claim.End = func(ctx context.Context, record *dispatcher.IdempotencyCached) error {
			if record == nil {
				s.releases++
			} else {
				s.completions++
				s.completionContextError = ctx.Err()
			}

			return end(ctx, record)
		}
	}

	return claim, err
}

type failingTypedOutput struct{ panicValue any }

func (out failingTypedOutput) MarshalJSON() ([]byte, error) {
	if out.panicValue != nil {
		panic(out.panicValue)
	}

	return nil, &contract.Error{Code: contract.CodePermissionDenied, Message: "private output credential", Details: map[string]any{"private": true}}
}

func requireSanitizedOutputFailure(t *testing.T, data json.RawMessage, meta contract.ResponseMeta, err error) {
	t.Helper()

	var ce *contract.Error
	if !errors.As(err, &ce) || ce.Code != contract.CodeInternal || strings.Contains(err.Error(), "private") || len(ce.Details) != 0 || ce.Retryable || data != nil || !reflect.DeepEqual(meta, contract.ResponseMeta{}) {
		t.Fatalf("unsanitized output failure: data=%s meta=%+v err=%v", data, meta, err)
	}
}

func TestTypedOutputFailureConsumesClaim(t *testing.T) {
	for name, output := range map[string]any{"nan": math.NaN(), "unsupported": make(chan int), "marshaler-error": failingTypedOutput{}, "marshaler-panic": failingTypedOutput{panicValue: "private panic credential"}} {
		for _, secret := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "/ordinary", true: "/secret"}[secret], func(t *testing.T) {
				store := &outputEncodingStore{IdempotencyStore: AdaptIdempotencyStore(idempotency.NewInMemoryStore())}
				d := dispatcher.NewWithOptions(nil, dispatcher.WithIdempotencyStore(store))
				req, p := bindingRequest(), bindingPrincipal()
				calls := 0

				var opts []dispatcher.RegisterOption
				if secret {
					opts = append(opts, dispatcher.SecretResponse())
				}

				if err := dispatcher.RegisterCommand(d, req.Contributor, req.Intent, req.IntentVersion, func(context.Context, struct{}, contract.Principal) (any, error) {
					calls++

					return output, nil
				}, opts...); err != nil {
					t.Fatal(err)
				}

				data, meta, err := d.Dispatch(context.Background(), req, p)
				requireSanitizedOutputFailure(t, data, meta, err)

				record, hit := store.Lookup(context.Background(), req.IdempotencyKey, "operator:run.cancel")
				if !hit || record.Status != dispatcher.TombstoneStatus || len(record.WireBody) != 0 || store.completions != 1 || store.releases != 0 || store.stores != 0 {
					t.Fatalf("post-success ownership: hit=%t record=%+v completions=%d releases=%d stores=%d", hit, record, store.completions, store.releases, store.stores)
				}

				data, meta, err = d.Dispatch(context.Background(), req, p)
				requireCacheReason(t, err, dispatcher.ReasonAlreadyRan)

				if calls != 1 || data != nil || !reflect.DeepEqual(meta, contract.ResponseMeta{}) || store.completions != 1 || store.releases != 0 || store.stores != 0 {
					t.Fatal("retry reexecuted or changed consumed record")
				}
			})
		}
	}
}

func TestTypedOutputFailurePhaseBoundaries(t *testing.T) {
	for _, mode := range []string{"domain-error", "domain-panic", "bypass", "query", "cancelled-success"} {
		t.Run(mode, func(t *testing.T) {
			store := &outputEncodingStore{IdempotencyStore: AdaptIdempotencyStore(idempotency.NewInMemoryStore())}
			d := dispatcher.NewWithOptions(nil, dispatcher.WithIdempotencyStore(store))
			req, p := bindingRequest(), bindingPrincipal()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			calls := 0
			domainErr := &contract.Error{Code: contract.CodeUnavailable, Message: "domain failed"}
			handler := func(context.Context, struct{}, contract.Principal) (any, error) {
				calls++

				switch mode {
				case "domain-error":
					return nil, domainErr
				case "domain-panic":
					panic("domain panic")
				case "cancelled-success":
					cancel()
				}

				return failingTypedOutput{}, nil
			}

			if mode == "query" {
				req.Kind = contract.KindQuery
				if err := dispatcher.RegisterQuery(d, req.Contributor, req.Intent, req.IntentVersion, handler); err != nil {
					t.Fatal(err)
				}
			} else {
				var opts []dispatcher.RegisterOption
				if mode == "bypass" {
					opts = append(opts, dispatcher.BypassIdempotency())
				}

				if err := dispatcher.RegisterCommand(d, req.Contributor, req.Intent, req.IntentVersion, handler, opts...); err != nil {
					t.Fatal(err)
				}
			}

			for range 2 {
				if mode == "domain-panic" {
					func() {
						defer func() {
							if recovered := recover(); recovered != "domain panic" {
								t.Fatalf("domain panic changed: %v", recovered)
							}
						}()

						_, _, _ = d.Dispatch(ctx, req, p)

						t.Error("domain panic recovered outside output phase")
					}()
				} else {
					data, meta, err := d.Dispatch(ctx, req, p)

					switch {
					case mode == "domain-error":
						if !errors.Is(err, domainErr) {
							t.Fatalf("domain error changed: %v", err)
						}
					case mode == "cancelled-success" && calls == 1 && store.claims == 2:
						requireCacheReason(t, err, dispatcher.ReasonAlreadyRan)
					default:
						requireSanitizedOutputFailure(t, data, meta, err)
					}
				}

				if mode == "cancelled-success" {
					ctx = context.Background()
				}
			}

			switch mode {
			case "domain-error", "domain-panic":
				if calls != 2 || store.releases != 2 || store.completions != 0 || store.stores != 0 {
					t.Fatalf("domain failure ownership: %+v calls=%d", store, calls)
				}
			case "bypass", "query":
				if calls != 2 || store.claims != 0 || store.completions != 0 || store.releases != 0 || store.stores != 0 {
					t.Fatalf("noncached path touched cache: %+v calls=%d", store, calls)
				}
			case "cancelled-success":
				if calls != 1 || store.completions != 1 || store.releases != 0 || store.stores != 0 || store.completionContextError != nil {
					t.Fatalf("cancelled completion: %+v calls=%d", store, calls)
				}
			}
		})
	}
}

func TestTypedOutputFailureLostLeaseHasNoFallback(t *testing.T) {
	now := time.Now()
	backend := middleware.NewMemoryIdempotencyStore(middleware.MemoryIdempotencyClock(func() time.Time { return now }))
	inner := idempotency.NewSharedStore(backend)
	store := &outputEncodingStore{IdempotencyStore: AdaptIdempotencyStore(inner)}
	d := dispatcher.NewWithOptions(nil, dispatcher.WithIdempotencyStore(store))
	req, p := bindingRequest(), bindingPrincipal()
	calls := 0

	if err := dispatcher.RegisterCommand(d, req.Contributor, req.Intent, req.IntentVersion, func(context.Context, struct{}, contract.Principal) (any, error) {
		calls++
		now = now.Add(2 * time.Minute)

		successor, err := inner.Claim(context.Background(), req.IdempotencyKey, "operator:run.cancel")
		if err != nil {
			t.Fatal(err)
		}

		if err := successor.End(context.Background(), nil); err != nil {
			t.Fatal(err)
		}

		return math.NaN(), nil
	}); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		data, meta, err := d.Dispatch(context.Background(), req, p)
		requireSanitizedOutputFailure(t, data, meta, err)
	}

	if _, hit := inner.Lookup(context.Background(), req.IdempotencyKey, "operator:run.cancel"); hit || calls != 2 || store.completions != 2 || store.releases != 0 || store.stores != 0 {
		t.Fatalf("lease-loss limitation or ownership changed: hit=%t calls=%d store=%+v", hit, calls, store)
	}
}

type matchingDomainError struct{ isCalls *int }

func (matchingDomainError) Error() string { return "domain failure" }

func (err matchingDomainError) Is(error) bool {
	*err.isCalls++

	return true
}

func TestTypedOutputFailureDomainIsDoesNotConsume(t *testing.T) {
	for _, secret := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "secret"}[secret], func(t *testing.T) {
			store := &outputEncodingStore{IdempotencyStore: AdaptIdempotencyStore(idempotency.NewInMemoryStore())}
			d := dispatcher.NewWithOptions(nil, dispatcher.WithIdempotencyStore(store))
			req, p := bindingRequest(), bindingPrincipal()
			calls, isCalls := 0, 0

			var opts []dispatcher.RegisterOption
			if secret {
				opts = append(opts, dispatcher.SecretResponse())
			}

			if err := dispatcher.RegisterCommand(d, req.Contributor, req.Intent, req.IntentVersion, func(context.Context, struct{}, contract.Principal) (any, error) {
				calls++

				return nil, matchingDomainError{isCalls: &isCalls}
			}, opts...); err != nil {
				t.Fatal(err)
			}

			var results []error

			for range 2 {
				data, meta, err := d.Dispatch(context.Background(), req, p)
				results = append(results, err)

				if data != nil || !reflect.DeepEqual(meta, contract.ResponseMeta{}) {
					t.Fatal("failed domain call returned data or metadata")
				}
			}

			record, hit := store.Lookup(context.Background(), req.IdempotencyKey, "operator:run.cancel")
			if calls != 2 || store.releases != 2 || store.completions != 0 || store.stores != 0 || hit {
				t.Fatalf("domain error consumed: calls=%d releases=%d completions=%d stores=%d hit=%t record=%+v", calls, store.releases, store.completions, store.stores, hit, record)
			}

			// The existing public mapper still classifies this broad Is match as
			// cancellation. Only the private post-success phase check must bypass Is.
			for _, err := range results {
				var ce *contract.Error
				if !errors.As(err, &ce) || ce.Code != contract.CodeUnavailable || ce.Message != "request cancelled" || !ce.Retryable {
					t.Fatalf("public error mapping changed: %v", err)
				}
			}

			if isCalls != 2 {
				t.Fatalf("Is calls=%d; want one public classification per failed attempt", isCalls)
			}
		})
	}
}
