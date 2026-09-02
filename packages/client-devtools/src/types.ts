import type { NearMiss } from './tag.js';

/**
 * What the inspector reports, and what the log records.
 *
 * Everything here is a **copy**. Nothing in this file aliases a live registry
 * entry, a live entity record or a live socket: the inspector's first rule is
 * that reading the cache must not change it, and handing out the store's own
 * objects would make an accidental write from a devtools panel indistinguishable
 * from a cache bug.
 */

/** One query the registry remembers, mounted or not. */
export interface QuerySnapshot {
  readonly key: string;
  /** `METHOD /path`, as the cache keys it. */
  readonly operation: string;
  /** The arguments this query was called with. Treat as frozen. */
  readonly args: unknown;
  /** How many places have it mounted. Zero means nobody is watching. */
  readonly mounts: number;
  /** Known to be behind the server. Refetches now if mounted, on mount if not. */
  readonly stale: boolean;
  /** Whether a response has ever settled into it. */
  readonly settled: boolean;
  /** `ops[x].provides`, still as templates. */
  readonly provides: readonly string[];
  /** Everything it provides: resolved templates plus its entity dependencies. */
  readonly tags: readonly string[];
  /** The entity keys its skeleton reached. */
  readonly deps: readonly string[];
  /** The invalidation clock reading at its last settle. */
  readonly settledAt: number;
}

/**
 * One query, joined across the registry and the record.
 *
 * `QuerySnapshot` is what the registry knows: what this query provides, and
 * whether it is behind the server. This adds what the record knows, which is
 * what it is doing right now. The detail pane needs both, and neither half is
 * reachable from the other.
 *
 * `value` is a bounded copy of the last settled response, not the live value
 * and not a rehydrated one: reading it must not build a store memo.
 */
export interface QueryDetail extends QuerySnapshot {
  readonly status: 'idle' | 'pending' | 'success' | 'error';
  readonly fetching: boolean;
  /** Reduced to a message. The error object itself is never retained. */
  readonly error: string | undefined;
  /** A request sequence is in flight. */
  readonly inflight: boolean;
  readonly restart: boolean;
  readonly frameRestarts: number;
  /** The last settled response, copied and capped. */
  readonly value: unknown;
}

/** One entity record, as it sits in the store. */
export interface EntitySnapshot {
  readonly key: string;
  readonly type: string;
  readonly id: string;
  /** Bumps only when the record's data actually moved. */
  readonly version: number;
  /** The frame-clock reading when a stream frame last wrote it. 0 if none did. */
  readonly frameAt: number;
  /** The record's fields. References appear as `{__ref: 'Order:7'}`. */
  readonly fields: Readonly<Record<string, unknown>>;
  /** Entity keys this record points at, one hop. */
  readonly refs: readonly string[];
  /**
   * Query keys whose settled result reached this entity.
   *
   * Only filled in by `entity(key)`. Listing it for every record would be
   * quadratic, and a store with fifty thousand records is an ordinary one.
   */
  readonly dependents: readonly string[];
}

/** One tag, and who is on either side of it. */
export interface TagSnapshot {
  readonly tag: string;
  /** Query keys carrying it, mounted or not. */
  readonly carriers: readonly string[];
  /** Of those, the ones an invalidation would reach right now. */
  readonly mounted: readonly string[];
}

/** The counters that say whether anything is leaking. */
export interface StoreSnapshot {
  /** Entity records held. */
  readonly records: number;
  /** Total record writes. Bumps only on real change. */
  readonly version: number;
  /** How many stream-frame batches have committed. */
  readonly frameVersion: number;
  /** Frame-evicted keys still holding a stamp. Bounded; see `EntityStore`. */
  readonly tombstones: number;
  /** Queries the cache is tracking, watched or merely remembered. */
  readonly tracked: number;
  /** Queries the registry remembers. */
  readonly remembered: number;
  /** Of those, the ones with at least one mount. */
  readonly mounted: number;
  /** Tags with at least one mounted query. */
  readonly indexedTags: number;
  /** Tags holding an invalidation stamp. Bounded; see `QueryRegistry`. */
  readonly stampedTags: number;
}

/** Everything, in one read. */
export interface CacheSnapshot {
  readonly store: StoreSnapshot;
  readonly queries: readonly QuerySnapshot[];
  readonly tags: readonly TagSnapshot[];
}

/**
 * One recorded event.
 *
 * `seq` is monotonic for the life of the devtools and survives the ring buffer
 * wrapping, so a `cause` reference to an entry that has since been dropped is
 * recognisable as such rather than pointing at whatever now occupies the slot.
 *
 * `session` counts identity changes. Every entry carries the session it
 * happened in, because a log that spans a login change otherwise shows two
 * principals' traffic as one timeline -- and the cache in between them was
 * emptied, so the earlier half explains nothing about the current one.
 */
export interface LogBase {
  readonly seq: number;
  readonly at: number;
  readonly session: number;
}

/** A mutation settled, with the tags it actually raised. A cause. */
export interface MutationLog extends LogBase {
  readonly kind: 'mutation';
  /** `METHOD /path`. */
  readonly operation: string;
  /** The cache key its arguments produce, truncated. Never the response. */
  readonly args: string;
  /** `invalidates`, resolved against the arguments and the response. */
  readonly tags: readonly string[];
  /**
   * Templates that resolved to nothing and were skipped.
   *
   * The single most common cause of an invalidation that silently did not
   * happen, and invisible without this.
   */
  readonly unresolved: readonly string[];
}

