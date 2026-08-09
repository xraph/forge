package trackers

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/streaming/backends/local"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// failingPresenceStore fails a chosen operation so the tracker's error handling
// can be driven. Unimplemented methods panic, flagging an unmodelled path.
type failingPresenceStore struct {
	streaming.PresenceStore

	getErr        error
	setErr        error
	onlineErr     error
	offlineErr    error
	getOnlineErr  error
	activityErr   error
	countErr      error
	setMultiErr   error
	cleanupErr    error
	presence      *streaming.UserPresence
	onlineUserIDs []string

	mu       sync.Mutex
	cleanups int
}

func (s *failingPresenceStore) Get(ctx context.Context, userID string) (*streaming.UserPresence, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}

	if s.presence != nil {
		return s.presence, nil
	}

	return nil, streaming.ErrPresenceNotFound
}

func (s *failingPresenceStore) Set(ctx context.Context, userID string, presence *streaming.UserPresence) error {
	return s.setErr
}

func (s *failingPresenceStore) SetOnline(ctx context.Context, userID string, ttl time.Duration) error {
	return s.onlineErr
}

func (s *failingPresenceStore) SetOffline(ctx context.Context, userID string) error {
	return s.offlineErr
}

func (s *failingPresenceStore) GetOnline(ctx context.Context) ([]string, error) {
	if s.getOnlineErr != nil {
		return nil, s.getOnlineErr
	}

	return s.onlineUserIDs, nil
}

func (s *failingPresenceStore) UpdateActivity(ctx context.Context, userID string, timestamp time.Time) error {
	return s.activityErr
}

func (s *failingPresenceStore) CountByStatus(ctx context.Context) (map[string]int, error) {
	if s.countErr != nil {
		return nil, s.countErr
	}

	return map[string]int{streaming.StatusOnline: len(s.onlineUserIDs)}, nil
}

func (s *failingPresenceStore) SetMultiple(ctx context.Context, presences map[string]*streaming.UserPresence) error {
	return s.setMultiErr
}

func (s *failingPresenceStore) CleanupExpired(ctx context.Context, olderThan time.Duration) error {
	s.mu.Lock()
	s.cleanups++
	s.mu.Unlock()

	return s.cleanupErr
}

func (s *failingPresenceStore) cleanupCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.cleanups
}

func newPresenceTracker(store streaming.PresenceStore) streaming.PresenceTracker {
	return NewPresenceTracker(store, streaming.DefaultPresenceOptions(), nil, nil)
}

// --- SetPresence -----------------------------------------------------------

func TestPresenceTracker_SetPresenceValidatesStatus(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		status  string
		wantErr bool
	}{
		{name: "online", status: streaming.StatusOnline},
		{name: "away", status: streaming.StatusAway},
		{name: "busy", status: streaming.StatusBusy},
		{name: "offline", status: streaming.StatusOffline},
		{name: "unknown status", status: "invisible", wantErr: true},
		{name: "empty status", status: "", wantErr: true},
		{name: "wrong case", status: "ONLINE", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := newPresenceTracker(local.NewPresenceStore())

			err := tracker.SetPresence(ctx, "alice", tt.status)

			if tt.wantErr {
				if !errors.Is(err, streaming.ErrInvalidStatus) {
					t.Errorf("SetPresence(%q) = %v, want ErrInvalidStatus", tt.status, err)
				}

				return
			}

			if err != nil {
				t.Fatalf("SetPresence(%q) = %v, want nil", tt.status, err)
			}

			got, err := tracker.GetPresence(ctx, "alice")
			if err != nil {
				t.Fatalf("GetPresence: %v", err)
			}

			if got.Status != tt.status {
				t.Errorf("stored status = %q, want %q", got.Status, tt.status)
			}
		})
	}
}

