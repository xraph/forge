<p align="center">
  <img src="assets/readme-banner.svg" alt="Forge Queue, a message queue and background job extension for Go with in-memory, Redis, RabbitMQ and NATS JetStream backends" width="100%">
</p>

# Forge Queue: message queues for Go with Redis, RabbitMQ and NATS

Forge Queue is the message queue extension for [Forge](https://github.com/xraph/forge), the Go backend framework. It puts one `Queue` interface in front of four backends (in-memory, Redis Streams, RabbitMQ and NATS JetStream), so your Go service can push background jobs from an HTTP handler and process them in workers, with dead-letter queues, message priority and TTL where the backend supports them. You swap the backend by changing the driver, and your publishing and consuming code stays the same.

[![Go 1.26+](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/dl/) [![Go CI](https://github.com/xraph/forge/actions/workflows/go.yml/badge.svg)](https://github.com/xraph/forge/actions/workflows/go.yml) [![Go Reference](https://pkg.go.dev/badge/github.com/xraph/forge/extensions/queue.svg)](https://pkg.go.dev/github.com/xraph/forge/extensions/queue) [![Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](../../LICENSE)

[Documentation](https://www.xraph.com/docs/forge/extensions/queue) · [Quick start](#quick-start) · [Features](#features) · [Backends](#backends) · [Publishing](#publish-messages) · [Consuming](#consume-messages) · [Configuration](#configuration) · [FAQ](#faq)

## Quick start

You need [Go 1.26 or later](https://go.dev/dl/). The default driver is in-memory, so nothing else has to be running.

```bash
go get github.com/xraph/forge@latest
go get github.com/xraph/forge/extensions/queue@latest
```

This app queues the body of every `POST /emails` and a worker picks each job up:

```go
package main

import (
	"context"
	"io"
	"log"
	"net/http"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/queue"
)

func main() {
	app := forge.NewApp(forge.AppConfig{
		Name:        "mailer",
		HTTPAddress: ":8080",
		Extensions: []forge.Extension{
			queue.NewExtension(), // in-memory driver
		},
	})

	// Declare the queue and start a worker once the extension has connected.
	err := app.RegisterHookFn(forge.PhaseAfterStart, "email-worker",
		func(ctx context.Context, a forge.App) error {
			q, err := queue.GetQueue(a.Container())
			if err != nil {
				return err
			}

			if err := q.DeclareQueue(ctx, "emails", queue.DefaultQueueOptions()); err != nil {
				return err
			}

			opts := queue.DefaultConsumeOptions()
			opts.AutoAck = true

			return q.Consume(ctx, "emails", func(ctx context.Context, msg queue.Message) error {
				log.Printf("sending email: %s", msg.Body)

				return nil
			}, opts)
		})
	if err != nil {
		log.Fatal(err)
	}

	err = app.Router().POST("/emails", func(ctx forge.Context) error {
		q, err := queue.GetQueue(app.Container())
		if err != nil {
			return err
		}

		body, err := io.ReadAll(ctx.Request().Body)
		if err != nil {
			return err
		}

		if err := q.Publish(ctx.Context(), "emails", queue.Message{Body: body}); err != nil {
			return err
		}

		return ctx.JSON(http.StatusAccepted, map[string]string{"status": "queued"})
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
```

Run it and queue a job:

```bash
go mod tidy
go run .

# in another terminal
curl -X POST localhost:8080/emails -d 'welcome ada@example.com'
```

The worker logs `sending email: welcome ada@example.com`. To move to Redis, change one line: `queue.NewExtension(queue.WithDriver("redis"), queue.WithURL("redis://localhost:6379"))`.

## Features

| You need | Forge Queue gives you |
| --- | --- |
| Background jobs from HTTP handlers | `Publish` and `PublishBatch`, with headers, priority, TTL and metadata on each message |
| Workers | `Consume` with a handler function per queue, and `StopConsuming` |
| One API across brokers | In-memory, Redis Streams, RabbitMQ and NATS JetStream behind the same `Queue` interface |
| Failed jobs you can inspect | Dead-letter queues on the in-memory and Redis drivers, and broker dead-lettering on RabbitMQ |
| Queue management | `DeclareQueue`, `DeleteQueue`, `ListQueues`, `GetQueueInfo`, `PurgeQueue` and `Stats` |
| Credentials that stay out of logs | Connection URLs are redacted in log lines and connection errors |

The extension registers `*queue.QueueService` by type and under the alias `queue`, plus the `queue.Queue` interface backed by the same instance. Get it with `queue.GetQueue(c)`, `queue.MustGetQueue(c)`, `queue.GetQueueFromApp(app)` or `forge.Inject[queue.Queue](c)`. The extension's `Start` connects to the backend, so if the broker isn't reachable, the app doesn't start.

## Backends

| Driver | Backend | Connection |
| --- | --- | --- |
| `inmemory` | Slices in process memory. Lost on restart | none |
| `redis` | Redis Streams, one stream per queue (`queue:<name>:stream`) with the consumer group `consumers` | `url` as `redis://` or `rediss://`, or a `host:port` in `url` or `hosts` |
| `rabbitmq` | A RabbitMQ queue per name, published through the default exchange as persistent messages | `url` as `amqp://` or `amqps://`, or `hosts[0]` with `username`, `password` and `vhost` |
| `nats` | A NATS JetStream stream per queue, file storage, subjects `<name>.*` | `url` as `nats://`, or `hosts[0]`, with `username` and `password` |

The backends don't behave the same in every corner, and it's worth knowing where they differ before you pick one:

| | `inmemory` | `redis` | `rabbitmq` | `nats` |
| --- | --- | --- | --- | --- |
| Declare before use | Before `Publish` and `Consume` | Before `Consume` (the group starts at messages published after the declare) | Before `Publish` (the default exchange drops messages for a missing queue) | Before `Publish` and `Consume` |
| Handler returns an error | Moved to the DLQ | Pushed to the `queue:<name>:dlq` list and acknowledged | Nacked, then dead-lettered by the broker if the queue has `DeadLetterQueue` set | Terminated, or redelivered with `NakWithDelay` while `msg.Retries < msg.MaxRetries` |
| Handler returns nil | Removed with `AutoAck`, otherwise held until you call `Ack(ctx, msg.ID)` | Acknowledged | Acknowledged | Acknowledged |
| `PublishDelayed` | Blocks the caller for the delay, then publishes | Not delivered (see the FAQ) | A `<name>.delayed` queue with a TTL that dead-letters back to `<name>` | A goroutine publishes after the delay, lost if the process exits |
| `GetDeadLetterQueue` | Returns the failed messages | Returns the failed messages | Returns nothing | Returns nothing |
| `ConsumeOptions.Concurrency` | Worker goroutines | Not used | Not used | Not used |

`Ack`, `Nack` and `Reject` only do something on the in-memory driver. On Redis, RabbitMQ and NATS the consumer loop acknowledges for you based on what your handler returns. Nothing increments `Message.Retries` yet, so on RabbitMQ and NATS a message with `MaxRetries` above zero keeps coming back until your handler succeeds.

## Publish messages

```go
err := q.Publish(ctx, "emails", queue.Message{
	Body:    payload,
	Headers: map[string]string{"tenant": tenantID},
})

err = q.PublishBatch(ctx, "emails", []queue.Message{{Body: first}, {Body: second}})
```

`Body` is raw bytes, so encode your job however you like (JSON is the usual choice). `Priority` (0 to 9) and `Expiration` map to RabbitMQ's message priority and per-message TTL. Priority only takes effect on a RabbitMQ queue declared with `MaxPriority`. The in-memory driver sets `ID`, `Queue` and `PublishedAt` on publish, and the other drivers set `ID` on delivery from the broker's own identifiers.

Declare queues with `QueueOptions`. `DefaultQueueOptions()` gives you a durable queue with no limits:

```go
opts := queue.DefaultQueueOptions()
opts.MaxLength = 100_000
opts.MessageTTL = 24 * time.Hour
opts.DeadLetterQueue = "emails.failed" // RabbitMQ: dead-letter through the default exchange

err := q.DeclareQueue(ctx, "emails", opts)
```

RabbitMQ turns `MessageTTL`, `MaxLength`, `MaxPriority` and `DeadLetterQueue` into `x-` queue arguments. NATS maps `MaxLength` and `MessageTTL` to the stream's message limit and max age.

## Consume messages

`Consume` starts the worker and returns straight away. Your handler gets a context and a `queue.Message`:

```go
opts := queue.DefaultConsumeOptions() // prefetch 10, concurrency 1, 30s timeout
opts.PrefetchCount = 50

err := q.Consume(ctx, "emails", func(ctx context.Context, msg queue.Message) error {
	return send(ctx, msg.Body)
}, opts)

// later
err = q.StopConsuming(ctx, "emails")
```

`PrefetchCount` becomes the read batch on Redis and `MaxAckPending` on NATS, and RabbitMQ applies the extension's `default_prefetch` as the channel QoS. `Timeout` bounds each handler call on RabbitMQ and NATS (and sets the JetStream ack wait). On the in-memory, Redis and RabbitMQ drivers, cancelling the context you pass to `Consume` stops the worker too. On NATS, call `StopConsuming`.

Read failed messages back with `q.GetDeadLetterQueue(ctx, "emails")`. On the in-memory driver `RequeueDeadLetter(ctx, "emails", id)` puts one back on the queue.

## Tracing and metrics

Anything you publish or consume through `*queue.QueueService` (that's what `queue.GetQueue` hands you) is traced and timed. Set `enable_tracing: false` or `enable_metrics: false` to turn either off. Both default to on.

A `Publish` or `PublishDelayed` starts a producer span named `publish emails`. It also adds `traceparent` (and `tracestate`, when there is one) to the message's `Headers`. Your handler then runs inside a `process emails` span that is a child of the publisher's, so a trace that starts in an HTTP handler carries on into the worker. Spans you start from the handler's `ctx` nest under it. An error from your handler marks the span as failed.

`PublishBatch` publishes the messages one at a time, so every message gets its own span, and it stops at the first error the backend returns.

The service copies the headers before it adds anything. The map you pass in is never touched, so reusing one map across publishes or goroutines is safe. Messages from a publisher that doesn't send trace headers just start a new trace in the worker.

Two histograms record the timing, labelled with `messaging_system` (the driver, with `inmemory` shown as `memory`), `messaging_destination_name` (the queue), `messaging_operation_name`, `messaging_operation_type` and `error_type`:

| Series | Measures |
| --- | --- |
| `messaging_client_operation_duration_seconds` | `Publish` and `PublishDelayed` |
| `messaging_process_duration_seconds` | each handler call |

If a test of yours asserts the exact headers on a published message, allow for `traceparent`. And if you call a backend like `NewInMemoryQueue` directly, without the service, you get no spans and no headers.

## Configuration

Configure the extension in code:

```go
queue.NewExtension(
	queue.WithDriver("rabbitmq"),
	queue.WithURL("amqp://guest:guest@localhost:5672/"),
	queue.WithPrefetch(20),
	queue.WithDeadLetter(true),
)
```

The options are `WithDriver`, `WithURL`, `WithHosts`, `WithAuth`, `WithVHost`, `WithMaxConnections`, `WithPrefetch`, `WithConcurrency`, `WithTimeout`, `WithDeadLetter`, `WithPersistence`, `WithPriority`, `WithDelayed`, `WithTLS`, `WithMetrics`, `WithTracing`, `WithRequireConfig` and `WithConfig`. `NewExtensionWithConfig(cfg)` takes a whole `queue.Config`, which you'd normally start from `queue.DefaultConfig()`.

The extension also reads `extensions.queue` from your config file, falling back to a top-level `queue` key. The file overrides the defaults and your options override the file, so you can pick the driver there:

```yaml
extensions:
  queue:
    driver: redis
    url: redis://queue.internal:6379
    username: mailer
```

These are the fields that change behaviour today:

| Key | Default | What it does |
| --- | --- | --- |
| `driver` | `inmemory` | `inmemory`, `redis`, `rabbitmq` or `nats` |
| `url`, `hosts` | empty | Where to connect. Redis, RabbitMQ and NATS need one of them |
| `username`, `password` | empty | Credentials for Redis, NATS, and RabbitMQ when it builds the URL from `hosts` |
| `vhost` | empty | RabbitMQ virtual host when the URL is built from `hosts` |
| `max_connections` | `10` | Redis pool size. Validation also needs `max_idle_connections` (5) to be no larger |
| `connect_timeout`, `read_timeout`, `write_timeout` | `10s`, `30s`, `30s` | Redis dial, read and write timeouts. `connect_timeout` must be positive |
| `max_retries` | `3` | Redis command retries, and NATS reconnect attempts |
| `default_prefetch` | `10` | RabbitMQ channel prefetch |
| `default_concurrency`, `max_message_size` | `1`, `1048576` | Validated as positive, not applied yet |
| `enable_dead_letter` | `true` | Keep failed messages on the in-memory and Redis drivers |
| `enable_tracing`, `enable_metrics` | `true`, `true` | Producer and consumer spans, and the two messaging duration histograms. See [Tracing and metrics](#tracing-and-metrics) |

`enable_tls` and the `tls_*` files, `enable_priority`, `enable_delayed`, `enable_persistence`, `dead_letter_suffix`, `default_timeout`, `retry_backoff`, `retry_multiplier` and `max_retry_backoff` are on the struct but no driver reads them yet. For TLS, use a `rediss://`, `amqps://` or `tls://` URL.

## Documentation

| Topic | Where |
| --- | --- |
| Overview and what the extension registers | [Queue extension docs](https://www.xraph.com/docs/forge/extensions/queue) |
| Publishing, consuming, dead letters and backends | [Features](https://www.xraph.com/docs/forge/extensions/queue/features) |
| Config fields and options | [Configuration](https://www.xraph.com/docs/forge/extensions/queue/configuration) |
| Method reference | [Functions](https://www.xraph.com/docs/forge/extensions/queue/functions) and [pkg.go.dev](https://pkg.go.dev/github.com/xraph/forge/extensions/queue) |
| Durable jobs, workflows and cron | [Dispatch](https://github.com/xraph/dispatch), the forgery library for background jobs |

## FAQ

### How do I run background jobs in a Go web app?

Add `queue.NewExtension()` to your Forge app, declare a queue and call `Consume` from an after-start hook, then `Publish` from your HTTP handlers. The [quick start](#quick-start) does exactly that with the in-memory driver, and the same code runs on Redis, RabbitMQ or NATS once you change the driver.

### Which backend should I use?

In-memory for tests and local development. RabbitMQ when you want broker-side dead-lettering, priorities and per-message TTL. NATS JetStream when you already run NATS. Redis Streams when Redis is the only thing you have, keeping in mind that a failed message goes straight to the dead-letter list without a retry. If you need retries with backoff, schedules and workflows, look at [Dispatch](https://github.com/xraph/dispatch).

### Can I schedule a job for later?

On RabbitMQ, `PublishDelayed` works through a TTL queue. On NATS it waits in a goroutine, so a restart loses it, and on the in-memory driver it blocks the caller. On Redis, delayed messages go into a sorted set that no consumer reads today, so they're never delivered. Don't use `PublishDelayed` with the Redis driver yet.

### Does a failed job get retried?

Not automatically on in-memory or Redis: it goes to the dead-letter queue. RabbitMQ and NATS redeliver while `msg.Retries < msg.MaxRetries`, and since nothing increments `Retries`, setting `MaxRetries` means "retry until it succeeds". `ConsumeOptions.RetryStrategy` isn't read by any driver yet. Retry inside your handler if you need a bounded number of attempts.

## Contributing

The extension is its own Go module inside the Forge repository. Test it from its directory:

```bash
cd extensions/queue
GOWORK=off go build ./...
GOWORK=off go vet ./...
GOWORK=off go test -short -count=1 ./...
```

`-short` skips the NATS, Redis and RabbitMQ integration tests. The RabbitMQ ones also need `QUEUE_TEST_RABBITMQ_HOST`, `QUEUE_TEST_RABBITMQ_USER` and `QUEUE_TEST_RABBITMQ_PASS`. Run `golangci-lint run ./...` from the same directory before you open a pull request, and see [AGENTS.md](../../AGENTS.md) for the toolchain, lint rules and commit format.

## License

Forge Queue is part of [Forge](https://github.com/xraph/forge) and is available under the [Apache License 2.0](../../LICENSE).
