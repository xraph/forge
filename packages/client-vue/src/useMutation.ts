import { computed, getCurrentScope, onScopeDispose, shallowRef, toValue } from 'vue';
import type { ComputedRef, MaybeRefOrGetter, ShallowRef } from 'vue';
import type { MutationBinding, MutationOptions, TagContext } from '@forge-go/client-core';
import { useForgeClient } from './context';

/** Where a mutation is in its lifecycle. Local to the scope that fired it. */
export type MutationStatus = 'idle' | 'pending' | 'success' | 'error';

export interface MutationState<T> {
  readonly status: MutationStatus;
  readonly data: T | undefined;
  readonly error: unknown;
  readonly isPending: boolean;
}

export interface UseMutationResult<T> {
  /** The whole snapshot, in one `shallowRef`, for the same reason a query has one. */
  readonly state: ShallowRef<MutationState<T>>;
  readonly data: ComputedRef<T | undefined>;
  readonly error: ComputedRef<unknown>;
  readonly status: ComputedRef<MutationStatus>;
  readonly isPending: ComputedRef<boolean>;
  /**
   * Run the mutation. **Never rejects.**
   *
   * The failure is recorded in `status` and `error`, and the promise resolves
   * with `undefined`. This is the one an event handler calls, and it is the
   * safe one deliberately: `@click="create.mutate(...)"` is what a caller
   * writes first and copies out of the README, so it is the spelling that must
   * not be able to misbehave.
   *
   * A version that recorded the error *and* rejected asks every caller to
   * remember a `.catch` for a failure the composable has already handled and
   * the interface has already displayed. Most will not, and each one that does
   * not is an `unhandledrejection` per failed write: console noise in
   * development, and in production a global handler firing, which is an alert
   * about an error the user is currently looking at.
   *
   * Vue offers no convention that argues the other way. A template event
   * handler returning a rejected promise is unhandled exactly as it is in
   * React -- `app.config.errorHandler` never sees it, because it is not a
   * render or lifecycle error -- so the React adapter's split stands here
   * unchanged.
   */
  mutate(args?: TagContext, options?: MutationOptions): Promise<T | undefined>;
  /**
   * Run the mutation and reject on failure, for a caller that sequences work
   * after a write and must not continue when it did not happen.
   *
   * Records exactly the same state as `mutate`. The only difference is who is
   * responsible for the failure: here, the caller, who asked for it by name.
   */
  mutateAsync(args?: TagContext, options?: MutationOptions): Promise<T>;
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

/**
 * Bind one write operation to a scope.
 *
 * Unlike a query, a mutation is **not shared**: two components calling
 * `useOrderCreate()` are two independent operations with two independent
 * statuses, so this state is a local ref rather than a cache subscription.
 * What *is* shared is everything the write causes -- the entity commit, the
 * tag invalidation, the placement callbacks -- and all of that happens inside
 * `QueryCache.mutate`, reaching every mounted `useQuery` through its own
 * subscription. This composable adds no invalidation logic of its own,
 * deliberately.
 *
 * `options` may be a getter or a ref, so `place` callbacks can close over
 * reactive state and still be current when the mutation actually runs. Only
 * `client` is read once, at setup: which cache a write goes to is not
 * something that can change under an in-flight request.
 */
export function useMutation<T>(
  op: MutationBinding<T>,
  options?: MaybeRefOrGetter<MutationOptions | undefined>,
): UseMutationResult<T> {
  const client = useForgeClient(toValue(options)?.client);
  const state = shallowRef<MutationState<T>>(IDLE);

  /**
   * Whether this scope is still alive, so a response that lands after it has
   * gone does not publish into a ref nobody will read.
   *
   * There is no Vue equivalent of React's StrictMode double-invoked effect, so
   * this is the whole of the lifecycle story: one scope, one disposal, one
   * flag. It is still needed -- a component unmounted while a write is in
   * flight would otherwise settle into `success` behind the user's back, and
   * on a `<KeepAlive>`d or re-created component that state is observable.
   */
  let alive = true;

  /**
   * Guarded, where `useQuery`'s is not. A query outside a scope leaks a live
   * subscription and deserves Vue's development warning; a mutation outside a
   * scope holds nothing, so the same warning would be noise telling a caller
   * to fix something that is not broken.
   */
  if (getCurrentScope() !== undefined) {
    onScopeDispose(() => {
      alive = false;
    });
  }

  // Distinguishes concurrent calls. Two rapid submits must not have the first
  // response overwrite the second's, whichever order they land in.
  let seq = 0;

  async function mutateAsync(args?: TagContext, perCall?: MutationOptions): Promise<T> {
    const call = ++seq;

    state.value = PENDING;

    try {
      // The hook-level options are read *now*, not captured at setup, so a
      // getter that depends on reactive state contributes its current value.
      const data = await op(args, { ...toValue(options), ...perCall, client });

      if (alive && seq === call) {
        state.value = { status: 'success', data, error: undefined, isPending: false };
      }

      return data;
    } catch (error) {
      if (alive && seq === call) {
        state.value = { status: 'error', data: undefined, error, isPending: false };
      }

      throw error;
    }
  }

  return {
    state,
    data: computed(() => state.value.data),
    error: computed(() => state.value.error),
    status: computed(() => state.value.status),
    isPending: computed(() => state.value.isPending),
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
      state.value = IDLE;
    },
  };
}
