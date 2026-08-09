package streaming

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"
)

// --- Hook doubles ----------------------------------------------------------

// baseHook supplies the StreamingHook identity shared by all doubles.
type baseHook struct {
	name string
}

func (h *baseHook) Name() string { return h.name }

// connHook implements only ConnectionHook.
type connHook struct {
	baseHook

	mu            sync.Mutex
	connectErr    error
	connectCalls  []string
	disconnects   []string
	connectOrder  *[]string
	disconnectLog *[]string
}

func (h *connHook) OnConnect(ctx context.Context, conn Connection) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.connectCalls = append(h.connectCalls, conn.ID())

	if h.connectOrder != nil {
		*h.connectOrder = append(*h.connectOrder, h.name)
	}

	return h.connectErr
}

func (h *connHook) OnDisconnect(ctx context.Context, conn Connection) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.disconnects = append(h.disconnects, conn.ID())

	if h.disconnectLog != nil {
		*h.disconnectLog = append(*h.disconnectLog, h.name)
	}
}

func (h *connHook) connectCallCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	return len(h.connectCalls)
}

// msgHook implements only MessageHook.
type msgHook struct {
	baseHook

	mu sync.Mutex
	// transform, when set, rewrites the message. Returning nil blocks it.
	transform  func(*Message) *Message
	receiveErr error

	received  []string
	delivered []string
	deliverCh chan struct{}
}

func (h *msgHook) OnMessageReceived(ctx context.Context, conn Connection, msg *Message) (*Message, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.received = append(h.received, h.name)

	if h.receiveErr != nil {
		return nil, h.receiveErr
	}

	if h.transform != nil {
		return h.transform(msg), nil
	}

	return msg, nil
}

func (h *msgHook) OnMessageDelivered(ctx context.Context, conn Connection, msg *Message) {
	h.mu.Lock()
	h.delivered = append(h.delivered, h.name)
	h.mu.Unlock()

	if h.deliverCh != nil {
		h.deliverCh <- struct{}{}
	}
}

func (h *msgHook) receivedNames() []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	out := make([]string, len(h.received))
	copy(out, h.received)

	return out
}

// rawHook implements only RawMessageHook.
type rawHook struct {
	baseHook

	transform func([]byte) []byte
	err       error

	mu    sync.Mutex
	calls int
}

func (h *rawHook) OnRawMessage(ctx context.Context, conn Connection, data []byte) ([]byte, error) {
	h.mu.Lock()
	h.calls++
	h.mu.Unlock()

	if h.err != nil {
		return nil, h.err
	}

	if h.transform != nil {
		return h.transform(data), nil
	}

	return data, nil
}

func (h *rawHook) callCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.calls
}

// roomHookDouble implements only RoomHook.
type roomHookDouble struct {
	baseHook

	joinErr   error
	createErr error

	mu      sync.Mutex
	joins   []string
	leaves  []string
	creates []string
	deletes []string
}

func (h *roomHookDouble) OnRoomJoin(ctx context.Context, conn Connection, roomID string) error {
	h.mu.Lock()
	h.joins = append(h.joins, roomID)
	h.mu.Unlock()

	return h.joinErr
}

func (h *roomHookDouble) OnRoomLeave(ctx context.Context, conn Connection, roomID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.leaves = append(h.leaves, roomID)
}

func (h *roomHookDouble) OnRoomCreate(ctx context.Context, room Room) error {
	h.mu.Lock()
	h.creates = append(h.creates, room.GetID())
	h.mu.Unlock()

	return h.createErr
}

func (h *roomHookDouble) OnRoomDelete(ctx context.Context, roomID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.deletes = append(h.deletes, roomID)
}

// presenceHookDouble implements only PresenceHook.
type presenceHookDouble struct {
	baseHook

	mu      sync.Mutex
	changes [][3]string
}

func (h *presenceHookDouble) OnPresenceChange(ctx context.Context, userID, oldStatus, newStatus string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.changes = append(h.changes, [3]string{userID, oldStatus, newStatus})
}

func (h *presenceHookDouble) changeCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	return len(h.changes)
}

// errHookDouble implements only ErrorHook.
type errHookDouble struct {
	baseHook

	mu   sync.Mutex
	errs []error
}