func TestPresenceTracker_SetPresenceCreatesThenUpdates(t *testing.T) {
	ctx := context.Background()
	tracker := newPresenceTracker(local.NewPresenceStore())

	if err := tracker.SetPresence(ctx, "alice", streaming.StatusOnline); err != nil {
		t.Fatalf("SetPresence: %v", err)
	}

	online, err := tracker.IsOnline(ctx, "alice")
	if err != nil {
		t.Fatalf("IsOnline: %v", err)
	}

	if !online {
		t.Error("IsOnline = false after going online, want true")
	}

	if err := tracker.SetPresence(ctx, "alice", streaming.StatusOffline); err != nil {
		t.Fatalf("SetPresence: %v", err)
	}

	if online, _ := tracker.IsOnline(ctx, "alice"); online {
		t.Error("IsOnline = true after going offline, want false")
	}

	got, err := tracker.GetPresence(ctx, "alice")
	if err != nil {
		t.Fatalf("GetPresence: %v", err)
	}

	if got.Status != streaming.StatusOffline {
		t.Errorf("status = %q, want offline", got.Status)
	}
}

func TestPresenceTracker_AwayAndBusyDropOutOfTheOnlineSet(t *testing.T) {
	// The tracker's own switch only calls SetOnline/SetOffline for the online
	// and offline statuses, but the local store's Set already maintains the
	// online index: any status other than "online" removes the user from it.
	// So an away or busy user is reported as not online and disappears from
	// GetOnlineUsers, even though a presence record still exists.
	ctx := context.Background()

	for _, status := range []string{streaming.StatusAway, streaming.StatusBusy} {
		t.Run(status, func(t *testing.T) {
			tracker := newPresenceTracker(local.NewPresenceStore())

			if err := tracker.SetPresence(ctx, "alice", streaming.StatusOnline); err != nil {
				t.Fatalf("SetPresence: %v", err)
			}

			if online, _ := tracker.IsOnline(ctx, "alice"); !online {
				t.Fatal("IsOnline = false right after going online")
			}

			if err := tracker.SetPresence(ctx, "alice", status); err != nil {
				t.Fatalf("SetPresence(%s): %v", status, err)
			}

			online, err := tracker.IsOnline(ctx, "alice")
			if err != nil {
				t.Fatalf("IsOnline: %v", err)
			}

			if online {
				t.Errorf("IsOnline = true after switching to %s, want false", status)
			}

			// The presence record itself survives with the new status.
			got, err := tracker.GetPresence(ctx, "alice")
			if err != nil {
				t.Fatalf("GetPresence: %v", err)
			}

			if got.Status != status {
				t.Errorf("status = %q, want %q", got.Status, status)
			}
		})
	}
}

func TestPresenceTracker_SetPresencePropagatesStoreErrors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		store  *failingPresenceStore
		status string
	}{
		{
			name:   "get failure other than not-found aborts",
			store:  &failingPresenceStore{getErr: errStore},
			status: streaming.StatusOnline,
		},
		{
			name:   "set failure aborts",
			store:  &failingPresenceStore{setErr: errStore},
			status: streaming.StatusOnline,
		},
		{
			name:   "set-online failure aborts",
			store:  &failingPresenceStore{onlineErr: errStore},
			status: streaming.StatusOnline,
		},
		{
			name:   "set-offline failure aborts",
			store:  &failingPresenceStore{offlineErr: errStore},
			status: streaming.StatusOffline,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := newPresenceTracker(tt.store)

			if err := tracker.SetPresence(ctx, "alice", tt.status); !errors.Is(err, errStore) {
				t.Errorf("SetPresence = %v, want it to wrap errStore", err)
			}
		})
	}
}

func TestPresenceTracker_SetPresenceTreatsNotFoundAsFirstSighting(t *testing.T) {
	// ErrPresenceNotFound is not a failure: it means the user has no record yet,
	// so the tracker builds one instead of propagating the error.
	ctx := context.Background()
	tracker := newPresenceTracker(&failingPresenceStore{getErr: streaming.ErrPresenceNotFound})

	if err := tracker.SetPresence(ctx, "alice", streaming.StatusOnline); err != nil {
		t.Errorf("SetPresence = %v, want nil for a first-time user", err)
	}
}

// --- Activity and online sets ----------------------------------------------

