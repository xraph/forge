import { QueryRegistry } from './registry.js';
import type { QueryEntry } from './registry.js';
import { resolveTags } from './tags.js';
import type { TagContext } from './tags.js';

/**
 * When a batch runs.
 *
 * A function rather than an object because there is only ever one thing to
 * ask of it, and because the two implementations that matter -- a microtask
 * and "when the test says so" -- are each one line.
 */
export type Scheduler = (flush: () => void) => void;

/**
 * The default: one batch per microtask.
 *
 * Every invalidation raised synchronously in the same turn -- a mutation
 * settling, or three of them in a `Promise.all` -- lands in one batch, and the
 * batch runs before the browser can paint. Not a timer: a timeout that is
 * "long enough" on a developer's machine is a flake on a loaded CI box and a
 * visible delay on a slow phone.
 */
export const microtaskScheduler: Scheduler = (flush) => {
  // `Promise.resolve().then` rather than `queueMicrotask`, which is a host
  // global this package would otherwise have to assume exists and be typed for.
  void Promise.resolve().then(flush);
};

/** A scheduler a test drives by hand, plus the handle to drive it with. */
export interface ManualScheduler {
  readonly schedule: Scheduler;
  /** Run the pending batch, if there is one. */
  flush(): void;
  /** Whether a batch is waiting. */
  pending(): boolean;
}

/**
 * A scheduler that runs nothing until asked.
 *
 * This is what makes the coalescing window testable without sleeping. A test
 * that waits 10ms for a batch to settle passes forty-nine runs out of fifty
 * and is deleted after the fiftieth. Also serviceable in SSR, where there is
 * no paint to batch against and the caller wants the flush at a point it
 * chooses.
 */
export function manualScheduler(): ManualScheduler {
  let queued: (() => void) | undefined;

  return {
    schedule: (flush) => {
      queued = flush;
    },
    flush() {
      const run = queued;
      queued = undefined;
      run?.();
    },
    pending() {
      return queued !== undefined;
    },
  };
}

/**
 * The placement escape hatch, declared per tag at the mutation site.
 *
 * `created` is what the mutation produced, `current` is what the query is
 * holding now, `args` are the query's own arguments -- the filter, the page,
 * the sort. Return the new list to place the entity yourself and skip the
 * refetch; return `undefined` to say *I don't know* and fall back to it.
 *
 * That third answer is the whole point. The runtime cannot decide whether a
 * new `Order` belongs inside a filtered or paginated window without modelling
 * the server's query semantics, which is the tarpit Relay's connection
 * directives are known for. Here it never tries, and the application is
 * allowed to decline for the cases it cannot decide either.
 */
export type Placement = (
  created: unknown,
  current: unknown,
  args: TagContext,
) => unknown[] | undefined;

/** A settled mutation, as the generated facade reports it. */
export interface MutationSettled {
  /** `ops[operation].invalidates`, still as templates. */
  readonly invalidates?: readonly string[];
  /** The mutation's own arguments, for resolving `{req.x}` and bare `{x}`. */
  readonly args?: TagContext;
  /** The mutation's response, for resolving `{res.a.b}`. */
  readonly response?: unknown;
  /** What to hand placement callbacks. Defaults to the response. */
  readonly created?: unknown;
  /** Per-tag placement callbacks. */
  readonly place?: Readonly<Record<string, Placement>>;
}

export interface InvalidatorOptions {
  /**
   * Refetch these queries. One call per batch.
   *
   * Injected rather than performed here: this chunk decides *which* queries
   * are behind and *when*, and the transport chunk decides what a refetch is.
   * That split is also what makes every test below run with no network.
   */
  readonly execute: (batch: readonly QueryEntry[]) => void;
  readonly scheduler?: Scheduler;
  /**
   * A tag template resolved to nothing, and the tag was skipped.
   *
   * The default warns once per template. Skipping is the decision because the
   * three alternatives are worse: emitting `Customer:` is the silent failure
   * the design forbids, throwing turns a cache defect into an application
   * error for a write the server has already committed, and dropping the whole
   * `invalidates` list would lose the tags that did resolve. Generation-time
   * validation is where an unresolvable template is supposed to be caught;
   * this is the runtime's report that one got past it.
   */
  readonly onUnresolved?: (template: string, context: string) => void;
  /** A placement callback threw, or the executor did. */
  readonly onError?: (error: unknown, context: string) => void;
  /** A placement callback answered, and this query will not be refetched. */
  readonly onPlace?: (entry: QueryEntry, value: unknown[]) => void;
  /**
   * This query was hit, reported **synchronously**, before anything is decided
   * about what to do with it.
   *
   * `execute` is the wrong place for a holder of in-flight requests to learn
   * that a query went stale, and for two independent reasons. Placement
   * `continue`s past the queue entirely, so a query answered by a callback
   * never reaches a batch at all. And the batch itself runs on the scheduler,
   * which by default is a microtask -- long enough for a request dispatched
   * *before* this invalidation to arrive and commit an answer that predates
   * the write.
   *
   * Both are the same bug: the moment a query is known to be behind is this
   * one, and a consumer that owns in-flight requests has to hear about it
   * here. `execute` still decides what to *fetch*; this only says what is now
   * known to be stale.
   */
  readonly onInvalidated?: (entry: QueryEntry, matched: Set<string>) => void;
}

/**
 * Turns "this mutation invalidates `Order[]`" into "these three mounted
 * queries must refetch".
 *
 * Owns the policy; `QueryRegistry` owns the state. What lands here is tag
 * resolution, the placement negotiation, coalescing, and the decision not to
 * refetch things nobody is looking at.
 */
