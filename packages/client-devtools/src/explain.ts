import { resolveTags } from '@forge-go/client-core';
import type { OperationMeta, QueryCache, TagContext } from '@forge-go/client-core';
import type { EventLog } from './log';
import { nearMisses } from './tag';
import type {
  CauseSummary,
  InvalidationPreview,
  LogEntry,
  MissOutcome,
  MissReport,
  RefetchReport,
} from './types';

/**
 * The two questions, and the reason this package exists.
 *
 * "Why did this query refetch" is answered from the log: a request was
 * dispatched, an invalidation preceded it, and a mutation or a frame batch
 * preceded that. It is a matter of bookkeeping.
 *
 * "Why did this query *not* refetch" is the hard one, and it is the one that
 * matters. A missed invalidation produces no event, no error and no request --
 * the screen is simply wrong, and stays wrong, and the developer's only handle
 * on it is a mental model of a tag graph they cannot see. The whole point of
 * tags rather than a heuristic cache is that the graph is *declared*, which
 * means the failure is *inspectable*: put the two tag sets side by side, show
 * where they meet, and when they do not meet, say how they nearly did.
 *
 * Nothing in here runs anything. `wouldInvalidate` resolves an operation's
 * templates through the same `resolveTags` the `Invalidator` uses, against
 * arguments the caller supplies, and asks the registry which mounted queries
 * each resolved tag reaches -- all map reads. Asking "what would happen if I
 * ran this" must not be answered by running it.
 */

/** Truncate an argument key so one enormous request body cannot fill the ring. */
export function argsKey(args: unknown, limit = 200): string {
  let text: string;

  try {
    text = args === undefined ? '' : JSON.stringify(args) ?? String(args);
  } catch {
    // A cyclic or unserialisable argument object. The identity of the query is
    // not worth throwing over.
    text = '[unserialisable]';
  }

  return text.length > limit ? `${text.slice(0, limit)}...` : text;
}

/** `METHOD /path`, the name the cache keys an operation under. */
export function operationName(meta: OperationMeta): string {
  return `${meta.method} ${meta.path}`;
}

/**
 * What this operation would invalidate, and who it would reach.
 *
 * Answers a question that is otherwise only answerable by trying it against a
 * real server: the tags a mutation raises depend on its response, so a
 * developer wanting to know whether `Order[]` is covered has to place a real
 * order to find out. Pass a representative response and this says so for free.
 *
 * `missed` is the interesting column. A tag no query carries is not an error --
 * the query may simply not be open -- but a mutation whose entire `invalidates`
 * list is missed against a screen the developer is looking at is the exact
 * shape of the defect.
 */
export function wouldInvalidate(
  cache: QueryCache,
  meta: OperationMeta,
  args: TagContext = {},
  response?: unknown,
): InvalidationPreview {
  const { tags, unresolved } = resolveTags(meta.invalidates, { ...args, response });
  const hits: { tag: string; queries: string[] }[] = [];
  const missed: string[] = [];

  for (const tag of tags) {
    const queries = cache.registry.queriesFor(tag).map((entry) => entry.key);

    hits.push({ tag, queries: queries.sort() });

    if (queries.length === 0) missed.push(tag);
  }

  return {
    operation: operationName(meta),
    templates: [...meta.invalidates],
    tags,
    unresolved,
    hits,
    missed,
  };
}

/** What a caller may hand `whyNotRefetched` as the thing that should have hit. */
export type MissCause =
  /** An operation, resolved here exactly as the invalidator would resolve it. */
  | { readonly meta: OperationMeta; readonly args?: TagContext; readonly response?: unknown }
  /** Tags already resolved -- a recorded cause, or a hand-written hypothesis. */
  | {
      readonly tags: readonly string[];
      readonly unresolved?: readonly string[];
      readonly label?: string;
      readonly seq?: number;
    };

function summarise(cause: MissCause): CauseSummary {
  if ('meta' in cause) {
    const resolved = resolveTags(cause.meta.invalidates, { ...cause.args, response: cause.response });

    return {
      label: operationName(cause.meta),
      seq: undefined,
      tags: resolved.tags,
      unresolved: resolved.unresolved,
    };
  }

  return {
    label: cause.label ?? 'tags',
    seq: cause.seq,
    tags: [...cause.tags],
    unresolved: cause.unresolved === undefined ? [] : [...cause.unresolved],
  };
}