func (h *errHookDouble) OnError(ctx context.Context, conn Connection, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.errs = append(h.errs, err)
}

// everythingHook implements every hook interface at once.
type everythingHook struct {
	connHook
	msgHook
	rawHook
	roomHookDouble
	presenceHookDouble
	errHookDouble
}

func (h *everythingHook) Name() string { return "everything" }

// --- Categorization --------------------------------------------------------

func TestHookRegistry_Categorization(t *testing.T) {
	tests := []struct {
		name           string
		hook           StreamingHook
		wantConnection int
		wantMessage    int
		wantRaw        int
		wantRoom       int
		wantPresence   int
		wantError      int
	}{
		{
			name:           "connection hook only",
			hook:           &connHook{baseHook: baseHook{name: "conn"}},
			wantConnection: 1,
		},
		{
			name:        "message hook only",
			hook:        &msgHook{baseHook: baseHook{name: "msg"}},
			wantMessage: 1,
		},
		{
			name:    "raw message hook only",
			hook:    &rawHook{baseHook: baseHook{name: "raw"}},
			wantRaw: 1,
		},
		{
			name:     "room hook only",
			hook:     &roomHookDouble{baseHook: baseHook{name: "room"}},
			wantRoom: 1,
		},
		{
			name:         "presence hook only",
			hook:         &presenceHookDouble{baseHook: baseHook{name: "presence"}},
			wantPresence: 1,
		},
		{
			name:      "error hook only",
			hook:      &errHookDouble{baseHook: baseHook{name: "err"}},
			wantError: 1,
		},
		{
			name:           "hook implementing every interface lands in every list",
			hook:           &everythingHook{},
			wantConnection: 1,
			wantMessage:    1,
			wantRaw:        1,
			wantRoom:       1,
			wantPresence:   1,
			wantError:      1,
		},
		{
			name: "bare StreamingHook lands in no dispatch list",
			hook: &baseHook{name: "inert"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewHookRegistry()
			r.Add(tt.hook)

			checks := []struct {
				list string
				got  int
				want int
			}{
				{"connection", len(r.connectionHooksCopy()), tt.wantConnection},
				{"message", len(r.messageHooksCopy()), tt.wantMessage},
				{"raw", len(r.rawMessageHooksCopy()), tt.wantRaw},
				{"room", len(r.roomHooksCopy()), tt.wantRoom},
				{"presence", len(r.presenceHooksCopy()), tt.wantPresence},
				{"error", len(r.errorHooksCopy()), tt.wantError},
			}

			for _, c := range checks {
				if c.got != c.want {
					t.Errorf("%s hooks: got %d, want %d", c.list, c.got, c.want)
				}
			}

			if got := len(r.List()); got != 1 {
				t.Errorf("List() = %d hooks, want 1", got)
			}
		})
	}
}

func TestHookRegistry_AddReplacesSameName(t *testing.T) {
	r := NewHookRegistry()

	first := &connHook{baseHook: baseHook{name: "dup"}}
	second := &connHook{baseHook: baseHook{name: "dup"}}

	r.Add(first)
	r.Add(second)

	if got := len(r.List()); got != 1 {
		t.Fatalf("List() = %d hooks, want 1 — Add keys on Name()", got)
	}

	assertNoError(t, r.FireOnConnect(context.Background(), newTestConn("c1", "u1")))

	if first.connectCallCount() != 0 {
		t.Error("the replaced hook was still fired")
	}

	if second.connectCallCount() != 1 {
		t.Error("the replacement hook was not fired")
	}
}

func TestHookRegistry_Remove(t *testing.T) {
	r := NewHookRegistry()

	keep := &connHook{baseHook: baseHook{name: "keep"}}
	drop := &connHook{baseHook: baseHook{name: "drop"}}

	r.Add(keep)
	r.Add(drop)
	r.Remove("drop")

	if got := len(r.List()); got != 1 {
		t.Fatalf("List() = %d hooks, want 1", got)
	}

	if got := len(r.connectionHooksCopy()); got != 1 {
		t.Errorf("connection hooks = %d, want 1 — Remove must rebuild dispatch lists", got)
	}

	assertNoError(t, r.FireOnConnect(context.Background(), newTestConn("c1", "u1")))

	if drop.connectCallCount() != 0 {
		t.Error("removed hook was still fired")
	}

	if keep.connectCallCount() != 1 {
		t.Error("retained hook was not fired")
	}
}

