# Conduit

Conduit connects Forge services through typed events, durable streams and named HTTP or gRPC clients. You choose the broker. Your handlers keep the same API.

Every process has two identities: `ServiceID` names the logical service, while `InstanceID` names its running replica. Consumer identity includes the namespace, service and subscription. Competing consumers share that identity across replicas; broadcast consumers add a subscriber identity so each instance has its own cursor.

## Start a service

```go
import (
    "context"
    "time"

    "github.com/xraph/forge/extensions/conduit"
    "github.com/xraph/forge/extensions/conduit/providers/jetstream"
)

type OrderPlaced struct {
    OrderID string `json:"orderID"`
}

var OrderPlacedV1 = conduit.Event[OrderPlaced]("orders.placed.v1")

broker := jetstream.New(jetstream.Options{URL: "nats://localhost:4222"})
ext, err := conduit.NewExtension(conduit.WithConfig(conduit.Config{
    Streams: map[string]conduit.StreamConfig{
        "orders": {Provider: "events", Subjects: []string{"orders.>"}, MaxAge: 7 * 24 * time.Hour, Replicas: 3},
    },
    Subscriptions: map[string]conduit.SubscriptionConfig{
        "process-orders": {Stream: "orders", Mode: conduit.Competing, Durable: true, Concurrency: 4, MaxInFlight: 16},
        "refresh-cache":  {Stream: "orders", Mode: conduit.Broadcast},
    },
}), conduit.WithProvider("events", broker))
if err != nil { return err }

err = conduit.Subscribe(ext.Runtime(), OrderPlacedV1,
    func(ctx context.Context, msg conduit.Message[OrderPlaced]) error {
        return chargeOrder(ctx, msg.Data.OrderID, msg.Envelope.ID)
    }, conduit.Consumer("process-orders"))
if err != nil { return err }

err = conduit.Subscribe(ext.Runtime(), OrderPlacedV1,
    func(ctx context.Context, msg conduit.Message[OrderPlaced]) error {
        return refreshOrderCache(ctx, msg.Data.OrderID)
    }, conduit.Consumer("refresh-cache"))
if err != nil { return err }

// Register ext through app.RegisterExtension or AppConfig.Extensions.
// Forge starts and stops the runtime with the application.
```

Identity, version and endpoints are optional when you use the Forge extension. You can call `conduit.NewExtension()` and put provider, stream and subscription settings under `extensions.conduit` in your application configuration. Bind handlers before registration. Forge validates the loaded configuration when it registers the extension.

Conduit reads the native `.forge.yaml` or `.forge.yml` application name, version, namespace and development listener settings, then fills missing values from the Forge application. The advertised host follows explicit discovery configuration, `FORGE_ADVERTISE_ADDR`, `POD_IP`, the manifest host and the machine hostname. Wildcard listeners are never advertised. Explicit Conduit fields win. `conduit.New` remains strict for standalone runtimes.

