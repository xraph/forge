import { ENVIRONMENT_INITIALIZER } from '@angular/core';
import type { Provider } from '@angular/core';
import { hydrateBoundary } from '@forge-go/client-core';
import type { DehydratedState, OperationMeta, QueryCache } from '@forge-go/client-core';
import { injectClient } from './context.js';

export interface HydrationOptions {
  /** Use this cache rather than the injected or configured one. */
  readonly client?: QueryCache;
  /** Settle the hydrated queries behind the server, so mounting refetches. */
  readonly stale?: boolean;
}

/**
 * Hydrate a payload into the cache an injector's queries read from.
 *
 * ```ts
 * bootstrapApplication(App, {
 *   providers: [provideClient(cache), provideHydration(state, ops)],
 * });
 * ```
 *
 * A provider rather than a boundary component, which is where this parts
 * company with the React and Vue adapters, and it is Angular's content model
 * that forces it. A `<forge-hydration-boundary>` wrapping `<ng-content>` does
 * not own its children: projected content is instantiated by the *parent*
 * template, so `injectQuery` in a child has already run by the time the
 * wrapper's constructor does. The one thing a boundary has to guarantee, that
 * the cache is populated before anything reads it, is the one thing that shape
 * cannot deliver.
 *
 * An environment initializer runs when the injector is created, before any
 * component in it exists, which is the guarantee restated in Angular's own
 * vocabulary. Use it at bootstrap for a whole application, or in a lazy
 * route's `providers` for one route:
 *
 * ```ts
 * { path: 'orders', providers: [provideHydration(state, ops)], loadComponent: ... }
 * ```
 *
 * `ENVIRONMENT_INITIALIZER` rather than the newer `provideEnvironmentInitializer`
 * so this keeps working on the Angular 17 this package still supports.
 */
export function provideHydration(
  state: DehydratedState | undefined,
  ops: Readonly<Record<string, OperationMeta>>,
  options: HydrationOptions = {},
): Provider {
  return {
    provide: ENVIRONMENT_INITIALIZER,
    multi: true,
    useValue: (): void => {
      // Runs in an injection context, so `injectClient` resolves the provider
      // registered alongside this one. Resolving lazily also means the order
      // of the two providers in the array does not matter.
      hydrateBoundary(options.client ?? injectClient(), state, {
        ops,
        ...(options.stale === true ? { stale: true } : {}),
      });
    },
  };
}
