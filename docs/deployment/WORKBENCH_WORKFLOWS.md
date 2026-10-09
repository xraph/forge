# Deployment targets, delivery and workbench storage

Status: proposed additions to the deployment design, 9 October 2026. The dashboard mock implements the configuration interactions and browser persistence. The CLI flags, schema additions, authentication and deployment operations below still need implementation.

These workflows extend the earlier design in `docs/superpowers/specs/2026-10-09-forge-deploy-design.md`. Keep its typed compiler, inline deploy block, descriptor catalog, runtime overlays and immutable plan approval. Add these fields to the shared contracts before implementing the page or adapters. The current CLI does not accept the mock's downloaded schema.

## Pick targets before configuring the stack

You first see large selectable target cards, with Compose selected by default. Choose several when you need a local stack alongside Kubernetes or a managed platform. Continue opens the sidebar workbench. Your choices survive reload, and Manage targets opens the cards again.

The mock keeps one profile per provider type. The implementation should support named target instances too, such as two Kubernetes clusters. Each target instance owns its account/context, environment, service selection, resource placement, build and delivery settings. Project identity and workbench persistence are shared project settings.

Switching a target restores its profile. It must not load a preset over your edits. Removing a target from configuration does not destroy anything remotely. Applying one target never applies every saved target; an explicit multi-target request needs a separate plan and approval for each target and environment.

The sample export names environments `compose-development` and `render-production`, so several targets can have a production profile without colliding. The final schema needs stable target and environment IDs, independent of display labels.

## Deploy selected services

The complete service catalog stays in `.forge.yml`. An environment's `services` list defines the apply scope. The proposed `--services gateway,api` flag narrows that scope for a particular plan; an empty list is an error. Record the selection in the plan hash and journal.

| Selection           | Required behavior                                                                                                                    |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| All services        | Order dependencies, migrations and rollout normally.                                                                                 |
| Gateway without API | Require an existing API binding, preserve service authentication and verify its compatibility and reachability. Do not redeploy API. |
| Worker only         | Generate its overlays and necessary database/cache/broker bindings. No gateway, ingress or HTTP probe is created.                    |
| API without worker  | Deploy API and its resources. Keep the existing consumer and shared data unchanged.                                                  |
| No services         | Stop with a diagnostic. Never interpret this as all services.                                                                        |

Compute resources from selected bindings. Keep a dependency outside the apply scope when an existing binding satisfies it; otherwise explain which service or resource is missing and let you include it. Never silently expand the selection. Migrations run only when their owning service is selected, with a compatibility check for excluded consumers of the shared schema. Resource cleanup is a separate reviewed operation; deselecting a service cannot delete its database, bucket or broker.

The mock updates overlays, image operations, resource counts, topology and rehearsals with the selection. It preserves sample source configuration for every catalog service. `deploy/selection.yml` is a review aid, not a second authoritative configuration file.

## Messaging outside Kubernetes

A broker is a resource with a protocol, driver, delivery requirements and storage lifecycle. Forge queue configuration already names `inmemory`, `redis`, `rabbitmq` and `nats` drivers in `extensions/queue/config.go`; events declares named brokers in `extensions/events/config.go`. An in-memory broker cannot connect separate processes.

The mock offers NATS JetStream, RabbitMQ and Redis queue. Adding one proposes queue bindings for selected API/worker participants. The compiler must first verify that the application actually declares the corresponding extension and driver, and that the driver satisfies its required acknowledgement, replay, ordering and persistence semantics. A Redis queue option does not establish Redis Streams support.

| Placement            | Plan requirements                                                                                                                                           |
| -------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Existing broker      | Secret reference, private reachability or TLS, protocol/capability checks and scoped producer/consumer permissions.                                         |
| Container on target  | A qualified recipe, retained storage, initialization, health checks and explicit networking. Compose and Kubernetes development recipes come first.         |
| Provider managed     | An adapter that can provision the required broker and verify delivery/durability. An unsupported capability stops planning.                                 |
| Separate Docker host | An explicit resource-only target, Docker context, broker recipe and connection binding from the application environment. No host is provisioned implicitly. |

A platform with private TCP networking might host a broker, but that alone does not establish durable storage, recovery or application compatibility. The mock blocks unqualified managed and platform container recipes. You can bind an existing broker, or configure a companion host; production self-hosting stays blocked until availability, backup and restore policies are supplied. This qualification status describes the proposed Forge adapter, not a blanket limitation of the provider.

## Build, registry and Git delivery

You choose an image source separately from the deployment target.

