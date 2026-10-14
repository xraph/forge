<p align="center">
  <img src="assets/readme-banner.svg" alt="Forge Search, a full-text search extension for Go applications built on Forge, with filters, suggestions and autocomplete" width="100%">
</p>

# Forge Search: full-text search for Go services

Forge Search is the search extension for [Forge](https://github.com/xraph/forge), the Go backend framework. You add it to your app and get one `Search` interface in the dependency injection container for index management, document indexing, full-text search with filters and field boosts, search suggestions and autocomplete. The in-memory backend is the one that works today. The config also accepts `elasticsearch`, `meilisearch` and `typesense`, but those drivers are placeholders that never contact a server, so read [Backends](#backends) before you plan around them.

[![Go 1.26+](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/dl/) [![Go CI](https://github.com/xraph/forge/actions/workflows/go.yml/badge.svg)](https://github.com/xraph/forge/actions/workflows/go.yml) [![Go Reference](https://pkg.go.dev/badge/github.com/xraph/forge/extensions/search.svg)](https://pkg.go.dev/github.com/xraph/forge/extensions/search) [![Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](../../LICENSE)

[Documentation](https://www.xraph.com/docs/forge/extensions/search) · [Quick start](#quick-start) · [Features](#features) · [Searching](#index-and-search-documents) · [Backends](#backends) · [Configuration](#configuration) · [FAQ](#faq)

## Quick start

You need [Go 1.26 or later](https://go.dev/dl/) and a Go module:

```bash
go get github.com/xraph/forge@latest
go get github.com/xraph/forge/extensions/search@latest
```

Save this as `main.go`. It creates a `products` index when the app starts, adds three documents, and serves a search endpoint:

```go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/search"
)

func main() {
	app := forge.NewApp(forge.AppConfig{
		Name:        "search-demo",
		HTTPAddress: ":8080",
		Extensions: []forge.Extension{
			search.NewExtension(search.WithDriver("inmemory")),
		},
	})

	// The search service connects when the extension starts, so seed the
	// index once the app is up.
	err := forge.OnStarted(app, "seed-products", func(ctx context.Context, a forge.App) error {
		s := search.MustGetSearchFromApp(a)

		if err := s.CreateIndex(ctx, "products", search.IndexSchema{
			Fields: []search.FieldSchema{
				{Name: "title", Type: "text", Searchable: true},
				{Name: "category", Type: "keyword", Filterable: true},
			},
		}); err != nil {
			return err
		}

		return s.BulkIndex(ctx, "products", []search.Document{
			{ID: "1", Fields: map[string]any{"title": "Mechanical keyboard", "category": "input"}},
			{ID: "2", Fields: map[string]any{"title": "Wireless mouse", "category": "input"}},
			{ID: "3", Fields: map[string]any{"title": "USB-C keyboard dock", "category": "hubs"}},
		})
	})
	if err != nil {
		log.Fatal(err)
	}

	err = app.Router().GET("/search", func(ctx forge.Context) error {
		s, err := search.GetSearchFromApp(app)
		if err != nil {
			return err
		}

		results, err := s.Search(ctx.Context(), search.SearchQuery{
			Index: "products",
			Query: ctx.Query("q"),
			Limit: 10,
		})
		if err != nil {
			return err
		}

		return ctx.JSON(http.StatusOK, results)
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
```

Run it and search:

```bash
go mod tidy
go run .

# in another terminal
curl 'http://localhost:8080/search?q=keyboard'
```

You get the keyboard and the dock back, each with a score, plus `total`, `offset` and `limit`. Hits with equal scores can come back in either order. The index lives in process memory, so when you stop the app with Ctrl+C and run it again, you start from an empty index and the start hook seeds it for you.

## Features

| You need | Forge Search gives you |
| --- | --- |
| One search API for your services | The `Search` interface, registered in the container as `*search.SearchService` and as `search.Search` |
| Indexes with a schema | `CreateIndex`, `DeleteIndex`, `ListIndexes` and `GetIndexInfo`, with field types, analyzers, synonyms and ranking settings in `IndexSchema` |
| Document writes | `Index`, `BulkIndex`, `Get`, `Update` and `Delete` |
| Full-text search | Case-insensitive matching across every field, with per-field boosts, a minimum score, an offset and a limit capped by `MaxLimit` |
| Filters | `=`, `!=`, `EXISTS` and `NOT EXISTS` on any field |
| Search-as-you-type | `Suggest` for substring matches on one field, `Autocomplete` for prefix matches |
| Lifecycle and health | The backend connects on start, disconnects on stop, and reports health through `Ping` |
| Sentinel errors | `ErrIndexNotFound`, `ErrIndexAlreadyExists`, `ErrDocumentNotFound`, `ErrNotConnected` and more, for `errors.Is` |

## Index and search documents

Resolve the service from the container once the app has started. `search.GetSearch(container)`, `search.GetSearchFromApp(app)` and their `Must...` variants do it for you, and `forge.Inject[*search.SearchService](container)` works too.

```go
s := search.MustGetSearchFromApp(app)

err := s.Index(ctx, "products", search.Document{
	ID:     "4",
	Fields: map[string]any{"title": "Laptop stand", "category": "desk", "brand": "Acme"},
})

results, err := s.Search(ctx, search.SearchQuery{
	Index:       "products",
	Query:       "stand",
	Filters:     []search.Filter{{Field: "category", Operator: "=", Value: "desk"}},
	BoostFields: map[string]float64{"title": 2},
	Offset:      0,
	Limit:       20,
})
```

`Index` writes or replaces the document with that ID. You have to create the index first, otherwise you get `ErrIndexNotFound`, and calling `CreateIndex` on a name you already used returns `ErrIndexAlreadyExists`, which you can check with `errors.Is` if your startup code might run twice.

Here is what the in-memory backend does with each field of a `SearchQuery`:

| Field | Behaviour |
| --- | --- |
| `Query` | Lowercased substring match against the string form of every field. An empty query matches every document |
| Scoring | Each matching field adds 1, multiplied by its `BoostFields` entry, and doubled when the whole field equals the query. Hits come back highest score first |
| `MinScore` | Drops hits that score below it |
| `Filters` | Supports `=` (or `==`), `!=`, `EXISTS` and `NOT EXISTS`, compared as strings. Any other operator matches nothing |
| `Limit` | `0` uses `DefaultLimit` (20). Anything above `MaxLimit` (100) is capped |
| `Sort`, `Facets`, `Highlight`, `FuzzyLevel` | Accepted, not applied yet |

One small trap. `ProcessingTime` in the results is a Go `time.Duration`, so when you return the results as JSON you get a nanosecond count, even though the key says `processing_time_ms`.

For search-as-you-type, ask one field for completions:

```go
done, err := s.Autocomplete(ctx, search.AutocompleteQuery{
	Index: "products",
	Field: "title",
	Query: "wire",
	Limit: 5,
})
// done.Completions[0].Text == "Wireless mouse"
```

`Suggest` takes a `SuggestQuery` with the same fields and matches anywhere in the value. Both default to 10 results.

## Backends

| Driver | Status | Needs |
| --- | --- | --- |
| `inmemory` | Works. Indexes live in process memory and are lost on restart | Nothing |
| `elasticsearch` | Placeholder. It connects without contacting a server, writes are dropped, and `Search` returns no hits | `url` or `hosts` to pass validation |
| `meilisearch` | Placeholder, same as above | `url` |
| `typesense` | Placeholder, same as above | `url` or `hosts`, and `api_key` |

The placeholders are there so the config shape and the service wiring are settled before the client code lands. Do not point production traffic at them. An app you configure for Elasticsearch starts fine, logs that it connected, and silently indexes nothing.

## Configuration

You can configure the extension in code with options, or in YAML under `extensions.search` (the extension also reads a top-level `search` key).

```go
search.NewExtension(
	search.WithDriver("inmemory"),
	search.WithDefaultLimit(25),
	search.WithMaxLimit(200),
)
```

| Option | Sets |
| --- | --- |
| `WithDriver(string)` | `Driver`: `inmemory`, `elasticsearch`, `meilisearch` or `typesense` |
| `WithURL(string)`, `WithHosts(...string)` | Server address, or several |
| `WithAuth(user, pass)`, `WithAPIKey(string)` | Credentials |
| `WithMaxConnections(int)`, `WithTimeout(time.Duration)` | Pool size and `RequestTimeout` |
| `WithDefaultLimit(int)`, `WithMaxLimit(int)` | Result limits |
| `WithTLS(cert, key, ca)` | Turns on TLS with those files |
| `WithMetrics(bool)`, `WithTracing(bool)` | Observability flags |
| `WithRequireConfig(bool)` | Fail at startup if no YAML config is found |
| `WithConfig(search.Config)` | Replaces the whole config |

`NewExtensionWithConfig(cfg)` is the same as `NewExtension(WithConfig(cfg))`. Validation runs at registration, so a bad combination (say `max_limit` below `default_limit`) stops the app before it serves anything.

The YAML keys are the snake_case names from the struct tags:

```yaml
extensions:
  search:
    driver: inmemory
    default_limit: 25
    max_limit: 200
    request_timeout: 30s
```

| Key | Default |
| --- | --- |
| `driver` | `inmemory` |
| `url`, `hosts`, `username`, `password`, `api_key` | empty |
| `max_connections`, `max_idle_connections` | `10`, `5` |
| `connect_timeout`, `request_timeout`, `keep_alive` | `10s`, `30s`, `60s` |
| `max_retries`, `retry_backoff`, `retry_on_timeout` | `3`, `100ms`, `true` |
| `default_limit`, `max_limit`, `default_min_score` | `20`, `100`, `0` |
| `enable_highlight`, `enable_facets` | `true`, `true` |
| `bulk_size`, `flush_interval`, `enable_compression` | `100`, `5s`, `false` |
| `enable_tls`, `tls_cert_file`, `tls_key_file`, `tls_ca_file`, `insecure_skip_verify` | off and empty |
| `enable_metrics`, `enable_tracing` | `true`, `true` |

Most of the connection, retry, bulk and TLS settings only matter to the network drivers, which are not implemented yet.

YAML overrides the defaults, and options you set in code override YAML. A `driver` or `default_limit` in the file takes effect with a plain `search.NewExtension()`, while `search.WithDefaultLimit(70)` still beats whatever the file says. Fields you leave out of the YAML keep their `DefaultConfig()` values.

Forge reads `config.yaml` only when `EnableConfigAutoDiscovery` is on. `forge.DefaultAppConfig()` turns it on, but an `AppConfig` literal like the one in the quick start leaves it off, so set it yourself or pass your own `ConfigManager`.

## Documentation

| Topic | Where |
| --- | --- |
| Overview | [Search docs](https://www.xraph.com/docs/forge/extensions/search) |
| Features and usage | [Search features](https://www.xraph.com/docs/forge/extensions/search/features) |
| Configuration reference | [Search configuration](https://www.xraph.com/docs/forge/extensions/search/configuration) |
| Function reference | [Search functions](https://www.xraph.com/docs/forge/extensions/search/functions) |
| Go API | [pkg.go.dev](https://pkg.go.dev/github.com/xraph/forge/extensions/search) |
| How extensions load config and register services | [Forge extension system](../../docs/content/docs/forge/extensions-system.mdx) |

The docs pages were written before this README. Where they disagree about backends, filter operators or YAML key names, the tables above match the code.

## FAQ

### How do I add full-text search to a Go app built on Forge?

Add `search.NewExtension()` to `Extensions` in `forge.AppConfig`, create an index once the app has started, and call `Search` on the service from your handlers. The [quick start](#quick-start) is a complete program.

### Does Forge Search work with Elasticsearch, Meilisearch or Typesense?

Not yet. The drivers exist and pass config validation, but they never talk to a server: writes are dropped and searches return nothing. Use `inmemory` until a real client lands, or wrap your own client behind the `search.Search` interface.

### Is the in-memory search index persistent?

No. It is a map in process memory, so it suits development, tests and small data sets you can rebuild on startup. Every restart starts empty.

### Does Forge Search support fuzzy matching, facets and sorting?

The query types carry those fields (`FuzzyLevel`, `Facets`, `Sort`, `Highlight`), but the in-memory backend ignores them today. It does case-insensitive substring matching with boosts, a minimum score and simple filters.

### Is the in-memory backend safe for concurrent use?

Yes. The index map and each index's documents sit behind `sync.RWMutex` locks, so concurrent handlers can index and search at the same time.

## Contributing

Search is its own Go module inside the [Forge repository](https://github.com/xraph/forge). Build, vet and test it from its directory the way CI does:

```bash
cd extensions/search
GOWORK=off go build ./...
GOWORK=off go vet ./...
GOWORK=off go test -short -count=1 ./...
golangci-lint run ./...
```

Forge's [AGENTS.md](../../AGENTS.md) has the pinned lint version and the rest of the repository conventions. Commits follow [Conventional Commits](https://www.conventionalcommits.org/), for example `feat(search): ...`.

## License

Forge Search is part of Forge and is available under the [Apache License 2.0](../../LICENSE).
