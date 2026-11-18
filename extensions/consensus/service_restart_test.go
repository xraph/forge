package consensus

import (
	"context"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/consensus/internal"
)

func diskClusterConfigs(t *testing.T, storageType string, size int) []internal.Config {
	t.Helper()

	cfgs := clusterConfigs(t, "tcp", size)
	for i := range cfgs {
		cfgs[i].Storage.Type = storageType
		cfgs[i].Storage.Path = t.TempDir()
	}

	return cfgs
}

func restartAll(t *testing.T, cfgs []internal.Config) []*Service {
	t.Helper()

	services := make([]*Service, len(cfgs))

	for i, cfg := range cfgs {
		s, err := startService(t, cfg)
		if err != nil {
			t.Fatalf("restart %s: %v", cfg.NodeID, err)
		}

		services[i] = s
	}

	return services
}

func sortedVoters(s *Service) []string {
	v := s.raft().Configuration().Voters()
	sort.Strings(v)

	return v
}

// A plain (unencrypted) cluster on disk restarts with its data and its
// membership, whether membership is still the bootstrap peer list or came
// from a change in the log, and whether the log was compacted.
func TestServiceRestartKeepsDataAndMembership(t *testing.T) {
	for _, storageType := range []string{"boltdb", "badger"} {
		t.Run(storageType, func(t *testing.T) {
			cfgs := diskClusterConfigs(t, storageType, 4)
			for i := range 3 {
				cfgs[i].Peers = cfgs[i].Peers[:3]
			}

			cfgs[3].Join = true

			services := startServices(t, cfgs)
			applySet(t, services[:3], "before", "growth")

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			onLeader(t, services[:3], func(s *Service) error {
				return s.AddNode(ctx, "n4", "127.0.0.1", cfgs[3].BindPort)
			})

			applySet(t, services, "after", "growth")
			waitForValue(t, services, "after", "growth")

			onLeader(t, services, func(s *Service) error { return s.Snapshot(ctx) })

			applySet(t, services, "after", "snapshot")
			waitForValue(t, services, "after", "snapshot")
			stopServices(t, services)

			restarted := restartAll(t, cfgs)

			waitForValue(t, restarted, "before", "growth")
			waitForValue(t, restarted, "after", "snapshot")

			want := []string{"n1", "n2", "n3", "n4"}
			for _, s := range restarted {
				if got := sortedVoters(s); !reflect.DeepEqual(got, want) {
					t.Fatalf("%s voters after restart %v, want %v", s.config.NodeID, got, want)
				}
			}

			applySet(t, restarted, "post", "restart")
			waitForValue(t, restarted, "post", "restart")
		})
	}
}
