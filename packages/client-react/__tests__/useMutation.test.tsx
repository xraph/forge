import { StrictMode, useState } from 'react';
import type { ReactNode } from 'react';
import { act, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { ClientProvider, useMutation, useQuery } from '../src';
import type { UseMutationResult } from '../src';
import { deferred, harness, orderCreate, orderList, useOrderCreate, useOrderList } from './harness';
import type { Harness, Order } from './harness';

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 8; i++) await Promise.resolve();
  });
}

function wrap(h: Harness, children: ReactNode): ReactNode {
  return <ClientProvider client={h.cache}>{children}</ClientProvider>;
}

describe('useMutation', () => {
  it('reports idle, pending and success around one call', async () => {
    const gate = deferred<unknown>();
    const h = harness(() => gate.promise);

    let handle!: UseMutationResult<Order>;

    function Create() {
      handle = useMutation<Order>(useOrderCreate);

      return <div data-testid="status">{handle.status}</div>;
    }

    render(wrap(h, <Create />));

    expect(screen.getByTestId('status').textContent).toBe('idle');

    let settled: Promise<Order | undefined>;

    await act(async () => {
      settled = handle.mutate({ body: { total: 5 } });
    });

    expect(screen.getByTestId('status').textContent).toBe('pending');
    expect(handle.isPending).toBe(true);

    await act(async () => {
      gate.resolve({ id: 9, total: 5 });
      await settled;
    });

    expect(screen.getByTestId('status').textContent).toBe('success');
    expect(handle.data).toEqual({ id: 9, total: 5 });
    expect(handle.isPending).toBe(false);
  });

  it('records an error, and reset returns it to idle', async () => {
    const h = harness(() => {
      throw new Error('conflict');
    });

    let handle!: UseMutationResult<Order>;

    function Create() {
      handle = useMutation<Order>(useOrderCreate);

      return <div data-testid="status">{handle.status}</div>;
    }

    render(wrap(h, <Create />));

    let caught: unknown;

    await act(async () => {
      await handle.mutateAsync({ body: {} }).catch((error: unknown) => {
        caught = error;
      });
    });

    expect((caught as Error).message).toBe('conflict');
    expect(screen.getByTestId('status').textContent).toBe('error');
    expect((handle.error as Error).message).toBe('conflict');

    await act(async () => {
      handle.reset();
    });

    expect(screen.getByTestId('status').textContent).toBe('idle');
  });

  it('resolves rather than rejecting when the mutation fails', async () => {
    const h = harness(() => {
      throw new Error('conflict');
    });

    let handle!: UseMutationResult<Order>;

    function Create() {
      handle = useMutation<Order>(useOrderCreate);

      return <div data-testid="status">{handle.status}</div>;
    }

    render(wrap(h, <Create />));

    let resolved: Order | undefined | 'never' = 'never';

    await act(async () => {
      // No `.catch`, exactly as the README's onClick writes it.
      resolved = await handle.mutate({ body: {} });
    });

    // The promise settled, and settled by resolving.
    expect(resolved).toBeUndefined();
    // And the failure is not lost -- it is where the interface reads it.
    expect(screen.getByTestId('status').textContent).toBe('error');
    expect((handle.error as Error).message).toBe('conflict');
  });

  it('raises no unhandled rejection from the documented click handler', async () => {
    const h = harness(() => {
      throw new Error('conflict');
    });

    let handle!: UseMutationResult<Order>;

    function Create() {
      handle = useMutation<Order>(useOrderCreate);

      return (
        <button type="button" onClick={() => handle.mutate({ body: {} })}>
          create
        </button>
      );
    }

    render(wrap(h, <Create />));

    const seen: unknown[] = [];
    const listener = (reason: unknown): void => {
      seen.push(reason);
    };

    process.on('unhandledRejection', listener);

    try {
      await act(async () => {
        screen.getByRole('button').click();
      });

      // A zero-delay timer, not a sleep: `unhandledRejection` is emitted once
      // the microtask queue has drained within a tick, so this yields the
      // queue rather than waiting for a duration. The assertion does not
      // depend on how long it takes.
      await new Promise((resolve) => {
        setTimeout(resolve, 0);
      });
    } finally {
      process.off('unhandledRejection', listener);
    }

    // Before the mutate/mutateAsync split this fired once per failed write:
    // console noise in development, and a global handler in production paging
    // someone about an error already on screen.
    expect(seen).toEqual([]);
  });

  it('rejects from mutateAsync, for a caller that sequences on the write', async () => {
    const h = harness(() => {
      throw new Error('conflict');
    });

    let handle!: UseMutationResult<Order>;

    function Create() {
      handle = useMutation<Order>(useOrderCreate);

      return <div data-testid="status">{handle.status}</div>;
    }

    render(wrap(h, <Create />));

    await act(async () => {
      await expect(handle.mutateAsync({ body: {} })).rejects.toThrow('conflict');
    });

    // Same state either way. The only difference is who owns the failure.
    expect(screen.getByTestId('status').textContent).toBe('error');
  });

  it('updates the queries the mutation invalidated', async () => {
    let next = 1;
    const h = harness((request) => {
      if (request.meta === orderCreate) return { id: 9, total: 5 };

      return [{ id: next++, total: 99 }];
    });

    let handle!: UseMutationResult<Order>;

    function Screen_() {
      const list = useQuery<Order[]>(useOrderList);

      handle = useMutation<Order>(useOrderCreate);

      return <div data-testid="list">{list.data?.[0]?.id ?? '-'}</div>;
    }

    render(wrap(h, <Screen_ />));
    await flush();

    expect(screen.getByTestId('list').textContent).toBe('1');

    await act(async () => {
      await handle.mutate({ body: { total: 5 } });
      h.scheduler.flush();
    });
    await flush();

    // `POST /orders` declares `Order[]`, the list provides it, so the list
    // refetched -- with no invalidation authored in this component.
    expect(screen.getByTestId('list').textContent).toBe('2');
    expect(h.transport.countOf(orderList)).toBe(2);
  });

  it('applies a placement callback instead of refetching', async () => {
    const h = harness((request) =>
      request.meta === orderCreate ? { id: 9, total: 5 } : [{ id: 1, total: 99 }],
    );

    let handle!: UseMutationResult<Order>;

    function Screen_() {
      const list = useQuery<Order[]>(useOrderList);

      handle = useMutation<Order>(useOrderCreate, {
        place: {
          'Order[]': (created, current) => [created, ...(current as Order[])],
        },
      });

      return <div data-testid="list">{(list.data ?? []).map((o) => o.id).join(',')}</div>;
    }

    render(wrap(h, <Screen_ />));
    await flush();

    expect(screen.getByTestId('list').textContent).toBe('1');

    await act(async () => {
      await handle.mutate({ body: { total: 5 } });
      h.scheduler.flush();
    });
    await flush();

    expect(screen.getByTestId('list').textContent).toBe('9,1');
    // Placed, so the list was never refetched.
    expect(h.transport.countOf(orderList)).toBe(1);
  });

  it('reads the placement callbacks handed to the latest render, not the first', async () => {
    const h = harness((request) =>
      request.meta === orderCreate ? { id: 9, total: 5 } : [{ id: 1, total: 99 }],
    );

    let handle!: UseMutationResult<Order>;
    let bump!: () => void;

    function Screen_() {
      const list = useQuery<Order[]>(useOrderList);
      const [reversed, setReversed] = useState(false);

      bump = () => {
        setReversed(true);
      };
      handle = useMutation<Order>(useOrderCreate, {
        // A fresh object literal every render -- the shape a caller actually
        // writes, and the reason the options are read through a ref rather
        // than captured in the callback's closure.
        place: {
          'Order[]': (created, current) =>
            reversed ? [...(current as Order[]), created] : [created, ...(current as Order[])],
        },
      });

      return <div data-testid="list">{(list.data ?? []).map((o) => o.id).join(',')}</div>;
    }

    render(wrap(h, <Screen_ />));
    await flush();

    await act(async () => {
      bump();
    });

    await act(async () => {
      await handle.mutate({ body: { total: 5 } });
      h.scheduler.flush();
    });
    await flush();

    expect(screen.getByTestId('list').textContent).toBe('1,9');
  });

  it('lets the last of two overlapping calls win', async () => {
    const first = deferred<unknown>();
    const second = deferred<unknown>();
    const h = harness((_request, call) => (call === 0 ? first.promise : second.promise));

    let handle!: UseMutationResult<Order>;

    function Create() {
      handle = useMutation<Order>(useOrderCreate);

      return <div data-testid="total">{handle.data?.total ?? '-'}</div>;
    }

    render(wrap(h, <Create />));

    let a!: Promise<Order | undefined>;
    let b!: Promise<Order | undefined>;

    await act(async () => {
      a = handle.mutate({ body: { total: 1 } });
      b = handle.mutate({ body: { total: 2 } });
    });

    // The second settles first, then the first: the stale answer must not
    // overwrite the fresh one.
    await act(async () => {
      second.resolve({ id: 2, total: 2 });
      await b;
      first.resolve({ id: 1, total: 1 });
      await a;
    });
    await flush();

    expect(screen.getByTestId('total').textContent).toBe('2');
  });

  it('still reports status after a StrictMode mount / unmount / mount', async () => {
    const h = harness(() => ({ id: 9, total: 5 }));

    let handle!: UseMutationResult<Order>;

    function Create() {
      handle = useMutation<Order>(useOrderCreate);

      return <div data-testid="status">{handle.status}</div>;
    }

    render(
      <StrictMode>
        <ClientProvider client={h.cache}>
          <Create />
        </ClientProvider>
      </StrictMode>,
    );

    // Development mounted, unmounted and remounted this component. A liveness
    // flag initialised to `true` and cleared by the cleanup is never restored
    // by the second mount, and every result after this point is discarded --
    // in development only, which is why it gets reported as "works in
    // production, broken locally" and dismissed.
    await act(async () => {
      await handle.mutate({ body: { total: 5 } });
    });
    await flush();

    expect(screen.getByTestId('status').textContent).toBe('success');
    expect(handle.data).toEqual({ id: 9, total: 5 });
  });
});
