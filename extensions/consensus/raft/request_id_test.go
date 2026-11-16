package raft

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
	"github.com/xraph/forge/extensions/consensus/statemachine"
)

// captureTransport hands every outgoing message to the test and never
// delivers it anywhere.
type captureTransport struct {
	sent  chan internal.Message
	inbox chan internal.Message
}

func newCaptureTransport() *captureTransport {
	return &captureTransport{sent: make(chan internal.Message, 64), inbox: make(chan internal.Message, 64)}
}

func (c *captureTransport) Start(context.Context) error { return nil }
func (c *captureTransport) Stop(context.Context) error  { return nil }

func (c *captureTransport) Send(_ context.Context, _ string, message any) error {
	c.sent <- message.(internal.Message)

	return nil
}

func (c *captureTransport) Receive() <-chan internal.Message  { return c.inbox }
func (c *captureTransport) AddPeer(string, string, int) error { return nil }
func (c *captureTransport) RemovePeer(string) error           { return nil }
func (c *captureTransport) GetAddress() string                { return "capture" }

func (c *captureTransport) next(t *testing.T) internal.Message {
	t.Helper()

	select {
	case m := <-c.sent:
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("no message sent")

		return internal.Message{}
	}
}

// newLeader builds a started leader in term 2 with one peer, "p".
func newLeader(t *testing.T) (*Node, *captureTransport) {
	t.Helper()

	lg := forge.NewNoopLogger()
	tr := newCaptureTransport()
	sm := statemachine.NewMemoryStateMachine(statemachine.MemoryStateMachineConfig{}, lg)

	n, err := NewNode(Config{
		NodeID:             "L",
		HeartbeatInterval:  time.Hour,
		ElectionTimeoutMin: time.Hour,
		ElectionTimeoutMax: 2 * time.Hour,
	}, lg, sm, tr, newMemStorage(t))
	if err != nil {
		t.Fatal(err)
	}

	n.ctx, n.cancel = context.WithCancel(context.Background())
	t.Cleanup(n.cancel)

	n.AddPeer("p")
	n.currentTerm = 2
	n.role.Store(internal.RoleLeader)

	if err := n.log.AppendBatch(ents(2, 1, 10)); err != nil {
		t.Fatal(err)
	}

	return n, tr
}

// Each request carries a fresh ID, and a response is only delivered to the
// request whose ID it echoes.
func TestLateResponseIsNotMatchedToNewerRequest(t *testing.T) {
	n, tr := newLeader(t)

	// First request times out without an answer.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	firstErr := make(chan error, 1)

	go func() {
		_, err := n.call(ctx, "p", internal.MessageTypeAppendEntries, &internal.AppendEntriesRequest{Term: 2})
		firstErr <- err
	}()

	first := tr.next(t)

	if err := <-firstErr; err == nil {
		t.Fatal("first request should have timed out")
	}

	// Second request is in flight when the first one's response arrives late.
	got := make(chan any, 1)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		resp, _ := n.call(ctx, "p", internal.MessageTypeAppendEntries, &internal.AppendEntriesRequest{Term: 2})
		got <- resp
	}()

	second := tr.next(t)
	if second.RequestID == first.RequestID {
		t.Fatalf("two requests to the same peer share request ID %d", first.RequestID)
	}

	late := &internal.AppendEntriesResponse{Term: 2, Success: true, MatchIndex: 1}
	n.handleMessage(internal.Message{
		Type: internal.MessageTypeAppendEntriesResponse, From: "p", To: "L",
		RequestID: first.RequestID, Payload: late,
	})

	select {
	case resp := <-got:
		t.Fatalf("late response %+v was delivered to the newer request", resp)
	case <-time.After(100 * time.Millisecond):
	}

	current := &internal.AppendEntriesResponse{Term: 2, Success: true, MatchIndex: 10}
	n.handleMessage(internal.Message{
		Type: internal.MessageTypeAppendEntriesResponse, From: "p", To: "L",
		RequestID: second.RequestID, Payload: current,
	})

	if resp := <-got; resp != current {
		t.Fatalf("second request got %+v, want its own response", resp)
	}
}