/** A recorded mutation or frame batch, as a cause an explanation can use. */
export function causeOf(entry: LogEntry): CauseSummary | undefined {
  if (entry.kind === 'mutation') {
    return {
      label: `mutation ${entry.operation}`,
      seq: entry.seq,
      tags: entry.tags,
      unresolved: entry.unresolved,
    };
  }

  if (entry.kind === 'frames') {
    return {
      label: `${String(entry.frames)} stream frame${entry.frames === 1 ? '' : 's'}`,
      seq: entry.seq,
      tags: entry.tags,
      unresolved: [],
    };
  }

  return undefined;
}

/**
 * Why a query did not refetch when a cause went past it.
 *
 * The report is deliberately not a verdict. It is the two tag sets, their
 * intersection, the query's mount state, and -- when the intersection is empty
 * -- the pairs that came closest, each with the declaration to change. A
 * developer who disagrees with the conclusion can still read the evidence,
 * which is the property that separates this from a tool that says "cache miss"
 * and leaves.
 *
 * The five outcomes are five different bugs, and conflating any two of them
 * costs an afternoon:
 *
 * - `missed` -- the tag sets are disjoint. A declaration is wrong. Read
 *   `nearest`.
 * - `stale-while-unmounted` -- they intersected, but nothing has the query
 *   mounted, so no request was made. This is *correct*: refetching data nobody
 *   is looking at is how a smart cache becomes a bandwidth complaint. It
 *   refetches on the next mount. Reported as its own outcome because it is the
 *   single most common false report of a broken cache.
 * - `placed` -- a placement callback answered, and the application supplied the
 *   value the refetch would have produced. If the screen is wrong, the callback
 *   is wrong; no amount of tag work will fix it.
 * - `refetched` -- it did refetch. The problem is elsewhere: the server's
 *   answer, or a component reading something other than this query.
 * - `not-tracked` -- the cache has never heard of this key. Usually a key typed
 *   by hand that does not match the arguments the query is actually mounted
 *   with, since the key includes them.
 */
export function whyNotRefetched(
  cache: QueryCache,
  key: string,
  cause: MissCause,
  log?: EventLog,
): MissReport {
  const summary = summarise(cause);
  const entry = cache.registry.get(key);

  if (entry === undefined) {
    return {
      query: key,
      outcome: 'not-tracked',
      reason:
        `the cache has never heard of \`${key}\`. A query key is its operation plus its ` +
        `arguments, so a key that looks right but was assembled by hand often is not; list ` +
        `\`queries()\` and copy one.`,
      cause: summary,
      mounts: 0,
      settled: false,
      invalidated: summary.tags,
      carried: [],
      matched: [],
      nearest: [],
      suggestions: [],
    };
  }

  const carried = [...entry.tags].sort();
  const matched = summary.tags.filter((tag) => entry.tags.has(tag));
  const nearest = matched.length > 0 ? [] : nearMisses(summary.tags, carried);
  const placed =
    summary.seq !== undefined &&
    log?.last((e) => e.kind === 'placed' && e.query === key && e.seq > (summary.seq as number)) !==
      undefined;

  const outcome: MissOutcome =
    matched.length === 0
      ? 'missed'
      : placed
        ? 'placed'
        : entry.mounts === 0
          ? 'stale-while-unmounted'
          : 'refetched';

  const suggestions: string[] = [];

  if (outcome === 'missed') {
    if (summary.unresolved.length > 0) {
      suggestions.push(
        `${String(summary.unresolved.length)} of this operation's Invalidates templates ` +
          `resolved to nothing and were skipped: ${summary.unresolved.join(', ')}. A template ` +
          `naming \`{res.x}\` needs the response to carry \`x\`; one naming \`{req.x}\` needs the ` +
          `request to. This is the most common cause of an invalidation that silently did not ` +
          `happen.`,
      );
    }

    for (const miss of nearest) suggestions.push(miss.hint);

    if (entry.value === undefined) {
      suggestions.push(
        `\`${key}\` has never settled, so it carries only its \`provides\` templates and none of ` +
          `the entity keys a response would have added. Until it loads, an invalidation of a ` +
          `specific entity cannot reach it.`,
      );
    }

    if (suggestions.length === 0) {
      suggestions.push(
        `nothing the cause raised resembles anything this query carries. Check that the ` +
          `operation declares Invalidates at all -- an empty list invalidates nothing and reports ` +
          `nothing.`,
      );
    }
  }

  return {
    query: key,
    outcome,
    reason: reasonFor(outcome, key, summary, matched, entry.mounts),
    cause: summary,
    mounts: entry.mounts,
    settled: entry.value !== undefined,
    invalidated: summary.tags,
    carried,
    matched,
    nearest,
    suggestions,
  };
}