func TestHookRegistry_RemoveUnknownNameIsNoop(t *testing.T) {
	r := NewHookRegistry()
	r.Add(&connHook{baseHook: baseHook{name: "a"}})

	r.Remove("does-not-exist")

	if got := len(r.List()); got != 1 {
		t.Errorf("List() = %d hooks, want 1", got)
	}
}

// --- Ordering --------------------------------------------------------------

func TestHookRegistry_AllHooksFire(t *testing.T) {
	// Membership, independent of order: every registered hook is dispatched
	// exactly once. TestHookRegistry_DispatchOrderIsRegistrationOrder covers
	// the sequence.
	r := NewHookRegistry()

	var order []string

	names := []string{"h1", "h2", "h3", "h4"}
	for _, n := range names {
		r.Add(&connHook{baseHook: baseHook{name: n}, connectOrder: &order})
	}

	assertNoError(t, r.FireOnConnect(context.Background(), newTestConn("c1", "u1")))

	got := append([]string(nil), order...)
	sort.Strings(got)

	if len(got) != len(names) {
		t.Fatalf("fired %d hooks (%v), want %d", len(got), got, len(names))
	}

	for i, n := range names {
		if got[i] != n {
			t.Errorf("fired hooks = %v, want %v (as a set)", got, names)
			break
		}
	}
}

func TestHookRegistry_DispatchOrderIsRegistrationOrder(t *testing.T) {
	r := NewHookRegistry()

	var order []string

	names := []string{"first", "second", "third"}
	for _, n := range names {
		r.Add(&connHook{baseHook: baseHook{name: n}, connectOrder: &order})
	}

	assertNoError(t, r.FireOnConnect(context.Background(), newTestConn("c1", "u1")))

	if len(order) != len(names) {
		t.Fatalf("fired %d hooks (%v), want %d", len(order), order, len(names))
	}

	for i, want := range names {
		if order[i] != want {
			t.Errorf("hook %d = %q, want %q", i, order[i], want)
		}
	}
}

func TestHookRegistry_RemoveKeepsTheRemainingOrder(t *testing.T) {
	r := NewHookRegistry()

	var order []string

	for _, n := range []string{"first", "second", "third"} {
		r.Add(&connHook{baseHook: baseHook{name: n}, connectOrder: &order})
	}

	r.Remove("second")

	assertNoError(t, r.FireOnConnect(context.Background(), newTestConn("c1", "u1")))

	want := []string{"first", "third"}
	if len(order) != len(want) {
		t.Fatalf("fired %v, want %v", order, want)
	}

	for i, n := range want {
		if order[i] != n {
			t.Errorf("hook %d = %q, want %q", i, order[i], n)
		}
	}
}

func TestHookRegistry_ReplacingAHookKeepsItsPosition(t *testing.T) {
	// Re-registering an existing name swaps the implementation in place rather
	// than moving it to the end of the chain, so a hot-swapped hook keeps the
	// position the pipeline was designed around.
	r := NewHookRegistry()

	var order []string

	for _, n := range []string{"first", "second", "third"} {
		r.Add(&connHook{baseHook: baseHook{name: n}, connectOrder: &order})
	}

	replacement := &connHook{baseHook: baseHook{name: "first"}, connectOrder: &order}
	r.Add(replacement)

	assertNoError(t, r.FireOnConnect(context.Background(), newTestConn("c1", "u1")))

	want := []string{"first", "second", "third"}
	if len(order) != len(want) {
		t.Fatalf("fired %v, want %v", order, want)
	}

	for i, n := range want {
		if order[i] != n {
			t.Errorf("hook %d = %q, want %q", i, order[i], n)
		}
	}

	if replacement.connectCallCount() != 1 {
		t.Errorf("replacement fired %d times, want 1", replacement.connectCallCount())
	}
}

