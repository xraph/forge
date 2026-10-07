package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

type mintIn struct {
	Name string `json:"name"`
}

type mintOut struct {
	ID  string `json:"id"`
	Raw string `json:"raw"`
}

const rawKey = "sk_live_do_not_keep"

func mintCommand(t *testing.T, store IdempotencyStore, opts ...RegisterOption) (*Dispatcher, *int64) {
	t.Helper()

	d := NewWithOptions(NoopMetricsEmitter{}, WithIdempotencyStore(store))
	calls := new(int64)

	err := RegisterCommand(d, "keysmith", "keys.create", 1, func(_ context.Context, in mintIn, _ contract.Principal) (mintOut, error) {
		atomic.AddInt64(calls, 1)

		return mintOut{ID: "key_1", Raw: rawKey}, nil
	}, opts...)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	return d, calls
}

func mintRequest() contract.Request {
	return contract.Request{
		Envelope: "v1", Kind: contract.KindCommand,
		Contributor: "keysmith", Intent: "keys.create", IntentVersion: 1,
		IdempotencyKey: "k1",
		Payload:        json.RawMessage(`{"name":"ci"}`),
	}
}

func alice() contract.Principal {
	return contract.PrincipalFor(&dashauth.UserInfo{Subject: "alice"})
}

func requireSecretConflict(t *testing.T, err error) {
	t.Helper()

	var ce *contract.Error
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want *contract.Error", err)
	}

	if ce.Code != contract.CodeConflict {
		t.Fatalf("code = %s, want %s", ce.Code, contract.CodeConflict)
	}

	if !strings.Contains(ce.Message, "already ran") || !strings.Contains(ce.Message, "secret") {
		t.Errorf("message = %q, want it to say the command already ran and its secret is not kept", ce.Message)
	}
}

func TestSecretCommand_FirstDispatchReturnsTheSecret(t *testing.T) {
	d, calls := mintCommand(t, newStubStore(), SecretResponse())

	data, _, err := d.Dispatch(context.Background(), mintRequest(), alice())
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	var got mintOut
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Raw != rawKey {
		t.Errorf("raw = %q, want the minted key on the first answer", got.Raw)
	}

	if atomic.LoadInt64(calls) != 1 {
		t.Errorf("handler ran %d times, want 1", *calls)
	}
}

func TestSecretCommand_StoresATombstoneWithNoBody(t *testing.T) {
	store := newStubStore()
	d, _ := mintCommand(t, store, SecretResponse())

	if _, _, err := d.Dispatch(context.Background(), mintRequest(), alice()); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if atomic.LoadInt64(&store.puts) != 1 {
		t.Fatalf("store writes = %d, want 1 tombstone", store.puts)
	}

	entry, ok := store.hits["k1|alice:keys.create"]
	if !ok {
		t.Fatalf("no entry under the key; store = %v", store.hits)
	}

	if len(entry.WireBody) != 0 {
		t.Errorf("tombstone body = %s, want none", entry.WireBody)
	}

	if entry.Status != TombstoneStatus {
		t.Errorf("tombstone status = %d, want %d", entry.Status, TombstoneStatus)
	}

	if entry.TTL != 24*time.Hour {
		t.Errorf("tombstone TTL = %s, want 24h like any other entry", entry.TTL)
	}

	for k, c := range store.hits {
		if strings.Contains(string(c.WireBody), rawKey) {
			t.Errorf("entry %q holds the raw key: %s", k, c.WireBody)
		}
	}
}

func TestSecretCommand_ReplayAnswersConflictWithoutRunning(t *testing.T) {
	d, calls := mintCommand(t, newStubStore(), SecretResponse())

	if _, _, err := d.Dispatch(context.Background(), mintRequest(), alice()); err != nil {
		t.Fatalf("first dispatch: %v", err)
	}

	data, _, err := d.Dispatch(context.Background(), mintRequest(), alice())
	requireSecretConflict(t, err)

	if data != nil {
		t.Errorf("replay data = %s, want none", data)
	}

	if atomic.LoadInt64(calls) != 1 {
		t.Errorf("handler ran %d times, want 1 (a replay must not mint a second key)", *calls)
	}
}

func TestSecretCommand_AnotherUserIsNotBlocked(t *testing.T) {
	d, calls := mintCommand(t, newStubStore(), SecretResponse())

	if _, _, err := d.Dispatch(context.Background(), mintRequest(), alice()); err != nil {
		t.Fatalf("alice: %v", err)
	}

	bob := contract.PrincipalFor(&dashauth.UserInfo{Subject: "bob"})
	if _, _, err := d.Dispatch(context.Background(), mintRequest(), bob); err != nil {
		t.Fatalf("bob: %v", err)
	}

	if atomic.LoadInt64(calls) != 2 {
		t.Errorf("handler ran %d times, want 2 (the key is per user)", *calls)
	}
}

