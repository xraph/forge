/**
 * `@forge-go/client-devtools` -- why the cache did what it did.
 *
 * A normalized cache with a tag graph is the right shape and has a
 * characteristic failure mode, and it is not "it does not work". It is *it did
 * something surprising and there is no lever to find out why* -- the tarpit
 * Relay and Apollo are known for. The surprise is almost never a crash. A query
 * refetched that should not have, or -- far worse, because it is silent -- one
 * did not refetch that should have, and the screen is quietly wrong for as long
 * as the session lasts.
 *
 * The whole justification for declaring tags rather than guessing is that the
 * dependency graph becomes *inspectable*. This package is where that is cashed
 * in. It answers, in order of how much they matter:
 *
 * - **Why did this query NOT refetch?** `whyNotRefetched(key)` puts the tags
 *   the cause raised beside the tags the query carries, shows the intersection,
 *   and when it is empty, names the pairs that came closest and which
 *   declaration to change. A query carrying `Order[]` against a mutation
 *   invalidating `Order:7` is the canonical near miss and is reported as one.
 * - **Why did this query refetch?** `whyRefetched(key)` -- which tag, raised by
 *   which operation, and when.
 * - **What is in the cache for `Order:7`, and at what version?** `entity(key)`.
 * - **Which queries depend on this entity?** `dependents(key)`, or the
 *   `dependents` field of `entity`.
 * - **Which sockets are open, for which channels, with what ref count?**
 *   `sockets()`.
 *
 * Two properties hold everything else up.
 *
 * **Inspection does not mutate.** Not one function here calls `getState`,
 * `fetch`, `read` or `denormalize`. Reading through the inspector does not open
 * a record, does not advance a memo, does not touch the LRU order and does not
 * change what garbage collection will do next. See the note at the top of
 * `inspect.ts` for why that is less obvious than it sounds.
 *
 * **Production pays nothing.** The core's seam is a single nullable field and
 * six optional calls; an optional call does not evaluate its arguments, so with
 * no inspector attached there is not even an allocation. The core keeps no
 * history: the ring buffer, the causal attribution and the analysis all live
 * here, in a package a production build never imports. Import it dynamically
 * behind a development check and a bundler drops both this package and the
 * branch.
 *
 * The UI is deliberately secondary. `./overlay` is a DOM panel -- no framework,
 * no React tree, nothing an Angular or a Vue application has to take a
 * dependency on -- and it is a view over this API and nothing more.
 */

export { createActions } from './actions.js';
export type { DevtoolsActions } from './actions.js';
export { attach } from './devtools.js';
export type { Devtools, DevtoolsOptions } from './devtools.js';
export { argsKey, causeOf, operationName, whyNotRefetched, whyRefetched, wouldInvalidate } from './explain.js';
export type { MissCause } from './explain.js';
export { capture, FrameRing } from './frames.js';
export {
  binderView,
  dependents,
  detail,
  entities,
  entity,
  queries,
  query,
  records,
  snapshot,
  sockets,
  store,
  tags,
} from './inspect.js';
export type { EntityFilter } from './inspect.js';
export { EventLog } from './log.js';
export { nearMisses, parseTag } from './tag.js';
export type { NearMiss, NearMissRelation, ParsedTag } from './tag.js';
export type {
  ActionLog,
  CacheSnapshot,
  CauseSummary,
  EntitySnapshot,
  ErrorLog,
  FetchLog,
  FetchReason,
  FrameCapture,
  FramesLog,
  InvalidatedLog,
  InvalidationPreview,
  LogBase,
  LogDraft,
  LogEntry,
  MissOutcome,
  MissReport,
  MutationLog,
  PlacedLog,
  PrincipalLog,
  QueryDetail,
  QuerySnapshot,
  RecordSnapshot,
  RefetchReport,
  SettleLog,
  StoreSnapshot,
  TagSnapshot,
} from './types.js';
