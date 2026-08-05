import { QueryCache } from './cache';
import { microtaskScheduler } from './invalidate';
import type { Scheduler } from './invalidate';
import { entityKey, isIdentity } from './ref';
import { SubscriptionManager } from './stream';
import type { StreamBinding } from './stream';
import { resolveTags } from './tags';
import type { TagContext } from './tags';
import type { OperationMeta } from './transport';
import type { EntityKey } from './types';

/** One stream frame, matched to its manifest binding. See `applyFrames`. */
export interface StreamFrame {
  readonly binding: StreamBinding;
  /** The decoded message payload -- the entity, or its identity for an evict. */
  readonly payload: unknown;
}

/** How a batch of frames reports a failure it survived. */
export interface ApplyFramesOptions {
  readonly onError?: (error: unknown, context: string) => void;
}

/**
 * Commit a batch of stream frames: the mutation path, for a write the client
 * did not initiate.
 *
 * A free function over the cache's public surface rather than a method on it,
 * for two reasons that point the same way. It keeps the streams-only code out
 * of a REST-only bundle -- `QueryCache` is pulled in whole by anyone who
 * imports it, and this is measurably 0.19 kB gzipped. And it makes the streams
 * surface *explicit*, rather than bolting a second top-level write method onto
 * `QueryCache` beside `mutate` and hoping the reader notices which one streams
 * use.
 *
 * It is emphatically not a second apply path. Every line below has a
 * counterpart in `mutate`, and goes through the same public seams: the same
 * `EntityStore` with the same `entities` table, the same `notifyChanged`, and
 * the same `Invalidator` reached through `cache.invalidate`. A socket frame
 * *is* a mutation somebody else performed.
 *
 * The three intents:
 *
 * - `upsert` and `patch` normalize the payload and merge it in. Dependent
 *   queries re-render off the store with no request, which is the whole claim
 *   of the design: `order.updated` costs nothing. They differ only in what the
 *   generator put in `invalidates` -- a created order changes list membership
 *   and the server says so, an updated one does not.
 * - `evict` drops the record, leaves a tombstone so a response already in
 *   flight cannot resurrect the row, **and raises `${entity}[]` whatever the
 *   binding declared**. See `evictionTags`.
 *
 * **The ordering guarantee.** The whole batch takes one reading of the store's
 * frame clock and stamps every record it writes with it. A request records the
 * reading at dispatch; when its response arrives it is normalized but not
 * committed, and if any entity it carries has been stamped by a frame since,
 * that data does not commit. So: **a committed write never overwrites an entity
 * with a value older than a frame the client has already applied.** Which of
 * the two arrives last on the wire does not decide it, because arrival order is
 * not the order the facts happened in.
 *
 * How the loser converges differs by path, and the difference is deliberate:
 *
 * - A **query** response is discarded and the request re-run, because re-reading
 *   is free and idempotent. One re-run per frame, not a loop -- the re-run
 *   postdates the frame -- bounded by `frameRestarts`, past which it falls back
 *   to the rule below.
 * - A **mutation** response is *never* re-issued; retrying a write is the
 *   duplicate-orders hazard. It commits around the raced keys instead: the
 *   entities the frame touched keep the frame's value, everything else lands.
 *
 * Note what is *not* claimed. Two frames on one channel apply in arrival order,
 * because the transport is ordered and the client has no server clock to do
 * better with; frames on two different channels have no defined order relative
 * to each other, for the same reason. And the guarantee is per entity, not per
 * field: a raced record is skipped whole.
 */
