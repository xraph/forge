package consensus

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/admin"
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

	leader := waitForServiceLeader(t, core)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := leader.Apply(ctx, Command{Type: "set", Payload: map[string]any{"key": "a", "value": "1"}}); err != nil {
		t.Fatal(err)
	}

	if joiner.IsLeader() || joiner.GetTerm() != 0 {
		t.Fatal("the joining server campaigned before it was added")
	}

	router := forge.NewRouter()
	admin.NewAPI(leader, nil, nil, forge.NewNoopLogger()).RegisterRoutes(router, "/consensus")

	if code := postAdmin(t, router, "/consensus/nodes/add", map[string]any{
		"node_id": "n4", "address": "127.0.0.1", "port": cfgs[3].BindPort,
	}); code != http.StatusOK {
		t.Fatalf("admin add node: status %d", code)
	}

	readEventually(t, joiner, "a", "1")

	if size := leader.GetClusterManager().GetClusterSize(); size != 4 {
		t.Fatalf("cluster size %d after adding n4, want 4", size)
	}

	if err := leader.TransferLeadership(ctx, "n4"); err != nil {
		t.Fatalf("transfer to n4: %v", err)
	}

	if got := waitForServiceLeader(t, services); got != joiner {
		t.Fatalf("leader is %s, want n4", got.config.NodeID)
	}

	if err := joiner.GetClusterManager().RemoveNode("n2"); err != nil {
		t.Fatalf("remove n2: %v", err)
	}

	if err := joiner.Apply(ctx, Command{Type: "set", Payload: map[string]any{"key": "b", "value": "2"}}); err != nil {
		t.Fatal(err)
	}

	for _, s := range []*Service{services[0], services[2], joiner} {
		readEventually(t, s, "b", "2")
	}

	info := joiner.GetClusterInfo()
	if info.TotalNodes != 3 || info.Leader != "n4" {
		t.Fatalf("cluster info after removing n2: %+v", info)
	}

	for _, n := range info.Nodes {
		if n.ID == "n2" {
			t.Fatal("n2 is still listed as a member")
		}
	}
}
