import { InjectionToken, inject } from '@angular/core';
import type { Provider } from '@angular/core';
import { getClient } from '@forge-go/client-core';
import type { QueryCache } from '@forge-go/client-core';

/**
 * The token an injector supplies a cache under.
 *
 * Exported so an application can inject the cache anywhere it injects
 * anything, without going through this package's functions at all.
 */
export const CLIENT = new InjectionToken<QueryCache>('forge.client');

/**
 * Supply a cache to an injector. **Optional.**
 *
 * A generated `hooks.ts` is a list of bindings created at module scope, long
 * before an application exists to hand anything to. Making a provider
 * mandatory would therefore mean the generator had decided how the consuming
 * application does dependency injection -- and a file regenerated from a Go
 * route table is the worst possible place for that decision to live. So the
 * module-level `configureClient()` remains sufficient on its own, and this is
 * for the cases a global cannot serve: an SSR request that must not share a
 * cache with the request beside it, a test that must not leak into the next
 * one, an application talking to two backends from two routes.
 *
 * Named for Angular's `provideX` convention and usable anywhere providers are:
 * `bootstrapApplication(App, { providers: [provideClient(cache)] })`, a
 * lazy route's `providers`, or a component's own.
 */
export function provideClient(client: QueryCache): Provider {
  return { provide: CLIENT, useValue: client };
}

/**
 * Resolve the cache a binding should use: explicit, then injected, then the
 * module-level default.
 *
 * The precedence is the point. A per-call `client` beats a provider, because
 * somebody wrote it at the call site on purpose; a provider beats the module
 * default, because supplying one is itself a deliberate act. Falling through
 * to `getClient()` is what keeps the provider optional -- and `getClient`
 * throws a named error rather than minting a scratch cache, so "I configured
 * nothing" fails loudly instead of producing a component that fetches forever
 * into a cache nobody else can see.
 *
 * The explicit override is checked *before* `inject`, so a caller who has
 * already been handed a cache does not need an injection context to use it.
 */
export function injectClient(override?: QueryCache): QueryCache {
  if (override !== undefined) return override;

  return inject(CLIENT, { optional: true }) ?? getClient();
}
