import { DestroyRef, computed, inject, runInInjectionContext, signal } from '@angular/core';
import type { Injector, Signal } from '@angular/core';
import type { MutationBinding, MutationOptions, TagContext } from '@forge-go/client-core';
import { injectClient } from './context';

/** Where a mutation is in its lifecycle. Local to the context that fired it. */
export type MutationStatus = 'idle' | 'pending' | 'success' | 'error';

export interface MutationState<T> {
  readonly status: MutationStatus;
  readonly data: T | undefined;
  readonly error: unknown;
  readonly isPending: boolean;
}

/**
 * Hook-level options, static or reactive.
 *
 * A getter -- or a signal, which is one -- so a `place` callback closing over
 * component state contributes its current value when the write actually runs
 * rather than the value it had at construction.
 */
export type InjectMutationOptions<E = unknown> =
  | (MutationOptions<E> & { readonly injector?: Injector })
  | (() => MutationOptions<E> | undefined)
  | undefined;

/**
 * `E` is the entity an `optimistic` patch is checked against.
 *
 * Threaded rather than defaulted away, because dropping it here is where the
 * feature's type safety would quietly stop. `MutationOptions` defaults its
 * entity parameter to `unknown`, `Partial<unknown>` resolves to `{}`, and `{}`
 * accepts `{stauts: 'shipped'}` -- a patch that compiles, dispatches, and
 * silently changes nothing. A generated `MutationBinding<Order, Order>` is
 * assignable to `MutationBinding<T>`, so the erasure costs no error anywhere:
 * it simply stops checking. See `__tests__/types.test-d.ts`.
 *
 * Defaulted to `unknown` so an untyped binding, or a call site that names only
 * the response type, still compiles exactly as it did.
 */
export interface InjectMutationResult<T, E = unknown> {
  readonly state: Signal<MutationState<T>>;
  readonly data: Signal<T | undefined>;
  readonly error: Signal<unknown>;
  readonly status: Signal<MutationStatus>;
  readonly isPending: Signal<boolean>;
  /**
   * Run the mutation. **Never rejects.**
   *
   * The failure is recorded in `status` and `error`, and the promise resolves
   * with `undefined`. This is the one a template's `(click)` calls, and it is
   * the safe one deliberately: it is what a caller writes first and copies out
   * of the README, so it is the spelling that must not be able to misbehave.
   *
   * A version that recorded the error *and* rejected asks every caller to
   * remember a `.catch` for a failure the binding has already handled and the
   * interface has already displayed. Most will not, and each one that does not
   * is an `unhandledrejection` per failed write: console noise in development,
   * and in production a global handler firing about an error the user is
   * currently looking at.
   *
   * Angular offers no convention that argues the other way. `ErrorHandler`
   * receives errors thrown *through* Angular -- a template expression, a
   * lifecycle hook, an effect -- and a rejected promise returned from an event
   * binding is not one of them, so the rejection would be nobody's.
   */
  mutate(args?: TagContext, options?: MutationOptions<E>): Promise<T | undefined>;
  /**
   * Run the mutation and reject on failure, for a caller that sequences work
   * after a write and must not continue when it did not happen.
   *
   * Records exactly the same state as `mutate`. The only difference is who is
   * responsible for the failure: here, the caller, who asked for it by name.
   */
  mutateAsync(args?: TagContext, options?: MutationOptions<E>): Promise<T>;
  /** Back to `idle`, discarding the last result. */
  reset(): void;
}

const IDLE: MutationState<never> = Object.freeze({
  status: 'idle' as const,
  data: undefined,
  error: undefined,
  isPending: false,
});

const PENDING: MutationState<never> = Object.freeze({
  status: 'pending' as const,
  data: undefined,
  error: undefined,
  isPending: true,
});

function resolve<E>(options: InjectMutationOptions<E>): MutationOptions<E> | undefined {
  return typeof options === 'function' ? options() : options;
}

/**
 * Bind one write operation to an injection context.
 *
 * Unlike a query, a mutation is **not shared**: two components calling
 * `injectMutation(useOrderCreate)` are two independent operations with two
 * independent statuses, so this state is a local signal rather than a cache
 * subscription. What *is* shared is everything the write causes -- the entity
 * commit, the tag invalidation, the placement callbacks -- and all of that
 * happens inside `QueryCache.mutate`, reaching every live `injectQuery`
 * through its own subscription. This binding adds no invalidation logic of its
 * own, deliberately.
 */
export function injectMutation<T, E = unknown>(
  op: MutationBinding<T, E>,
  options?: InjectMutationOptions<E>,
): InjectMutationResult<T, E> {
  const injector = typeof options === 'function' ? undefined : options?.injector;

  return injector === undefined
    ? bind(op, options)
    : runInInjectionContext(injector, () => bind(op, options));
}

function bind<T, E>(
  op: MutationBinding<T, E>,
  options: InjectMutationOptions<E>,
): InjectMutationResult<T, E> {
  // Which cache a write goes to is resolved once: it is not something that can
  // change under an in-flight request.
  const client = injectClient(resolve(options)?.client);
  const state = signal<MutationState<T>>(IDLE);

  /**
   * Whether this context is still alive, so a response that lands after it has
   * been destroyed does not publish into a signal nobody will read.
   *
   * Angular has no equivalent of React's StrictMode double-invoked effect, so
   * this is the whole of the lifecycle story: one `DestroyRef`, one flag. It
   * is still needed -- a component destroyed mid-write would otherwise settle
   * into `success` behind the user's back, and that state is observable on a
   * component the router re-creates.
   */
  let alive = true;

  inject(DestroyRef).onDestroy(() => {
    alive = false;
  });

  // Distinguishes concurrent calls. Two rapid submits must not have the first
  // response overwrite the second's, whichever order they land in.
  let seq = 0;

  async function mutateAsync(args?: TagContext, perCall?: MutationOptions<E>): Promise<T> {
    const call = ++seq;

    state.set(PENDING);

    try {
      // The hook-level options are read *now*, not captured at construction.
      const data = await op(args, { ...resolve(options), ...perCall, client });

      if (alive && seq === call) {
        state.set({ status: 'success', data, error: undefined, isPending: false });
      }

      return data;
    } catch (error) {
      if (alive && seq === call) {
        state.set({ status: 'error', data: undefined, error, isPending: false });
      }

      throw error;
    }
  }

  return {
    state: state.asReadonly(),
    data: computed(() => state().data),
    error: computed(() => state().error),
    status: computed(() => state().status),
    isPending: computed(() => state().isPending),
    /**
     * The safe spelling: the rejection is consumed here, by the one place that
     * can be sure the failure has already been recorded in `status` and
     * `error`. `.catch` over a second copy of the body, so there is exactly
     * one implementation and no way for the two entry points to disagree.
     */
    mutate: (args, perCall) => mutateAsync(args, perCall).catch(() => undefined),
    mutateAsync,
    reset: () => {
      // Supersede anything in flight, so its result does not land after a reset.
      seq++;
      state.set(IDLE);
    },
  };
}
