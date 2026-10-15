# Working in forge

Forge is the Go framework the xraph forgery stack runs on. It gives you an app with a router, a vessel DI container, confy config, go-utils logging and metrics, and an extension system with a fixed lifecycle, plus the `forge` CLI and the client generators that sit beside them.

Every forgery repo requires `github.com/xraph/forge`: grove, chronicle, relay, keysmith, ledger, sentinel, shield, vault, warden, nexus, bastion, herald, dispatch, weave, trove, authsome and cortex, plus fabriq. When you change something here, all of them get it the next time they re-pin.

## Modules

There are 39 go.mod files outside `cmd/forge/internal/deploy/testdata/`. Release tags go on the root and the 19 extension modules.

| Module | Directory | Release tag |
|---|---|---|
| `github.com/xraph/forge` | `.` | `vX.Y.Z` |
| `github.com/xraph/forge/extensions/<name>` | `extensions/<name>/` | `extensions/<name>/vX.Y.Z` |
| `github.com/xraph/forge/cmd/forge` | `cmd/forge/` | none, binaries ship from the root tag |
| `github.com/xraph/forge/models` | `models/` | none |
| examples (17 modules) | `examples/*/`, `extensions/*/examples/*/` | none |

The extension modules are auth, cache, conduit, consensus, discovery, events, features, graphql, grpc, hls, kafka, mcp, mqtt, orpc, queue, search, security, streaming and webrtc. The same list appears twice in `.github/workflows/release.yml`, as the `module` choices and as `RELEASE_EXTENSIONS`, so when you add an extension, add it to both.

The dashboard isn't one of them. `extensions/dashboard/` has no go.mod and ships inside the root module. `extensions/ai`, `cron`, `database`, `gateway` and `storage` hold only a README that points at the replacement (ai-sdk, dispatch, grove plus `models`, bastion, trove). Their old tags are still in `git tag`; nothing new gets tagged there.

Conduit's tags start at `extensions/conduit/v1.0.0` and then jump to the shared line, because an `all` release tags every extension at the root's version (`git tag -l 'extensions/conduit/*'`). `all` can't skip one. If you want an extension on its own version line, release `forge` and each extension separately, or take it out of `RELEASE_EXTENSIONS`.

The CLI module has no tags of its own. `go install github.com/xraph/forge/cmd/forge@latest` resolves to a pseudo-version of main's head, so whatever lands on main is what `@latest` users install. Its go.mod carries no `replace` (go install refuses one) and pins the root at a published version or pseudo-version. When you need a root fix in the CLI, push the fix first, then bump that require with `cd cmd/forge && GOWORK=off go get github.com/xraph/forge@<commit>`.

`models` requires grove, not forge, and is consumed at pseudo-versions.

All nineteen extensions resolve the root through `replace github.com/xraph/forge => ../..`, and a few also replace sibling extensions. The example modules under `extensions/*/examples/` replace both the root and their own extension. Your replace disappears once the module is someone else's dependency. They resolve the require line, so raise it when an extension starts using root API from a newer release.

Outside Go:

| Directory | What it is | Published by |
|---|---|---|
| `packages/client-*` | browser runtime, `@forge-go/client-core`, `-react`, `-vue`, `-angular`, `-devtools`, `-react-devtools` | `npm-publish.yml` on a `vX.Y.Z` tag |
| `packages/client-fixtures/` | fixtures shared by the generator and runtime tests | not published |
| `dart-packages/` | `forge_client`, `_flutter`, `_riverpod`, `_offline`, `_grove`, `_devtools` | `dart-publish.yml`, every package at the forge tag's version (`forge_client_grove` is held back) |
| `vscode-forge/` | VS Code extension `forge-devtools` | `vscode-extension.yml` on a `vscode-forge/vX.Y.Z` tag |
| `docs/` | Fumadocs site, pages under `docs/content/docs/` | no workflow here |

## Build, test, lint