/** A batch of stream frames committed, with the tags it raised. Also a cause. */
export interface FramesLog extends LogBase {
  readonly kind: 'frames';
  readonly frames: number;
  readonly tags: readonly string[];
}

/** One mounted query was reached, by these tags, because of `cause`. */
export interface InvalidatedLog extends LogBase {
  readonly kind: 'invalidated';
  readonly query: string;
  readonly matched: readonly string[];
  /** The `seq` of the mutation or frame batch responsible, when known. */
  readonly cause: number | undefined;
}

/** A placement callback answered, so this query was not refetched. */
export interface PlacedLog extends LogBase {
  readonly kind: 'placed';
  readonly query: string;
  readonly cause: number | undefined;
}

/** Why a request went out. */
export type FetchReason =
  /** The query was hit by an invalidation. `cause` says by what. */
  | 'invalidation'
  /** Nobody had asked for this query before. */
  | 'mount'
  /** A refetch with no invalidation behind it: `refetch()`, or gap recovery. */
  | 'manual';

/** A request was dispatched for a query. */
export interface FetchLog extends LogBase {
  readonly kind: 'fetch';
  readonly query: string;
  readonly reason: FetchReason;
  readonly cause: number | undefined;
}

/** A response settled into a query. */
export interface SettleLog extends LogBase {
  readonly kind: 'settle';
  readonly query: string;
  /** The store's write counter afterwards. Unmoved means nothing changed. */
  readonly version: number;
}

/** A request failed. The message only: an error object retains its stack. */
export interface ErrorLog extends LogBase {
  readonly kind: 'error';
  readonly query: string;
  readonly message: string;
}

/** The identity changed and the whole cache was dropped. */
export interface PrincipalLog extends LogBase {
  readonly kind: 'principal';
}

export type LogEntry =
  | MutationLog
  | FramesLog
  | InvalidatedLog
  | PlacedLog
  | FetchLog
  | SettleLog
  | ErrorLog
  | PrincipalLog;

/**
 * One entry as the recorder hands it over: everything but the two fields the
 * log itself stamps.
 *
 * Written as a distributive conditional rather than `Omit<LogEntry, ...>`,
 * because `Omit` over a union collapses it to the fields the members share --
 * which here is `session` and nothing else, so every call site would be
 * rejected for specifying `query`.
 */
export type LogDraft = LogEntry extends infer T
  ? T extends LogEntry
    ? Omit<T, 'seq' | 'at'>
    : never
  : never;

/** What raised a set of tags, reduced to what an explanation needs. */
export interface CauseSummary {
  /** `mutation POST /orders`, `frames x3`, or whatever the caller labelled it. */
  readonly label: string;
  /** The `seq` of the log entry it came from, when it came from one. */
  readonly seq: number | undefined;
  readonly tags: readonly string[];
  readonly unresolved: readonly string[];
}

/** What happened to a query when a cause went past it. */
export type MissOutcome =
  /** The tags intersected and a request went out. Working as intended. */
  | 'refetched'
  /** The tags intersected; a placement callback answered instead of a refetch. */
  | 'placed'
  /**
   * The tags intersected, but nothing has the query mounted, so no request was
   * made. It is marked stale and will refetch the moment it mounts again.
   */
  | 'stale-while-unmounted'
  /** The tag sets are disjoint. Nothing reached the query at all. */
  | 'missed'
  /** The cache has never heard of this query key. */
  | 'not-tracked';

/** The answer to "why did this query not refetch". */
export interface MissReport {
  readonly query: string;
  readonly outcome: MissOutcome;
  /** One sentence. The thing to read first. */
  readonly reason: string;
  readonly cause: CauseSummary;
  /** How many places have the query mounted. */
  readonly mounts: number;
  /** Whether a response has ever settled into it. */
  readonly settled: boolean;
  /** What the cause raised. */
  readonly invalidated: readonly string[];
  /** What the query carries. */
  readonly carried: readonly string[];
  /** Where the two sets meet. Empty is the whole of the problem. */
  readonly matched: readonly string[];
  /** Where they nearly meet, most suspicious first. */
  readonly nearest: readonly NearMiss[];
  /** Concrete things to change, deduplicated. */
  readonly suggestions: readonly string[];
}

/** The answer to "why did this query refetch". */
export interface RefetchReport {
  readonly query: string;
  /** When the request was dispatched, on the injected clock. */
  readonly at: number;
  readonly reason: FetchReason;
  /** The mutation or frame batch responsible, when there was one. */
  readonly cause: CauseSummary | undefined;
  /** Which of the query's tags the cause reached. */
  readonly matched: readonly string[];
  /** One sentence. */
  readonly summary: string;
}

/** What an operation *would* invalidate, asked without running it. */
export interface InvalidationPreview {
  readonly operation: string;
  /** `invalidates`, as declared. */
  readonly templates: readonly string[];
  /** Those that resolved. */
  readonly tags: readonly string[];
  /** Those that did not, and would be skipped. */
  readonly unresolved: readonly string[];
  /** Per tag, the mounted queries it would reach. */
  readonly hits: readonly { readonly tag: string; readonly queries: readonly string[] }[];
  /** Tags no query carries at all. A refetch nobody asked for. */
  readonly missed: readonly string[];
}
