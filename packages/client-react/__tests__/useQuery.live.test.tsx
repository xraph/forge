import { StrictMode, useState } from 'react';
import type { ReactNode } from 'react';
import { act, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { ForgeProvider, useQuery } from '../src';
import { liveHarness, useOrderGet, useOrderList } from './harness';
import type { LiveHarness, Order } from './harness';

/** Let every already-queued microtask run, inside React's act scope. */
async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 8; i++) await Promise.resolve();
  });
}

function wrap(h: LiveHarness, children: ReactNode): ReactNode {
  return <ForgeProvider client={h.cache}>{children}</ForgeProvider>;
}

/** Deliver a frame and commit it, inside `act` so React sees the update. */
async function emit(h: LiveHarness, message: unknown): Promise<void> {
  await act(async () => {
    h.emit(message);

    for (let i = 0; i < 8; i++) await Promise.resolve();
  });
}

/**
 * The mounted subscription count for the release scheduler to actually act on.
 *
 * A socket whose last subscriber went away is not closed on the spot -- the
 * deferred close is what makes React's development double-invoke free -- so
 * every assertion about a *release* has to say when the deferral elapsed.
 */
function settleCloses(h: LiveHarness): void {
  act(() => {
    h.closes.flush();
  });
}

function List({ live }: { live?: boolean }) {
  const { data, status } = useQuery<Order[]>(useOrderList, undefined, { live: live ?? false });

  return (
    <div data-testid="list">
      {status}:{data?.map((order) => order.total).join(',') ?? '-'}
    </div>
  );
}

