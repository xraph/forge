/**
 * `@forge-go/client-core` -- the runtime a generated Forge client delegates to.
 *
 * Two layers so far.
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
 * Streaming transports, optimistic overlays and framework adapters land in
 * later chunks. Nothing here reaches the network on its own: the client, the
 * clock and the scheduler are all injected.
 */

export { normalize } from './normalize';
export { EntityStore, denormalize } from './store';
export type { WriteResult } from './store';
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
  MutateOptions,
  QueryCacheOptions,
  QueryState,
  QueryStatus,
  RequestOptions,
} from './cache';
export { configureClient, getClient, mutation, query, setClient } from './client';
export type {
  MutationBinding,
  MutationOptions,
  QueryBinding,
  QueryHandle,
  QueryOptions,
} from './client';
