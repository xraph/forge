/**
 * `@forge-go/client-core` -- the runtime a generated Forge client delegates to.
 *
 * Five layers.
 *
 * The **normalized entity store**: the normalizer that turns a response into a
 * flat store plus a skeleton of references, and the rehydration that turns a
 * skeleton back into a value without changing the identity of anything that
 * did not change.
 *
 * The **tag graph**: which mounted queries a settled mutation put behind, and
 * when they refetch. Tag templates resolve against an operation's arguments
 * and response, mounted queries are ref-counted and indexed by the tags they
 * provide, and invalidations coalesce into one batch per tick.
 *
 * The **query cache and REST transport**: `query(op)` and `mutation(op)` --
 * what a generated `hooks.ts` imports -- over a cache that runs an operation,
 * normalizes it, records its dependencies and hands back a value whose object
 * identity survives every read that did not change it. The transport drives
 * the generated REST client, retries idempotent methods only, deduplicates
 * concurrent identical requests, and refreshes a credential once per
 * stampede rather than once per 401.
 *
 * **Stream binding**: a ref-counted subscription manager -- one socket per
 * `(endpoint, principal)`, multiplexed by channel, closed on the last release
 * and not a moment before, because React's development double-invoke would
 * otherwise close it on a phantom unmount -- and a frame applier that decodes a
 * message, matches it to its manifest binding, and applies the declared intent
 * through the *same* path a mutation response takes. Frames coalesce into one
 * store commit per animation frame; a reconnect invalidates the channel's tags
 * and refetches the live queries on it, because a client that missed frames
 * looks correct while being wrong; and a write that a frame overtook while it
 * was in flight never commits over it -- a query response is re-run, a mutation
 * response commits around the raced entities, because re-issuing a write is how
 * duplicate orders happen.
 *
 * **Optimistic overlays**: a stack of pending changes layered over the store
 * rather than applied to it. What a subscriber sees is `fold(base, patches in
 * push order)`, recomputed on demand, so rollback is the removal of an entry
 * and no inverse is recorded anywhere -- an inverse is what goes wrong under
 * concurrency, having been computed against a base that already included a
 * mutation which may itself still fail. Two pending edits to one record
 * therefore compose rather than race, and a stream frame that lands underneath
 * one is not overwritten when it settles.
 *
 * Nothing here reaches the network on its own: the HTTP client, the socket, the
 * clock and the schedulers are all injected. The framework adapters are
 * separate packages -- `@forge-go/client-react`, `-vue`, `-angular` -- so an
 * application ships the one it uses rather than all three.
 */

export { normalize } from './normalize';
export { EntityStore, denormalize, OPTIMISTIC } from './store';
export type { CommitOptions, OverlayLayer, StagedWrite, WriteResult } from './store';
export { OverlayStack, targetOf } from './overlay';
export type {
  EntityPatch,
  MergeSource,
  OptimisticPatch,
  OptimisticSpec,
  OverlayEntry,
} from './overlay';
export { entityKey, isRef } from './ref';
export { queryKey, resolveTag, resolveTags } from './tags';
export type { ResolvedTags, TagContext } from './tags';
export { QueryRegistry } from './registry';
export type {
  QueryEntry,
  QueryRegistryOptions,
  QuerySpec,
  SettleResult,
  Unmount,
} from './registry';
export { Invalidator, manualScheduler, microtaskScheduler } from './invalidate';
export type {
  InvalidatorOptions,
  ManualScheduler,
  MutationSettled,
  Placement,
  Scheduler,
} from './invalidate';
export type {
  EntityKey,
  EntityMeta,
  EntityRecord,
  EntitySchema,
  NormalizeResult,
  Ref,
} from './types';
export {
  manualClock,
  operationUrl,
  realSleep,
  RestTransport,
  retryable,
  statusOf,
} from './transport';
export type {
  AuthProvider,
  ManualClock,
  OperationMeta,
  RestClientLike,
  RestRequestConfig,
  RestTransportOptions,
  RetryPolicy,
  Sleep,
  Transport,
  TransportRequest,
} from './transport';
export { QueryCache } from './cache';
export type {
  LiveBinding,
  MutateOptions,
  QueryCacheOptions,
  QueryState,
  QueryStatus,
  RequestOptions,
} from './cache';
export type { CacheEvent, CacheObserver } from './observe';
export { socketSnapshot, SubscriptionManager } from './stream';
export type {
  BackoffPolicy,
  ChannelSnapshot,
  FrameHandler,
  SocketSnapshot,
  StreamBinding,
  StreamConnect,
  StreamConnectContext,
  StreamConnection,
  StreamIntent,
  SubscriptionManagerOptions,
} from './stream';
export { animationFrameScheduler, applyFrames, decodeFrame, StreamBinder } from './live';
export type {
  ApplyFramesOptions,
  DecodedFrame,
  FrameDecoder,
  StreamBinderOptions,
  StreamFrame,
} from './live';
export { forgeStreamingDecoder } from './streaming';
export type { ForgeStreamingDecoderOptions } from './streaming';
export { configureClient, getClient, mutation, query, setClient } from './client';
export type {
  MutationBinding,
  MutationOptions,
  QueryBinding,
  QueryHandle,
  QueryOptions,
} from './client';