describe('useQuery({live})', () => {
  it('updates from a frame, with no request behind it', async () => {
    const h = liveHarness(() => [{ id: 7, total: 99 }]);

    render(wrap(h, <List live />));
    await flush();

    expect(screen.getByTestId('list').textContent).toBe('success:99');
    expect(h.transport.calls).toHaveLength(1);

    await emit(h, { type: 'order.updated', payload: { id: 7, total: 100 } });

    // The whole claim of the design: the value moved, and not one request was
    // spent on it. `order.updated` is a `patch`, so it invalidates nothing.
    expect(screen.getByTestId('list').textContent).toBe('success:100');
    expect(h.transport.calls).toHaveLength(1);
  });

  it('is one subscription for two components on the same live query', async () => {
    const h = liveHarness(() => [{ id: 7, total: 99 }]);

    render(
      wrap(
        h,
        <>
          <List live />
          <List live />
        </>,
      ),
    );
    await flush();

    // One socket, and one subscription on it. Two components must not mean two
    // connections -- a connection count that grows with the render tree is
    // precisely what the ref counting exists to prevent, and an adapter that
    // subscribed per component rather than per query would defeat it silently.
    expect(h.opened).toHaveLength(1);
    expect(h.manager.size).toBe(1);
    expect(h.manager.connected('/ws/orders')).toBe(true);
  });

  it('is one channel for two different live queries on the same entity', async () => {
    const h = liveHarness((request) =>
      request.meta.path === '/orders' ? [{ id: 7, total: 99 }] : { id: 7, total: 99 },
    );

    function Detail() {
      const { data } = useQuery<Order>(useOrderGet, { path: { id: 7 } }, { live: true });

      return <div data-testid="detail">{data?.total ?? '-'}</div>;
    }

    render(
      wrap(
        h,
        <>
          <List live />
          <Detail />
        </>,
      ),
    );
    await flush();

    // Two distinct queries, two cache keys, two requests -- and one socket,
    // because `Order` is pushed on one channel and the manager multiplexes.
    expect(h.transport.calls).toHaveLength(2);
    expect(h.opened).toHaveLength(1);
    expect(h.manager.size).toBe(1);

    // And one frame updates both of them.
    await emit(h, { type: 'order.updated', payload: { id: 7, total: 100 } });

    expect(screen.getByTestId('list').textContent).toBe('success:100');
    expect(screen.getByTestId('detail').textContent).toBe('100');
  });

  it('releases the socket when the last consumer unmounts, and not before', async () => {
    const h = liveHarness(() => [{ id: 7, total: 99 }]);

    function Pair() {
      const [both, setBoth] = useState(true);

      return (
        <>
          <List live />
          {both ? <List live /> : null}
          <button type="button" onClick={() => setBoth(false)} data-testid="drop">
            drop
          </button>
        </>
      );
    }

    render(wrap(h, <Pair />));
    await flush();

    expect(h.live()).toBe(1);

    // One of the two goes away. The other still wants the channel.
    await act(async () => {
      screen.getByTestId('drop').click();
      await Promise.resolve();
    });
    settleCloses(h);

    expect(h.live()).toBe(1);
  });

  it('releases the socket on the framework teardown path', async () => {
    const h = liveHarness(() => [{ id: 7, total: 99 }]);

    const view = render(wrap(h, <List live />));
    await flush();

    expect(h.live()).toBe(1);

    // React's own teardown -- unmounting the tree -- runs the effect cleanup,
    // which is the only place this adapter holds the release.
    view.unmount();
    settleCloses(h);

    expect(h.live()).toBe(0);
    expect(h.manager.size).toBe(0);
  });

  it('survives a StrictMode double-invoke without tearing the subscription down', async () => {
    const h = liveHarness(() => [{ id: 7, total: 99 }]);

    render(
      <StrictMode>
        <ForgeProvider client={h.cache}>
          <List live />
        </ForgeProvider>
      </StrictMode>,
    );
    await flush();
    // The deferred close, had the phantom unmount queued one, elapses here.
    settleCloses(h);

    // Development ran the effect, cleaned it up, and ran it again. Exactly one
    // socket must survive: zero is a live query that silently stops updating
    // in development only, and two is a connection per mount forever.
    expect(h.opened).toHaveLength(1);
    expect(h.live()).toBe(1);

    // And it still works, which is the assertion that would catch a socket
    // that was closed and replaced by one nothing is listening on.
    await emit(h, { type: 'order.updated', payload: { id: 7, total: 100 } });

    expect(screen.getByTestId('list').textContent).toBe('success:100');
  });

  it('subscribes and unsubscribes as `live` toggles, without refetching', async () => {
    const h = liveHarness(() => [{ id: 7, total: 99 }]);

    function Toggle() {
      const [live, setLive] = useState(false);

      return (
        <>
          <List live={live} />
          <button type="button" onClick={() => setLive((on) => !on)} data-testid="toggle">
            toggle
          </button>
        </>
      );
    }

    render(wrap(h, <Toggle />));
    await flush();

    expect(h.opened).toHaveLength(0);
    expect(h.transport.calls).toHaveLength(1);

    // Off -> on. A socket appears; the query is untouched.
    await act(async () => {
      screen.getByTestId('toggle').click();
      await Promise.resolve();
    });

    expect(h.live()).toBe(1);
    expect(h.transport.calls).toHaveLength(1);
    expect(screen.getByTestId('list').textContent).toBe('success:99');

    await emit(h, { type: 'order.updated', payload: { id: 7, total: 100 } });

    expect(screen.getByTestId('list').textContent).toBe('success:100');

    // On -> off. The socket goes; the query keeps the value it has, and still
    // no second request -- `live` is not a hidden refetch trigger in either
    // direction.
    await act(async () => {
      screen.getByTestId('toggle').click();
      await Promise.resolve();
    });
    settleCloses(h);

    expect(h.live()).toBe(0);
    expect(h.transport.calls).toHaveLength(1);
    expect(screen.getByTestId('list').textContent).toBe('success:100');

    // And it really is deaf now: a frame delivered onto the closed socket
    // reaches nothing.
    await emit(h, { type: 'order.updated', payload: { id: 7, total: 250 } });

    expect(screen.getByTestId('list').textContent).toBe('success:100');
  });

  it('opts a query out entirely when `live` is not asked for', async () => {
    const h = liveHarness(() => [{ id: 7, total: 99 }]);

    render(wrap(h, <List />));
    await flush();

    // The opt-in is real at the only level it can be: nothing subscribed, so
    // no socket was ever opened and there is no frame to arrive. This is what
    // "a developer reading the component can tell whether it holds a socket"
    // means operationally -- the absence of the word is the absence of the
    // connection.
    expect(h.opened).toHaveLength(0);
    expect(h.manager.size).toBe(0);

    await emit(h, { type: 'order.updated', payload: { id: 7, total: 100 } });

    expect(screen.getByTestId('list').textContent).toBe('success:99');
    expect(h.transport.calls).toHaveLength(1);
  });

  it('re-subscribes for the new principal rather than going deaf', async () => {
    let total = 99;
    const h = liveHarness(() => [{ id: 7, total }]);

    render(wrap(h, <List live />));
    await flush();

    const first = h.opened[0];

    total = 5;

    await act(async () => {
      h.cache.setPrincipal('user-b');

      for (let i = 0; i < 8; i++) await Promise.resolve();
    });

    // The previous principal's socket is gone -- a socket that outlived the
    // identity change would push the previous session's entities into the new
    // one's store -- and a replacement was opened for the new principal
    // without the component doing anything at all.
    expect(first?.closed).toBe(true);
    expect(h.opened).toHaveLength(2);
    expect(h.live()).toBe(1);

    // And the replacement is wired: a frame on it still reaches the query.
    await emit(h, { type: 'order.updated', payload: { id: 7, total: 6 } });

    expect(screen.getByTestId('list').textContent).toBe('success:6');
  });
});