// Any entry found for a secret command answers CONFLICT, whatever it holds.
// The old fallthrough re-ran the handler on an undecodable or non-OK entry,
// which for a secret command would mint a second key.
func TestSecretCommand_NeverFallsThroughOnAnOddEntry(t *testing.T) {
	cases := map[string]IdempotencyCached{
		"empty body":       {Status: 200},
		"undecodable body": {Status: 200, WireBody: json.RawMessage(`not json`)},
		"non-OK envelope":  {Status: 200, WireBody: json.RawMessage(`{"ok":false}`)},
		"tombstone":        {Status: TombstoneStatus},
		"OK envelope":      {Status: 200, WireBody: json.RawMessage(`{"ok":true,"data":{"raw":"old"}}`)},
	}

	for name, entry := range cases {
		t.Run(name, func(t *testing.T) {
			store := newStubStore()
			store.hits["k1|alice:keys.create"] = entry
			d, calls := mintCommand(t, store, SecretResponse())

			data, _, err := d.Dispatch(context.Background(), mintRequest(), alice())
			requireSecretConflict(t, err)

			if data != nil {
				t.Errorf("data = %s, want none", data)
			}

			if atomic.LoadInt64(calls) != 0 {
				t.Errorf("handler ran %d times, want 0", *calls)
			}
		})
	}
}

// A tombstone answers CONFLICT even when the command is not registered as
// secret here, say after a restart that dropped the option, or on a host that
// forwards the command to a remote contributor.
func TestTombstone_AnswersConflictForAnyRegistration(t *testing.T) {
	store := newStubStore()
	store.hits["k1|alice:keys.create"] = IdempotencyCached{Status: TombstoneStatus, StoredAt: time.Now(), TTL: time.Hour}
	d, calls := mintCommand(t, store)

	_, _, err := d.Dispatch(context.Background(), mintRequest(), alice())
	requireSecretConflict(t, err)

	if atomic.LoadInt64(calls) != 0 {
		t.Errorf("handler ran %d times, want 0", *calls)
	}
}

func TestTombstone_AnswersConflictOnTheRemotePath(t *testing.T) {
	store := newStubStore()
	store.hits["k1|alice:keys.create"] = IdempotencyCached{Status: TombstoneStatus}
	d := NewWithOptions(NoopMetricsEmitter{}, WithIdempotencyStore(store))

	remote := &countingRemote{}
	d.SetRemoteDispatcher(remote)

	_, _, err := d.Dispatch(context.Background(), mintRequest(), alice())
	requireSecretConflict(t, err)

	if atomic.LoadInt64(&remote.calls) != 0 {
		t.Errorf("remote ran %d times, want 0", remote.calls)
	}
}

type countingRemote struct{ calls int64 }

func (r *countingRemote) Dispatch(context.Context, contract.Request, contract.Principal) (json.RawMessage, contract.ResponseMeta, error) {
	atomic.AddInt64(&r.calls, 1)

	return json.RawMessage(`{"raw":"remote"}`), contract.ResponseMeta{}, nil
}

func TestSecretCommand_FailureStoresNothing(t *testing.T) {
	store := newStubStore()
	d := NewWithOptions(NoopMetricsEmitter{}, WithIdempotencyStore(store))

	err := RegisterCommand(d, "keysmith", "keys.create", 1, func(context.Context, mintIn, contract.Principal) (mintOut, error) {
		return mintOut{}, &contract.Error{Code: contract.CodeUnavailable, Message: "down", Retryable: true}
	}, SecretResponse())
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if _, _, err := d.Dispatch(context.Background(), mintRequest(), alice()); err == nil {
		t.Fatal("dispatch: want the handler's error")
	}

	if atomic.LoadInt64(&store.puts) != 0 {
		t.Errorf("store writes = %d, want 0 so a retry can run", store.puts)
	}
}

func TestNonSecretCommand_ReplayStillReturnsTheCachedData(t *testing.T) {
	store := newStubStore()
	d, calls := mintCommand(t, store)

	first, _, err := d.Dispatch(context.Background(), mintRequest(), alice())
	if err != nil {
		t.Fatalf("first dispatch: %v", err)
	}

	entry := store.hits["k1|alice:keys.create"]
	if entry.Status != 200 || entry.TTL != 24*time.Hour {
		t.Errorf("entry status %d TTL %s, want 200 and 24h", entry.Status, entry.TTL)
	}

	if !strings.Contains(string(entry.WireBody), `"id":"key_1"`) {
		t.Errorf("entry body = %s, want the full envelope", entry.WireBody)
	}

	again, _, err := d.Dispatch(context.Background(), mintRequest(), alice())
	if err != nil {
		t.Fatalf("replay: %v", err)
	}

	if string(again) != string(first) {
		t.Errorf("replay = %s, want %s", again, first)
	}

	if atomic.LoadInt64(calls) != 1 {
		t.Errorf("handler ran %d times, want 1", *calls)
	}
}

func TestRegister_SecretResponseOnTheRawPath(t *testing.T) {
	store := newStubStore()
	d := NewWithOptions(NoopMetricsEmitter{}, WithIdempotencyStore(store))
	calls := int64(0)

	err := d.Register("keysmith", "keys.create", 1, func(context.Context, json.RawMessage, map[string]any, contract.Principal) (*Result, error) {
		atomic.AddInt64(&calls, 1)

		return &Result{Data: json.RawMessage(`{"raw":"` + rawKey + `"}`)}, nil
	}, SecretResponse())
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if _, _, err := d.Dispatch(context.Background(), mintRequest(), alice()); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if body := store.hits["k1|alice:keys.create"].WireBody; len(body) != 0 {
		t.Errorf("tombstone body = %s, want none", body)
	}

	_, _, err = d.Dispatch(context.Background(), mintRequest(), alice())
	requireSecretConflict(t, err)

	if atomic.LoadInt64(&calls) != 1 {
		t.Errorf("handler ran %d times, want 1", calls)
	}
}
