<p align="center">
  <img src="assets/readme-banner.svg" alt="Forge Conduit, typed events, durable streams, broker RPC and service clients for Go on NATS JetStream, Redis Streams, PostgreSQL and Kafka" width="100%">
</p>

# Forge Conduit: typed events, durable streams and service clients for Go

Conduit connects [Forge](https://github.com/xraph/forge) services through typed events, durable streams, broker RPC and named HTTP or gRPC clients. You choose the broker (NATS JetStream, Redis Streams, PostgreSQL, Kafka or in-memory) and your handlers keep the same Go API. It covers at-least-once delivery with retries and dead letters, competing and broadcast consumers, a transactional outbox and inbox on PostgreSQL, service discovery, and operator controls for pause, replay and backfill.

[![Go 1.26+](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/dl/) [![Go CI](https://github.com/xraph/forge/actions/workflows/go.yml/badge.svg)](https://github.com/xraph/forge/actions/workflows/go.yml) [![Go Reference](https://pkg.go.dev/badge/github.com/xraph/forge/extensions/conduit.svg)](https://pkg.go.dev/github.com/xraph/forge/extensions/conduit) [![Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](../../LICENSE)

[Documentation](https://www.xraph.com/docs/forge/extensions/conduit/configuration) · [Quick start](#quick-start) · [Features](#features) · [Events](#publish-and-handle) · [Service clients](#call-a-service-by-name) · [RPC](#typed-broker-rpc) · [Outbox and inbox](#commit-business-data-and-an-event-together) · [Providers](#providers) · [Configuration](#configuration) · [FAQ](#faq)

## Quick start

You need [Go 1.26 or later](https://go.dev/dl/). This example uses the in-memory provider, so you don't need a broker to try it.

```bash
go get github.com/xraph/forge@latest
go get github.com/xraph/forge/extensions/conduit@latest
```

```go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/conduit"
	"github.com/xraph/forge/extensions/conduit/providers/memory"
)

type OrderPlaced struct {
	OrderID string `json:"orderID"`
}

var OrderPlacedV1 = conduit.Event[OrderPlaced]("orders.placed.v1")

func main() {
	ext, err := conduit.NewExtension(
		conduit.WithProvider("events", memory.New()),
		conduit.WithConfig(conduit.Config{
			Streams: map[string]conduit.StreamConfig{
				"orders": {Provider: "events", Subjects: []string{"orders.>"}},
			},
			Subscriptions: map[string]conduit.SubscriptionConfig{
				"process-orders": {Stream: "orders", Mode: conduit.Competing},
			},
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Bind handlers before the app starts.
	err = conduit.Subscribe(ext.Runtime(), OrderPlacedV1,
		func(ctx context.Context, msg conduit.Message[OrderPlaced]) error {
			log.Printf("charging order %s (message %s)", msg.Data.OrderID, msg.Envelope.ID)

			return nil
		}, conduit.Consumer("process-orders"))
	if err != nil {
		log.Fatal(err)
	}

	app := forge.NewApp(forge.AppConfig{
		Name:        "orders",
		HTTPAddress: ":8080",
		Extensions:  []forge.Extension{ext},
	})

	err = app.Router().POST("/orders/:id", func(ctx forge.Context) error {
		id := ctx.Param("id")

		receipt, err := conduit.Publish(ctx.Context(), ext.Runtime(), OrderPlacedV1,
			OrderPlaced{OrderID: id}, conduit.MessageID("order-"+id+"-placed"))
		if err != nil {
			return err
		}

		return ctx.JSON(http.StatusAccepted, receipt)
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
```

Run it and place an order:

```bash
go mod tidy
go run .

# in another terminal
curl -X POST localhost:8080/orders/42
# {"messageID":"order-42-placed","sequence":1,"persisted":false,"duplicate":false}
```

The app logs `charging order 42 (message order-42-placed)`. `persisted` is false because memory keeps nothing on disk. For real retention, swap the provider for JetStream and make the subscription durable:

```go
ext, err := conduit.NewExtension(
	conduit.WithProvider("events", jetstream.New(jetstream.Options{URL: "nats://localhost:4222"})),
	conduit.WithConfig(conduit.Config{
		Streams: map[string]conduit.StreamConfig{
			"orders": {Provider: "events", Subjects: []string{"orders.>"}, MaxAge: 7 * 24 * time.Hour, Replicas: 3},
		},
		Subscriptions: map[string]conduit.SubscriptionConfig{
			"process-orders": {Stream: "orders", Mode: conduit.Competing, Durable: true, Concurrency: 4, MaxInFlight: 16},
			"refresh-cache":  {Stream: "orders", Mode: conduit.Broadcast},
		},
	}),
)
```

The memory provider refuses `Durable: true`, so the app fails at registration if you ask for a guarantee the broker can't give.

## Features

| You need | Conduit gives you |
| --- | --- |
| Events with a schema in your Go types | `conduit.Event[T]`, `Publish` and `Subscribe` with typed handlers |
| Work shared across replicas, or fan-out to each one | Competing and broadcast subscriptions, durable or ephemeral |
| At-least-once delivery you can reason about | Receipts that say whether the broker persisted the message, retries, `Permanent` errors and dead letters |
| Business data and events that commit together | A PostgreSQL outbox and inbox in `transaction`, plus `PublishTx` on the PostgreSQL provider |
| Request and reply over the broker | `conduit.Procedure`, `Handle` and `Call`, with typed errors |
| HTTP and gRPC calls by service name | `conduit.Clients(...).HTTP` and `.GRPC`, resolved through discovery |
| Policy, tracing and audit in one place | Publish, handle and RPC hooks, handler middleware and observers |
| Operator controls | Consumer lag, pause and resume, bounded backfill, dead-letter replay, and a dashboard plugin |
| A choice of broker | NATS JetStream, Redis Streams, PostgreSQL, Kafka and memory, behind one provider interface |

## Identity and topology

Every process you run has two identities: `ServiceID` names the logical service, while `InstanceID` names its running replica. Consumer identity includes the namespace, service and subscription. Competing consumers share that identity across replicas, and broadcast consumers add a subscriber identity so each instance has its own cursor.

Streams belong to a namespace, and wherever you declare one, its retention settings must match. Your services can use different subscriptions, delivery modes, retry policies and worker counts against the same stream. A competing group's `MaxInFlight`, timeout and retry settings must agree across its replicas, and `Concurrency` controls each replica's workers.

Conduit generates an instance ID when you omit it. Supply your pod or process identity when you need to correlate it with deployment records, and advertise an address that clients can reach: a listener's wildcard bind address is not an endpoint.

## Publish and handle

```go
receipt, err := conduit.Publish(ctx, ext.Runtime(), OrderPlacedV1,
	OrderPlaced{OrderID: "order-42"},
	conduit.MessageID("order-42-placed"),
	conduit.Correlation("checkout-42"),
)
```

The receipt confirms broker acceptance. `Persisted` tells you whether the provider stored the message durably. It does not mean a handler has finished. If publication returns `conduit.ErrOutcomeUnknown`, retry with the same message ID: the broker may have accepted the first attempt even though its acknowledgement didn't reach you.

Delivery is at least once, so you should write idempotent handlers. JetStream deduplication has a finite window, and a handler can commit its work before losing an acknowledgement. `conduit.Permanent(err)` ends processing retries immediately. Other failures retry with a linearly increasing delay, then enter the provider's dead letter store. A storage failure leaves the delivery eligible for retry instead of dropping it.

Ephemeral broadcast starts with new events by default. Durable broadcast needs a stable `BroadcastID`, unique to each intended subscriber, so a replacement process can resume that subscriber's cursor. An auto-generated instance ID can't identify a durable broadcast subscriber after a restart.

## Hooks and middleware

Use `conduit.WithHooks` at construction or `Runtime.RegisterHook` before startup. A hook needs only a name and the interfaces it uses, and `HookFuncs` covers the common cases:

```go
conduit.WithHooks(conduit.HookFuncs{
	HookName: "tenant-and-audit",
	OnPublish: func(ctx context.Context, draft *conduit.Envelope) error {
		if draft.Headers == nil {
			draft.Headers = map[string]string{}
		}
		draft.Headers["tenant"] = tenantFrom(ctx)

		return nil
	},
	OnHandle: func(ctx context.Context, msg conduit.Envelope, delivery conduit.DeliveryInfo) error {
		return authorizeEvent(ctx, msg.Source.ServiceID, msg.Type)
	},
	OnEvent: func(ctx context.Context, event conduit.HookEvent) {
		recordOutcome(ctx, event.Stage, event.Delivery)
	},
})
```

Publish hooks run before final typed validation. They can enrich metadata or reject a draft, but they can't change its message ID, source or type. Handle hooks can reject an attempt before your handler runs. `conduit.AroundHandle` wraps individual subscriptions with middleware in outermost-first order.

Observers receive copied outcomes for lifecycle, provider connections, subscription startup, publication, processing, acknowledgement, retry, dead letter storage and replay. Observer panics are isolated. Their bounded queue drops observations under sustained backpressure, which you can see through `ObserverDrops`. An observer is best effort, so put mandatory policy or audit work in a control hook or your business transaction. The Forge extension also counts every outcome in `forge_forge_conduit_outcomes_total` (the registry adds its own `forge_` prefix), labelled by namespace, service and outcome.

## Tracing

Conduit carries trace context for you, so a hook doesn't need to copy `traceparent` by hand. `Prepare` records a `create` span and writes its context into `Envelope.Headers`. Because that happens before the publish hooks run, the trace survives the transactional outbox, a backfill and dead letter replay: whatever sends the envelope later, the consumer still joins the trace that created it.

Here's what you'll see for one `conduit.Publish`:

- `create {stream}` is the producer span, recorded by `Prepare` in your request's trace.
- `send {stream}` wraps the provider's publish. It carries a link to the create span. When you call `Send` from an outbox relay there's no caller span, so `send` becomes a root span with that link, and the trace still connects through create.
- `process {stream}` wraps your handler and is a child of the create span. Retries each get their own process span. Ack, retry and dead letter settlement run after it has ended.
- `call {service}.{method}` and `serve {service}.{method}` are the client and server spans of a typed RPC. The server span is a child of the call span.

A failed publish, handler or RPC marks its span as an error and sets `error.type`. If a publish hook replaces `Headers` wholesale, it replaces `traceparent` too, so add to the map instead. Messages from older publishers have no `traceparent`; they give you a root process span and no error.

The extension records two histograms, labelled with the OpenTelemetry messaging attributes. `messaging_client_operation_duration_seconds` times sends and RPC calls, and `messaging_process_duration_seconds` times handlers and RPC servers. `messaging_system` is the provider's name, such as `memory`, `redis-streams`, `nats-jetstream`, `postgres` or `kafka`. If you build a runtime yourself, call `Runtime.RegisterMetrics(metrics)` before `Start` to get the same histograms.

The old `forge_conduit_processing_duration_seconds` histogram (exported as `forge_forge_conduit_processing_duration_seconds`) is gone. For handler time, query `histogram_quantile(0.95, sum by (le, messaging_destination_name) (rate(messaging_process_duration_seconds_bucket[5m])))`. The counter above is unchanged.

## Call a service by name

```go
clients := conduit.Clients(ext.Runtime())
httpClient := clients.HTTP("inventory", authenticatedTransport)

request, err := http.NewRequestWithContext(ctx, "GET", "http://inventory/items/42", nil)
if err != nil {
	return err
}

response, err := httpClient.Do(request)

conn, err := clients.GRPC("inventory", transportCredentials)
// Pass conn to your generated protobuf client's constructor.
```

The HTTP client resolves ready endpoints on every request you make and spreads calls across them. The gRPC resolver refreshes addresses every second and uses round robin. You keep credentials and request authentication in your own transport. Conduit doesn't retry API mutations, and informational source headers don't authenticate a caller.

When Forge discovery is installed, Conduit reuses its backend and leaves its lifecycle to Forge, including only records that carry the matching Conduit namespace. Otherwise a single broker registry is picked automatically: memory doesn't count, and JetStream wins when it's one of several. Set `discovery` to a provider name, `forge` or `none` to choose yourself, and remember that a programmatic `WithRegistry` always wins. JetStream's registry stores a 45-second lease per instance, renewed every ten seconds, so crashed members disappear when their leases expire. `discovery.NewStatic` gives you a fixed registry for tests and fixed deployments, with no crash expiry.

## Typed broker RPC

Declare a procedure once and share it with callers and handlers:

```go
var lookup = conduit.Procedure[LookupRequest, LookupResponse]("billing.lookup.v1")

err := conduit.Handle(ext.Runtime(), lookup,
	func(ctx context.Context, request conduit.Request[LookupRequest]) (LookupResponse, error) {
		return billing.Lookup(ctx, request.Data.ID)
	})

response, err := conduit.Call(ctx, runtime, "billing", lookup, LookupRequest{ID: "invoice-42"})
```

Replicas share one procedure queue, and each call reaches one instance. `rpc.provider` picks the connection when several providers support RPC. With one provider, selection is automatic. With several and `rpc.provider` unset, memory is left out: if one provider remains, it serves RPC, and if several remain and exactly one is JetStream, JetStream does. Otherwise set `rpc.provider` yourself. `rpc.timeout`, `rpc.concurrency` and `rpc.max_in_flight` are per service.

Cancellation and deadlines reach your handlers, but cancellation is best effort and a timed-out call may already have caused an effect. Calls send once and never retry business effects, so keep an application idempotency key for mutations. Return `&conduit.RPCError{Code: conduit.RPCPermissionDenied, Message: "Billing access required"}` for an intentional public error. Arbitrary errors and panics become `INTERNAL` responses without their private cause. The `RPCCalling`, `RPCReceived`, `RPCHandling`, `RPCHandled`, `RPCReturned` and `RPCFailed` hook stages expose the request lifecycle. RPC is live request and reply with no event retention, so durable workflows belong on streams and the transactional inbox.

## Commit business data and an event together

`transaction.Store` uses PostgreSQL through `database/sql`. Run `Migrate` through your migration process, then enqueue a prepared event in the same transaction as your business write:

```go
store := &transaction.Store{DB: db}

publication, err := conduit.Prepare(ctx, ext.Runtime(), OrderPlacedV1, order)
if err != nil {
	return err
}

tx, err := db.BeginTx(ctx, nil)
if err != nil {
	return err
}
defer tx.Rollback()

if err := saveOrder(ctx, tx, order); err != nil {
	return err
}

if err := store.Enqueue(ctx, tx, publication.Stream, publication.Envelope); err != nil {
	return err
}

return tx.Commit()
```

Run `store.Relay(ctx, runtime, onError)` under your application's lifecycle context. It uses `FOR UPDATE SKIP LOCKED`, keeps prepared message IDs and removes a row only after durable broker acceptance, so several relay workers can share the table. A crash after broker acceptance can publish the same ID again.

On the consuming side, bind `store.Inbox(handler)` through `Runtime.Bind`. Your handler receives the same SQL transaction as the inbox marker. The deduplication key includes namespace, service, subscription and message, so replicas share protection while independent subscribers each get their own business effect. External effects still need their own idempotency keys. Inbox rows stay until you apply a retention policy, so keep them longer than the events you might replay.

## Consumer operations

Call `runtime.Consumers(ctx)` and you get pending messages, outstanding acknowledgements and redeliveries for each broker consumer, with processing and publish-to-delivery latency summaries for the current instance (count, total, maximum and average, in nanoseconds, not fleet percentiles).

`runtime.PauseSubscription(ctx, "process-orders", true)` pauses intake on the broker cursor, and `false` resumes it. A competing cursor is shared by the service's replicas; a broadcast cursor belongs to its subscriber. Accepted work can finish while the cursor is paused. JetStream, Redis Streams and PostgreSQL keep pause state across restarts, and memory keeps it for the life of the process.

`runtime.Backfill(ctx, conduit.BackfillInput{ID: "billing-recovery-42", Subscription: "process-orders", Start: 10, End: 30})` replays a range of at most 100 sequences, with a 30-second deadline per run. Matching events keep their original IDs and go only to the selected consumer. Reuse the same ID and range to resume after an interruption, and a completed operation returns its recorded result. Business effects can repeat unless your inbox protects them.

## Providers

| Provider | Package | Durable streams | Replay | Dead letters | Discovery |
| --- | --- | --- | --- | --- | --- |
| NATS JetStream | `providers/jetstream` | File storage with configured replicas | Sequence cursors and targeted recovery | Persisted KV records | Leased KV records |
| Redis Streams (7.2+, AOF on) | `providers/redisstreams` | AOF-confirmed primary and configured replicas | Per-stream sequences and consumer groups | AOF-backed records | Leased keys |
| PostgreSQL 13+ | `providers/postgres` | Committed transactions | Per-stream sequences in commit-safe order | Rows | Leased rows |
| Kafka | `providers/kafka` | All configured in-sync replicas | One-based single-partition offsets | Retained failure log | Use Forge or a registry |
| Memory | `providers/memory` | Process memory only | Retained process-local records | Process-local records | Process-local leases |

JetStream, Redis and PostgreSQL report the same capabilities: retained events, dead letters, targeted replay, pause and resume, resumable backfill, broker RPC and a leased registry. Kafka supplies retained events, dead letters and targeted replay, so pair it with Forge discovery and the named HTTP and gRPC clients. No supplied provider promises ordered processing by message key.

A few things you'll want to know before you pick one:

| Provider | Worth knowing |
| --- | --- |
| JetStream | `Options.LeaseTTL` has to match the TTL of an existing `FC_DISC_*` bucket, or every registry call returns a conflict |
| Redis Streams | Confirms local and replica AOF writes with `WAITAOF`, and deduplicates publications for 24 hours by default. RPC keeps two pooled connections blocked, so leave room for both |
| PostgreSQL | Deduplicates on message ID while the original is retained, and gives you `PublishTx` to publish inside your own transaction. A consumer never reads past the oldest open transaction, which `VisibilityLag` measures, and after a dump and restore you call `Rebase` once |
| Kafka | One partition per stream, one active competing replica at a time, age retention only (`MaxMessages` is rejected), and no publication deduplication |

The [providers guide](https://www.xraph.com/docs/forge/extensions/conduit/providers) covers PostgreSQL roles and grants, PgBouncer, retention and the rest of each provider's operating notes.

To add a broker, implement `core.Provider` and report your real `Capabilities`. Management operations use `core.Management`, discovery uses `core.Registry`, and transient request and reply uses `core.RPCProvider`. Unsupported guarantees fail explicitly. `providers/conformance.Run` checks replica delivery, broadcast, isolation, durable restart, takeover, retries, recovery, topology conflicts and retention against a real broker, then runs a group for each capability the provider claims.

## Configuration

You can call `conduit.NewExtension()` with no options and put providers, streams and subscriptions under `extensions.conduit`. Bind handlers before registration, and Forge validates the loaded configuration when it registers the extension.

```yaml
extensions:
  conduit:
    providers:
      events:
        type: jetstream
        url: nats://nats:4222
        dead_letter_replicas: 3
    streams:
      orders:
        provider: events
        subjects: [orders.>]
        max_age: 168h
        replicas: 3
    subscriptions:
      process-orders:
        stream: orders
        delivery: competing
        durable: true
        concurrency: 4
        max_in_flight: 16
        timeout: 30s
```

Provider `type` accepts `jetstream`, `redis-streams`, `postgres`, `kafka` and `memory`. Kafka URLs are comma-separated `host:port` addresses. TLS and authentication go in each provider's native `Options` in Go.

Identity, version and endpoints are optional with the Forge extension. Conduit reads the application name, version, namespace and development listener settings from `.forge.yaml` or `.forge.yml`, then fills missing values from the Forge application. The advertised host comes from explicit discovery configuration, then `FORGE_ADVERTISE_ADDR`, `POD_IP`, the manifest host and the machine hostname, and wildcard listeners are never advertised. Explicit Conduit fields win. `conduit.New` stays strict for standalone runtimes.

`CONDUIT_NAMESPACE`, `CONDUIT_SERVICE_ID`, `CONDUIT_INSTANCE_ID`, `CONDUIT_VERSION`, `CONDUIT_DISCOVERY` and `CONDUIT_PROVIDERS_<NAME>_URL` override loaded values unless you set those values explicitly in Go. Forge's normal configuration sources and deployment overlays apply too.

## Dashboard

With the dashboard extension installed, Conduit contributes the `conduit` dashboard contract automatically. The React package is `@forge-go/dashboard-plugin-conduit`. It shows per-instance counters, provider guarantees, streams, subscription policies, registered replicas, the latest 100 hook outcomes and cursor-paged dead letters, with real pause and resume, bounded backfill and dead-letter replay. Replay targets the original consumer and keeps the original message ID. Listings leave out payloads, headers and handler error text.

Namespace and service claims, when present, must match the configured runtime, and malformed claims are denied. The dashboard's own authentication, CSRF, command idempotency and audit adapters stay in charge of transport access.

## Documentation

| Topic | Where |
| --- | --- |
| Starting a service, replicas and delivery | [Conduit overview](https://www.xraph.com/docs/forge/extensions/conduit) |
| YAML, inferred metadata and environment overrides | [Configuration](https://www.xraph.com/docs/forge/extensions/conduit/configuration) |
| Publishing, hooks and PostgreSQL transactions | [Events](https://www.xraph.com/docs/forge/extensions/conduit/events) |
| JetStream, Redis Streams, PostgreSQL, Kafka and custom providers | [Providers](https://www.xraph.com/docs/forge/extensions/conduit/providers) |
| Broker RPC and discovery-backed clients | [RPC and discovery](https://www.xraph.com/docs/forge/extensions/conduit/rpc-discovery) |
| Consumer controls, scoped access, shutdown and the demo | [Operations](https://www.xraph.com/docs/forge/extensions/conduit/operations) |
| Package reference | [pkg.go.dev](https://pkg.go.dev/github.com/xraph/forge/extensions/conduit) |

## Run the demo

```sh
cd extensions/conduit
GOWORK=off go run ./cmd/demo
```

The demo listens on `127.0.0.1:8098` and runs two instances against the memory broker. Set `CONDUIT_DEMO_NATS` to use a real JetStream server instead. In `forge-dashboard`, start the shell with `FORGE_DASHBOARD_BACKEND=http://127.0.0.1:8098` and open `/@conduit/`.

## FAQ

### How do I send events between Go microservices with Forge?

Declare the event once with `conduit.Event[T]`, add the Conduit extension with a provider and a stream, `Subscribe` in the consuming service and `Publish` in the producing one. Both services use the same stream declaration, and each has its own subscriptions.

### Is delivery exactly once?

No, it's at least once. Use stable message IDs when you publish, make handlers idempotent, and put database effects behind the transactional inbox. Broker deduplication windows are finite, and a handler can commit work before its acknowledgement is lost.

### Which broker should I use with Conduit?

JetStream if you're starting fresh: it's the provider Conduit prefers for RPC and the registry when you configure several. Redis Streams or PostgreSQL if that's what you already run, since both report the same capabilities as JetStream. Kafka when you're already on Kafka and can live with one partition per stream. Memory for tests and local work.

### Can I use Conduit without Forge?

Yes. `conduit.New(cfg, opts...)` builds a standalone runtime that you start and stop yourself. It's strict: you set `Identity.ServiceID` in the config, and nothing is read from `.forge.yaml` or a Forge app. The namespace defaults to `default` and the instance ID is generated.

## Contributing

The extension is its own Go module inside the Forge repository.

```sh
cd extensions/conduit
GOWORK=off go test -race ./...
CONDUIT_TEST_POSTGRES='postgres://postgres:password@localhost:5432/conduit?sslmode=disable' \
  GOWORK=off go test -race ./transaction ./providers/postgres .
```

PostgreSQL tests skip unless you set `CONDUIT_TEST_POSTGRES`, and `CONDUIT_TEST_REDIS` and `CONDUIT_TEST_KAFKA` turn on the other optional integrations. CI sets `CONDUIT_REQUIRE_PROVIDERS=1` so those checks can't silently skip. Broker restart tests run an embedded NATS server with a real file store. See [AGENTS.md](../../AGENTS.md) for the toolchain, lint rules and commit format.

## License

Conduit is part of [Forge](https://github.com/xraph/forge) and is available under the [Apache License 2.0](../../LICENSE).