export function applyFrames(
  cache: QueryCache,
  frames: readonly StreamFrame[],
  options: ApplyFramesOptions = {},
): void {
  if (frames.length === 0) return;

  // One reading for the batch. Frames coalesced into a single commit are one
  // event as far as ordering is concerned, and stamping them individually would
  // be a distinction nothing can observe.
  const stamp = cache.store.nextFrame();
  const tags = new Set<string>();

  for (const frame of frames) {
    const { binding, payload } = frame;

    if (binding.intent === 'evict') {
      const key = identify(cache, binding.entity, payload);

      if (key !== undefined) cache.store.evict(key, stamp);

      for (const tag of evictionTags(binding)) tags.add(tag);
    } else {
      cache.store.write(payload, cache.entities, binding.entity, { frameAt: stamp });
    }

    const context: TagContext = { body: payload, response: payload };
    const resolved = resolveTags(binding.invalidates, context);

    for (const tag of resolved.tags) tags.add(tag);
    for (const template of resolved.unresolved) {
      const report = options.onError ?? ((error, where) => cache.report(error, where));

      report(
        new Error(`[forge] stream tag ${template} (${binding.message}) resolved to nothing`),
        'frame',
      );
    }
  }

  // Before the invalidation, so a query the batch is about to refetch is holding
  // the post-frame value while it does -- and so a pure `patch`, whose
  // `invalidates` is empty by design, still reaches its subscribers. Nothing
  // else would tell them: no request is owed, so nothing settles.
  cache.notifyChanged();

  if (tags.size > 0) cache.invalidate(tags);
}

/**
 * The tags a delete raises, whatever the manifest said.
 *
 * `${entity}[]` is synthesized rather than trusted from the binding, because
 * the generator passes `Invalidates` through verbatim -- it does not synthesize
 * one -- so `forge.Emits[Order]("order.deleted").Invalidates()` reaches the
 * client declaring nothing at all. That leaves every mounted list holding a
 * reference to a record that no longer exists, with nothing scheduled to repair
 * it: measured at one transport call and a permanently short list.
 *
 * An eviction is an entity-level event, and an entity-level event necessarily
 * changes the membership of every collection that contained it. That is knowable
 * here without the server saying so, and `recover` already relies on exactly the
 * same reasoning for a reconnect. Applying it in one of the two places was the
 * defect.
 *
 * Deliberately *not* done for `upsert` or `patch`. A patch changes no membership,
 * and synthesizing `Order[]` for `order.updated` would refetch every mounted
 * list on every update -- destroying the property that makes live queries worth
 * having.
 */
function evictionTags(binding: StreamBinding): readonly string[] {
  return [`${binding.entity}[]`];
}

/**
 * The entity key a stream payload identifies, for the evict intent.
 *
 * A delete frame carries either the record (`{id: 7, ...}`) or the bare identity
 * (`7`), and both are ordinary on the wire. Anything else -- a typename with no
 * entry in the schema, an id that is not identity-shaped -- resolves to nothing
 * and the frame is skipped rather than evicting under a guessed key, which would
 * delete somebody else's row.
 */
function identify(cache: QueryCache, type: string, payload: unknown): EntityKey | undefined {
  if (isIdentity(payload)) return entityKey(type, payload);

  const meta = cache.entities[type];

  if (meta === undefined || payload === null || typeof payload !== 'object') return undefined;

  const id = (payload as Record<string, unknown>)[meta.idField];

  return isIdentity(id) ? entityKey(type, id) : undefined;
}

/** A frame pulled apart into the two things a binding is looked up by. */
export interface DecodedFrame {
  /** The message name, e.g. `order.created`. */
  readonly message: string;
  /** What the message carried. */
  readonly payload: unknown;
  /**
   * The channel, when the envelope names one.
   *
   * Only consulted when several channels are multiplexed over one socket and
   * the same message name is bound on more than one of them. Otherwise the
   * channel the frame arrived on is the answer.
   */
  readonly channel?: string;
}

/**
 * Pull a message apart. Injected, because the envelope is the server's
 * business rather than this package's.
 *
 * Returning `undefined` means "not a frame this runtime should look at", and is
 * how a transport-level keepalive or an ack is dropped without a warning.
 */
export type FrameDecoder = (message: unknown) => DecodedFrame | undefined;

const INTENTS = new Set<string>(['upsert', 'patch', 'evict']);

/**
 * The default envelope reader, over the three shapes in circulation.
 *
 * `type`/`payload` is what the Forge WebSocket handlers emit; `event`/`data` is
 * what an SSE adapter naturally produces, since `EventSource` dispatches by
 * event name; `name` is the AsyncAPI spelling. A message with a name and no
 * payload field is its own payload, which is what a server that sends the
 * entity flat with a `type` discriminator produces.
 */
