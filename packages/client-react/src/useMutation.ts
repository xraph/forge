import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { MutationBinding, MutationOptions, TagContext } from '@forge-go/client-core';
import { useClient } from './context';

/** Where a mutation is in its lifecycle. Local to the component that fired it. */
export type MutationStatus = 'idle' | 'pending' | 'success' | 'error';

export interface MutationState<T> {
  readonly status: MutationStatus;
  readonly data: T | undefined;
  readonly error: unknown;
  readonly isPending: boolean;
}

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
export interface UseMutationResult<T, E = unknown> extends MutationState<T> {
  /**
   * Run the mutation. **Never rejects.**
   *
   * The failure is recorded in `status` and `error`, and the promise resolves
   * with `undefined`. This is the one an event handler calls, and it is the
   * safe one deliberately: `onClick={() => create.mutate(...)}` is what a
   * caller writes first and copies out of the README, so it is the spelling
   * that must not be able to misbehave.
   *
   * A version that recorded the error *and* rejected -- which this hook used
   * to be -- asks every caller to remember a `.catch` for a failure the hook
   * has already handled and the interface has already displayed. Most will not
   * remember, and the ones who do not get an `unhandledrejection` per failed
   * write: console noise in development, and in production a global handler
   * firing, which is Sentry paging someone about an error the user is
   * currently looking at. Making the common spelling safe is worth more than
   * the symmetry.
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

/**
 * Bind one write operation to a component.
 *
 * Unlike a query, a mutation is **not shared**: two components calling
 * `useOrderCreate()` are two independent operations with two independent
 * statuses, so this state is local `useState` rather than a cache subscription.
 * What *is* shared is everything the write causes -- the entity commit, the
 * tag invalidation, the placement callbacks -- and all of that happens inside
 * `QueryCache.mutate`, reaching every mounted `useQuery` through its own
 * subscription. This hook adds no invalidation logic of its own, deliberately.
 */
export function useMutation<T, E = unknown>(
  op: MutationBinding<T, E>,
  options?: MutationOptions<E>,
): UseMutationResult<T, E> {
  const client = useClient(options?.client);
  const [state, setState] = useState<MutationState<T>>(IDLE);

  /**
   * The hook-level options, read at call time rather than captured. A caller
   * writing `useMutation(useOrderCreate, {place: {...}})` mints a fresh object
   * literal every render, and closing over it would rebuild `mutate` every
   * render for a value that is semantically unchanged.
   *
   * Published from an effect rather than assigned during render, and the
   * effect deliberately has no dependency array so it runs after **every**
   * commit. Assigning during render is idempotent under StrictMode's double
   * invocation, so it looks harmless, but under concurrent rendering a render
   * that is started and then thrown away would still have published its
   * options -- and a mutation fired afterwards would run with callbacks from a
   * tree that was never committed and that the user never saw.
   */
  const latest = useRef(options);

  useEffect(() => {
    latest.current = options;
  });

  /**
   * Whether this component is still on screen, so a response that lands after
   * it has gone does not schedule a state update nobody will read.
   *
   * The effect **re-arms on mount** rather than only disarming on cleanup, and
   * that single line is the whole StrictMode story for this file. Development
   * double-invokes effects: mount, unmount, mount. The obvious spelling --
   * `useRef(true)` with a cleanup that sets it to `false` and nothing that
   * ever sets it back -- is therefore permanently disarmed by the *phantom*
   * unmount, before the component has rendered anything a user could click.
   * Every mutation fired afterwards runs, hits the server, commits its
   * entities and invalidates its tags, and then silently declines to leave
   * `pending`: the button spins forever while the write it performed is
   * plainly visible in the list beside it.
   *
   * It reproduces in development and nowhere else, which is precisely what
   * makes it expensive. It is reported as "works in production, broken
   * locally", which sounds like a local environment problem, and is dismissed.
   *
   * `useRef(true)` rather than `useRef(false)` for the initial value, because
   * passive effects run after the commit and a mutation fired from a layout
   * effect would otherwise find the hook disarmed on the very first mount.
   */
  const alive = useRef(true);

  useEffect(() => {
    alive.current = true;

    return () => {
      alive.current = false;
    };
  }, []);

  // Distinguishes concurrent calls. Two rapid submits must not have the first
  // response overwrite the second's, whichever order they land in.
  const seq = useRef(0);

  const mutateAsync = useCallback(
    async (args?: TagContext, perCall?: MutationOptions<E>): Promise<T> => {
      const call = ++seq.current;

      setState(PENDING);

      try {
        const data = await op(args, { ...latest.current, ...perCall, client });

        if (alive.current && seq.current === call) {
          setState({ status: 'success', data, error: undefined, isPending: false });
        }

        return data;
      } catch (error) {
        if (alive.current && seq.current === call) {
          setState({ status: 'error', data: undefined, error, isPending: false });
        }

        throw error;
      }
    },
    [op, client],
  );

  /**
   * The safe spelling: the rejection is consumed here, by the one place that
   * can be sure the failure has already been recorded in `status` and `error`.
   *
   * `.catch` rather than a second copy of the body, so there is exactly one
   * implementation of what a mutation does and no way for the two entry points
   * to disagree about it.
   */
  const mutate = useCallback(
    (args?: TagContext, perCall?: MutationOptions<E>): Promise<T | undefined> =>
      mutateAsync(args, perCall).catch(() => undefined),
    [mutateAsync],
  );

  const reset = useCallback(() => {
    // Supersede anything in flight, so its result does not land after a reset.
    seq.current++;
    setState(IDLE);
  }, []);

  return useMemo(
    () => ({ ...state, mutate, mutateAsync, reset }),
    [state, mutate, mutateAsync, reset],
  );
}
