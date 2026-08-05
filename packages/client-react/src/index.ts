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
 * Streaming (`live: true`), devtools and SSR hydration land in later chunks.
 */

export { ForgeProvider, useForgeClient } from './context';
export type { ForgeProviderProps } from './context';
export { useQuery } from './useQuery';
export type { UseQueryOptions, UseQueryResult } from './useQuery';
export { useMutation } from './useMutation';
export type { MutationState, MutationStatus, UseMutationResult } from './useMutation';
