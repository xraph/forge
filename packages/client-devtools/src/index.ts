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
 * - **What is this one query actually doing right now?** `detail(key)`, which
 *   joins the registry entry to the record. `records()` is the same fields for
 *   every tracked query at once, minus the settled response, for anything that
 *   wants a column rather than a pane.
 *
 * It also *does* things, which is the part the list above does not cover.
 * `actions` refetches one query, invalidates it, raises a tag by hand, evicts
 * one entity, drops one query or clears the cache -- the six a panel with
 * buttons needs, and the six a console session reaches for.
 *
 * Two properties hold everything else up.
 *
 * **Inspection does not mutate.** Not one function on the read side calls
 * `getState`, `fetch`, `read` or `denormalize`. Reading through the inspector
 * does not open a record, does not advance a memo, does not touch the LRU
 * order and does not change what garbage collection will do next. See the note
 * at the top of `inspect.ts` for why that is less obvious than it sounds.
 *
 * The mutating half is real and it is exactly one file. `actions.ts` holds all
 * of it, on purpose and not as an accident of layout: kept there, the rule
 * over in `inspect.ts` is literally true of that file rather than mostly true
 * of the package, and the whole set of calls that can move a cache from here
 * fits on one screen where it can be read and argued with. Nothing in it
 * fabricates a state a response could not produce, and every call records
 * itself into the same log, in the same order, as the events it goes on to
 * cause.
 *
 * **Production pays nothing.** The core's seam is a single nullable field and
 * five optional calls; an optional call does not evaluate its arguments, so with
 * no inspector attached there is not even an allocation. The core keeps no
 * history: the ring buffer, the causal attribution and the analysis all live
 * here, in a package a production build never imports. Import it dynamically
 * behind a development check and a bundler drops both this package and the
 * branch. The one thing here that retains a payload -- the frame ring -- is
 * off unless asked for.
 *
 * The UI is deliberately secondary, and there are two of them, chosen rather
 * than layered. `./overlay` is the lean one, six read-only tables and a filter
 * box. `./panel` adds the detail pane, the action bar and the stream and frame
 * views. Both are `document.createElement` in a shadow root -- no framework, no
 * React tree, nothing an Angular or a Vue application has to take a dependency
 * on -- and both are views over this API and nothing more.
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