export const decodeFrame: FrameDecoder = (message) => {
  if (message === null || typeof message !== 'object') return undefined;

  const envelope = message as Record<string, unknown>;
  const name = envelope['type'] ?? envelope['event'] ?? envelope['name'];

  if (typeof name !== 'string' || name === '') return undefined;

  const payload =
    'payload' in envelope ? envelope['payload'] : 'data' in envelope ? envelope['data'] : envelope;
  const channel = envelope['channel'];

  return typeof channel === 'string'
    ? { message: name, payload, channel }
    : { message: name, payload };
};

/**
 * When a batch of frames is committed.
 *
 * One commit per animation frame, so a channel at 200 msg/s is 60 renders
 * rather than 200. Falls back to a microtask where there is no `requestAnimationFrame`
 * -- Node, a worker, a test environment -- which coalesces a burst that arrives
 * in one turn and is the honest best available when there is no paint to batch
 * against.
 */
export const animationFrameScheduler: Scheduler = (flush) => {
  const host = globalThis as { requestAnimationFrame?: (callback: () => void) => unknown };

  if (typeof host.requestAnimationFrame === 'function') {
    host.requestAnimationFrame(() => {
      flush();
    });

    return;
  }

  microtaskScheduler(flush);
};

export interface StreamBinderOptions {
  readonly cache: QueryCache;
  /** The generated manifest's `streams` table. */
  readonly streams: readonly StreamBinding[];
  readonly manager: SubscriptionManager;
  /** When a batch of frames commits. Defaults to one per animation frame. */
  readonly scheduler?: Scheduler;
  readonly decode?: FrameDecoder;
  /**
   * A message arrived that no binding claims.
   *
   * The default warns once per `(channel, message)` in development and does
   * nothing else. Throwing would be wrong on its face: the server deploys ahead
   * of the client, always, and a client that falls over on the first message
   * type it has not been regenerated for turns every additive backend release
   * into an outage.
   */
  readonly onUnknown?: (message: string, channel: string) => void;
  readonly onError?: (error: unknown, context: string) => void;
}

/** One live query, held while its subscription is. */
interface LiveQuery {
  readonly meta: OperationMeta;
  readonly args: TagContext | undefined;
  refs: number;
}

/**
 * Binds a channel to the cache: decode, match, apply, and recover the gap.
 *
 * The manifest is the whole of the configuration. A binding says which entity a
 * message carries, what it does to it, and what else it invalidates; this class
 * does the lookup and hands the result to `applyFrames` above, which is the
 * mutation path. Nothing on this class writes to the store.
 *
 * **One binder per manager.** The constructor claims the manager's
 * `onReconnect`, which is the gap-recovery trigger, so a second binder over the
 * same manager takes it -- and the first one silently stops recovering, which
 * is the failure this whole layer exists to make impossible. Wiring it here
 * rather than asking the caller to is the trade: it cannot be left unwired, and
 * in exchange it must not be wired twice.
 */
export class StreamBinder {
  /** The manager this binder claimed. See the note on sharing, above. */
  readonly manager: SubscriptionManager;

  private readonly cache: QueryCache;
  private readonly schedule: Scheduler;
  private readonly decode: FrameDecoder;
  private readonly onUnknown: (message: string, channel: string) => void;
  private readonly onError: ((error: unknown, context: string) => void) | undefined;

  /** The `slot` key for a (channel, message) pair, to its binding. */
  private readonly bindings = new Map<string, StreamBinding>();
  /** Every binding on a channel, for gap recovery. */
  private readonly byChannel = new Map<string, StreamBinding[]>();
  /** Which channels carry an entity, for `live`. */
  private readonly byEntity = new Map<string, string[]>();
  /** `channelsFor`, memoized by root typename. The schema does not change. */
  private readonly reach = new Map<string, readonly string[]>();

  /** Frames waiting for the next commit, with the identity they arrived under. */
  private queue: { readonly frame: StreamFrame; readonly owner: unknown }[] = [];
  private scheduled = false;

  /** Mounted live queries, by channel and then by cache key. */
  private readonly live = new Map<string, Map<string, LiveQuery>>();

  private readonly unwatch: () => void;