func TestPresenceTracker_TrackActivity(t *testing.T) {
	ctx := context.Background()
	store := local.NewPresenceStore()
	tracker := newPresenceTracker(store)

	if err := tracker.SetPresence(ctx, "alice", streaming.StatusOnline); err != nil {
		t.Fatalf("SetPresence: %v", err)
	}

	before, err := tracker.GetLastSeen(ctx, "alice")
	if err != nil {
		t.Fatalf("GetLastSeen: %v", err)
	}

	time.Sleep(2 * time.Millisecond)

	if err := tracker.TrackActivity(ctx, "alice"); err != nil {
		t.Fatalf("TrackActivity: %v", err)
	}

	after, err := tracker.GetLastSeen(ctx, "alice")
	if err != nil {
		t.Fatalf("GetLastSeen: %v", err)
	}

	if !after.After(before) {
		t.Errorf("last seen = %v, want it advanced past %v", after, before)
	}
}

func TestPresenceTracker_TrackActivityPropagatesErrors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name  string
		store *failingPresenceStore
	}{
		{name: "update-activity failure", store: &failingPresenceStore{activityErr: errStore}},
		{name: "set-online failure", store: &failingPresenceStore{onlineErr: errStore}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := newPresenceTracker(tt.store)

			if err := tracker.TrackActivity(ctx, "alice"); !errors.Is(err, errStore) {
				t.Errorf("TrackActivity = %v, want it to wrap errStore", err)
			}
		})
	}
}

func TestPresenceTracker_GetOnlineUsers(t *testing.T) {
	ctx := context.Background()
	tracker := newPresenceTracker(local.NewPresenceStore())

	for _, u := range []string{"alice", "bob"} {
		if err := tracker.SetPresence(ctx, u, streaming.StatusOnline); err != nil {
			t.Fatalf("SetPresence: %v", err)
		}
	}

	if err := tracker.SetPresence(ctx, "carol", streaming.StatusOffline); err != nil {
		t.Fatalf("SetPresence: %v", err)
	}

	users, err := tracker.GetOnlineUsers(ctx)
	if err != nil {
		t.Fatalf("GetOnlineUsers: %v", err)
	}

	if len(users) != 2 {
		t.Errorf("GetOnlineUsers = %v, want two users", users)
	}

	for _, want := range []string{"alice", "bob"} {
		if !slices.Contains(users, want) {
			t.Errorf("GetOnlineUsers = %v, missing %q", users, want)
		}
	}
}

func TestPresenceTracker_GetOnlineUsersInRoomWithoutAResolver(t *testing.T) {
	// Without WithRoomMembers the tracker has no way to know who is in a room,
	// so it falls back to every online user rather than pretending the room is
	// empty. Pinned because that fallback is a deliberate choice, not an
	// oversight — reporting nobody would silently break callers that never
	// wired a resolver.
	ctx := context.Background()
	tracker := newPresenceTracker(local.NewPresenceStore())

	for _, u := range []string{"alice", "bob"} {
		if err := tracker.SetPresence(ctx, u, streaming.StatusOnline); err != nil {
			t.Fatalf("SetPresence: %v", err)
		}
	}

	inRoom, err := tracker.GetOnlineUsersInRoom(ctx, "a-room-nobody-is-in")
	if err != nil {
		t.Fatalf("GetOnlineUsersInRoom: %v", err)
	}

	all, err := tracker.GetOnlineUsers(ctx)
	if err != nil {
		t.Fatalf("GetOnlineUsers: %v", err)
	}

	if len(inRoom) != len(all) {
		t.Errorf("GetOnlineUsersInRoom = %d users, want the unfiltered %d", len(inRoom), len(all))
	}
}

func TestPresenceTracker_GetOnlineUsersInRoomFiltersByMembership(t *testing.T) {
	ctx := context.Background()

	// Membership: alice and carol are in room-1; bob is not.
	members := map[string][]string{"room-1": {"alice", "carol"}}

	tracker := NewPresenceTracker(
		local.NewPresenceStore(),
		streaming.DefaultPresenceOptions(),
		nil, nil,
		WithRoomMembers(func(ctx context.Context, roomID string) ([]string, error) {
			return members[roomID], nil
		}),
	)

	// alice and bob are online; carol is a member but offline.
	for _, u := range []string{"alice", "bob"} {
		if err := tracker.SetPresence(ctx, u, streaming.StatusOnline); err != nil {
			t.Fatalf("SetPresence: %v", err)
		}
	}

	tests := []struct {
		name   string
		roomID string
		want   []string
	}{
		{name: "only online members", roomID: "room-1", want: []string{"alice"}},
		{name: "room with no members", roomID: "empty-room", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tracker.GetOnlineUsersInRoom(ctx, tt.roomID)
			if err != nil {
				t.Fatalf("GetOnlineUsersInRoom: %v", err)
			}

			if len(got) != len(tt.want) {
				t.Fatalf("GetOnlineUsersInRoom = %v, want %v", got, tt.want)
			}

			for i, want := range tt.want {
				if got[i] != want {
					t.Errorf("user %d = %q, want %q", i, got[i], want)
				}
			}
		})
	}
}