Run every module except `cmd/forge` with `GOWORK=off`. CI does. Your checkout may have an untracked `go.work` at the root (it's gitignored) that uses `.` and `./cmd/forge`, and a root workspace governs every module under it, so the ones it doesn't list fail with "directory prefix . does not contain modules listed in go.work". Leave that file alone if you find it; other sessions may be using it. `cmd/forge` is the opposite case: without a workspace it compiles against the root version pinned in its go.mod, so give it a workfile outside the repo, the way CI does.

```sh
GOWORK=off go test -race ./...                       # root module, as the Test job runs it
(cd extensions/conduit && GOWORK=off go build ./... && GOWORK=off go vet ./... \
  && GOWORK=off go test -short -count=1 ./...)       # one extension, as Build All Modules runs it

rm -f /tmp/forge-ci.work
GOWORK=/tmp/forge-ci.work go work init "$PWD" "$PWD/cmd/forge"
(cd cmd/forge && GOWORK=/tmp/forge-ci.work go test -short -count=1 ./...)   # the CLI, as CI tests it

gofmt -l .                                           # prints nothing when formatted
golangci-lint run ./...                              # from inside the module you changed
python3 -m unittest discover -s .github/scripts -p 'test_*.py'      # after touching a workflow
```

`./...` stops at module boundaries. A command at the root covers the root module only.

| Target | What it does |
|---|---|
| `make check` | `go build ./...` then `go vet ./...` in every module, the closest match to Build All Modules |
| `make test` | `go test -v -race -timeout=5m ./...` in every module, examples included (CI uses `-short` for sub-modules) |
| `make test-short` / `make test-integration` | root module packages with `-short`, or with `-tags=integration` |
| `make test-cli` | `cmd/forge` and `cli/` |
| `make lint` | golangci-lint in every module, the same loop the Lint job runs. Run `make install-lint` first so you get the pinned version, because another version reports different findings |
| `make lint-fix` | golangci-lint `--fix`, root module only |
| `make fmt` / `make vet` / `make tidy` | `go fmt`, `go vet` or `go mod tidy` in every module |
| `make build` | the CLI into `bin/forge` |
| `make hooks` | points `core.hooksPath` at `.githooks`, whose commit-msg hook mirrors the PR commitlint rules |
| `make schema-deploy` | copies `cmd/forge/internal/deploy/spec/deploy.schema.json` to `schema/forge-deploy.schema.json` |

The Makefile loops don't touch GOWORK. Export `GOWORK=off` before you use them, and accept that `cmd/forge` then builds against its pinned root.

Some targets don't do what their names say:

- `make fmt-check` runs `go fmt -l`, which go fmt rejects. The rejected command prints nothing on stdout, `test -z` passes, and the target can't fail, so the format check inside `make verify` and `make ci` passes no matter what. Use `gofmt -l .`.
- `make tidy-check` runs `go mod tidy` on the root before it diffs, so it rewrites go.mod and go.sum.
- `make lint-fix` (and `golangci-lint --fix` in general) writes some wrong code here. Read every line of the diff before you keep it. The modernize fix can give two nested loops the same variable, so a comparison turns into `v == v`. errorlint rewrites `ce, ok := err.(*T); !ok || ce.Code != X` into a bare `errors.As` and drops the code check. A fix that spans files can land in the wrong one, and perfsprint swaps in `errors.New` without adding the import. Whitespace-only linters (wsl_v5, whitespace, nlreturn, godot) are safe to auto-fix.
- `make lint` on a Mac passes findings that the Linux Lint job fails. Some types differ by OS (`Statfs_t.Bsize` is `int64` on Linux and unsigned on macOS), so gosec's G115 only fires on Linux. Before you push, run `GOOS=linux golangci-lint run ./...` in the module you changed. Point `GOLANGCI_LINT_CACHE` at an empty directory too. The result cache replays old findings under their old file paths, which hides whether a path-based exclusion matches.
- `make test-extensions` runs `go test ./...` from `extensions/`. That reaches the root-module packages there (the dashboard) and none of the extension modules.
- `make release-main`, `make release-all` and `make release-extensions` call `scripts/release-modules.sh`, which creates tags on your machine and pushes them. Never use them. The `modules-*` targets hardcode five of the nineteen extensions.

### What CI runs

`.github/workflows/go.yml` runs on every push and pull request to main. CI Summary fails the run when any job except the Windows one fails.

| Job | What it runs | Fails CI |
|---|---|---|
| Test (ubuntu, macOS) | `xraph/workflows` go-ci.yml: `go test -race ./...` on the root module only, with Node and the pinned typescript and esbuild installed | yes |
| Test (windows) | the same, without coverage | no |
| Build All Modules | build and vet every module, then `go test -short -count=1 -timeout=5m ./...` in every module except the root | yes |
| Lint | `make install-lint` builds golangci-lint v2.12.2 with Go 1.26.9, `make l` lints every module, then `go vet ./...` on the root | yes |
| Security Scan | gosec (`\|\| true`) and govulncheck (`continue-on-error`) | not on findings |
| Build CLI | `go work init . ./cmd/forge` on the runner, build, `./forge --version` | yes |
| Deploy Compose Lifecycle, Deploy Kubernetes Lifecycle | `-tags integration` deploy tests in `cmd/forge` against Docker and kind | yes |
| Deploy Workbench | rebuilds `cmd/forge/internal/deploy/workbench/_ui` with bun and fails if the committed `dist/` changes, then runs the workbench integration tests | yes |
| Conduit Broker and PostgreSQL Integration | conduit's tests against Postgres, Redis and Kafka service containers | yes |

Build All Modules has two steps. The first walks every go.mod (skipping `testdata/` and `docs/`), builds and vets each one, keeps going past failures and exits non-zero only at the end, and the second, which runs the sub-module tests, starts only if the first one passed. So one module that fails to build or vet hides every sub-module test result behind it. You fix the build, push again, and often get a second, unrelated round of test failures. Plan for it.

In that test step `cmd/forge` runs with `GOWORK` pointing at the throwaway workspace and everything else runs with `GOWORK=off`. The deploy exec helper builds a child's environment with `append(os.Environ(), cmd.Env...)` (`cmd/forge/internal/deploy/execx/runner.go`), so a test that copies a fixture module from `testdata/` and runs `go build` or `go list` on it inherits that GOWORK and fails, because the copy isn't in the workspace. It passes under a plain `go test` and fails in CI, or under the workfile command above. Put `t.Setenv("GOWORK", "off")` in the test (`plugins/deploy_runtime_test.go` does) or pass `GOWORK=off` in the command's Env, which wins because it's appended last.

### Tests that skip

Read the `-v` output for SKIP lines before you call a change tested.

- The sub-module test step uses `-short`, which skips webrtc's integration tests, `TestLogReplication` in `extensions/consensus/testing`, queue's NATS, Redis and RabbitMQ integration tests, and conduit's failover and lease tests. Conduit's run in the conduit job. The webrtc, consensus and queue ones don't run anywhere in CI.
- These skip when their env var is unset: `CONDUIT_TEST_REDIS`, `CONDUIT_TEST_KAFKA` and `CONDUIT_TEST_POSTGRES` (set `CONDUIT_REQUIRE_PROVIDERS=1` and `CONDUIT_REQUIRE_INTEGRATION=1` to turn the skip into a failure, as CI does), `FORGE_EVENTS_REDIS_TEST_ADDR` in `extensions/events/brokers`, and `QUEUE_TEST_RABBITMQ_HOST` with `_USER` and `_PASS` in `extensions/queue`. CI sets neither of the last two.
- Generator tests need a JavaScript toolchain. The TypeScript type check skips without `tsc` or `npx`, the runtime tests skip without `esbuild` and `node`, and the Dart analyzer gate in `internal/client/generators/dart` skips without `fvm`. CI runs that gate only in the Dart Packages workflow. Install the typescript and esbuild versions go.yml pins (`npm-global-packages`) before you touch a generator.
- Untagged `cmd/forge` tests that shell out to `docker` skip without it. The `-tags integration` ones fail instead, and the workbench ones need `FORGE_DEPLOY_CLI` pointing at a freshly built CLI.
- Nothing here uses testcontainers. External services come from those env vars or from CI's service containers.

## Layout

Root module:

- The `forge` package (`*.go` at the top): App, routing, the extension interfaces, DI and config helpers, logger and errors. Much of it is type aliases over `internal/` (`forge.Router = router.Router`, `forge.Logger = logger.Logger`), so when you change a type in `internal/router` or `internal/shared`, you change forge's public API.
- `internal/`: `router` (with `forgemux` and the OpenAPI and AsyncAPI generators), `shared`, `logger`, `metrics`, `health`, `observability`, `resilience`, `idempotency`, `scheduler`, and `client` (the IR, the introspector, and `generators/typescript`, `generators/dart`, `generators/golang`).
- `errors/` (structured and HTTP errors over go-utils/errs), `middleware/`, `cli/` (the CLI framework library), `farp/` (Forge wrappers for `github.com/xraph/farp`), `extras/` (bunrouter, chi and httprouter adapters), `testing/` (quiet loggers and metrics for tests) and `schema/` (`forge.schema.json`, `forge-app.schema.json`, `forge-deploy.schema.json`).
- `cmd/dashboard-contract-probe/`: posts one contract envelope and prints the HTTP status and body.
- `extensions/dashboard/`: the dashboard contract server, with `auth/`, `collector/`, `handlers/`, `discovery/` and `security/` around the contract packages.

| Package under `extensions/dashboard/contract/` | Holds |
|---|---|
| `.` | envelope, error codes, registry, manifest, warden, predicates, audit; design notes as `*.md` |
| `dispatcher/` | the handler table, kind checks, `BeforeDispatch` admission, idempotency binding |
| `transport/` | the HTTP handler, the error code to status mapping, CSRF, capabilities, the SSE stream and its control endpoint |
| `idempotency/` | the replay `Store` interface and the in-memory store |
| `remote/`, `server/` | forwarding to contributors in other services, and serving one from another service |
| `loader/`, `pilot/` | YAML manifest loading and cross-reference validation, and the built-in contributor |

The CLI module keeps `main.go`, one file per command in `plugins/` (init, dev, generate, client, database, deploy, dashboard, doctor, infra and more) and the deploy engine in `internal/deploy/`: providers, the workbench with its embedded `_ui`, `execx`, and fixtures in `testdata/`.

`forge client` (`cmd/forge/plugins/client.go`) drives the generators. Part of the Dart generator is ported from the TypeScript one by `go generate ./internal/client/generators/dart`, and `TestPortedFilesMatchTypeScript` fails when you change a TypeScript helper and don't regenerate.

36 directories carry an `llm.txt` written for agents. When you change a package's public surface, update its `llm.txt` in the same commit.

`.gitignore` ignores `**/*.md` except README.md files, CONTRIBUTING.md, AGENTS.md, `docs/**`, `extensions/dashboard/contract/*.md` and the Dart CHANGELOGs. Any other new Markdown file never shows up in `git status`.

## Conventions

### Extensions

When you write an extension, copy the shape of `extensions/cache`. The struct embeds `*forge.BaseExtension`, the constructor is `NewExtension(opts ...ConfigOption) forge.Extension` with `type ConfigOption func(*Config)`, and options are `WithXxx` setters over `DefaultConfig()`, with `WithConfig` and `WithRequireConfig` in most extensions.

The lifecycle is in `extension.go`. `Register(app)` registers services, `Start(ctx)` runs once every extension has registered and the container has started, `Health(ctx)` is polled, and `Stop(ctx)` runs in reverse dependency order. Name dependencies with `Dependencies()`, or `DepsSpec()` when you need eager, lazy or optional ones. Optional interfaces cover the rest: `ConfigurableExtension`, `ObservableExtension`, `HotReloadableExtension`, `MiddlewareExtension`, `MigratableExtension`, `RunnableExtension` and `CLICommandProvider`.

In `Register`, call `e.BaseExtension.Register(app)` first, since it sets the logger and metrics. Then `e.LoadConfig("<key>", &cfg, e.config, DefaultConfig(), e.config.RequireConfig)` reads `extensions.<key>`, falls back to `<key>`, and layers defaults, then the file, then the options. An option beats the file when it differs from `DefaultConfig()`, zero values included, so `WithReflection(false)` over a default of `true` wins. `WithLimit(20)` against a default of 20 still loses to a file that says 50, because the loader can't tell it from no option. Give every `Config` field a snake_case `yaml` tag, and `yaml:"-"` on function fields and `RequireConfig`; confy binds an untagged field by its exact Go name. A key spelled as the Go name of a field that has a different tag still binds, with a deprecation warning, so tagging an old struct doesn't break files that used the Go names, and when both spellings are present the tag's key wins. Register services through vessel with `e.RegisterConstructor(func(logger forge.Logger, metrics forge.Metrics) (*Service, error) { ... })`; parameters resolve by type, and `vessel.WithAliases(...)` adds names. Callers resolve with `forge.Inject[T]` or `vessel.Inject[T]`.

### Errors and logging

Framework code uses `github.com/xraph/forge/errors`: structured constructors such as `ErrConfigError` and `ErrServiceNotFound`, `*Sentinel` values for `errors.Is`, and HTTP errors (`forge.BadRequest`, `forge.NotFound`, `forge.NewHTTPError`) that handlers return. In an extension, wrap with the extension's name and `%w`, as in `fmt.Errorf("cache: failed to load required config: %w", err)`. Dashboard handlers return `*contract.Error` with a `contract.Code*` value; the transport maps the code to a status and finds a wrapped one with `errors.As`.

`forge.Logger` is go-utils/log's logger, re-exported through `internal/logger`. Pass fields with `forge.F("key", v)` or the typed helpers (`forge.String`, `forge.Int`). Library code doesn't print with `fmt.Print*`.

### Lint rules that bite

`.golangci.yml` is golangci-lint v2 with `default: none` and an explicit list. Run it from inside a sub-module and it walks up to the same file.

- `wsl_v5` and `nlreturn` police blank lines. wsl decides which statements may sit next to each other, and nlreturn wants a blank line before a `return`, `break` or `continue` that ends a block longer than one statement.
- `godot` wants comments to end with a period. `errorlint` wants `%w` and `errors.Is`/`errors.As`, and `errname` wants `ErrXxx` for sentinels and `XxxError` for types.
- `nilnil` rejects `return nil, nil`. Where that's the contract, write `//nolint:nilnil // reason`.
- `containedctx` keeps `context.Context` out of struct fields. Also on: `noctx`, `gochecknoinits` (no `init()`), `forbidigo`, `usetesting` (`t.Context()`, `t.TempDir()`, `t.Setenv()`), `intrange`, `modernize`, `perfsprint`, `canonicalheader`, `recvcheck` and `inamedparam`.
- gosec and errcheck are off in `_test.go`. `nolintlint` reports directives that suppress nothing, so a `//nolint:gosec` in a test file is dead.
- `examples/` is excluded, and generated-file detection is `lax`: any file that mentions "code generated" or "do not edit" is skipped.
- goimports (local prefix `github.com/xraph/forge`) and gofumpt are configured, but there's no `formatters.enable` list. Group imports as standard library, third party, then `github.com/xraph/forge/...`.

### Commits

Commits follow Conventional Commits, and `pr-conventional-commits.yml` checks them on pull requests: `build`, `chore`, `ci`, `docs`, `feat`, `fix`, `perf`, `refactor`, `revert`, `style` or `test`, a header of at most 100 characters, body lines of at most 100, and a subject that isn't Start Case, PascalCase or UPPER CASE. Scopes are normal (`fix(dashboard):`, `feat(deploy):`, `ci(dart):`). The release workflow builds CHANGELOG.md from commit subjects since the previous `vX.Y.Z` tag and leaves out commits that only touch `extensions/` or `docs/`.

## Downstream impact

A downstream repo finds a compile error the day it re-pins. A behaviour change that keeps every Go signature intact takes longer to surface, and the dashboard contract is where that has hurt most. Between v1.12.3 and v1.12.7, three contract changes broke the contract tests in keysmith, nexus and cortex while their production code was fine:

1. `86bea0da` (first tagged in v1.12.4). A contract error returns the HTTP status for its code, through `errorStatus` in `extensions/dashboard/contract/transport/http.go`. A replay `CONFLICT` is 409 where it used to be 500, `PERMISSION_DENIED` is 403, and a wrapped `*contract.Error` keeps its code.
2. `81e035c4` (first tagged in v1.12.5). Generic command replay is bound to a sha256 digest of the request and the full principal. Ordinary successes are stored in a versioned `forge.dashboard.idempotency` envelope whose outer status is 409, and a legacy record or a mismatch returns `CONFLICT` with reason `idempotency.binding_conflict`. The section "Generic command replay" in `extensions/dashboard/README.md` has the binding rules and the rollout order.
3. `da20c5ce` (first tagged in v1.12.5). Dispatch rejects a request whose kind doesn't match the handler's registration with `BAD_REQUEST` ("handler requires kind ..."), before admission or any cache access.

Treat a change to a wire status code, to a stored format that an older reader or writer may share, or to an admission or validation rule as breaking for consumers, even when it compiles everywhere. Say so in the commit body: what a consumer sees before and after, and what their tests will need to change. The re-pin commits in keysmith, nexus and cortex show what the consumer then has to do. Update the dashboard README in the same commit when contract behaviour moves.

## Releasing

You cut a release by dispatching `.github/workflows/release.yml`. Never push a version tag by hand.

```sh
gh run list --workflow go.yml --branch main --limit 1        # main has to be green first
gh workflow run release.yml --ref main -f module=all -f version=X.Y.Z -f dry_run=true
gh workflow run release.yml --ref main -f module=all -f version=X.Y.Z -f dry_run=false
gh run list --workflow release.yml --limit 1                 # then gh run watch <run-id>
```

`dry_run` defaults to false, so a dispatch without it publishes. `version` takes `X.Y.Z` or `X.Y.Z-pre` with no leading `v`. `source_commit` pins the checkout, and only `forge-library` and extension releases accept it. `skip_tests` is for a hotfix CI has already tested, and `prerelease` turns on by itself when the version has a hyphen.

| `module` | Tags | Ships |
|---|---|---|
| `forge` | `vX.Y.Z` | GoReleaser CLI archives, Homebrew, Scoop and nfpm (Docker images are skipped with `--skip=docker` for now), a Go proxy ping, `npm-publish.yml` and `dart-publish.yml` for stable versions, then a CHANGELOG.md commit to main |
| `forge-library` | `vX.Y.Z` | a GitHub release not marked latest, and the proxy ping |
| `cli` | `vX.Y.Z` | GoReleaser only |
| `<extension>` | `extensions/<name>/vX.Y.Z` | a GitHub release and the proxy ping |
| `all` | `vX.Y.Z` and every `extensions/<name>/vX.Y.Z`, on one commit | what `forge` ships; the extension tags get no release or proxy ping of their own |

`forge`, `forge-library` and `cli` all create the root `vX.Y.Z`, so a version one of them used is taken for the others. A guard refuses any tag that already exists at a different commit, dry runs included. The first job pushes the tags before the tests run, so a red test job leaves published tags with no release behind it. If the code was fine, rerun the same version with `--ref vX.Y.Z`; if it needs a fix, ship the next patch. After a `forge` or `all` release, pull before you push again, because the changelog commit has landed on main.

Release Please is not the release path. `release-please.yml` runs on every push to main and fails with "GitHub Actions is not permitted to create or approve pull requests". Ignore that red run. README.md, CONTRIBUTING.md and `.github/RELEASE.md` still describe Release Please; they're out of date.

After a forge release each downstream repo re-pins (`go get github.com/xraph/forge@vX.Y.Z`, plus any `forge/extensions/<name>` it imports) and releases in dependency order:

1. grove
2. chronicle, relay, keysmith, ledger, sentinel, shield, vault, warden, nexus, bastion
3. herald, dispatch, fabriq
4. weave, authsome, trove
5. cortex

trove and dispatch need each other at the module level. Their AGENTS.md files give the order for that pair.

## CI traps in other workflows

Dart Packages (`dart-packages.yml`) only runs when a changed path matches its `paths:` filter. The filter covers `dart-packages/`, the Dart and TypeScript generators, `internal/router`, `internal/shared`, `cmd/forge/plugins/client.go`, `packages/client-fixtures`, the client docs, its own scripts and a few named files. It doesn't cover `go.yml`, `release.yml`, `go.mod` or `go.sum`. Its scripts job runs every `.github/scripts/test_*.py`, and `test_dart_workflow.py` asserts that the generator job installs exactly the `node-version` and `npm-global-packages` go.yml declares. Bump the toolchain in go.yml alone and nothing fails until the next push that touches a Dart path, which then takes the blame. Change the pin in go.yml, in `dart-packages.yml` and in release.yml's test job together, and run the Python suite before you push. `test_release_workflow.py` lives in the same suite, so a release.yml edit goes untested until Dart Packages next runs. When a Dart job goes red, `git log -S '<the asserted text>'` before you blame the newest commit.

## Branches

You can push straight to main. `gh api repos/xraph/forge/rules/branches/main` shows only `deletion` and `non_fast_forward` rules, and there are no required status checks, so nothing stops a red push. Go CI runs on every push to main and every pull request against it.
