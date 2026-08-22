/**
 * `@forge-go/client-react` -- the React binding over `@forge-go/client-core`.
 *
 * Two hooks and a provider, and nothing else. Every decision about identity,
 * staleness, deduplication and invalidation was made in the core, where it can
 * be tested without a renderer; what is left here is the narrow job of
 * satisfying `useSyncExternalStore`'s contract without undoing any of it.
 *
 * ```ts
 * import { useQuery, useMutation } from '@forge-go/client-react';
 * import { useOrderList, useOrderCreate } from './generated/hooks';
 *
 * const { data, status, refetch } = useQuery(useOrderList, {query: {status: 'open'}});
 * const { mutate, isPending } = useMutation(useOrderCreate);
 * ```
 *
 * React is a **peer** dependency, and so is the core. Bundling either would
 * give an application two copies: two Reacts means hooks called against the
 * wrong dispatcher, and two cores means two module-level caches, so the client
 * the application configured is not the one its generated hooks read from.
 *
 * SSR is here: `HydrationBoundary` hydrates a payload from `dehydrate`
 * during render, so a server render emits real markup rather than a spinner.
 */

export { ClientProvider, useClient } from './context.js';
export type { ClientProviderProps } from './context.js';
export { useQuery } from './useQuery.js';
export type { UseQueryOptions, UseQueryResult } from './useQuery.js';
export { useMutation } from './useMutation.js';
export type { MutationState, MutationStatus, UseMutationResult } from './useMutation.js';
export { HydrationBoundary } from './hydration.js';
export type { HydrationBoundaryProps } from './hydration.js';