func TestPresenceTracker_GetOnlineUsersInRoomPropagatesResolverErrors(t *testing.T) {
	tracker := NewPresenceTracker(
		local.NewPresenceStore(),
		streaming.DefaultPresenceOptions(),
		nil, nil,
		WithRoomMembers(func(ctx context.Context, roomID string) ([]string, error) {
			return nil, errStore
		}),
	)

	_, err := tracker.GetOnlineUsersInRoom(context.Background(), "room-1")
	if !errors.Is(err, errStore) {
		t.Errorf("GetOnlineUsersInRoom = %v, want it to wrap errStore", err)
	}
}

// --- Custom status ---------------------------------------------------------

func TestPresenceTracker_CustomStatus(t *testing.T) {
	ctx := context.Background()
	tracker := newPresenceTracker(local.NewPresenceStore())

	if err := tracker.SetPresence(ctx, "alice", streaming.StatusOnline); err != nil {
		t.Fatalf("SetPresence: %v", err)
	}

	if err := tracker.SetCustomStatus(ctx, "alice", "heads down"); err != nil {
		t.Fatalf("SetCustomStatus: %v", err)
	}

	got, err := tracker.GetCustomStatus(ctx, "alice")
	if err != nil {
		t.Fatalf("GetCustomStatus: %v", err)
	}

	if got != "heads down" {
		t.Errorf("GetCustomStatus = %q, want %q", got, "heads down")
	}
}

func TestPresenceTracker_CustomStatusRequiresAnExistingRecord(t *testing.T) {
	// Both calls read through to the store first, so a user with no presence
	// record surfaces the store's not-found error rather than being created.
	ctx := context.Background()
	tracker := newPresenceTracker(local.NewPresenceStore())

	if err := tracker.SetCustomStatus(ctx, "ghost", "x"); err == nil {
		t.Error("SetCustomStatus for an unknown user = nil, want an error")
	}

	if _, err := tracker.GetCustomStatus(ctx, "ghost"); err == nil {
		t.Error("GetCustomStatus for an unknown user = nil, want an error")
	}
}

// --- Bulk operations -------------------------------------------------------

func TestPresenceTracker_SetPresenceForUsers(t *testing.T) {
	ctx := context.Background()
	tracker := newPresenceTracker(local.NewPresenceStore())

	updates := map[string]string{
		"alice": streaming.StatusOnline,
		"bob":   streaming.StatusAway,
	}

	if err := tracker.SetPresenceForUsers(ctx, updates); err != nil {
		t.Fatalf("SetPresenceForUsers: %v", err)
	}

	for userID, want := range updates {
		got, err := tracker.GetPresence(ctx, userID)
		if err != nil {
			t.Fatalf("GetPresence(%s): %v", userID, err)
		}

		if got.Status != want {
			t.Errorf("%s status = %q, want %q", userID, got.Status, want)
		}
	}
}

func TestPresenceTracker_SetPresenceForUsersValidatesStatus(t *testing.T) {
	ctx := context.Background()
	tracker := newPresenceTracker(local.NewPresenceStore())

	err := tracker.SetPresenceForUsers(ctx, map[string]string{"alice": "not-a-status"})
	if !errors.Is(err, streaming.ErrInvalidStatus) {
		t.Errorf("SetPresenceForUsers = %v, want ErrInvalidStatus", err)
	}
}