  constructor(options: StreamBinderOptions) {
    this.cache = options.cache;
    this.manager = options.manager;
    this.schedule = options.scheduler ?? animationFrameScheduler;
    this.decode = options.decode ?? decodeFrame;
    this.onUnknown = options.onUnknown ?? warnUnknown;
    this.onError = options.onError;

    for (const binding of options.streams) {
      if (!INTENTS.has(binding.intent)) {
        // A manifest this client cannot act on. Reported once, at wiring time,
        // rather than once per frame at three in the morning.
        this.onUnknown(`${binding.message} (intent ${binding.intent})`, binding.channel);

        continue;
      }

      this.bindings.set(slot(binding.channel, binding.message), binding);

      const channel = this.byChannel.get(binding.channel);

      if (channel === undefined) this.byChannel.set(binding.channel, [binding]);
      else channel.push(binding);

      const entity = this.byEntity.get(binding.entity);

      if (entity === undefined) this.byEntity.set(binding.entity, [binding.channel]);
      else if (!entity.includes(binding.channel)) entity.push(binding.channel);
    }

    // The gap-recovery trigger, wired here rather than by the caller so it
    // cannot be left unwired -- which is a client that looks correct and is not.
    this.manager.onReconnect = (endpoint, channels) => {
      this.recover(channels);
    };

    // How `useQuery(op, args, {live: true})` finds this object. The adapters
    // resolve a cache -- from a per-call option, a provider, or the module
    // default -- and read the runtime off it, so all three resolution paths
    // reach the right binder with no second provider to wire up and nothing in
    // a generated file that has to know streams exist. Assigned here rather
    // than taken as a constructor option for the same reason `onReconnect` is:
    // it cannot then be left unwired.
    this.cache.live = this;

    this.unwatch = this.cache.watchPrincipal(() => {
      // The queue holds frames decoded under the previous identity, and the
      // store they were destined for has just been emptied. Committing them
      // would write the previous session's entities into the new one.
      this.queue = [];
      this.manager.repartition();
    });
  }

  /**
   * Which channels a query's entities are pushed on. Empty means it cannot be
   * live.
   *
   * **The resolution rule.** A query's live channels are every channel with a
   * binding whose `entity` is reachable from the query's declared result type
   * -- its `ops[x].entity`, plus everything `entities[T].fields` descends into,
   * transitively. That set is exactly the set of typenames `normalize` can
   * lift out of this operation's response, which is to say exactly the entities
   * that can appear in its skeleton.
   *
   * Root-only would be the smaller rule and is wrong in a way that is hard to
   * see: an order list rendering `order.customer.name` holds `Customer` records
   * in its skeleton, and a `customer.updated` frame changes what is on screen.
   * A client subscribed to `/ws/orders` alone renders that name stale forever,
   * with every order in the list perfectly current around it.
   *
   * Derived from the manifest and nothing else -- **not** from the query's
   * settled `deps`. Two reasons, and they point the same way. Deps do not exist
   * until the first response lands, so a deps-based rule is deaf during exactly
   * the window whose frames matter most. And deps follow the *data*: a second
   * page of orders that happens to contain no `Invoice` would drop the invoice
   * channel and re-acquire it on the page after, which is subscription churn
   * driven by pagination.
   *
   * Memoized per root typename, because the schema is generated and frozen.
   */
  channelsFor(meta: OperationMeta): readonly string[] {
    const root = meta.entity;

    if (root === undefined) return [];

    const memo = this.reach.get(root);

    if (memo !== undefined) return memo;

    const channels: string[] = [];
    const seen = new Set<string>([root]);
    const pending = [root];

    // Iterative and `seen`-guarded, because the schema is a graph and not a
    // tree: `Order.customer -> Customer.orders -> Order` is an ordinary
    // generated table, and a recursive walk over it does not terminate.
    while (pending.length > 0) {
      const type = pending.pop() as string;

      for (const channel of this.byEntity.get(type) ?? []) {
        if (!channels.includes(channel)) channels.push(channel);
      }

      const fields = this.cache.entities[type]?.fields;

      if (fields === undefined) continue;

      for (const target of Object.values(fields)) {
        if (seen.has(target)) continue;

        seen.add(target);
        pending.push(target);
      }
    }

    this.reach.set(root, channels);

    return channels;
  }

