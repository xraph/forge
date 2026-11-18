# Consensus quick start

This page gets you from nothing to a running Raft cluster with this module, and tells you plainly where the module stops today. Read the [status table in the README](README.md#status) alongside it.

The short version: you can assemble nodes yourself from the `raft`, `storage` and `transport` packages, or let the Forge extension (`consensus.NewExtension`) build one node per app from config. Either way you get leader election, a replicated log, snapshots, membership changes and leadership transfer, over the in-process, TCP, HTTP or gRPC transport. The module hasn't run a production cluster yet, and there's no Prometheus endpoint or deployment recipe, which is why this page has no Docker or Kubernetes section.

## Install

You need Go 1.26 or later.

```bash
go get github.com/xraph/forge@latest
go get github.com/xraph/forge/extensions/consensus@latest
```

## Run a three-node cluster in one process

Each node is a `raft.Node` built from a transport, log storage and a state machine. This example uses the in-process transport (`transport.NewLocalTransport`) so it runs in one process with nothing to configure. Save this as `main.go`:

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

- Write through the leader. `Propose` on a follower returns a not-leader error that names the current leader, and `consensus.IsNotLeaderError(err)` recognises it. If the leader loses its term before your entry commits, `Propose` returns `raft.ErrProposalDropped` and you can retry on the new leader.
- Followers apply committed entries too, a heartbeat or so after the leader. A read from a follower's state machine can lag the leader; reads aren't linearizable.
- The memory state machine understands `set`, `delete` and `increment` commands and `{"type": "get", "key": ...}` queries. For your own data, implement `consensus.StateMachine` (`Apply`, `Snapshot`, `Restore` and `Query`) and pass it to `raft.NewNode` in place of `sm`.
- For a real deployment, swap in `storage.NewBadgerStorage` or `storage.NewBoltDBStorage` for an on-disk log, and `transport.NewTCPTransport`, `NewHTTPTransport` or `NewGRPCTransport` for the network. Three node clusters on all three network transports are covered by the module's tests. Give each node the others' addresses with `AddPeerAddress` instead of `AddPeer`, so servers you add later can reach them.

## Add the Forge extension

You can add the extension to an app, and it will load and validate your config:

```go
app := forge.NewApp(forge.AppConfig{
	Name: "orders",
	Extensions: []forge.Extension{
		consensus.NewExtension(
			consensus.WithNodeID("node-1"),
			consensus.WithClusterID("orders"),
			consensus.WithBindAddress("10.0.0.1", 7000),
			consensus.WithPeers([]consensus.PeerConfig{
				{ID: "node-2", Address: "10.0.0.2", Port: 7000},
				{ID: "node-3", Address: "10.0.0.3", Port: 7000},
			}),
		),
	},
})
```

`Start` builds the node from that config: the storage under `storage.path` (`badger` by default, or `boltdb` or `memory`), the transport on `bind_addr:bind_port` (`tcp` by default, or `http` or `grpc`), a memory state machine, and a Raft node whose first members are the `peers` list. Start the same app on every server with its own `node_id` and bind address, and they elect a leader between them. Mind you, `bind_addr` defaults to `127.0.0.1`, so set it to an address the other servers can reach.

The container holds `*consensus.Service` (also named `consensus`). Take it from the extension and call it like this:

| Call | What happens |
| --- | --- |
| `Apply` | On the leader, appends the command and returns once it is committed and applied. Elsewhere it returns a not-leader error |
| `Read` | Queries this node's state machine, which may lag the leader |
| `Snapshot` | Snapshots the state machine and compacts the log |
| `AddNode`, `RemoveNode` | Change membership one server at a time, on the leader |
| `TransferLeadership`, `StepDown` | Hand leadership to a chosen voter, or step down and let an election decide |

To grow the cluster, start the new server with `join: true` (or `consensus.WithJoin(true)`) and the existing members in its `peers`, so it waits to be added instead of electing itself. Then call `AddNode` on the leader with the new server's ID and address. It joins as a learner, catches up, and becomes a voter. After that first change the log holds the membership, so editing `peers` later does nothing.

Turn on `security.enable_tls` or `enable_mtls` to protect traffic between nodes, and `security.enable_encryption` to encrypt what each node writes to disk. The README covers [encryption at rest](README.md#encryption-at-rest) in detail.

The admin routes are off by default. With `admin_api.enabled: true` they're registered on your app's router under `/consensus`, and they can write to the replicated log, so set `admin_api.enable_auth: true` and an `api_key` whenever anyone else can reach that router.

The full list of config keys, defaults and validation rules is in the [README's configuration section](README.md#configuration).

## Next steps

- [README](README.md) for the package map, the extension and the FAQ
- [`examples/basic`](examples/basic/) and the cluster harness in [`testing/`](testing/)
- `GOWORK=off go test -count=1 ./testing/...` from this directory runs the in-process cluster tests, including `TestLogReplication`, which CI skips under `-short`