| Source             | Delivery                                                                                                                                          |
| ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| Local Docker build | Keep images on the selected Compose host, or publish them to a registry. A remote target cannot use an image that exists only on your laptop.     |
| Remote builder     | Name a configured builder, publish to a reachable registry and verify target architectures.                                                       |
| CI build           | Publish immutable image digests from the configured workflow. Record its source commit and build provenance.                                      |
| Existing image     | Require an immutable digest for every selected service and verify target pull access. No build or push is requested.                              |
| Provider Git build | Connect a repository, branch, per-service source root and build contract. Resolve and approve a source commit; no local image build is requested. |

The registry screen contains the host, namespace, visibility and authentication method. It offers GHCR, Docker Hub and a custom OCI registry. Connection and access buttons rehearse the flow; they do not authenticate or contact a registry. The page accepts credential references, and actual authentication runs through the CLI, a credential helper or the provider's login flow. Secret values never belong in browser storage, exported files or build arguments.

For [GHCR](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry), local authentication can use a credential helper backed by a classic personal access token. GitHub Actions can publish with `GITHUB_TOKEN` when the workflow has the required package permissions. Builder push access and provider pull access are separate checks. Do not assume a workflow token grants the deployment platform ongoing access. Public anonymous authentication is only a pull path, and never authorizes publishing.

[Render's Blueprint reference](https://render.com/docs/blueprint-spec) distinguishes Git services (`repo`, `branch`) from prebuilt images (`image`). Its `autoDeployTrigger` supports `off`, `checksPass` and `commit`. The adapter should emit the selected trigger explicitly, with manual deployment as the initial default. Each selected service needs the right source root, Dockerfile and runtime bindings; the mock offers these paths in its Service build contracts disclosure.

A branch is mutable. Resolve it to a commit SHA for an approved manual deployment and include that commit in the plan. Automatic provider deployment is an explicit policy that can replace the approved commit later. [Render documents the differences between deploying a specific commit through its CLI, API and deploy hook](https://render.com/docs/deploys); an adapter must preserve the requested automatic-deploy policy and store hook URLs as secrets.

Kubernetes GitOps is another delivery mode: render manifests into the configured repository/path, propose a commit, and let an existing Flux or Argo CD controller reconcile it. Record the approved commit and observed controller revision. Image building and registry publishing remain separate. Pushing a Git commit or receiving an accepted provider request does not establish a healthy deployment. Controller installation, repository writes and provider Git connection are outside this mock.

## Environment creation and persistence

Environment setup offers existing or create. A Kubernetes create plan can propose a namespace; Compose can propose an isolated project/network. Provider account, project and environment creation need separate capability checks, ownership, quotas and permission checks before apply. The mock blocks unqualified creation for its managed platform presets.

Files remain the default for `forge deploy start`.

| Backend    | Proposed contract                                                                                                                                                   |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Files      | `.forge.yml` settings, expected file hashes, restrictive gitignored journal files and target/environment locks under `.forge/state/`.                               |
| SQLite     | Versioned settings, journal and transactions in a local `.forge/*.db` file. Export settings to files for review. A local file cannot coordinate different machines. |
| PostgreSQL | Versioned settings, durable journal and fenced target/environment locks shared across sessions. The DSN is resolved through a secret reference outside the page.    |

These are planned commands:

```bash
forge deploy start
forge deploy start --store sqlite --store-ref .forge/deploy.db
forge deploy start --store postgres --store-ref env:FORGE_DEPLOY_STATE_DSN
forge deploy plan --target render --env render-production --services api,worker --out plan.json
```

Use one persistence interface for settings revisions, journals, lock leases, releases and export/import. Keep store connection settings in a bootstrap file or CLI options so opening a database does not depend on reading that same database first. A database backend can own configuration; exporting `.forge.yml` gives you a reviewable snapshot with its revision. Import requires an explicit conflict check. Do not let database and file copies silently become two writable authorities.

Changing backends needs a preview, schema migration, configuration and journal copy, verification, and an explicit switch of authority. Database migrations and permissions must be checked before accepting writes. A database outage or lost lock stops apply; there is no automatic fallback to an independent file journal. Locking, idempotency and recovery must behave consistently for all stores.

The mock persists its drafts in browser storage regardless of the selected backend. PostgreSQL and SQLite choices only affect proposal settings and CLI previews. No database is opened and no repository files are written.

## Implementation gates

Extend the shared schema and CLI contracts first, then connect the compiler, persistence interface and provider capabilities to the page. Add acceptance fixtures for multiple target instances, profile switching, selective rollout with excluded dependencies, shared-resource retention, remote build and denied registry access, immutable image pulls, provider Git commits/triggers, GitOps revision observation, broker recovery and store migration/concurrent locking.

Keep live qualification separate. Test Compose and a fresh cluster, then real provider accounts with both Git and image delivery. Verify private calls, messaging delivery after restart, Grove/Trove write/read persistence, denied access, failed builds and migrations, interruption/resume and controller/provider drift. The interactive mock does not pass those gates.
