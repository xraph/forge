import { StrictMode, useState } from 'react';
import type { ReactNode } from 'react';
import { act, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { ClientProvider, useQuery } from '../src';
import type { UseQueryResult } from '../src';
import { harness, orderGet, orderList, useOrderGet, useOrderList, useOrderPatch } from './harness';
import type { Harness, Order } from './harness';

/** Let every already-queued microtask run, inside React's act scope. */
async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 8; i++) await Promise.resolve();
  });
}

interface Recorder<T> {
  readonly renders: UseQueryResult<T>[];
  push(result: UseQueryResult<T>): void;
  readonly count: number;
  last(): UseQueryResult<T>;
}

/** A component's render log: what it saw, in order, and how many times. */
function recorder<T>(): Recorder<T> {
  const renders: UseQueryResult<T>[] = [];

  return {
    renders,
    push: (result) => {
      renders.push(result);
    },
    get count() {
      return renders.length;
    },
    last() {
      const value = renders[renders.length - 1];

      if (value === undefined) throw new Error('component never rendered');

      return value;
    },
  };
}

function wrap(h: Harness, children: ReactNode): ReactNode {
  return <ClientProvider client={h.cache}>{children}</ClientProvider>;
}

describe('useQuery', () => {
  it('renders its value, and re-renders when an entity it depends on changes', async () => {
    let total = 0;
    const h = harness((request) => {
      total += 100;

      return { id: request.args.path?.['id'], total };
    });

    function Detail() {
      const { data, status } = useQuery<Order>(useOrderGet, { path: { id: 1 } });

      return (
        <div data-testid="detail">
          {status}:{data === undefined ? '-' : data.total}
        </div>
      );
    }

    render(wrap(h, <Detail />));

    // The subscription started a request during the mount effect, so the first
    // paint the test can observe is the loading branch.
    expect(screen.getByTestId('detail').textContent).toBe('pending:-');

    await flush();

    expect(screen.getByTestId('detail').textContent).toBe('success:100');

    // Patching order 1 invalidates `Order:1`, which is this query's own tag --
    // acquired both from `provides` and from the entity its response
    // normalized to.
    await act(async () => {
      await h.cache.mutate(useOrderPatch.meta, { path: { id: 1 } });
      h.scheduler.flush();
    });
    await flush();

    expect(screen.getByTestId('detail').textContent).toBe('success:300');
  });

  it('does not re-render a component reading an unchanged sibling entity', async () => {
    const h = harness((request) => ({
      id: request.args.path?.['id'],
      total: h.transport.calls.length,
    }));

    const one = recorder<Order>();
    const two = recorder<Order>();

    function Detail({ id, seen }: { id: number; seen: Recorder<Order> }) {
      const result = useQuery<Order>(useOrderGet, { path: { id } });

      seen.push(result);

      return <div>{result.data?.total ?? '-'}</div>;
    }

    render(
      wrap(
        h,
        <>
          <Detail id={1} seen={one} />
          <Detail id={2} seen={two} />
        </>,
      ),
    );
    await flush();

    const settled = two.count;

    await act(async () => {
      await h.cache.mutate(useOrderPatch.meta, { path: { id: 1 } });
      h.scheduler.flush();
    });
    await flush();

    // Order 1 moved, so its component did.
    expect(one.count).toBeGreaterThan(settled);
    // Order 2 did not, and neither did its component. This is what the three
    // chunks below this one were built for: a write to `Order:1` is invisible
    // to everything that does not reference it.
    expect(two.count).toBe(settled);
  });

  it('keeps the identity of an entity a refetch did not change', async () => {
    // A fresh object literal on every call: structurally equal, referentially
    // new. Anything short of real structural sharing fails this.
    const h = harness(() => [{ id: 1, total: 99 }]);

    const seen = recorder<Order[]>();

    function List() {
      const result = useQuery<Order[]>(useOrderList);

      seen.push(result);

      return <div>{result.data?.length ?? 0}</div>;
    }

    render(wrap(h, <List />));
    await flush();

    const first = seen.last().data;

    expect(first).toEqual([{ id: 1, total: 99 }]);

    await act(async () => {
      await seen.last().refetch();
    });
    await flush();

    expect(h.transport.countOf(orderList)).toBe(2);
    // `Order:1` is addressable and provably unchanged, so it is the same
    // object it was, and a `memo`'d row rendering it skips.
    expect(seen.last().data?.[0]).toBe(first?.[0]);
    // So is the array around it. The refetch built a second skeleton, but
    // every element of the rebuilt list is identical to the one before it, so
    // the store hands back the container it already had rather than a new one
    // holding the same things. See `EntityStore#read`.
    expect(seen.last().data).toBe(first);
  });

  it('gives the list a new identity when the refetch really did change it', async () => {
    let total = 99;
    const h = harness(() => [{ id: 1, total }]);
    const seen = recorder<Order[]>();

    function List() {
      const result = useQuery<Order[]>(useOrderList);

      seen.push(result);

      return <div>{result.data?.length ?? 0}</div>;
    }

    render(wrap(h, <List />));
    await flush();

    const first = seen.last().data;

    total = 120;

    await act(async () => {
      await seen.last().refetch();
    });
    await flush();

    expect(seen.last().data).not.toBe(first);
    expect(seen.last().data?.[0]).not.toBe(first?.[0]);
    expect(seen.last().data?.[0]?.total).toBe(120);
  });

  it('hands React a getSnapshot whose result survives unrelated re-renders', async () => {
    const h = harness(() => [{ id: 1, total: 99 }]);

    let bump!: () => void;
    const seen = recorder<Order[]>();

    function List() {
      // An inline argument object: a new literal on every single render, which
      // is exactly how a caller writes it.
      const result = useQuery<Order[]>(useOrderList, { query: { status: 'open' } });
      const [, setTick] = useState(0);

      bump = () => {
        setTick((n) => n + 1);
      };
      seen.push(result);

      return <div>{result.data?.length ?? 0}</div>;
    }

    render(wrap(h, <List />));
    await flush();

    const entry = h.cache.registry.get(h.cache.key(orderList, { query: { status: 'open' } }));

    expect(entry?.mounts).toBe(1);

    const settled = seen.last();

    // Ten parent-driven re-renders. A `getSnapshot` returning a fresh object
    // would loop until React's update-depth limit; a `subscribe` rebuilt per
    // render would churn the mount count and could drop the cache entry.
    for (let i = 0; i < 10; i++) {
      await act(async () => {
        bump();
      });
    }

    // The same result object, ten renders later: the snapshot React read is
    // the identical `QueryState` every time, so nothing downstream of this
    // hook sees a change that did not happen.
    expect(seen.last()).toBe(settled);
    expect(entry?.mounts).toBe(1);
    // No resubscription, so no second request.
    expect(h.transport.countOf(orderList)).toBe(1);
  });

  it('serves two components on one query from a single request', async () => {
    const h = harness(() => [{ id: 1, total: 99 }]);

    function List({ testid }: { testid: string }) {
      const { data } = useQuery<Order[]>(useOrderList);

      return <div data-testid={testid}>{data?.[0]?.total ?? '-'}</div>;
    }

    render(
      wrap(
        h,
        <>
          <List testid="a" />
          <List testid="b" />
        </>,
      ),
    );
    await flush();

    expect(h.transport.countOf(orderList)).toBe(1);
    expect(screen.getByTestId('a').textContent).toBe('99');
    expect(screen.getByTestId('b').textContent).toBe('99');
    // One entry with two listeners, not two entries: the tag index must not
    // fan one invalidation out into two refetches of identical data.
    expect(h.cache.registry.mounted).toBe(1);
    expect(h.cache.registry.get(h.cache.key(orderList))?.mounts).toBe(1);
  });

  it('releases the subscription when the last consumer unmounts', async () => {
    const h = harness(() => [{ id: 1, total: 99 }]);

    function List() {
      const { data } = useQuery<Order[]>(useOrderList);

      return <div>{data?.length ?? 0}</div>;
    }

    function Shell({ show }: { show: number }) {
      return (
        <>
          {show > 0 ? <List /> : null}
          {show > 1 ? <List /> : null}
        </>
      );
    }

    const view = render(wrap(h, <Shell show={2} />));

    await flush();

    const key = h.cache.key(orderList);

    expect(h.cache.registry.get(key)?.mounts).toBe(1);

    // One of two consumers goes away: still mounted.
    view.rerender(wrap(h, <Shell show={1} />));
    await flush();
    expect(h.cache.registry.get(key)?.mounts).toBe(1);
    expect(h.cache.registry.mounted).toBe(1);

    // The last one: released.
    view.rerender(wrap(h, <Shell show={0} />));
    await flush();
    expect(h.cache.registry.get(key)?.mounts).toBe(0);
    expect(h.cache.registry.mounted).toBe(0);
  });

  it('survives a StrictMode mount / unmount / mount with a live subscription', async () => {
    let total = 0;
    const h = harness(() => {
      total += 10;

      return [{ id: 1, total }];
    });

    function List() {
      const { data } = useQuery<Order[]>(useOrderList);

      return <div data-testid="list">{data?.[0]?.total ?? '-'}</div>;
    }

    render(
      <StrictMode>
        <ClientProvider client={h.cache}>
          <List />
        </ClientProvider>
      </StrictMode>,
    );
    await flush();

    // Development double-invokes effects, so `subscribe` ran, was cleaned up,
    // and ran again. Exactly one mount must survive -- not zero (the query
    // silently stops updating, in development only) and not two (one
    // invalidation fans out into two refetches).
    expect(h.cache.registry.get(h.cache.key(orderList))?.mounts).toBe(1);
    // And the phantom unmount must not have provoked a second request.
    expect(h.transport.countOf(orderList)).toBe(1);
    expect(screen.getByTestId('list').textContent).toBe('10');

    // The subscription is live: an invalidation raised now still lands.
    await act(async () => {
      h.cache.invalidate(['Order[]']);
      h.scheduler.flush();
    });
    await flush();

    expect(screen.getByTestId('list').textContent).toBe('20');
    expect(h.transport.countOf(orderList)).toBe(2);
  });

  it('re-subscribes to the new query when its arguments change', async () => {
    const h = harness((request) => ({ id: request.args.path?.['id'], total: 7 }));

    function Detail({ id }: { id: number }) {
      const { data } = useQuery<Order>(useOrderGet, { path: { id } });

      return <div data-testid="detail">{data?.id ?? '-'}</div>;
    }

    const view = render(wrap(h, <Detail id={1} />));

    await flush();
    expect(screen.getByTestId('detail').textContent).toBe('1');

    view.rerender(wrap(h, <Detail id={2} />));
    await flush();

    expect(screen.getByTestId('detail').textContent).toBe('2');
    expect(h.cache.registry.get(h.cache.key(orderGet, { path: { id: 1 } }))?.mounts).toBe(0);
    expect(h.cache.registry.get(h.cache.key(orderGet, { path: { id: 2 } }))?.mounts).toBe(1);
  });

  it('keeps the last good value beside an error from a failed refetch', async () => {
    const h = harness((_request, call) => {
      if (call > 0) throw new Error('boom');

      return [{ id: 1, total: 99 }];
    });

    const seen = recorder<Order[]>();

    function List() {
      const result = useQuery<Order[]>(useOrderList);

      seen.push(result);

      return <div>{result.status}</div>;
    }

    render(wrap(h, <List />));
    await flush();

    const good = seen.last().data;

    await act(async () => {
      await seen.last().refetch().catch(() => undefined);
    });
    await flush();

    expect(seen.last().status).toBe('error');
    expect((seen.last().error as Error).message).toBe('boom');
    // Stale data plus a warning beats an empty screen, and the value is the
    // same object it was: a failure does not invalidate identity.
    expect(seen.last().data).toBe(good);
  });

  it('reports a failed first fetch as an error state rather than throwing in render', async () => {
    const h = harness(() => {
      throw new Error('nope');
    });

    function List() {
      const { status, error } = useQuery<Order[]>(useOrderList, undefined, { client: h.cache });

      return (
        <div data-testid="list">
          {status}:{error === undefined ? '-' : (error as Error).message}
        </div>
      );
    }

    render(<List />);
    await flush();

    expect(screen.getByTestId('list').textContent).toBe('error:nope');
  });

  it('passes a per-call staleTime through to the cache', async () => {
    const h = harness(() => [{ id: 1, total: 10 }]);

    function List() {
      useQuery<Order[]>(useOrderList, undefined, { staleTime: 250 });

      return <div data-testid="list" />;
    }

    render(wrap(h, <List />));
    await flush();

    expect(h.cache.effectiveStaleTime(useOrderList.meta)).toBe(250);
  });

  it('rebuilds the handle when staleTime changes', async () => {
    const h = harness(() => [{ id: 1, total: 10 }]);

    function List({ ms }: { ms: number }) {
      useQuery<Order[]>(useOrderList, undefined, { staleTime: ms });

      return <div data-testid="list" />;
    }

    const view = render(wrap(h, <List ms={250} />));
    await flush();
    expect(h.cache.effectiveStaleTime(useOrderList.meta)).toBe(250);

    // The memo-dependency trap. Without `staleTime` in the deps this still
    // reads 250 and the change is silently lost.
    view.rerender(wrap(h, <List ms={50} />));
    await flush();
    expect(h.cache.effectiveStaleTime(useOrderList.meta)).toBe(50);
  });
});