func TestPresenceTracker_GetPresenceBulk(t *testing.T) {
	ctx := context.Background()
	tracker := newPresenceTracker(local.NewPresenceStore())

	for _, u := range []string{"alice", "bob"} {
		if err := tracker.SetPresence(ctx, u, streaming.StatusOnline); err != nil {
			t.Fatalf("SetPresence: %v", err)
		}
	}

	got, err := tracker.GetPresenceBulk(ctx, []string{"alice", "bob", "ghost"})
	if err != nil {
		t.Fatalf("GetPresenceBulk: %v", err)
	}

	for _, u := range []string{"alice", "bob"} {
		if _, ok := got[u]; !ok {
			t.Errorf("GetPresenceBulk missing %q", u)
		}
	}

	if _, ok := got["ghost"]; ok {
		t.Error("GetPresenceBulk contains a user with no presence record")
	}
}

// --- Watching --------------------------------------------------------------

func TestPresenceTracker_WatchUser(t *testing.T) {
	ctx := context.Background()
	tracker := newPresenceTracker(local.NewPresenceStore())

	if err := tracker.WatchUser(ctx, "watcher-1", "alice"); err != nil {
		t.Fatalf("WatchUser: %v", err)
	}

	if err := tracker.WatchUser(ctx, "watcher-2", "alice"); err != nil {
		t.Fatalf("WatchUser: %v", err)
	}

	// Watching twice must not duplicate the entry.
	if err := tracker.WatchUser(ctx, "watcher-1", "alice"); err != nil {
		t.Fatalf("WatchUser (duplicate): %v", err)
	}

	watchers, err := tracker.GetWatchers(ctx, "alice")
	if err != nil {
		t.Fatalf("GetWatchers: %v", err)
	}

	if len(watchers) != 2 {
		t.Errorf("GetWatchers = %v, want two distinct watchers", watchers)
	}

	watching, err := tracker.GetWatching(ctx, "watcher-1")
	if err != nil {
		t.Fatalf("GetWatching: %v", err)
	}

	if len(watching) != 1 || watching[0] != "alice" {
		t.Errorf("GetWatching(watcher-1) = %v, want [alice]", watching)
	}
}

func TestPresenceTracker_UnwatchUser(t *testing.T) {
	ctx := context.Background()
	tracker := newPresenceTracker(local.NewPresenceStore())

	for _, w := range []string{"watcher-1", "watcher-2"} {
		if err := tracker.WatchUser(ctx, w, "alice"); err != nil {
			t.Fatalf("WatchUser: %v", err)
		}
	}

	if err := tracker.UnwatchUser(ctx, "watcher-1", "alice"); err != nil {
		t.Fatalf("UnwatchUser: %v", err)
	}

	watchers, _ := tracker.GetWatchers(ctx, "alice")
	if len(watchers) != 1 || watchers[0] != "watcher-2" {
		t.Errorf("GetWatchers = %v, want [watcher-2]", watchers)
	}

	if err := tracker.UnwatchUser(ctx, "watcher-2", "alice"); err != nil {
		t.Fatalf("UnwatchUser: %v", err)
	}

	// The last unwatch reclaims the entry entirely.
	watchers, _ = tracker.GetWatchers(ctx, "alice")
	if len(watchers) != 0 {
		t.Errorf("GetWatchers = %v, want empty", watchers)
	}

	// Unwatching what was never watched is a no-op.
	if err := tracker.UnwatchUser(ctx, "nobody", "alice"); err != nil {
		t.Errorf("UnwatchUser(nobody) = %v, want nil", err)
	}
}

func TestPresenceTracker_GetWatchersReturnsACopy(t *testing.T) {
	ctx := context.Background()
	tracker := newPresenceTracker(local.NewPresenceStore())

	if err := tracker.WatchUser(ctx, "watcher-1", "alice"); err != nil {
		t.Fatalf("WatchUser: %v", err)
	}

	watchers, _ := tracker.GetWatchers(ctx, "alice")
	watchers[0] = "mutated"

	again, _ := tracker.GetWatchers(ctx, "alice")
	if again[0] != "watcher-1" {
		t.Error("GetWatchers handed out the internal slice; mutating it changed the tracker")
	}
}

func TestPresenceTracker_GetWatchersUnknownUser(t *testing.T) {
	watchers, err := newPresenceTracker(local.NewPresenceStore()).GetWatchers(context.Background(), "ghost")
	if err != nil {
		t.Fatalf("GetWatchers: %v", err)
	}

	if len(watchers) != 0 {
		t.Errorf("GetWatchers(ghost) = %v, want empty", watchers)
	}
}

// --- Statistics ------------------------------------------------------------

