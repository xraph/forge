# Running streaming on several nodes

This guide covers running the streaming extension on more than one node: what the Redis backend shares between nodes, what stays local to each node, how to put a load balancer in front, and what isn't there yet. Read the [README](README.md) first for the single-node setup.

The short version: point every node at the same Redis server with the Redis backend, give each node a stable node ID, and put your own load balancer in front. Rooms, channels, history, presence and typing state live in Redis, and a broadcast on one node reaches clients on the others. Rate limits, connection limits and resumable WebSocket sessions are still per node.

## What you need

- Go 1.26 or later.
- One Redis server that every node can reach. The extension talks to a single Redis server (see [Limits](#limits)), not Redis Cluster or Sentinel.
- A load balancer that supports WebSocket upgrades and long-lived SSE responses (NGINX, HAProxy, Envoy or a cloud load balancer).

NATS isn't an option yet. `WithNATSBackend` and `backend: nats` make the extension fail at registration with "failed to create stores", because the NATS stores aren't implemented.

## Configure each node

`WithRedisBackend` turns on the Redis backend and distributed mode together. Give every node its own `WithNodeID`. If you leave it out, the node generates a random ID on every start.

```go
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/streaming"
)

func main() {
	stream := streaming.NewExtension(
		streaming.WithRedisBackend(os.Getenv("REDIS_ADDR")), // host:port, for example redis:6379
		streaming.WithAuthentication("", os.Getenv("REDIS_PASSWORD")),
		streaming.WithNodeID(os.Getenv("NODE_ID")),
		streaming.WithSessionResumption(10*time.Minute),
	)

	app := forge.NewApp(forge.AppConfig{
		Name:        "live",
		HTTPAddress: ":8080",
		Extensions:  []forge.Extension{stream},
	})

	err := forge.OnStarted(app, "streaming-routes", func(_ context.Context, a forge.App) error {
		return stream.RegisterRoutes(a.Router(), "/ws", "/events")
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
```

Pass the Redis address as `host:port`. The value goes straight to the go-redis client as its address, so a URL such as `redis://redis:6379` doesn't parse as an address and every Redis call fails. Only the first entry in the backend URL list is used.

The same settings in YAML, under `extensions.streaming`:

```yaml
extensions:
  streaming:
    backend: redis
    backend_urls: ["redis:6379"]
    backend_password: ""
    enable_distributed: true
    node_id: api-1
    enable_session_resumption: true
    session_resumption_ttl: 10m
```

Validation fails at startup if `enable_distributed` is on with the local backend, or if the Redis backend has no `backend_urls`.

### TLS to Redis

`WithTLS(cert, key, ca)` turns on TLS for the Redis connection with TLS 1.2 or newer. Validation requires a client certificate and key whenever TLS is on, and the CA file is accepted but never loaded, so the server certificate is checked against the host's trust store.

## What's shared and what's per node

| State | Where it lives | What that means for you |
| --- | --- | --- |
| Rooms, members, channels | Redis | A room created on one node is visible on all of them |
| Message history and sequence numbers | Redis | History queries and SSE replay work on any node (see [Sequences and reconnects](#sequences-and-reconnects)) |
| Presence and typing | Redis | `GetPresence` and `GetTypingUsers` agree across nodes |
| Broadcasts | Relayed over Redis Pub/Sub by the coordinator | `Broadcast`, `BroadcastToRoom` and `SendToUser` reach clients connected to other nodes |
| Open connections | In memory, per node | `GetAllConnections` and `ConnectionCount` only see this node |
| Rate limits | In memory, per node | A user gets `max_messages_per_second` on each node they're connected to |
| Connection limits | Per node | `max_connections_per_user`, `max_anonymous_connections` and `max_total_connections` count local connections only |
| Resumable WebSocket sessions | In memory, per node | A client has to reconnect to the same node to get its rooms and channels back |

If the coordinator's Redis client can't be created, the node logs "distributed messaging disabled" and keeps running without cross-node broadcasts. Watch for that line when you roll out.

## Put a load balancer in front

Nothing in the extension routes clients to nodes, so your load balancer decides where each connection lands.

- WebSocket: the upgrade needs `Upgrade` and `Connection` headers passed through, and idle timeouts longer than the ping interval (30 seconds by default). If you use session resumption, make sessions sticky (by cookie or by a hash of the `session_id` query parameter), because resumable sessions are kept in the memory of the node that issued them.
- SSE: turn off response buffering for the SSE path. Sticky sessions aren't needed: a browser that reconnects to another node with `Last-Event-ID` gets the room messages it missed, because history and sequences live in Redis.
- The `POST {ssePath}/subscribe` and `/unsubscribe` calls change the subscriptions of a connection by its `conn_id`, and a connection only exists on the node that holds it. Route those calls to the same node as the SSE stream, or the node that gets them answers 404.

`WithLoadBalancer(strategy)` doesn't replace any of this. With distributed mode on, it keeps an in-process registry of the nodes it hears about over the coordinator, plus a health checker for them, but nothing in the extension uses that registry to pick a node for a client.

## Sequences and reconnects

Each room message carries a `Sequence` assigned by the message store, and a reconnecting SSE client presents a cursor (`Last-Event-ID`) to get exactly what it missed. Across nodes this only works if a sequence means the same thing everywhere, and two things make sure it does:

1. The Redis message store assigns sequences with `INCR`, which is atomic on the server. The local store's counter is per process, so two nodes on the local backend would hand out the same numbers to different messages. That's one more reason the local backend is for a single node only.
2. A message that already carries a sequence keeps it. The store only assigns one when `Sequence` is zero, so a message never ends up with different numbers on different nodes.

The shared contract suite in `backends/storetest` runs against both the local and the Redis message stores and checks this behaviour.

When a node can't honour a cursor, the stream sends a `resync_required` event and your client reloads.

## Monitoring

Every node reports to its own Forge metrics, so aggregate across nodes in your metrics backend. Useful ones, all under `streaming.`:

- `connections.active`, `connections.total`, `connections.rejected_at_capacity`, `connections.anon_rejected`
- `messages.broadcast`, `messages.room_broadcast`, `messages.rate_limited`, `messages.oversize`, `messages.send_denied`, `messages.validation_failed`
- `rooms.joins`, `rooms.joins_denied`
- `sendqueue.depth`, `sendqueue.dropped`
- `sessions.resumed`, `sessions.resume_denied`, `replay.messages`, `replay.sessions`

There are no coordinator metrics. `Manager().GetStats(ctx)` returns a snapshot for one node, and the extension's health check fails when that node's manager is unhealthy, so wire it into your load balancer's health check.

## Limits

These are the gaps you're most likely to hit:

- One Redis server. The client is a plain go-redis client on the first address in the list. Redis Cluster and Sentinel aren't supported.
- No NATS backend. `coordinator.NewNATSCoordinator` exists in the package but the extension never uses it.
- Rate limits, connection limits and resumable sessions are per node, as the table above shows.
- The presence and room state synchronizers in the `coordinator` package aren't wired in. Shared state comes from the Redis stores instead.

Identity works the same on every node: the built-in handlers take the caller from the `user_id` value your middleware sets. [The README FAQ](README.md#how-do-i-authenticate-websocket-connections) shows how.

## Troubleshooting

Broadcasts don't reach clients on other nodes. Check every node's startup log for "distributed messaging disabled", make sure every node uses the same Redis server, and make sure each node has a different `node_id`.

Every Redis call fails right after startup. Check that the backend URL is `host:port` and not `redis://host:port`.

A reconnecting WebSocket client loses its rooms. The client landed on a different node from the one that holds its session. Make the load balancer sticky, or have the client rejoin its rooms after it reconnects.

SSE subscribe calls return 404. The call reached a node that doesn't hold that `conn_id`. Route it to the node that holds the stream.
