import { useState } from 'react';
import type { ReactNode } from 'react';
import { act, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { setClient } from '@forge-go/client-core';
import { ClientProvider, useInvalidate, useQuery } from '../src';
import type { Invalidate } from '../src';
import {
  harness,
  orderGet,
  orderList,
  orderSearch,
  useOrderCreate,
  useOrderGet,
  useOrderList,
  useOrderSearch,
} from './harness';
import type { Harness, Order } from './harness';

/** Let every already-queued microtask run, inside React's act scope. */
async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 8; i++) await Promise.resolve();
  });
}

/**
 * Run the invalidator's pending batch and let the refetches it started settle.
 *
 * `invalidate` is deliberately not awaitable -- it marks queries stale and
 * returns -- so a test asserting on the requests it caused has to drive the
 * same scheduler the application's microtask would have driven.
 */
async function settle(h: Harness): Promise<void> {
  await act(async () => {
    h.scheduler.flush();
  });
  await flush();
}

function wrap(h: Harness, children: ReactNode): ReactNode {
  return <ClientProvider client={h.cache}>{children}</ClientProvider>;
}

describe('useInvalidate', () => {
  it('refreshes a list held by a sibling that this component cannot see', async () => {
    let served = 0;
    const h = harness(() => [{ id: 1, total: ++served }]);

    let invalidate!: Invalidate;

    function List() {
      const { data } = useQuery<Order[]>(useOrderList);

      return <div data-testid="list">{data?.[0]?.total ?? '-'}</div>;
    }

    // The migration case exactly: a dialog that holds no query of its own,
    // imports no component, and still has to refresh what its write changed.
    function Dialog() {
      invalidate = useInvalidate();

      return null;
    }

    render(
      wrap(
        h,
        <>
          <List />
          <Dialog />
        </>,
      ),
    );
    await flush();

    expect(screen.getByTestId('list').textContent).toBe('1');

    await act(async () => {
      invalidate(useOrderList);
    });
    await settle(h);

    expect(screen.getByTestId('list').textContent).toBe('2');
  });

  it('reaches a query the tag graph cannot, because it declares no tags', async () => {
    const h = harness((request) => {
      if (request.meta === orderSearch) return [{ id: 1, total: h.transport.countOf(orderSearch) }];

      return { id: 2, total: 0 };
    });

    let invalidate!: Invalidate;

    function Search() {
      const { data } = useQuery<Order[]>(useOrderSearch);
      invalidate = useInvalidate();

      return <div data-testid="search">{data?.[0]?.total ?? '-'}</div>;
    }

    render(wrap(h, <Search />));
    await flush();

    expect(screen.getByTestId('search').textContent).toBe('1');

    // A create declaring `invalidates: ['Order[]']`. The search carries
    // `Order:1` from its own response and nothing else, so the tag graph has
    // no edge to it and this write is invisible -- which is the whole defect.
    await act(async () => {
      await h.cache.mutate(useOrderCreate.meta, { body: { total: 7 } });
      h.scheduler.flush();
    });
    await flush();

    expect(h.transport.countOf(orderSearch)).toBe(1);

    // Addressed by operation, it refetches regardless of what it declares.
    await act(async () => {
      invalidate(useOrderSearch);
    });
    await settle(h);

    expect(h.transport.countOf(orderSearch)).toBe(2);
    expect(screen.getByTestId('search').textContent).toBe('2');
  });

  it('hits every argument variant when no arguments are given', async () => {
    const h = harness((request) => ({
      id: request.args.path?.['id'],
      total: h.transport.calls.length,
    }));

    let invalidate!: Invalidate;

    function Detail({ id }: { id: number }) {
      const { data } = useQuery<Order>(useOrderGet, { path: { id } });

      return <div data-testid={`detail-${id}`}>{data?.total ?? '-'}</div>;
    }

    function Toolbar() {
      invalidate = useInvalidate();

      return null;
    }

    render(
      wrap(
        h,
        <>
          <Detail id={1} />
          <Detail id={2} />
          <Toolbar />
        </>,
      ),
    );
    await flush();

    expect(h.transport.countOf(orderGet)).toBe(2);

    await act(async () => {
      invalidate(useOrderGet);
    });
    await settle(h);

    // Both variants, not just one, and not the whole cache either.
    expect(h.transport.countOf(orderGet)).toBe(4);
  });

  it('targets exactly one variant when arguments are given', async () => {
    const h = harness((request) => ({
      id: request.args.path?.['id'],
      total: h.transport.calls.length,
    }));

    let invalidate!: Invalidate;

    function Detail({ id }: { id: number }) {
      const { data } = useQuery<Order>(useOrderGet, { path: { id } });

      return <div data-testid={`detail-${id}`}>{data?.total ?? '-'}</div>;
    }

    function Toolbar() {
      invalidate = useInvalidate();

      return null;
    }

    render(
      wrap(
        h,
        <>
          <Detail id={1} />
          <Detail id={2} />
          <Toolbar />
        </>,
      ),
    );
    await flush();
    expect(h.transport.countOf(orderGet)).toBe(2);

    await act(async () => {
      invalidate(useOrderGet, { path: { id: 1 } });
    });
    await settle(h);

    expect(h.transport.countOf(orderGet)).toBe(3);
  });

  it('refetches a query fetched with no arguments at all', async () => {
    // The `open` args trap. `useQuery(useOrderList)` keys as `GET /orders`
    // while its registry entry holds `{}`, which re-derives as
    // `GET /orders|{}`. Refetching the entry's own `args` would open a second,
    // empty record and refresh nothing the component is watching.
    let served = 0;
    const h = harness(() => [{ id: 1, total: ++served }]);

    let invalidate!: Invalidate;

    function List() {
      const { data } = useQuery<Order[]>(useOrderList);
      invalidate = useInvalidate();

      return <div data-testid="list">{data?.[0]?.total ?? '-'}</div>;
    }

    render(wrap(h, <List />));
    await flush();

    const size = h.cache.size;

    await act(async () => {
      invalidate(useOrderList);
    });
    await settle(h);

    expect(screen.getByTestId('list').textContent).toBe('2');
    // No second record was opened behind the component's back.
    expect(h.cache.size).toBe(size);
  });

  it('does not fetch an unmounted query now, and refetches it on its next mount', async () => {
    let served = 0;
    const h = harness(() => ({ id: 1, total: ++served }));

    let invalidate!: Invalidate;
    let show!: (visible: boolean) => void;

    function Detail() {
      const { data } = useQuery<Order>(useOrderGet, { path: { id: 1 } });

      return <div data-testid="detail">{data?.total ?? '-'}</div>;
    }

    function Screen_() {
      const [visible, setVisible] = useState(true);

      show = setVisible;
      invalidate = useInvalidate();

      return visible ? <Detail /> : null;
    }

    render(wrap(h, <Screen_ />));
    await flush();
    expect(h.transport.countOf(orderGet)).toBe(1);

    await act(async () => {
      show(false);
    });
    await flush();

    await act(async () => {
      invalidate(useOrderGet);
    });
    await settle(h);

    // Nobody is watching it, so nothing is fetched: a write must not stampede
    // the network for every list the user has navigated away from.
    expect(h.transport.countOf(orderGet)).toBe(1);

    await act(async () => {
      show(true);
    });
    await settle(h);

    // The staleness was remembered, and paid for at the moment it matters.
    expect(h.transport.countOf(orderGet)).toBe(2);
    expect(screen.getByTestId('detail').textContent).toBe('2');
  });

  it('resolves refetch only once the mounted query has settled', async () => {
    let served = 0;
    const h = harness(() => [{ id: 1, total: ++served }]);

    let invalidate!: Invalidate;

    function List() {
      const { data } = useQuery<Order[]>(useOrderList);
      invalidate = useInvalidate();

      return <div data-testid="list">{data?.[0]?.total ?? '-'}</div>;
    }

    render(wrap(h, <List />));
    await flush();

    expect(screen.getByTestId('list').textContent).toBe('1');

    await act(async () => {
      await invalidate.refetch(useOrderList);
    });

    // Awaited, so the new value is on screen with no scheduler flush and no
    // second act: this is the spelling a dialog uses before it closes.
    expect(screen.getByTestId('list').textContent).toBe('2');
    expect(h.transport.countOf(orderList)).toBe(2);

    // And the batch the mark queued must not spend a second request on it.
    await settle(h);
    expect(h.transport.countOf(orderList)).toBe(2);
  });

  it('forwards tags to the tag graph', async () => {
    let served = 0;
    const h = harness(() => [{ id: 1, total: ++served }]);

    let invalidate!: Invalidate;

    function List() {
      const { data } = useQuery<Order[]>(useOrderList);
      invalidate = useInvalidate();

      return <div data-testid="list">{data?.[0]?.total ?? '-'}</div>;
    }

    render(wrap(h, <List />));
    await flush();

    await act(async () => {
      invalidate.tags(['Order[]']);
    });
    await settle(h);

    expect(screen.getByTestId('list').textContent).toBe('2');
  });

  it('keeps its identity across re-renders, so it is safe in a dependency array', async () => {
    const h = harness(() => [{ id: 1, total: 1 }]);

    const seen: Invalidate[] = [];
    let bump!: () => void;

    function Toolbar() {
      const [, setTick] = useState(0);

      bump = () => setTick((n) => n + 1);
      seen.push(useInvalidate());

      return null;
    }

    render(wrap(h, <Toolbar />));
    await flush();

    await act(async () => {
      bump();
    });

    expect(seen.length).toBeGreaterThan(1);
    expect(new Set(seen).size).toBe(1);
  });

  it('resolves its cache explicitly, then provided, then global', async () => {
    const global_ = harness(() => [{ id: 1, total: 1 }]);
    const provided = harness(() => [{ id: 1, total: 2 }]);
    const explicit = harness(() => [{ id: 1, total: 3 }]);

    setClient(global_.cache);

    let byDefault!: Invalidate;
    let byOverride!: Invalidate;

    // One mounted list in each cache, so each has something to refresh.
    function Lists() {
      useQuery<Order[]>(useOrderList, undefined, { client: global_.cache });
      useQuery<Order[]>(useOrderList, undefined, { client: provided.cache });
      useQuery<Order[]>(useOrderList, undefined, { client: explicit.cache });

      byDefault = useInvalidate();
      byOverride = useInvalidate(explicit.cache);

      return null;
    }

    render(
      <ClientProvider client={provided.cache}>
        <Lists />
      </ClientProvider>,
    );
    await flush();

    expect(global_.transport.countOf(orderList)).toBe(1);

    await act(async () => {
      await byDefault.refetch(useOrderList);
      await byOverride.refetch(useOrderList);
    });

    expect(provided.transport.countOf(orderList)).toBe(2);
    expect(explicit.transport.countOf(orderList)).toBe(2);
    // The provider beat the global, and the override beat the provider.
    expect(global_.transport.countOf(orderList)).toBe(1);
  });
});