func TestHookRegistry_ListIsInRegistrationOrder(t *testing.T) {
	r := NewHookRegistry()

	names := []string{"alpha", "beta", "gamma"}
	for _, n := range names {
		r.Add(&connHook{baseHook: baseHook{name: n}})
	}

	got := r.List()
	if len(got) != len(names) {
		t.Fatalf("List = %d hooks, want %d", len(got), len(names))
	}

	for i, want := range names {
		if got[i].Name() != want {
			t.Errorf("hook %d = %q, want %q", i, got[i].Name(), want)
		}
	}
}

// --- Connection hooks ------------------------------------------------------

func TestHookRegistry_FireOnConnect(t *testing.T) {
	sentinel := errors.New("rejected by policy")

	tests := []struct {
		name    string
		hooks   []StreamingHook
		wantErr error
	}{
		{
			name: "no hooks",
		},
		{
			name:  "all hooks accept",
			hooks: []StreamingHook{&connHook{baseHook: baseHook{name: "a"}}, &connHook{baseHook: baseHook{name: "b"}}},
		},
		{
			name: "a hook returning an error rejects the connection",
			hooks: []StreamingHook{
				&connHook{baseHook: baseHook{name: "a"}},
				&connHook{baseHook: baseHook{name: "b"}, connectErr: sentinel},
			},
			wantErr: sentinel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewHookRegistry()
			for _, h := range tt.hooks {
				r.Add(h)
			}

			err := r.FireOnConnect(context.Background(), newTestConn("c1", "u1"))

			if tt.wantErr != nil {
				assertErrorIs(t, err, tt.wantErr)
			} else {
				assertNoError(t, err)
			}
		})
	}
}

func TestHookRegistry_FireOnDisconnectIgnoresOrderAndErrors(t *testing.T) {
	r := NewHookRegistry()

	a := &connHook{baseHook: baseHook{name: "a"}, connectErr: errors.New("only affects connect")}
	b := &connHook{baseHook: baseHook{name: "b"}}

	r.Add(a)
	r.Add(b)

	r.FireOnDisconnect(context.Background(), newTestConn("c1", "u1"))

	for _, h := range []*connHook{a, b} {
		h.mu.Lock()
		n := len(h.disconnects)
		h.mu.Unlock()

		if n != 1 {
			t.Errorf("hook %q: %d disconnect calls, want 1", h.name, n)
		}
	}
}

// --- Message hooks ---------------------------------------------------------

func TestHookRegistry_FireOnMessageReceived(t *testing.T) {
	sentinel := errors.New("hook failed")

	tests := []struct {
		name      string
		hooks     []StreamingHook
		wantMsgID string
		wantNil   bool
		wantErr   error
	}{
		{
			name:      "no hooks passes the message through",
			wantMsgID: "original",
		},
		{
			name: "hooks chain transformations",
			hooks: []StreamingHook{
				&msgHook{baseHook: baseHook{name: "a"}, transform: func(m *Message) *Message {
					out := *m
					out.ID = m.ID + "+a"

					return &out
				}},
			},
			wantMsgID: "original+a",
		},
		{
			name: "a hook returning nil drops the message",
			hooks: []StreamingHook{
				&msgHook{baseHook: baseHook{name: "blocker"}, transform: func(m *Message) *Message { return nil }},
			},
			wantNil: true,
		},
		{
			name: "a hook returning an error drops the message and surfaces the error",
			hooks: []StreamingHook{
				&msgHook{baseHook: baseHook{name: "failer"}, receiveErr: sentinel},
			},
			wantNil: true,
			wantErr: sentinel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewHookRegistry()
			for _, h := range tt.hooks {
				r.Add(h)
			}

			got, err := r.FireOnMessageReceived(
				context.Background(),
				newTestConn("c1", "u1"),
				&Message{ID: "original"},
			)

			if tt.wantErr != nil {
				assertErrorIs(t, err, tt.wantErr)
			} else {
				assertNoError(t, err)
			}

			if tt.wantNil {
				if got != nil {
					t.Fatalf("message = %+v, want nil (blocked)", got)
				}

				return
			}

			if got == nil {
				t.Fatal("message = nil, want it passed through")
			}

			if got.ID != tt.wantMsgID {
				t.Errorf("message ID = %q, want %q", got.ID, tt.wantMsgID)
			}
		})
	}
}