// Responses are only handled by the goroutine that sent the request. A
// response arriving through the message loop must not touch replication state
// a second time.
func TestUnsolicitedResponseDoesNotChangeReplicationState(t *testing.T) {
	n, _ := newLeader(t)

	n.mu.Lock()
	n.nextIndex["p"] = 8
	n.matchIndex["p"] = 5
	n.mu.Unlock()

	for _, resp := range []*internal.AppendEntriesResponse{
		{Term: 2, Success: false},
		{Term: 2, Success: true, MatchIndex: 10},
	} {
		n.handleMessage(internal.Message{
			Type: internal.MessageTypeAppendEntriesResponse, From: "p", To: "L",
			RequestID: 999, Payload: resp,
		})
	}

	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.nextIndex["p"] != 8 || n.matchIndex["p"] != 5 {
		t.Fatalf("nextIndex=%d matchIndex=%d, want 8 and 5", n.nextIndex["p"], n.matchIndex["p"])
	}
}

// Reordered responses never move matchIndex backwards or past what the
// request carried, and a stale rejection does not back nextIndex off again.
func TestResponseHandlingIsBoundedByItsRequest(t *testing.T) {
	n, _ := newLeader(t)
	peer := n.peers["p"]

	newer := &internal.AppendEntriesRequest{Term: 2, PrevLogIndex: 0, Entries: ents(2, 1, 10)}
	older := &internal.AppendEntriesRequest{Term: 2, PrevLogIndex: 0, Entries: ents(2, 1, 3)}

	// The follower claims more than the request carried.
	n.handleAppendEntriesResponse(peer, newer, &internal.AppendEntriesResponse{Term: 2, Success: true, MatchIndex: 50})

	if n.matchIndex["p"] != 10 || n.nextIndex["p"] != 11 {
		t.Fatalf("matchIndex=%d nextIndex=%d, want 10 and 11", n.matchIndex["p"], n.nextIndex["p"])
	}

	// The older, shorter request's response arrives last.
	n.handleAppendEntriesResponse(peer, older, &internal.AppendEntriesResponse{Term: 2, Success: true, MatchIndex: 3})

	if n.matchIndex["p"] != 10 || n.nextIndex["p"] != 11 {
		t.Fatalf("stale success moved matchIndex=%d nextIndex=%d", n.matchIndex["p"], n.nextIndex["p"])
	}

	// A rejection for a request that no longer matches nextIndex is ignored.
	n.handleAppendEntriesResponse(peer, &internal.AppendEntriesRequest{Term: 2, PrevLogIndex: 4},
		&internal.AppendEntriesResponse{Term: 2, Success: false, LastLogIndex: 2})

	if n.nextIndex["p"] != 11 {
		t.Fatalf("stale rejection changed nextIndex to %d", n.nextIndex["p"])
	}

	// The current rejection backs off once, using the follower's hint, but
	// never below matchIndex+1.
	n.matchIndex["p"] = 0
	n.handleAppendEntriesResponse(peer, &internal.AppendEntriesRequest{Term: 2, PrevLogIndex: 10},
		&internal.AppendEntriesResponse{Term: 2, Success: false, LastLogIndex: 4})

	if n.nextIndex["p"] != 5 {
		t.Fatalf("nextIndex = %d, want 5 from the follower's last index hint", n.nextIndex["p"])
	}
}

// A vote granted in an earlier term does not count in a later election.
func TestVoteFromOlderTermIsNotCounted(t *testing.T) {
	n, tr := newLeader(t)
	peer := n.peers["p"]

	granted := make(chan bool, 1)

	go func() { granted <- n.requestVote(peer, 5, 10, 2) }()

	req := tr.next(t)
	n.handleMessage(internal.Message{
		Type: internal.MessageTypeRequestVoteResponse, From: "p", To: "L",
		RequestID: req.RequestID, Payload: &internal.RequestVoteResponse{Term: 4, VoteGranted: true},
	})

	if <-granted {
		t.Fatal("counted a vote granted for term 4 in the term 5 election")
	}
}
