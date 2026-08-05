/**
 * `@forge-go/client-core` -- the runtime a generated Forge client delegates to.
 *
 * This entry point currently exposes the normalized entity store: the
 * normalizer that turns a response into a flat store plus a skeleton of
 * references, and the rehydration that turns a skeleton back into a value
 * without changing the identity of anything that did not change.
 *
 * The query engine, transports and framework adapters land in later chunks.
 */

export { normalize } from './normalize';
export { EntityStore, denormalize } from './store';
export type { WriteResult } from './store';
export { entityKey, isRef } from './ref';
export type {
  EntityKey,
  EntityMeta,
  EntityRecord,
  EntitySchema,
  NormalizeResult,
  Ref,
} from './types';