  /** Frames decoded but not yet committed. */
  get pending(): number {
    return this.queue.length;
  }

  /**
   * Make one query live: subscribe it to every channel bound to the entity it
   * reads, and register it for gap recovery.
   *
   * The entity comes from the operation's manifest entry rather than from the
   * skeleton of a response, so a query that has not loaded yet is still live
   * from the moment it mounts -- which is the only useful moment, since the
   * frames it would otherwise miss are the ones that arrive during the first
   * load.
   *
   * Ref-counted per query as well as per socket: two components with the same
   * `useOrderList({live: true})` are one subscription and one refetch on
   * reconnect, not two.
   */
  subscribe(meta: OperationMeta, args?: TagContext): () => void {
    const channels = this.channelsFor(meta);

    if (channels.length === 0) {
      this.onUnknown(`${meta.method} ${meta.path}`, `live: no channel binds ${meta.entity ?? '?'}`);

      return () => undefined;
    }

    const key = this.cache.key(meta, args);
    const releases = channels.map((channel) => {
      let queries = this.live.get(channel);

      if (queries === undefined) {
        queries = new Map<string, LiveQuery>();
        this.live.set(channel, queries);
      }

      const existing = queries.get(key);

      if (existing === undefined) queries.set(key, { meta, args, refs: 1 });
      else existing.refs++;

      const release = this.manager.subscribe(channel, (message, arrived) => {
        this.accept(message, arrived);
      });

      return () => {
        const held = this.live.get(channel)?.get(key);

        if (held !== undefined) {
          held.refs--;

          if (held.refs === 0) {
            this.live.get(channel)?.delete(key);

            if (this.live.get(channel)?.size === 0) this.live.delete(channel);
          }
        }

        release();
      };
    });

    let released = false;

    return () => {
      if (released) return;

      released = true;

      for (const release of releases) release();
    };
  }

  /** Subscribe to a channel without binding a query to it. */
  channel(name: string): () => void {
    return this.manager.subscribe(name, (message, arrived) => {
      this.accept(message, arrived);
    });
  }

  /** Commit the queued frames now, whatever the scheduler had planned. */
  flush(): void {
    this.scheduled = false;

    const queued = this.queue;
    this.queue = [];

    if (queued.length === 0) return;

    // A frame decoded before an identity change that this flush is only now
    // reaching. The watcher above empties the queue synchronously, so this is
    // belt to that braces -- and it is the belt worth having, because the cost
    // of being wrong is one principal's rows appearing under another's.
    const owner = this.cache.owner;
    const frames = queued.filter((entry) => entry.owner === owner).map((entry) => entry.frame);

    if (frames.length === 0) return;

    try {
      applyFrames(this.cache, frames, this.onError === undefined ? {} : { onError: this.onError });
    } catch (error) {
      this.onError?.(error, 'frames');
    }
  }

  /** Stop watching the cache's identity. Subscriptions are released separately. */
  dispose(): void {
    this.unwatch();
    this.queue = [];

    // Only if it is still ours. A second binder constructed over this cache has
    // already taken the slot, and clearing it here would leave the live one
    // unreachable from every `{live: true}` call site in the application.
    if (this.cache.live === this) this.cache.live = undefined;
  }

  /**
   * A socket that had dropped is back, so frames were missed.
   *
   * Both halves are needed and neither subsumes the other. Invalidating the
   * channel's tags catches everything whose *membership* may have moved --
   * `Order[]` covers a list that gained or lost rows while the laptop lid was
   * shut. Refetching the registered live queries catches the rest: a channel
   * that only ever emits `order.updated` declares no invalidations at all, by
   * design, so there is no tag to raise and the missed patches would otherwise
   * be invisible for as long as the session lasts.
   *
   * The order matters. The invalidation runs first, so a query that is refetched
   * below has already been marked stale and the batch will skip it rather than
   * spending a second request on it.
   */
  recover(channels: readonly string[]): void {
    const tags = new Set<string>();

    for (const channel of channels) {
      for (const binding of this.byChannel.get(channel) ?? []) {
        // The entity's list tag, which is what `provides` spells for any query
        // that loaded a collection of them. Declared invalidations are per
        // message and describe one event; a gap is every event that did not
        // arrive, so the recovery has to be broader than any single one of them.
        tags.add(`${binding.entity}[]`);

        // A template naming a request argument cannot resolve here -- there is
        // no request. Skipped rather than emitted half-substituted; the list
        // tag above already covers the entity itself.
        for (const tag of resolveTags(binding.invalidates, {}).tags) tags.add(tag);
      }
    }

    if (tags.size > 0) this.cache.invalidate(tags);

    const seen = new Set<string>();

    for (const channel of channels) {
      for (const [key, query] of this.live.get(channel) ?? []) {
        if (seen.has(key)) continue;

        seen.add(key);
        void this.cache.refetch(query.meta, query.args).catch(() => undefined);
      }
    }
  }