func TestPresenceTracker_GetOnlineStats(t *testing.T) {
	ctx := context.Background()
	tracker := newPresenceTracker(local.NewPresenceStore())

	for _, u := range []string{"alice", "bob", "carol"} {
		if err := tracker.SetPresence(ctx, u, streaming.StatusOnline); err != nil {
			t.Fatalf("SetPresence: %v", err)
		}
	}

	stats, err := tracker.GetOnlineStats(ctx)
	if err != nil {
		t.Fatalf("GetOnlineStats: %v", err)
	}

	if stats.Current != 3 {
		t.Errorf("Current = %d, want 3", stats.Current)
	}

	// Peak and average are derived from the current count rather than tracked
	// over time; pinned so a real implementation has to update this test.
	if stats.Peak24h != stats.Current {
		t.Errorf("Peak24h = %d, want it to mirror Current (%d) today", stats.Peak24h, stats.Current)
	}
}

func TestPresenceTracker_GetOnlineStatsPropagatesErrors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name  string
		store *failingPresenceStore
	}{
		{name: "get-online failure", store: &failingPresenceStore{getOnlineErr: errStore}},
		{name: "count-by-status failure", store: &failingPresenceStore{countErr: errStore}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := newPresenceTracker(tt.store)

			if _, err := tracker.GetOnlineStats(ctx); !errors.Is(err, errStore) {
				t.Errorf("GetOnlineStats = %v, want it to wrap errStore", err)
			}
		})
	}
}

// --- Lifecycle -------------------------------------------------------------

func TestPresenceTracker_CleanupLoopRunsAndStops(t *testing.T) {
	ctx := context.Background()

	store := &failingPresenceStore{}

	tracker := NewPresenceTracker(store, streaming.PresenceOptions{
		OfflineTimeout:  time.Minute,
		CleanupInterval: 10 * time.Millisecond,
	}, nil, nil)

	if err := tracker.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && store.cleanupCount() == 0 {
		time.Sleep(time.Millisecond)
	}

	if store.cleanupCount() == 0 {
		t.Fatal("cleanup loop never ran")
	}

	if err := tracker.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	settled := store.cleanupCount()
	time.Sleep(60 * time.Millisecond)

	if got := store.cleanupCount(); got > settled+1 {
		t.Errorf("cleanup ran %d more times after Stop, want it halted", got-settled)
	}
}

func TestPresenceTracker_StopIsIdempotent(t *testing.T) {
	tracker := newPresenceTracker(local.NewPresenceStore())

	ctx := context.Background()

	if err := tracker.Stop(ctx); err != nil {
		t.Fatalf("first Stop: %v", err)
	}

	if err := tracker.Stop(ctx); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

func TestPresenceTracker_CleanupExpiredPropagatesErrors(t *testing.T) {
	tracker := newPresenceTracker(&failingPresenceStore{cleanupErr: errStore})

	if err := tracker.CleanupExpired(context.Background()); !errors.Is(err, errStore) {
		t.Errorf("CleanupExpired = %v, want it to wrap errStore", err)
	}
}

// --- Concurrency -----------------------------------------------------------

func TestPresenceTracker_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	tracker := newPresenceTracker(local.NewPresenceStore())

	statuses := []string{
		streaming.StatusOnline,
		streaming.StatusAway,
		streaming.StatusBusy,
		streaming.StatusOffline,
	}

	var wg sync.WaitGroup

	for w := range 8 {
		wg.Add(1)

		go func(w int) {
			defer wg.Done()

			userID := fmt.Sprintf("u%d", w)

			for i := range 60 {
				_ = tracker.SetPresence(ctx, userID, statuses[i%len(statuses)])
				_ = tracker.TrackActivity(ctx, userID)
				_, _ = tracker.GetPresence(ctx, userID)
				_, _ = tracker.GetOnlineUsers(ctx)
				_, _ = tracker.IsOnline(ctx, userID)
				_ = tracker.WatchUser(ctx, userID, "shared-target")
				_, _ = tracker.GetWatchers(ctx, "shared-target")
				_, _ = tracker.GetWatching(ctx, userID)
				_ = tracker.UnwatchUser(ctx, userID, "shared-target")
			}
		}(w)
	}

	wg.Wait()
}
