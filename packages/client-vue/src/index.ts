/**
 * `@forge-go/client-vue` -- the Vue 3 binding over `@forge-go/client-core`.
 *
 * Two composables and an optional provider, and nothing else. Every decision
 * about identity, staleness, deduplication and invalidation was made in the
 * core, where it can be tested without a renderer; what is left here is the
 * narrow job of moving those values into Vue's reactivity without Vue
 * rewriting them on the way through.
 *
 * ```ts
 * import { useQuery, useMutation } from '@forge-go/client-vue';
 * import { useOrderList, useOrderCreate } from './generated/hooks';
 *
 * const { data, status, refetch } = useQuery(useOrderList, () => ({ query: { status: filter.value } }));
 * const { mutate, isPending } = useMutation(useOrderCreate);
 * ```
 *
 * Every snapshot lives in a `shallowRef`. A deep `ref` would hand components
 * reactive proxies of the cache's objects rather than the objects themselves,
 * which is the one thing this package must not do -- see `useQuery`.
 *
 * Vue is a **peer** dependency, and so is the core. Bundling either would give
 * an application two copies: two Vues means two reactivity systems whose
 * effects do not see each other's dependencies, and two cores means two
 * module-level caches, so the client the application configured is not the one
 * its generated hooks read from.
 *
 * Streaming (`live: true`), devtools and SSR hydration land in later chunks.
 */

export { clientPlugin, clientKey, provideClient, useClient } from './context';
export { useQuery } from './useQuery';
export type { UseQueryOptions, UseQueryResult } from './useQuery';
export { useMutation } from './useMutation';
export type { MutationState, MutationStatus, UseMutationResult } from './useMutation';
export { HydrationBoundary } from './hydration';