  /** One message off a socket: decode, match, queue. */
  private accept(message: unknown, arrived: string): void {
    let decoded: DecodedFrame | undefined;

    try {
      decoded = this.decode(message);
    } catch (error) {
      this.onError?.(error, 'decode');

      return;
    }

    if (decoded === undefined) return;

    const channel = decoded.channel ?? arrived;
    const binding = this.bindings.get(slot(channel, decoded.message));

    if (binding === undefined) {
      // Ignored, with a development warning. The server is ahead of this
      // client's manifest, which is the ordinary state of affairs and not an
      // error.
      this.onUnknown(decoded.message, channel);

      return;
    }

    this.queue.push({
      frame: { binding, payload: decoded.payload },
      owner: this.cache.owner,
    });

    if (this.scheduled) return;

    this.scheduled = true;
    this.schedule(() => {
      if (this.scheduled) this.flush();
    });
  }
}

/**
 * The key a `(channel, message)` pair is looked up under.
 *
 * Length-prefixed rather than joined by a delimiter, because a channel is a URL
 * path and a message is a dotted name, and any single character cheap enough to
 * use as a separator can in principle occur in one of them.
 *
 * An earlier version used a NUL, which is collision-free and was a mistake for
 * a reason that has nothing to do with correctness: a source file containing a
 * NUL byte is *binary* to git and grep, so `git show` will not render a diff of
 * it. The largest file in this chunk silently opted out of code review. A
 * format that cannot be read is a format that cannot be checked.
 */
function slot(channel: string, message: string): string {
  return `${String(channel.length)}:${channel}${message}`;
}

/**
 * Whether this is a development build.
 *
 * Written as a bare `process.env.NODE_ENV` reference on purpose: every bundler
 * substitutes that expression textually and then folds the branch away, so a
 * production build drops the warning *and* the set below entirely. Reading it
 * through `globalThis` -- which would be the tidier-looking way to avoid
 * assuming a Node global -- defeats the substitution and ships the whole thing.
 *
 * `typeof` guarded so a browser with no bundler shim does not throw; absent a
 * declared environment the safe answer for a diagnostic is "warn".
 */
declare const process: { env?: { NODE_ENV?: string } } | undefined;

function development(): boolean {
  return typeof process === 'undefined' || process?.env?.NODE_ENV !== 'production';
}

/**
 * Unknown message types already warned about.
 *
 * Capped, because the set is keyed by whatever the *server* chose to send. A
 * deploy that starts emitting a message name carrying an id -- or simply a
 * vocabulary larger than this client's manifest -- would otherwise mint one
 * permanent entry per distinct name, which is a leak whose size an operator
 * controls and this client does not.
 */
const warned = new Set<string>();
const WARN_LIMIT = 32;

function warnUnknown(message: string, channel: string): void {
  if (!development()) return;

  const slug = slot(channel, message);

  if (warned.has(slug)) return;

  const host = globalThis as { console?: { warn?: (text: string) => void } };

  // The point has been made. Going quiet is better than either growing without
  // bound or repeating the same line forever in a console somebody is trying to
  // read.
  if (warned.size >= WARN_LIMIT) {
    if (warned.size === WARN_LIMIT) {
      warned.add('');
      host.console?.warn?.(
        `[forge] more than ${String(WARN_LIMIT)} unbound stream message types; further warnings suppressed`,
      );
    }

    return;
  }

  warned.add(slug);

  host.console?.warn?.(
    `[forge] no stream binding for ${message} on ${channel}; the frame was ignored`,
  );
}
