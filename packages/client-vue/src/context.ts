import { getCurrentInstance, inject, provide } from 'vue';
import type { App, InjectionKey, Plugin } from 'vue';
import { getClient } from '@forge-go/client-core';
import type { QueryCache } from '@forge-go/client-core';

/**
 * The injection key a subtree's composables read their cache from.
 *
 * A `Symbol` rather than a string, so two copies of this package -- or an
 * application that happens to provide something under `'client'` -- cannot
 * collide. Exported because a test or an application-level `app.provide` needs
 * to name it, and because `@vue/test-utils` takes it as a `global.provide` key.
 */
export const clientKey: InjectionKey<QueryCache> = Symbol.for('forge.client');

/**
 * Supply a cache to the current component's subtree. **Optional.**
 *
 * A generated `hooks.ts` is a list of bindings created at module scope, long
 * before an application exists to hand anything to. Making a provider
 * mandatory would therefore mean the generator had decided how the consuming
 * application does dependency injection -- and a file regenerated from a Go
 * route table is the worst possible place for that decision to live. So the
 * module-level `configureClient()` remains sufficient on its own, and this is
 * for the cases a global cannot serve: an SSR request that must not share a
 * cache with the request beside it, a test that must not leak into the next
 * one, an application talking to two backends, a story with a fixture cache.
 *
 * Must be called from `setup()`, like every other `provide`.
 */
export function provideClient(client: QueryCache): void {
  provide(clientKey, client);
}

/**
 * The same thing at application scope: `app.use(clientPlugin(cache))`.
 *
 * Vue's own idiom for "one dependency, whole app", and the shape SSR wants --
 * `createApp()` per request, one cache per app, no module-level state to leak
 * between two requests being rendered concurrently.
 */
export function clientPlugin(client: QueryCache): Plugin {
  return {
    install(app: App) {
      app.provide(clientKey, client);
    },
  };
}

/**
 * Resolve the cache a composable should use: explicit, then provided, then
 * the module-level default.
 *
 * The precedence is the point. A per-call `client` beats a provider, because
 * somebody wrote it at the call site on purpose; a provider beats the module
 * default, because rendering one is itself a deliberate act. Falling through
 * to `getClient()` is what keeps the provider optional -- and `getClient`
 * throws a named error rather than minting a scratch cache, so "I configured
 * nothing" fails loudly instead of producing a component that fetches forever
 * into a cache nobody else can see.
 *
 * `inject` is only reached when there *is* a component instance. That guard is
 * not defensive noise: Vue logs `inject() can only be used inside setup()`
 * whenever it is called without one, and a composable running inside a bare
 * `effectScope()` -- a store, a router guard, a `watchEffect` in a plugin --
 * is a supported way to use this package, not a mistake to be warned about.
 * Such a call simply has no provider to find, and falls through to the global.
 */
export function useClient(override?: QueryCache): QueryCache {
  if (override !== undefined) return override;

  const provided = getCurrentInstance() === null ? undefined : inject(clientKey, undefined);

  return provided ?? getClient();
}
