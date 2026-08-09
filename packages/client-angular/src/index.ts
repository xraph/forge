/**
 * `@forge-go/client-angular` -- the Angular binding over `@forge-go/client-core`.
 *
 * Two bindings and an optional provider, and nothing else. Every decision
 * about identity, staleness, deduplication and invalidation was made in the
 * core, where it can be tested without a renderer; what is left here is the
 * narrow job of putting those values into the signal graph without copying
 * them on the way.
 *
 * ```ts
 * import { injectQuery, injectMutation } from '@forge-go/client-angular';
 * import { useOrderList, useOrderCreate } from './generated/hooks';
 *
 * export class Orders {
 *   readonly filter = signal('open');
 *   readonly orders = injectQuery(useOrderList, () => ({ query: { status: this.filter() } }));
 *   readonly create = injectMutation(useOrderCreate);
 * }
 * ```
 *
 * Named `inject*` because they must run in an injection context -- they
 * resolve the cache and the `DestroyRef` from the injector -- which is what
 * Angular calls such a function. Pass `{injector}` to call them from anywhere
 * else.
 *
 * Angular is a **peer** dependency, and so is the core. Bundling either would
 * give an application two copies: two Angulars means two DI trees and two
 * reactive graphs, and two cores means two module-level caches, so the client
 * the application configured is not the one its generated hooks read from.
 *
 * Streaming (`live: true`), devtools and SSR hydration land in later chunks.
 */

export { CLIENT, injectClient, provideClient } from './context';
export { injectQuery } from './injectQuery';
export type { InjectQueryOptions, InjectQueryResult, QueryArgs } from './injectQuery';
export { injectMutation } from './injectMutation';
export type {
  InjectMutationOptions,
  InjectMutationResult,
  MutationState,
  MutationStatus,
} from './injectMutation';