function reasonFor(
  outcome: MissOutcome,
  key: string,
  cause: CauseSummary,
  matched: readonly string[],
  mounts: number,
): string {
  switch (outcome) {
    case 'missed':
      return (
        `\`${key}\` did not refetch because none of the ${String(cause.tags.length)} tag(s) ` +
        `${cause.label} raised are tags it carries. The two sets are disjoint.`
      );
    case 'stale-while-unmounted':
      return (
        `\`${key}\` was reached by ${matched.join(', ')} and is marked stale, but nothing has it ` +
        `mounted, so no request was made. It refetches the moment it mounts again. This is the ` +
        `cache declining to fetch data nobody is looking at, not a missed invalidation.`
      );
    case 'placed':
      return (
        `\`${key}\` was reached by ${matched.join(', ')}, and a placement callback answered for ` +
        `it, so no refetch was owed. If what it is showing is wrong, the callback is what is ` +
        `wrong -- return \`undefined\` from it to fall back to a refetch.`
      );
    case 'refetched':
      return (
        `\`${key}\` was reached by ${matched.join(', ')} with ${String(mounts)} mount(s), so it ` +
        `did refetch. Whatever is wrong is downstream of the cache.`
      );
    case 'not-tracked':
      return `\`${key}\` is not a query this cache is tracking.`;
  }
}

/**
 * Why a query refetched, from the log.
 *
 * Reads the most recent dispatch for the key and follows the causal link the
 * recorder attached to it. The attribution is made when the events arrive
 * rather than reconstructed here, because the ordering that makes it sound --
 * a mutation event, then synchronously its invalidations, then the batch --
 * only exists at record time.
 */
export function whyRefetched(log: EventLog, key: string): RefetchReport | undefined {
  const dispatch = log.last((entry) => entry.kind === 'fetch' && entry.query === key);

  if (dispatch === undefined || dispatch.kind !== 'fetch') return undefined;

  const hit = log.last(
    (entry) => entry.kind === 'invalidated' && entry.query === key && entry.seq < dispatch.seq,
  );
  const matched = hit !== undefined && hit.kind === 'invalidated' ? hit.matched : [];
  const causeEntry = dispatch.cause === undefined ? undefined : log.find(dispatch.cause);
  const cause = causeEntry === undefined ? undefined : causeOf(causeEntry);

  return {
    query: key,
    at: dispatch.at,
    reason: dispatch.reason,
    cause,
    matched,
    summary: summaryFor(key, dispatch.reason, cause, matched),
  };
}

function summaryFor(
  key: string,
  reason: RefetchReport['reason'],
  cause: CauseSummary | undefined,
  matched: readonly string[],
): string {
  if (reason === 'mount') return `\`${key}\` was fetched because it was mounted for the first time.`;

  if (reason === 'invalidation' && cause !== undefined) {
    return (
      `\`${key}\` refetched because ${cause.label} invalidated ` +
      `${cause.tags.join(', ')}, of which it carries ${matched.join(', ')}.`
    );
  }

  if (reason === 'invalidation') {
    return (
      `\`${key}\` refetched because an invalidation reached it through ${matched.join(', ')}. ` +
      `The cause is older than the log's window.`
    );
  }

  return (
    `\`${key}\` refetched without an invalidation behind it -- an explicit \`refetch()\`, a ` +
    `remount onto a stale entry, or stream gap recovery after a reconnect.`
  );
}
