/**
 * `@forge-go/client-angular` -- the Angular binding over `@forge-go/client-core`.
 *
 * Three bindings and an optional provider, and nothing else. Every decision
 * about identity, staleness, deduplication and invalidation was made in the
 * core, where it can be tested without a renderer; what is left here is the
 * narrow job of putting those values into the signal graph without copying
 * them on the way.
 *
 * ```ts
 * import { injectQuery, injectMutation, injectInvalidate } from '@forge-go/client-angular';
 * import { useOrderList, useOrderCreate } from './generated/hooks';
 *
 * export class Orders {
 *   readonly filter = signal('open');
 *   readonly orders = injectQuery(useOrderList, () => ({ query: { status: this.filter() } }));
 *   readonly create = injectMutation(useOrderCreate);
 *
 *   // Refresh a query this component does not hold -- a list in a parent, a
 *   // sibling's detail pane -- by naming the operation, not the component.
 *   private readonly invalidate = injectInvalidate();
 * }
 * ```
 *
 * Named `inject*` because they must run in an injection context -- they
 * resolve the cache, and the two that own a lifetime resolve the `DestroyRef`
 * too, from the injector -- which is what Angular calls such a function. Pass
 * `{injector}` to call them from anywhere else.
 *
 * Angular is a **peer** dependency, and so is the core. Bundling either would
 * give an application two copies: two Angulars means two DI trees and two
 * reactive graphs, and two cores means two module-level caches, so the client
 * the application configured is not the one its generated hooks read from.
 *
 * Streaming (`live: true`), devtools and SSR hydration land in later chunks.
 */

export { CLIENT, injectClient, provideClient } from './context.js';
export { injectQuery } from './injectQuery.js';
export type { InjectQueryOptions, InjectQueryResult, QueryArgs } from './injectQuery.js';
export { injectMutation } from './injectMutation.js';
export type {
  InjectMutationOptions,
  InjectMutationResult,
  MutationState,
  MutationStatus,
} from './injectMutation.js';
export { injectInvalidate } from './injectInvalidate.js';
export type { Invalidate, InjectInvalidateOptions } from './injectInvalidate.js';
export { provideHydration } from './hydration.js';
export type { HydrationOptions } from './hydration.js';
