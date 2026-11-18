package consensus

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/admin"
	"github.com/xraph/forge/extensions/consensus/raft"
)

func readEventually(t *testing.T, s *Service, key, want string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)

	for {
		got, err := s.Read(t.Context(), map[string]any{"type": "get", "key": key})
		if err == nil && got == want {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("%s read %s = %v, %v; want %s", s.config.NodeID, key, got, err, want)
		}

		time.Sleep(20 * time.Millisecond)
	}
}

// onLeader runs op on the current leader, retrying on errors a client would
// retry after a leadership change: the leader elected a moment ago can lose
// its term before op completes.
func onLeader(t *testing.T, services []*Service, op func(*Service) error) {
	t.Helper()

	var err error

	for range 5 {
		leader := waitForServiceLeader(t, services)
		if err = op(leader); err == nil {
			return
		}

		if !IsNotLeaderError(err) && !errors.Is(err, raft.ErrProposalDropped) &&
			!errors.Is(err, raft.ErrConfigChangeInProgress) && !errors.Is(err, errAdminRetry) {
			break
		}
	}

	t.Fatalf("operation on the leader failed: %v", err)
}

// errAdminRetry marks a failed admin call so onLeader retries it.
var errAdminRetry = errors.New("admin call failed")

func postAdmin(t *testing.T, router forge.Router, path string, body any) int {
	t.Helper()

	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	return rec.Code
}

// A cluster built from configuration grows by a joining server added through
// the admin API, hands leadership to it, and shrinks again through the
// ClusterManager, replicating writes throughout.
func TestServiceMembershipAndTransfer(t *testing.T) {
	cfgs := clusterConfigs(t, "tcp", 4)

	// n1..n3 bootstrap the cluster; n4 knows where they are and waits to be
	// added.
	for i := range 3 {
		cfgs[i].Peers = cfgs[i].Peers[:3]
	}

	cfgs[3].Join = true
	services := startServices(t, cfgs)
	core, joiner := services[:3], services[3]

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	onLeader(t, core, func(s *Service) error {
		return s.Apply(ctx, Command{Type: "set", Payload: map[string]any{"key": "a", "value": "1"}})
	})

	if joiner.IsLeader() || joiner.GetTerm() != 0 {
		t.Fatal("the joining server campaigned before it was added")
	}

	onLeader(t, core, func(s *Service) error {
		router := forge.NewRouter()
		admin.NewAPI(s, nil, nil, forge.NewNoopLogger()).RegisterRoutes(router, "/consensus")

		if code := postAdmin(t, router, "/consensus/nodes/add", map[string]any{
			"node_id": "n4", "address": "127.0.0.1", "port": cfgs[3].BindPort,
		}); code != http.StatusOK {
			return fmt.Errorf("%w: admin add node: status %d", errAdminRetry, code)
		}

		return nil
	})

	readEventually(t, joiner, "a", "1")

	if size := waitForServiceLeader(t, core).GetClusterManager().GetClusterSize(); size != 4 {
		t.Fatalf("cluster size %d after adding n4, want 4", size)
	}

	onLeader(t, services, func(s *Service) error {
		if s == joiner {
			return nil
		}

		return s.TransferLeadership(ctx, "n4")
	})

	if got := waitForServiceLeader(t, services); got != joiner {
		t.Fatalf("leader is %s, want n4", got.config.NodeID)
	}

	onLeader(t, services, func(s *Service) error { return s.GetClusterManager().RemoveNode("n2") })

	remaining := []*Service{services[0], services[2], joiner}
	onLeader(t, remaining, func(s *Service) error {
		return s.Apply(ctx, Command{Type: "set", Payload: map[string]any{"key": "b", "value": "2"}})
	})

	for _, s := range remaining {
		readEventually(t, s, "b", "2")
	}

	current := waitForServiceLeader(t, remaining)

	info := current.GetClusterInfo()
	if info.TotalNodes != 3 || info.Leader != current.config.NodeID {
		t.Fatalf("cluster info after removing n2: %+v", info)
	}

	for _, n := range info.Nodes {
		if n.ID == "n2" {
			t.Fatal("n2 is still listed as a member")
		}
	}
}