export class Invalidator {
  private readonly queue = new Set<QueryEntry>();
  private scheduled = false;
  private readonly schedule: Scheduler;
  private readonly execute: (batch: readonly QueryEntry[]) => void;
  private readonly onUnresolved: (template: string, context: string) => void;
  private readonly onError: ((error: unknown, context: string) => void) | undefined;
  private readonly onPlace: ((entry: QueryEntry, value: unknown[]) => void) | undefined;
  private readonly onInvalidated:
    | ((entry: QueryEntry, matched: Set<string>) => void)
    | undefined;

  constructor(
    readonly registry: QueryRegistry,
    options: InvalidatorOptions,
  ) {
    this.execute = options.execute;
    this.schedule = options.scheduler ?? microtaskScheduler;
    this.onUnresolved = options.onUnresolved ?? warnUnresolved;
    this.onError = options.onError;
    this.onPlace = options.onPlace;
    this.onInvalidated = options.onInvalidated;

    // A query that mounts having been invalidated while it was unmounted
    // reaches the batch through the same queue as everything else, so it
    // refetches exactly once no matter how many tags went stale meanwhile.
    registry.onStale = (entry) => this.enqueue(entry);
    registry.onUnresolved ??= (template, entry) =>
      this.onUnresolved(template, `${entry.operation} provides`);
  }

  /**
   * A mutation settled. Resolve its tags, then apply them.
   *
   * Nothing here is specific to a mutation: a stream frame is a mutation the
   * client did not initiate, and takes the same path when that chunk lands.
   */
  settled(mutation: MutationSettled): void {
    const args = mutation.args ?? {};
    const context: TagContext = { ...args, response: mutation.response };
    const { tags, unresolved } = resolveTags(mutation.invalidates ?? [], context);

    for (const template of unresolved) this.onUnresolved(template, 'invalidates');

    this.apply(tags, mutation);
  }

  /** Invalidate tags that are already resolved. */
  invalidate(tags: Iterable<string>): void {
    this.apply(tags, {});
  }

  /**
   * Run the pending batch now, whatever the scheduler had planned.
   *
   * For a caller that has to observe the effect before yielding -- an SSR
   * pass, a test that would rather not await.
   */
  flush(): void {
    this.scheduled = false;

    // Unmounted between the invalidation and this flush. Refetching data
    // nobody is looking at is how a smart cache becomes a bandwidth
    // complaint; it stays stale and refetches if it mounts again.
    const batch = [...this.queue].filter((entry) => entry.mounts > 0 && entry.stale);

    this.queue.clear();

    if (batch.length === 0) return;

    try {
      this.execute(batch);
    } catch (error) {
      this.onError?.(error, 'execute');
    }
  }

  private apply(tags: Iterable<string>, mutation: MutationSettled): void {
    for (const [entry, matched] of this.registry.invalidated(tags)) {
      // Before placement is even attempted, and before anything is queued: a
      // consumer holding a request that was dispatched before this moment has
      // to learn about it now. See `onInvalidated`.
      this.onInvalidated?.(entry, matched);

      const placed = this.place(entry, matched, mutation);

      if (placed !== undefined) {
        this.registry.place(entry, placed);
        this.onPlace?.(entry, placed);
        continue;
      }

      this.registry.markStale(entry);
    }
  }

  /**
   * Ask the mutation's placement callbacks to answer for this query.
   *
   * All or nothing, per query: a query hit by `Order[]` and `Customer:3` where
   * only the first has a callback still refetches, because the second
   * invalidation is genuinely unhandled and placing the first would leave the
   * query looking updated while being wrong. Callbacks chain, so a query
   * matched by two tags that both place sees the first result as `current`
   * for the second.
   */
  private place(
    entry: QueryEntry,
    matched: Set<string>,
    mutation: MutationSettled,
  ): unknown[] | undefined {
    const callbacks = mutation.place;

    if (callbacks === undefined) return undefined;

    const created = 'created' in mutation ? mutation.created : mutation.response;
    let current = entry.value;
    let placed: unknown[] | undefined;

    for (const tag of matched) {
      const callback = callbacks[tag];

      if (callback === undefined) return undefined;

      let next: unknown[] | undefined;

      try {
        next = callback(created, current, entry.args);
      } catch (error) {
        // One application callback must not take the batch down with it. The
        // query falls back to a refetch, which is the answer `undefined`
        // would have given, and the throw is reported rather than swallowed.
        this.onError?.(error, `place ${tag}`);

        return undefined;
      }

      if (next === undefined) return undefined;

      current = next;
      placed = next;
    }

    return placed;
  }

  /**
   * Queue one query, and make sure exactly one batch is scheduled.
   *
   * The set is what makes N invalidations in a tick one batch and a query hit
   * by two tags one refetch; the flag is what makes it one scheduled callback
   * rather than N that each find an empty queue.
   */
  private enqueue(entry: QueryEntry): void {
    this.queue.add(entry);

    if (this.scheduled) return;

    this.scheduled = true;
    this.schedule(() => {
      if (this.scheduled) this.flush();
    });
  }
}

const warned = new Set<string>();

function warnUnresolved(template: string, context: string): void {
  if (warned.has(template)) return;

  warned.add(template);

  const host = globalThis as { console?: { warn?: (message: string) => void } };

  host.console?.warn?.(
    `[forge] tag template ${template} (${context}) resolved to nothing and was skipped`,
  );
}
