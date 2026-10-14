# Consensus quick start

This page gets you from nothing to a running Raft cluster with this module, and tells you plainly where the module stops today. Read the [status table in the README](README.md#status) alongside it.

The short version: the `raft` package elects a leader and replicates a log across nodes that you assemble yourself, on the in-process transport. The Forge extension (`consensus.NewExtension`) loads and validates config, but it never builds the storage, transport, state machine or Raft node, so it can't run a cluster yet. There's no networked cluster, admin API or Prometheus endpoint to deploy, which is why this page has no Docker or Kubernetes section.

## Install

You need Go 1.26 or later.

```bash
go get github.com/xraph/forge@latest
go get github.com/xraph/forge/extensions/consensus@latest
```

## Run a three-node cluster in one process

Each node is a `raft.Node` built from a transport, log storage and a state machine. The in-process transport (`transport.NewLocalTransport`) is the one the module's own cluster tests use, and the one that works end to end. Save this as `main.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus"
	"github.com/xraph/forge/extensions/consensus/raft"
	"github.com/xraph/forge/extensions/consensus/statemachine"
	"github.com/xraph/forge/extensions/consensus/storage"
	"github.com/xraph/forge/extensions/consensus/transport"
)

func main() {
	ctx := context.Background()
	logger := forge.NewNoopLogger()
	ids := []string{"node-1", "node-2", "node-3"}

	transports := map[string]*transport.LocalTransport{}
	nodes := map[string]*raft.Node{}
	machines := map[string]*statemachine.MemoryStateMachine{}

	for _, id := range ids {
		tr := transport.NewLocalTransport(transport.LocalTransportConfig{NodeID: id}, logger)
		st := storage.NewMemoryStorage(storage.MemoryStorageConfig{}, logger)
		sm := statemachine.NewMemoryStateMachine(statemachine.MemoryStateMachineConfig{}, logger)

		node, err := raft.NewNode(raft.Config{
			NodeID:             id,
			HeartbeatInterval:  100 * time.Millisecond,
			ElectionTimeoutMin: 300 * time.Millisecond,
			ElectionTimeoutMax: 600 * time.Millisecond,
		}, logger, sm, tr, st)
		if err != nil {
			log.Fatal(err)
		}

		if err := st.Start(ctx); err != nil {
			log.Fatal(err)
		}

		transports[id], nodes[id], machines[id] = tr, node, sm
	}

	// Wire every node to every other node, then start them.
	for _, id := range ids {
		for _, peer := range ids {
			if peer != id {
				transports[id].Connect(transports[peer])
				nodes[id].AddPeer(peer)
			}
		}
	}

	for _, id := range ids {
		if err := transports[id].Start(ctx); err != nil {
			log.Fatal(err)
		}

		if err := nodes[id].Start(ctx); err != nil {
			log.Fatal(err)
		}
	}

	leader := ""
	for leader == "" {
		time.Sleep(100 * time.Millisecond)

		for id, node := range nodes {
			if node.IsLeader() {
				leader = id
			}
		}
	}

	fmt.Println("leader:", leader)

	cmd, err := json.Marshal(consensus.Command{
		Type:    "set",
		Payload: map[string]any{"key": "greeting", "value": "hello"},
	})
	if err != nil {
		log.Fatal(err)
	}

	// Propose returns once a majority has the entry.
	if err := nodes[leader].Propose(ctx, cmd); err != nil {
		log.Fatal(err)
	}

	value, err := machines[leader].Query(map[string]any{"type": "get", "key": "greeting"})
	fmt.Println("greeting:", value, err)

	time.Sleep(300 * time.Millisecond)

	for _, id := range ids {
		fmt.Println(id, "commit index:", nodes[id].GetCommitIndex())
	}
}
```

Run it with `go mod tidy && go run .`. You'll see something like:

```text
leader: node-2
greeting: hello <nil>
node-1 commit index: 2
node-2 commit index: 2
node-3 commit index: 2
```

Entry 1 is the no-op every new leader writes, and entry 2 is your `set`. A few things to know before you build on it:

- Write through the leader. `Propose` on a follower returns a not-leader error that names the current leader, and `consensus.IsNotLeaderError(err)` recognises it.
- Read from the leader too. Followers learn the commit index on the next heartbeat but don't apply committed entries to their own state machine yet, so the same `Query` on a follower returns `key not found`.
- The memory state machine understands `set`, `delete` and `increment` commands and `{"type": "get", "key": ...}` queries. For your own data, implement `consensus.StateMachine` (`Apply`, `Snapshot`, `Restore` and `Query`) and pass it to `raft.NewNode` in place of `sm`.
- `storage.NewBadgerStorage` and `storage.NewBoltDBStorage` give you on-disk logs, and `transport` has gRPC, TCP and HTTP transports. They compile, but nothing in the repository runs a networked cluster on them yet, so treat them as untested.

## Add the Forge extension

You can add the extension to an app, and it will load and validate your config:

```go
app := forge.NewApp(forge.AppConfig{
	Name: "orders",
	Extensions: []forge.Extension{
		consensus.NewExtension(
			consensus.WithNodeID("node-1"),
			consensus.WithClusterID("orders"),
			consensus.WithBindAddress("0.0.0.0", 7000),
			consensus.WithPeers([]consensus.PeerConfig{
				{ID: "node-2", Address: "10.0.0.2", Port: 7000},
				{ID: "node-3", Address: "10.0.0.3", Port: 7000},
			}),
		),
	},
})
```

Here's what that gives you today. The container holds `*consensus.Service` (also named `consensus`), but its Raft node, state machine, storage and transport are all nil. Nothing listens on port 7000 and the node stays a follower, so:

| Call | What happens |
| --- | --- |
| `Apply` | Always returns `ErrNotLeader` |
| `Read` | Panics on the nil state machine |
| `HealthCheck` | Returns `storage not initialized`, and the service logs it on every health check |
| `AddNode`, `RemoveNode` | Return a not-leader error |
| `TransferLeadership`, `StepDown` | Return "not yet implemented" |

Turn off automatic snapshots if you add the extension anyway. They're on by default, and every `raft.snapshot_interval` (30 minutes) the snapshot call panics on the nil state machine and takes your process down:

```yaml
extensions:
  consensus:
    node_id: node-1
    advanced:
      enable_auto_snapshot: false
```

`admin_api.enabled` only writes a log line, so there are no `/consensus/...` routes. The `middleware` package's leadership helpers take a `ConsensusService`, which the extension can't back with a real node yet.

The full list of config keys, defaults and validation rules is in the [README's configuration section](README.md#configuration).

## Next steps

- [README](README.md) for the package map, the extension and the FAQ
- [`examples/basic`](examples/basic/) and the cluster harness in [`testing/`](testing/)
- `GOWORK=off go test -count=1 ./testing/...` from this directory runs the in-process cluster tests, including `TestLogReplication`, which CI skips under `-short`