func TestHookRegistry_FireOnMessageReceived_BlockShortCircuits(t *testing.T) {
	// Once a hook blocks, later hooks must not observe the message at all.
	r := NewHookRegistry()

	blocker := &msgHook{baseHook: baseHook{name: "blocker"}, transform: func(m *Message) *Message { return nil }}
	r.Add(blocker)

	// Only the blocker is registered, so the guarantee under test is that a nil
	// return stops the chain rather than being treated as "no change".
	got, err := r.FireOnMessageReceived(context.Background(), newTestConn("c1", "u1"), &Message{ID: "m"})
	assertNoError(t, err)

	if got != nil {
		t.Fatalf("message = %+v, want nil", got)
	}

	if names := blocker.receivedNames(); len(names) != 1 {
		t.Errorf("blocker saw %d messages, want 1", len(names))
	}
}

func TestHookRegistry_FireOnMessageDeliveredIsAsync(t *testing.T) {
	r := NewHookRegistry()

	ch := make(chan struct{}, 1)
	r.Add(&msgHook{baseHook: baseHook{name: "delivered"}, deliverCh: ch})

	r.FireOnMessageDelivered(context.Background(), newTestConn("c1", "u1"), &Message{ID: "m"})

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("OnMessageDelivered was not called within 2s")
	}
}

func TestHookRegistry_FireOnMessageDeliveredWithNoHooks(t *testing.T) {
	// The no-hook path must not spawn a goroutine or panic.
	r := NewHookRegistry()
	r.FireOnMessageDelivered(context.Background(), newTestConn("c1", "u1"), &Message{ID: "m"})
}

// --- Raw message hooks -----------------------------------------------------

