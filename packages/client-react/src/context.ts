import { createContext, createElement, useContext } from 'react';
import type { ReactNode } from 'react';
import { getClient } from '@forge-go/client-core';
import type { QueryCache } from '@forge-go/client-core';

/**
 * The cache a subtree's hooks read from, when the application supplies one.
 *
 * `undefined` is the meaningful default rather than a placeholder: it means
 * *no provider was rendered*, which is a legitimate configuration, not an
 * error to be papered over. See `useForgeClient`.
 */
const ClientContext = createContext<QueryCache | undefined>(undefined);

export interface ForgeProviderProps {
  readonly client: QueryCache;
  readonly children?: ReactNode;
}

/**
 * Supply a cache to a subtree. **Optional.**
 *
 * A generated `hooks.ts` is a list of bindings created at module scope, long
 * before an application exists to hand anything to. Making a provider
 * mandatory would therefore mean the generator had decided how the consuming
 * application does dependency injection -- and a file regenerated from a Go
 * route table is the worst possible place for that decision to live. So the
 * module-level `configureClient()` remains sufficient on its own, and this is
 * for the cases a global cannot serve: a server rendering two requests
 * concurrently, a test that must not leak state into the next one, an
 * application talking to two backends, a Storybook story with a fixture cache.
 *
 * Built with `createElement` rather than JSX so this package emits no
 * dependency on a JSX runtime and its consumers are free to configure theirs
 * however they like.
 */
export function ForgeProvider(props: ForgeProviderProps): ReactNode {
  return createElement(ClientContext.Provider, { value: props.client }, props.children);
}

/**
 * Resolve the cache a hook should use: explicit, then provided, then global.
 *
 * The precedence is the point. A per-call `client` beats a provider, because
 * somebody wrote it at the call site on purpose; a provider beats the module
 * default, because rendering one is itself a deliberate act. Falling through
 * to `getClient()` is what keeps the provider optional -- and `getClient`
 * throws a named error rather than minting a scratch cache, so "I configured
 * nothing" fails loudly instead of producing a component that fetches forever
 * into a cache nobody else can see.
 *
 * Exported because an application that reaches past the hooks -- to prefetch,
 * to invalidate from an event handler, to call `setPrincipal` on logout --
 * needs the same answer this file gives, resolved the same way.
 */
export function useForgeClient(override?: QueryCache): QueryCache {
  const provided = useContext(ClientContext);

  return override ?? provided ?? getClient();
}