```yaml
extensions:
  conduit:
    providers:
      events:
        type: jetstream
        url: ${NATS_URL}
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

`CONDUIT_NAMESPACE`, `CONDUIT_SERVICE_ID`, `CONDUIT_INSTANCE_ID`, `CONDUIT_VERSION`, `CONDUIT_DISCOVERY` and `CONDUIT_PROVIDERS_<NAME>_URL` override loaded values unless you supply those values explicitly in Go. Forge's normal configuration sources and deployment overlays also apply.

When Forge discovery is installed, Conduit reuses its backend and leaves its lifecycle with Forge. Only records carrying the matching Conduit namespace are included. Otherwise a single broker registry is selected automatically. Set `discovery` to a provider name, `forge` or `none` to choose explicitly. A programmatic `WithRegistry` always wins. The bridge removes only its own instance record, and its crash-expiry guarantees follow the existing discovery backend.

Conduit generates an instance ID when you omit it. Supply your pod or process identity when you need to correlate it with deployment records. Advertise an address that clients can reach; a listener's wildcard bind address is not an endpoint.

Streams belong to a namespace and must have matching retention settings wherever they are declared. Services can use different subscriptions, delivery modes, retry policies and worker counts against the same stream. A competing group's `MaxInFlight`, timeout and retry settings must agree across its replicas; `Concurrency` controls each replica's workers.

## Publish and handle

```go
receipt, err := conduit.Publish(ctx, ext.Runtime(), OrderPlacedV1,
    OrderPlaced{OrderID: "order-42"},
    conduit.MessageID("order-42-placed"),
    conduit.Correlation("checkout-42"),
)
```

The receipt confirms broker acceptance. `Persisted` tells you whether the provider stored the message durably. It does not mean a handler has finished. If publication returns `conduit.ErrOutcomeUnknown`, retry with the same message ID: the broker may have accepted the first attempt even though its acknowledgement did not reach you.

Delivery is at least once. Use idempotent handlers. JetStream deduplication has a finite window, and a handler can commit its work before losing an acknowledgement. `conduit.Permanent(err)` ends processing retries immediately. Other failures retry with a linearly increasing delay, then enter the provider's dead letter store. Storage failure leaves the delivery eligible for retry instead of dropping it.

Ephemeral broadcast starts with new events by default. Durable broadcast requires a stable `BroadcastID`, unique to each intended subscriber, so a replacement process can resume that subscriber's cursor. An auto-generated instance ID cannot identify a durable broadcast subscriber after a restart.

## Hooks and middleware

Use `conduit.WithHooks` at construction or `Runtime.RegisterHook` before startup. A hook needs only a name and the interfaces it uses. `HookFuncs` supplies callbacks for common cases:

```go
conduit.WithHooks(conduit.HookFuncs{
    HookName: "trace-and-audit",
    OnPublish: func(ctx context.Context, draft *conduit.Envelope) error {
        if draft.Headers == nil { draft.Headers = map[string]string{} }
        draft.Headers["traceparent"] = traceHeader(ctx)
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

Publish hooks run before final typed validation. They can enrich metadata or reject a draft, but cannot change its message ID, source or type. Handle hooks can reject an attempt before your handler runs. `AroundHandle` wraps individual subscriptions with middleware in outermost-first order.

Observers receive copied outcomes for lifecycle, provider connections, subscription startup, publication, processing, acknowledgement, retry, dead letter storage and replay. Observer panics are isolated. Their bounded queue drops observations under sustained backpressure, which you can inspect through `ObserverDrops`. Use a control hook or your business transaction for mandatory policy or audit work. An observer is best effort.

The Forge extension also reports these observations through `forge_conduit_outcomes_total`, labelled by namespace, service and outcome. Dashboard counters record runtime outcomes directly and distinguish handled messages from confirmed acknowledgements.

## Call a service by name

```go
clients := conduit.Clients(ext.Runtime())
httpClient := clients.HTTP("inventory", authenticatedTransport)
request, err := http.NewRequestWithContext(ctx, "GET", "http://inventory/items/42", nil)
if err != nil { return err }
response, err := httpClient.Do(request)

conn, err := clients.GRPC("inventory", transportCredentials)
// Pass conn to your generated protobuf client's constructor.
```

HTTP resolves ready endpoints on each request and distributes calls across them. The gRPC resolver refreshes addresses every second and uses round robin. Credentials and request authentication stay with your transport. Conduit does not retry API mutations automatically, and informational source headers do not authenticate a caller.

JetStream's discovery registry stores a 45-second lease per instance, renewed every ten seconds. Shutdown removes only that instance. Crashed members disappear when their leases expire. The registry is pluggable independently of messaging; `discovery.Static` supports tests and fixed deployments, with no automatic crash expiry.

## Commit business data and an event together

`transaction.Store` uses PostgreSQL through `database/sql`. Run `Migrate` through your application's migration process, then enqueue a prepared event in the same transaction as your business write:

```go
publication, err := conduit.Prepare(ctx, ext.Runtime(), OrderPlacedV1, order)
if err != nil { return err }
tx, err := db.BeginTx(ctx, nil)
if err != nil { return err }
defer tx.Rollback()
if err := saveOrder(ctx, tx, order); err != nil { return err }
if err := store.Enqueue(ctx, tx, publication.Stream, publication.Envelope); err != nil { return err }
if err := tx.Commit(); err != nil { return err }
```

Run `store.Relay(ctx, runtime, onError)` under your application's lifecycle context. It uses `FOR UPDATE SKIP LOCKED`, preserves prepared message IDs and removes a row only after durable broker acceptance. Multiple relay workers can share the table. A crash after broker acceptance can publish the same ID again.

For consumers, bind `store.Inbox(handler)` through `Runtime.Bind`. Your handler receives the same SQL transaction as the inbox marker. Its deduplication key includes namespace, service, subscription and message, so replicas share protection while independent subscribers each get their own business effect. External effects still need their own idempotency keys. Inbox rows remain until you apply your retention policy; keep them longer than the events you may replay.

## Dashboard and providers

The extension automatically contributes the `conduit` dashboard contract when the dashboard extension is present. The React package is `@forge-go/dashboard-plugin-conduit`. It shows per-instance counters, provider guarantees, streams, subscription policies, registered replicas, the latest 100 hook outcomes and cursor-paged dead letters. Replay targets the original consumer and keeps the original message ID. Listings exclude payloads, headers and arbitrary handler error text.

Namespace and service claims, when present, must match the configured runtime. Malformed claims are denied. Without those claims, the contract uses the explicitly configured runtime scope. The dashboard's normal authentication, CSRF, command idempotency and audit adapters remain in charge of transport access.

| Provider       | Durable streams                               | Replay                                   | Dead letters          | Discovery                        |
| -------------- | --------------------------------------------- | ---------------------------------------- | --------------------- | -------------------------------- |
| NATS JetStream | File storage with configured replicas         | Sequence cursors and targeted recovery   | Persisted KV records  | Leased KV records                |
| Redis Streams  | AOF-confirmed primary and configured replicas | Per-stream sequences and consumer groups | AOF-backed records    | Use Forge or a separate registry |
| Kafka          | All configured in-sync replicas               | One-based single-partition offsets       | Retained failure log  | Use Forge or a separate registry |
| Memory         | Process memory only                           | Retained process-local records           | Process-local records | Use a separate registry          |

Implement `core.Provider` and report your actual `Capabilities` to add a broker. Management operations use `core.Management`; discovery uses `core.Registry`. Unsupported guarantees fail explicitly. No supplied provider promises ordered processing by message key. Implement `core.RPCProvider` for transient request/reply. Named gRPC clients provide streaming RPC through generated clients.

## Run the checks and demo

```sh
cd extensions/conduit
GOWORK=off go test -race ./...
CONDUIT_TEST_POSTGRES='postgres://postgres:password@localhost:5432/conduit?sslmode=disable' GOWORK=off go test -race ./transaction
GOWORK=off go run ./cmd/demo
```

The demo listens on `127.0.0.1:8098` and runs two instances against the memory broker. Set `CONDUIT_DEMO_NATS` to use a real JetStream server instead. In `forge-dashboard`, start the shell with `FORGE_DASHBOARD_BACKEND=http://127.0.0.1:8098` and open `/@conduit/`. PostgreSQL integration tests skip unless you provide `CONDUIT_TEST_POSTGRES`; broker restart tests run an embedded NATS server with a real file store.

## Typed broker RPC

Declare your procedure once and share it with callers and handlers:

```go
var lookup = conduit.Procedure[LookupRequest, LookupResponse]("billing.lookup.v1")

err := conduit.Handle(ext.Runtime(), lookup,
    func(ctx context.Context, request conduit.Request[LookupRequest]) (LookupResponse, error) {
        return billing.Lookup(ctx, request.Data.ID)
    })

response, err := conduit.Call(ctx, runtime, "billing", lookup, LookupRequest{ID: "invoice-42"})
```

Replicas share one procedure queue. Each call reaches one instance. `rpc.provider` selects a connection when several providers support RPC; with one provider, selection is automatic. Configure `rpc.timeout`, `rpc.concurrency` and `rpc.max_in_flight` per service. Handlers can inspect caller identity, headers and correlation in `request.Envelope`. These fields are metadata; broker access controls and your authorization hooks authenticate callers.

Cancellation and deadlines propagate to handlers. Cancellation is best effort, and a timed-out call may already have caused an effect. Calls send once and never retry business effects automatically. Keep an application idempotency key for mutations. RPC uses live NATS request/reply without event retention. Durable workflows belong on streams and transactional inboxes.

Return `&conduit.RPCError{Code: conduit.RPCPermissionDenied, Message: "Billing access required"}` for intentional public errors. Arbitrary errors and panics become `INTERNAL` responses without their private cause. `RPCCalling`, `RPCReceived`, `RPCHandling`, `RPCHandled`, `RPCReturned` and `RPCFailed` hooks expose the request lifecycle. Existing publish and handle control hooks run before RPC work.

## Consumer operations

`runtime.Consumers(ctx)` reads pending messages, outstanding acknowledgements and redeliveries from each registered broker consumer. Processing and publication-to-delivery latency summaries count attempts on the current instance. They include count, total, maximum and average durations, in nanoseconds. They are not fleet percentiles. Hooks include attempt duration, and Forge exports a processing duration histogram in seconds.

`runtime.PauseSubscription(ctx, "process-orders", true)` pauses intake on the actual broker cursor. Pass `false` to resume. A competing cursor is shared by service replicas; a broadcast cursor belongs to its configured subscriber. Accepted work can finish while the cursor is paused. JetStream retains pause state across restarts; memory state lasts for the process.

Run controlled recovery with `runtime.Backfill(ctx, conduit.BackfillInput{ID: "billing-recovery-42", Subscription: "process-orders", Start: 10, End: 30})`. The inclusive range is capped at 100 sequences and each run has a 30-second deadline. Missing retained messages, other event types and earlier targeted recovery records are counted as skipped. Matching original events retain their IDs and go only to the selected logical consumer. Business effects may repeat unless your inbox protects them.

JetStream stores progress in a replicated file-backed KV bucket. Reuse the same operation ID and unchanged range to resume after an interruption; completed operations return their recorded result. A crash claim expires after one minute. Concurrent attempts conflict. Publication IDs are stable per operation and sequence, but broker deduplication has a finite window. Memory provides the same controls without disk persistence. The dashboard exposes real pause/resume commands, bounded backfill, scoped history and broker lag.

## Additional providers and published docs

Use `providers/redisstreams` for Redis 7.2+ with AOF enabled, or `providers/kafka` for Kafka. Forge configuration accepts `redis-streams` and `kafka`; Kafka URLs contain comma-separated `host:port` addresses. Native provider options carry TLS and authentication settings.

Redis confirms local and configured replica AOF writes with `WAITAOF`. It supports age/count retention and consumer groups, with 24-hour publication deduplication by default. Kafka uses one partition per stream and settles each event before fetching the next. One competing replica is active at a time, while broadcast groups have independent offsets. Kafka supports age retention and rejects `MaxMessages`. It preserves stable IDs without publication deduplication; retry attempt counts reset after group reassignment. Use the transactional inbox to protect business effects.

Redis and Kafka supply retained events, dead letters and targeted replay. Broker RPC, pause/resume and resumable backfill remain unsupported on those adapters. You can use Forge discovery and named HTTP/gRPC clients with either provider. Kafka failure listings fold the retained service failure log, so their cost grows with that log. Concurrent recovery can publish duplicates with the original message ID.

The shared `providers/conformance.Run` suite checks real broker replica delivery, broadcast, isolation, durable restart, unsettled takeover, retries, recovery, topology conflicts, sequence starts and age retention. Set `CONDUIT_TEST_REDIS` and `CONDUIT_TEST_KAFKA` to run the optional local integrations. CI sets `CONDUIT_REQUIRE_PROVIDERS=1` so those checks cannot silently skip.

Full usage and deployment details are in the [Conduit documentation](https://xraph.com/docs/forge/extensions/conduit). Install the published module with `go get github.com/xraph/forge/extensions/conduit@v1.0.0` and the dashboard package with `pnpm add @forge-go/dashboard-plugin-conduit`.
