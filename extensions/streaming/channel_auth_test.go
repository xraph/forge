package streaming

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge"
	streamauth "github.com/xraph/forge/extensions/streaming/auth"
	"github.com/xraph/forge/extensions/streaming/backends/local"
	"github.com/xraph/forge/extensions/streaming/filters"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// realManager wires a manager over the real local backends, the way
// Extension.Register does with the default config.
func realManager(t testing.TB, opts ...ManagerOption) (Manager, streaming.RoomStore, streaming.ChannelStore) {
	t.Helper()

	cfg := DefaultConfig()
	rooms := local.NewRoomStore()
	chans := local.NewChannelStore()
	msgs := local.NewMessageStore()
	ra := streamauth.NewRoomAuthorizer(rooms)

	base := []ManagerOption{
		WithRoomAuthorizer(ra),
		WithMessageAuthorizer(streamauth.NewMessageAuthorizer(ra, newAuthMessageStore(msgs))),
		WithChannelAuthorizer(streamauth.NewChannelAuthorizer(chans)),
		WithFilterChain(filters.NewFilterChain()),
		WithHookRegistry(NewHookRegistry()),
		WithCodecRegistry(NewCodecRegistry()),
	}

	m := NewManager(cfg, rooms, chans, msgs, newFakePresenceTracker(), newFakeTypingTracker(), nil, nil, nil,
		append(base, opts...)...)

	return m, rooms, chans
}

func createChannel(t *testing.T, chans streaming.ChannelStore, id string, private bool) {
	t.Helper()

	ch := local.NewLocalChannel(streaming.ChannelOptions{ID: id, Name: id, Private: private})
	if err := chans.Create(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
}

// Subscribe had no authorization, and handleSSE calls it for every id in
// ?channels=, so an unauthenticated EventSource could read any channel it
// could name. A private notifications channel delivered a password reset link
// to an anonymous subscriber.
func TestSubscribe_AnonymousCannotReadPrivateChannel(t *testing.T) {
	m, _, chans := realManager(t)
	ctx := context.Background()

	createChannel(t, chans, "user-42-notifications", true)

	attacker := newTestConn("attacker", "")
	assertNoError(t, m.Register(attacker))

	err := m.Subscribe(ctx, attacker.ID(), "user-42-notifications", nil)
	assertErrorIs(t, err, ErrChannelAccessDenied)

	_ = m.BroadcastToChannel(ctx, "user-42-notifications",
		&Message{ID: "secret", Type: MessageTypeMessage, ChannelID: "user-42-notifications", Data: "password reset link"})

	attacker.waitQuiet(t)

	if n := attacker.rec.totalWrites(); n != 0 {
		t.Fatalf("anonymous connection received %d private channel frames", n)
	}
}

func TestSubscribe_AnonymousRefusedByDefault(t *testing.T) {
	m, _, chans := realManager(t)
	createChannel(t, chans, "news", false)

	anon := newTestConn("anon", "")
	assertNoError(t, m.Register(anon))

	assertErrorIs(t, m.Subscribe(context.Background(), anon.ID(), "news", nil), ErrChannelAccessDenied)
}

func TestSubscribe_AnonymousAllowedWhenConfigured(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AllowAnonymousSubscriptions = true

	chans := local.NewChannelStore()
	m := NewManager(cfg, local.NewRoomStore(), chans, local.NewMessageStore(),
		newFakePresenceTracker(), newFakeTypingTracker(), nil, nil, nil)

	createChannel(t, chans, "news", false)
	createChannel(t, chans, "secret", true)

	anon := newTestConn("anon", "")
	assertNoError(t, m.Register(anon))

	assertNoError(t, m.Subscribe(context.Background(), anon.ID(), "news", nil))

	// Admitting anonymous subscribers does not open private channels: the
	// default authorizer, which NewManager installs on its own, still refuses.
	assertErrorIs(t, m.Subscribe(context.Background(), anon.ID(), "secret", nil), ErrChannelAccessDenied)
}

func TestSubscribe_PrivateChannelNeedsAuthorizer(t *testing.T) {
	m, _, chans := realManager(t)
	ctx := context.Background()

	createChannel(t, chans, "user-42-notifications", true)

	owner := newTestConn("owner", "user-42")
	other := newTestConn("other", "user-7")

	for _, c := range []*testConn{owner, other} {
		assertNoError(t, m.Register(c))
	}

	// The default policy cannot tell who belongs, so it refuses everyone.
	assertErrorIs(t, m.Subscribe(ctx, owner.ID(), "user-42-notifications", nil), ErrChannelAccessDenied)

	// An application policy that knows its users decides instead.
	m.(*manager).setChannelAuthorizer(streamauth.ChannelAuthorizerFunc(
		func(_ context.Context, userID, channelID string) (bool, error) {
			return channelID == "user-"+strings.TrimPrefix(userID, "user-")+"-notifications", nil
		}))

	assertNoError(t, m.Subscribe(ctx, owner.ID(), "user-42-notifications", nil))
	assertErrorIs(t, m.Subscribe(ctx, other.ID(), "user-42-notifications", nil), ErrChannelAccessDenied)
}

func TestSubscribe_AuthenticatedPublicChannelStillWorks(t *testing.T) {
	m, _, chans := realManager(t)
	createChannel(t, chans, "news", false)

	c := newTestConn("c1", "alice")
	assertNoError(t, m.Register(c))

	assertNoError(t, m.Subscribe(context.Background(), c.ID(), "news", nil))

	if !c.IsSubscribed("news") {
		t.Fatal("connection not subscribed after an allowed Subscribe")
	}
}

// End to end: the exact request from the finding, an anonymous
// GET /sse?channels=user-42-notifications, must not end up subscribed.
func TestHandleSSE_AnonymousChannelsParamIsRefused(t *testing.T) {
	app := forge.NewApp(forge.AppConfig{Name: "sse-auth"})
	ext := NewExtension(WithLocalBackend())

	if err := ext.Register(app); err != nil {
		t.Fatal(err)
	}

	if err := ext.RegisterRoutes(app.Router(), "/ws", "/sse"); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(app.Router().(http.Handler))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ext.Manager().CreateChannel(ctx, local.NewLocalChannel(streaming.ChannelOptions{
		ID: "user-42-notifications", Name: "n", Private: true,
	})); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/sse?channels=user-42-notifications", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "event: connected") {
			break
		}
	}

	conns := ext.Manager().GetAllConnections()
	if len(conns) != 1 {
		t.Fatalf("expected one registered SSE connection, got %d", len(conns))
	}

	// The handler subscribes from the query string just after it writes the
	// connected event. Watch for a while rather than checking once, so the
	// check cannot pass merely by running before the handler got there.
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if subs := conns[0].GetSubscriptions(); len(subs) != 0 {
			t.Fatalf("anonymous SSE connection subscribed to %v", subs)
		}

		time.Sleep(5 * time.Millisecond)
	}
}