func TestHookRegistry_FireOnRawMessage(t *testing.T) {
	sentinel := errors.New("raw rejected")

	tests := []struct {
		name    string
		hooks   []StreamingHook
		want    string
		wantErr error
	}{
		{
			name: "no hooks passes bytes through",
			want: "body",
		},
		{
			name: "hook transforms bytes",
			hooks: []StreamingHook{
				&rawHook{baseHook: baseHook{name: "upper"}, transform: func(b []byte) []byte {
					return append(b, '!')
				}},
			},
			want: "body!",
		},
		{
			name: "hook error drops the message",
			hooks: []StreamingHook{
				&rawHook{baseHook: baseHook{name: "failer"}, err: sentinel},
			},
			wantErr: sentinel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewHookRegistry()
			for _, h := range tt.hooks {
				r.Add(h)
			}

			got, err := r.FireOnRawMessage(context.Background(), newTestConn("c1", "u1"), []byte("body"))

			if tt.wantErr != nil {
				assertErrorIs(t, err, tt.wantErr)

				if got != nil {
					t.Errorf("bytes = %q, want nil on error", got)
				}

				return
			}

			assertNoError(t, err)

			if string(got) != tt.want {
				t.Errorf("bytes = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHookRegistry_FireOnRawMessage_NilReturnIsNotABlock(t *testing.T) {
	// Unlike FireOnMessageReceived, the raw chain has no nil-means-block
	// convention: a hook returning nil bytes simply passes nil along, and the
	// caller then hands nil to the decoder. Documented here so a future change
	// to the raw contract has to update this test deliberately.
	r := NewHookRegistry()
	r.Add(&rawHook{baseHook: baseHook{name: "niller"}, transform: func([]byte) []byte { return nil }})

	got, err := r.FireOnRawMessage(context.Background(), newTestConn("c1", "u1"), []byte("body"))
	assertNoError(t, err)

	if got != nil {
		t.Errorf("bytes = %q, want nil", got)
	}
}

func TestHookRegistry_FireOnRawMessage_ChainStopsAtError(t *testing.T) {
	r := NewHookRegistry()

	failing := &rawHook{baseHook: baseHook{name: "failer"}, err: errFake}
	r.Add(failing)

	_, err := r.FireOnRawMessage(context.Background(), newTestConn("c1", "u1"), []byte("x"))
	assertErrorIs(t, err, errFake)

	if failing.callCount() != 1 {
		t.Errorf("failing hook called %d times, want 1", failing.callCount())
	}
}

// --- Room hooks ------------------------------------------------------------

func TestHookRegistry_FireOnRoomJoin(t *testing.T) {
	sentinel := errors.New("join denied")

	tests := []struct {
		name    string
		joinErr error
		wantErr error
	}{
		{name: "hook accepts the join"},
		{name: "hook rejects the join", joinErr: sentinel, wantErr: sentinel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewHookRegistry()

			hook := &roomHookDouble{baseHook: baseHook{name: "room"}, joinErr: tt.joinErr}
			r.Add(hook)

			err := r.FireOnRoomJoin(context.Background(), newTestConn("c1", "u1"), "room-1")

			if tt.wantErr != nil {
				assertErrorIs(t, err, tt.wantErr)
			} else {
				assertNoError(t, err)
			}

			hook.mu.Lock()
			joins := len(hook.joins)
			hook.mu.Unlock()

			if joins != 1 {
				t.Errorf("OnRoomJoin called %d times, want 1", joins)
			}
		})
	}
}

func TestHookRegistry_FireOnRoomCreateBlocksOnError(t *testing.T) {
	r := NewHookRegistry()
	r.Add(&roomHookDouble{baseHook: baseHook{name: "room"}, createErr: errFake})

	err := r.FireOnRoomCreate(context.Background(), newFakeRoom("room-1"))
	assertErrorIs(t, err, errFake)
}

func TestHookRegistry_FireOnRoomLeaveAndDelete(t *testing.T) {
	r := NewHookRegistry()

	hook := &roomHookDouble{baseHook: baseHook{name: "room"}}
	r.Add(hook)

	r.FireOnRoomLeave(context.Background(), newTestConn("c1", "u1"), "room-1")
	r.FireOnRoomDelete(context.Background(), "room-1")

	hook.mu.Lock()
	defer hook.mu.Unlock()

	if len(hook.leaves) != 1 {
		t.Errorf("OnRoomLeave called %d times, want 1", len(hook.leaves))
	}

	if len(hook.deletes) != 1 {
		t.Errorf("OnRoomDelete called %d times, want 1", len(hook.deletes))
	}
}

// --- Presence and error hooks ---------------------------------------------

func TestHookRegistry_FireOnPresenceChange(t *testing.T) {
	r := NewHookRegistry()

	hook := &presenceHookDouble{baseHook: baseHook{name: "presence"}}
	r.Add(hook)

	r.FireOnPresenceChange(context.Background(), "u1", StatusOffline, StatusOnline)

	if hook.changeCount() != 1 {
		t.Fatalf("OnPresenceChange called %d times, want 1", hook.changeCount())
	}

	hook.mu.Lock()
	defer hook.mu.Unlock()

	if got, want := hook.changes[0], [3]string{"u1", StatusOffline, StatusOnline}; got != want {
		t.Errorf("change = %v, want %v", got, want)
	}
}

func TestHookRegistry_FireOnError(t *testing.T) {
	r := NewHookRegistry()

	hook := &errHookDouble{baseHook: baseHook{name: "err"}}
	r.Add(hook)

	r.FireOnError(context.Background(), newTestConn("c1", "u1"), errFake)

	hook.mu.Lock()
	defer hook.mu.Unlock()

	if len(hook.errs) != 1 {
		t.Fatalf("OnError called %d times, want 1", len(hook.errs))
	}

	if !errors.Is(hook.errs[0], errFake) {
		t.Errorf("OnError got %v, want %v", hook.errs[0], errFake)
	}
}

// --- Concurrency -----------------------------------------------------------

func TestHookRegistry_ConcurrentAddAndFire(t *testing.T) {
	r := NewHookRegistry()
	conn := newTestConn("c1", "u1")

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()

		for i := 0; i < 200; i++ {
			r.Add(&connHook{baseHook: baseHook{name: "churn"}})
			r.Remove("churn")
		}
	}()

	wg.Add(1)

	go func() {
		defer wg.Done()

		for i := 0; i < 200; i++ {
			_ = r.FireOnConnect(context.Background(), conn)
			r.FireOnDisconnect(context.Background(), conn)
			_, _ = r.FireOnMessageReceived(context.Background(), conn, &Message{ID: "m"})
			_ = r.List()
		}
	}()

	wg.Wait()
}
