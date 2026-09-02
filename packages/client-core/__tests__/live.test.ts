import { describe, expect, it, vi } from 'vitest';

import { QueryCache } from '../src/cache';
import { manualScheduler } from '../src/invalidate';
import { applyFrames, binderSnapshot, StreamBinder } from '../src/live';
import type { StreamBinderOptions } from '../src/live';
import { SubscriptionManager } from '../src/stream';
import type { StreamBinding } from '../src/stream';
import { manualClock } from '../src/transport';
import type { OperationMeta } from '../src/transport';
import { fakeSockets, fakeTransport, settleMicrotasks } from './harness';
import { schema } from './schema';

const orderList: OperationMeta = {
  method: 'GET',
  path: '/orders',
  entity: 'Order',
  provides: ['Order[]'],
  invalidates: [],
};

const orderGet: OperationMeta = {
  method: 'GET',
  path: '/orders/{id}',
  entity: 'Order',
  provides: ['Order:{id}'],
  invalidates: [],
};

/** What the generated manifest's `streams` table looks like for one channel. */
const streams: readonly StreamBinding[] = [
  {
    channel: '/ws/orders',
    message: 'order.created',
    entity: 'Order',
    intent: 'upsert',
    invalidates: ['Order[]'],
  },
  {
    channel: '/ws/orders',
    message: 'order.updated',
    entity: 'Order',
    intent: 'patch',
    invalidates: [],
  },
  {
    channel: '/ws/orders',
    message: 'order.deleted',
    entity: 'Order',
    intent: 'evict',
    invalidates: ['Order[]'],
  },
];

function harness(
  handler: Parameters<typeof fakeTransport>[0],
  bindings = streams,
  observe?: (flush: () => void) => void,
  binderOptions: Partial<StreamBinderOptions> = {},
) {
  const transport = fakeTransport(handler);
  const batches = manualScheduler();
  const frames = manualScheduler();
  const release = manualScheduler();
  const clock = manualClock();
  const sockets = fakeSockets();
  const unknown: { message: string; channel: string }[] = [];

  const cache = new QueryCache({ transport, entities: schema, scheduler: batches.schedule });
  const manager = new SubscriptionManager({
    connect: sockets.connect,
    sleep: clock.sleep,
    random: () => 0,
    backoff: { baseDelay: 1000 },
    release: release.schedule,
    principal: () => cache.owner,
  });
  const binder = new StreamBinder({
    cache,
    streams: bindings,
    manager,
    scheduler: (flush) => {
      observe?.(flush);
      frames.schedule(flush);
    },
    onUnknown: (message, channel) => unknown.push({ message, channel }),
    sleep: clock.sleep,
    ...binderOptions,
  });

  return { cache, manager, binder, transport, sockets, batches, frames, release, clock, unknown };
}

describe('intents', () => {
  it('upserts a created entity and invalidates what the binding declares', async () => {
    const { cache, binder, sockets, transport, frames, batches } = harness((_request, call) =>
      call === 0 ? [{ id: 7, total: 99 }] : [{ id: 9, total: 5 }, { id: 7, total: 99 }],
    );

    cache.subscribe(orderList, undefined, () => undefined);
    binder.subscribe(orderList);
    await settleMicrotasks();

    sockets.last().deliver({ type: 'order.created', payload: { id: 9, total: 5 } });
    frames.flush();

    // The entity is in the store before anything reaches the network.
    expect(cache.store.getRecord('Order:9')?.data).toEqual({ id: 9, total: 5 });

    // And `Order[]` was raised, so the list -- which cannot know whether a new
    // order belongs in its window -- refetches.
    batches.flush();
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(2);
    expect(cache.getState(orderList).data).toEqual([
      { id: 9, total: 5 },
      { id: 7, total: 99 },
    ]);
  });

  it('patches an entity with no request at all', async () => {
    const { cache, binder, sockets, transport, frames, batches } = harness(() => [
      { id: 7, total: 99, customer: { id: 'c-3', name: 'Ada' } },
    ]);
    let renders = 0;

    cache.subscribe(orderList, undefined, () => {
      renders++;
    });
    binder.subscribe(orderList);
    await settleMicrotasks();

    const before = renders;
    const customerBefore = (cache.getState(orderList).data as { customer: unknown }[])[0]?.customer;

    sockets.last().deliver({ type: 'order.updated', payload: { id: 7, total: 100 } });
    frames.flush();
    batches.flush();
    await settleMicrotasks();

    // The whole claim of the design, asserted: the list re-rendered, the value
    // is current, and not one request was spent on it.
    expect(renders).toBeGreaterThan(before);
    expect(transport.calls).toHaveLength(1);
    expect(cache.getState(orderList).data).toEqual([
      { id: 7, total: 100, customer: { id: 'c-3', name: 'Ada' } },
    ]);

    // The untouched subtree kept its identity, so a memo'd child skips.
    expect((cache.getState(orderList).data as { customer: unknown }[])[0]?.customer).toBe(
      customerBefore,
    );
  });

  it('evicts a deleted entity, from a record or from a bare id', async () => {
    const { cache, binder, sockets, frames } = harness(() => [
      { id: 7, total: 99 },
      { id: 8, total: 1 },
    ]);

    cache.subscribe(orderList, undefined, () => undefined);
    binder.subscribe(orderList);
    await settleMicrotasks();

    expect(cache.store.has('Order:7')).toBe(true);

    sockets.last().deliver({ type: 'order.deleted', payload: { id: 7 } });
    frames.flush();
    expect(cache.store.has('Order:7')).toBe(false);

    // A delete frame that carries only the identity, which is ordinary.
    sockets.last().deliver({ type: 'order.deleted', payload: 8 });
    frames.flush();
    expect(cache.store.has('Order:8')).toBe(false);

    // The settled skeleton still points at both, and nothing rewrites it -- so
    // the rehydration is where the hole has to be closed. An empty list, never
    // `[undefined, undefined]`.
    expect(cache.getState(orderList).data).toEqual([]);
  });

  it('hands a subscriber a list with no holes in it, synchronously', async () => {
    // The failure this covers throws in application code. `data.map(o => o.id)`
    // over `[undefined, {...}]` is the first thing any list component does, and
    // the subscriber is notified with that value *before* any refetch the
    // eviction triggered can land -- so repairing it on the refetch is too late.
    const { cache, binder, sockets, frames } = harness(() => [
      { id: 7, total: 99 },
      { id: 8, total: 1 },
    ]);
    const rendered: unknown[][] = [];

    cache.subscribe(orderList, undefined, () => {
      rendered.push(cache.getState<unknown[]>(orderList).data ?? []);
    });
    binder.subscribe(orderList);
    await settleMicrotasks();

    sockets.last().deliver({ type: 'order.deleted', payload: { id: 7 } });
    frames.flush();

    // Every value any subscriber could have rendered, not merely the last.
    for (const value of rendered) {
      expect(value).not.toContain(undefined);
      expect(() => value.map((order) => (order as { id: number }).id)).not.toThrow();
    }

    expect(rendered[rendered.length - 1]).toEqual([{ id: 8, total: 1 }]);
  });

  it('refetches a list after a delete whose binding declares no invalidation', async () => {
    // The generator passes `Invalidates` through verbatim; it does not
    // synthesize one. So a delete binding can reach the client declaring
    // nothing, and without a synthesized `Order[]` nothing ever repairs the
    // list -- one transport call, and a permanently short one.
    const silent: readonly StreamBinding[] = [
      {
        channel: '/ws/orders',
        message: 'order.deleted',
        entity: 'Order',
        intent: 'evict',
        invalidates: [],
      },
    ];
    const { cache, binder, sockets, transport, frames, batches } = harness(
      (_request, call) =>
        call === 0
          ? [
              { id: 7, total: 99 },
              { id: 8, total: 1 },
            ]
          : [{ id: 8, total: 1 }],
      silent,
    );

    cache.subscribe(orderList, undefined, () => undefined);
    binder.subscribe(orderList);
    await settleMicrotasks();
    expect(transport.calls).toHaveLength(1);

    sockets.last().deliver({ type: 'order.deleted', payload: { id: 7 } });
    frames.flush();
    batches.flush();
    await settleMicrotasks();

    // An eviction is an entity-level event, so it changes the membership of
    // every collection that held it -- knowable here without the server saying
    // so, which is the reasoning `recover` already applies on a reconnect.
    expect(transport.calls).toHaveLength(2);
    expect(cache.getState(orderList).data).toEqual([{ id: 8, total: 1 }]);
  });

  it('does not synthesize a list tag for a patch', async () => {
    // The counterpart. Doing this for `order.updated` would refetch every
    // mounted list on every update and destroy the property that makes a live
    // query worth having.
    const { cache, binder, sockets, transport, frames, batches } = harness(() => [
      { id: 7, total: 99 },
    ]);

    cache.subscribe(orderList, undefined, () => undefined);
    binder.subscribe(orderList);
    await settleMicrotasks();

    sockets.last().deliver({ type: 'order.updated', payload: { id: 7, total: 100 } });
    frames.flush();
    batches.flush();
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(1);
  });

  it('skips an evict whose payload identifies nothing', async () => {
    const { cache, binder, sockets, frames } = harness(() => [{ id: 7, total: 99 }]);

    cache.subscribe(orderList, undefined, () => undefined);
    binder.subscribe(orderList);
    await settleMicrotasks();

    sockets.last().deliver({ type: 'order.deleted', payload: { total: 99 } });
    frames.flush();

    // Nothing was guessed at, so nothing was deleted.
    expect(cache.store.has('Order:7')).toBe(true);
  });
});

describe('forward compatibility', () => {
  it('ignores a message type no binding claims, with a development warning', async () => {
    const { cache, binder, sockets, frames, unknown } = harness(() => [{ id: 7, total: 99 }]);

    cache.subscribe(orderList, undefined, () => undefined);
    binder.subscribe(orderList);
    await settleMicrotasks();

    expect(() => {
      sockets.last().deliver({ type: 'order.fulfilled', payload: { id: 7, total: 0 } });
    }).not.toThrow();

    frames.flush();

    expect(unknown).toEqual([{ message: 'order.fulfilled', channel: '/ws/orders' }]);
    expect(cache.store.getRecord('Order:7')?.data).toEqual({ id: 7, total: 99 });
    expect(binder.pending).toBe(0);
  });

  it('warns rather than throwing when a manifest carries an intent it cannot act on', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);

    try {
      const transport = fakeTransport(() => []);
      const cache = new QueryCache({ transport, entities: schema });
      const manager = new SubscriptionManager({ connect: fakeSockets().connect });

      const binder = new StreamBinder({
        cache,
        manager,
        streams: [
          {
            channel: '/ws/orders',
            message: 'order.merged',
            entity: 'Order',
            // A manifest from a newer generator, or a hand-edited one.
            intent: 'merge' as never,
            invalidates: [],
          },
        ],
      });

      expect(binder.channelsFor(orderList)).toEqual([]);
      expect(warn).toHaveBeenCalled();
    } finally {
      warn.mockRestore();
    }
  });

  it('drops a message the decoder does not recognise without warning about it', async () => {
    const { cache, binder, sockets, frames, unknown } = harness(() => [{ id: 7, total: 99 }]);

    cache.subscribe(orderList, undefined, () => undefined);
    binder.subscribe(orderList);
    await settleMicrotasks();

    // A keepalive. Not a frame, not an error, and not worth a console line.
    sockets.last().deliver({ ping: 1 });
    frames.flush();

    expect(unknown).toEqual([]);
    expect(binder.pending).toBe(0);
  });
});

describe('write batching', () => {
  it('coalesces a burst of frames into one store commit and one render', async () => {
    const { cache, binder, sockets, frames, batches } = harness(() => [{ id: 7, total: 0 }]);
    let renders = 0;

    cache.subscribe(orderList, undefined, () => {
      renders++;
    });
    binder.subscribe(orderList);
    await settleMicrotasks();

    const before = renders;
    const commits = cache.store.frameVersion;

    for (let i = 1; i <= 200; i++) {
      sockets.last().deliver({ type: 'order.updated', payload: { id: 7, total: i } });
    }

    // Nothing has been written yet: 200 messages, zero commits.
    expect(binder.pending).toBe(200);
    expect(cache.store.getRecord('Order:7')?.data['total']).toBe(0);
    expect(renders).toBe(before);

    frames.flush();
    batches.flush();

    // One commit, one notification, and the last frame's value.
    expect(cache.store.frameVersion).toBe(commits + 1);
    expect(cache.store.getRecord('Order:7')?.data['total']).toBe(200);
    expect(renders).toBe(before + 1);
  });

  it('schedules exactly one flush however many frames arrive', async () => {
    // Fifty callbacks that each find an empty queue is the shape of the bug:
    // the set is what makes N frames one commit, and the flag is what makes it
    // one scheduled callback.
    let scheduled = 0;
    const { cache, binder, sockets, frames } = harness(
      () => [{ id: 7, total: 0 }],
      streams,
      () => {
        scheduled++;
      },
    );

    cache.subscribe(orderList, undefined, () => undefined);
    binder.subscribe(orderList);
    await settleMicrotasks();

    for (let i = 0; i < 50; i++) {
      sockets.last().deliver({ type: 'order.updated', payload: { id: 7, total: i } });
    }

    expect(scheduled).toBe(1);

    frames.flush();

    // And the next burst gets a fresh one rather than being stranded behind a
    // flag nothing cleared.
    sockets.last().deliver({ type: 'order.updated', payload: { id: 7, total: 999 } });
    expect(scheduled).toBe(2);

    frames.flush();
    expect(cache.store.getRecord('Order:7')?.data['total']).toBe(999);
  });
});

describe('gap recovery', () => {
  /**
   * A second channel, on an entity with no edge to or from `Order` in
   * `./schema`, for tests that need two independent sockets.
   *
   * `Widget` deliberately has no entry in `./schema` at all: `Order`'s fields
   * reach `Customer`, `LineItem` and `Invoice` (and `Customer` reaches back to
   * `Order`), so any of those would make `orderList` live on this channel too
   * via `channelsFor`'s reachability walk -- which is correct behaviour for
   * that walk and exactly the entanglement these tests need to avoid to keep
   * the two endpoints under test isolated from each other.
   */
  const crossChannel: readonly StreamBinding[] = [
    ...streams,
    {
      channel: '/ws/widgets',
      message: 'widget.created',
      entity: 'Widget',
      intent: 'upsert',
      invalidates: ['Widget[]'],
    },
  ];

  const widgetList: OperationMeta = {
    method: 'GET',
    path: '/widgets',
    entity: 'Widget',
    provides: ['Widget[]'],
    invalidates: [],
  };

  it('invalidates the channel’s tags and refetches mounted live queries on reconnect', async () => {
    const { cache, binder, sockets, transport, clock, batches } = harness((_request, call) =>
      call === 0 ? [{ id: 7, total: 99 }] : [{ id: 7, total: 4242 }],
    );

    cache.subscribe(orderList, undefined, () => undefined);
    binder.subscribe(orderList);
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(1);

    // The lid closes. Frames are missed, and nothing about the client says so.
    sockets.last().drop();
    await clock.advance(1000);

    expect(sockets.opened).toHaveLength(2);

    // Past the grace window: no `forge.resumed` arrived, so recovery runs.
    await clock.advance(1000);

    batches.flush();
    await settleMicrotasks();

    // Exactly one refetch: the live-query refetch and the `Order[]` batch
    // converge on the same query rather than each spending a request.
    expect(transport.calls).toHaveLength(2);
    expect(cache.getState(orderList).data).toEqual([{ id: 7, total: 4242 }]);
  });

  it('marks a query the channel’s tags reach, even though it is not itself live', async () => {
    // The other half, isolated. A gap makes every list of that entity suspect,
    // not merely the one that happened to hold the socket -- one screen holds
    // `useOrderList({live: true})` while a filtered list elsewhere carries
    // `Order[]` and no subscription of its own.
    const filtered = { query: { status: 'open' } };
    const { cache, binder, sockets, transport, clock, batches } = harness((request) =>
      request.args.query === undefined ? [{ id: 7, total: 99 }] : [{ id: 8, total: 1 }],
    );

    cache.subscribe(orderList, undefined, () => undefined);
    cache.subscribe(orderList, filtered, () => undefined);
    binder.subscribe(orderList);
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(2);

    sockets.last().drop();
    await clock.advance(1000);

    // Past the grace window: no `forge.resumed` arrived, so recovery runs.
    await clock.advance(1000);

    batches.flush();
    await settleMicrotasks();

    // Both refetched: the live one directly, the filtered one through `Order[]`.
    expect(transport.calls).toHaveLength(4);
    expect(
      transport.calls.filter((call) => call.args.query !== undefined),
    ).toHaveLength(2);
  });

  it('refetches a live query no channel tag reaches', async () => {
    // The case the tag invalidation cannot cover. This channel's only patch
    // binding declares no invalidations -- correctly, since a patch changes no
    // membership -- and a detail query provides `Order:7`, not `Order[]`. If
    // recovery were tags alone, the missed updates would stay missed for the
    // life of the session.
    const patchOnly: readonly StreamBinding[] = [
      {
        channel: '/ws/orders',
        message: 'order.updated',
        entity: 'Order',
        intent: 'patch',
        invalidates: [],
      },
    ];
    const { cache, binder, sockets, transport, clock, batches } = harness(
      (_request, call) => (call === 0 ? { id: 7, total: 99 } : { id: 7, total: 4242 }),
      patchOnly,
    );

    cache.subscribe(orderGet, { path: { id: 7 } }, () => undefined);
    binder.subscribe(orderGet, { path: { id: 7 } });
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(1);

    sockets.last().drop();
    await clock.advance(1000);

    // Past the grace window: no `forge.resumed` arrived, so recovery runs.
    await clock.advance(1000);

    batches.flush();
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(2);
    expect(cache.store.getRecord('Order:7')?.data['total']).toBe(4242);
  });

  it('recovers once for a query two components hold live', async () => {
    const { cache, binder, sockets, transport, clock, batches } = harness(() => [
      { id: 7, total: 99 },
    ]);

    cache.subscribe(orderList, undefined, () => undefined);
    const a = binder.subscribe(orderList);
    const b = binder.subscribe(orderList);
    await settleMicrotasks();

    sockets.last().drop();
    await clock.advance(1000);

    // Past the grace window: no `forge.resumed` arrived, so recovery runs.
    await clock.advance(1000);

    batches.flush();
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(2);

    a();
    b();
  });

  it('stops recovering a query whose live subscription was released', async () => {
    const { cache, binder, sockets, transport, clock, batches, release } = harness(() => [
      { id: 7, total: 99 },
    ]);

    cache.subscribe(orderList, undefined, () => undefined);
    const stop = binder.subscribe(orderList);
    await settleMicrotasks();

    const socket = sockets.last();
    stop();
    release.flush();
    socket.drop();

    await clock.advance(60000);
    batches.flush();
    await settleMicrotasks();

    // Nobody is watching the channel, so there is no gap worth a request.
    expect(transport.calls).toHaveLength(1);
  });

  it('declines to make a query live when no channel binds its entity', () => {
    const { binder, unknown } = harness(() => []);

    const release = binder.subscribe({
      method: 'GET',
      path: '/invoices',
      entity: 'Invoice',
      provides: ['Invoice[]'],
      invalidates: [],
    });

    expect(unknown).toHaveLength(1);
    expect(() => {
      release();
    }).not.toThrow();
  });

  it('does not refetch when the server reports a completed replay', async () => {
    const { cache, binder, sockets, transport, clock, batches } = harness((_request, call) =>
      call === 0 ? [{ id: 7, total: 99 }] : [{ id: 7, total: 4242 }],
    );

    cache.subscribe(orderList, undefined, () => undefined);
    binder.subscribe(orderList);
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(1);

    sockets.last().drop();
    await clock.advance(1000);

    // The server replayed the gap and said so.
    sockets.last().deliver({ type: 'forge.resumed', payload: { from: 'e-1', count: 2 } });

    // Well past the grace window: the deferred recovery must have been cancelled,
    // not merely postponed.
    await clock.advance(5000);
    batches.flush();
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(1);
  });

  // Every other test in this file speaks the WebSocket envelope, and the Go
  // server sends the SSE one -- `event`/`data`, which is what `EventSource`
  // dispatches and what the generated SSE client forwards. Without a test on
  // this shape both suites can stay green while the two halves have stopped
  // agreeing on the wire. The payload field names are the ones asserted in
  // `internal/router/streaming_sse_replay_test.go`.
  it('does not refetch when a completed replay arrives in the SSE envelope', async () => {
    const { cache, binder, sockets, transport, clock, batches } = harness((_request, call) =>
      call === 0 ? [{ id: 7, total: 99 }] : [{ id: 7, total: 4242 }],
    );

    cache.subscribe(orderList, undefined, () => undefined);
    binder.subscribe(orderList);
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(1);

    sockets.last().drop();
    await clock.advance(1000);

    sockets.last().deliver({ event: 'forge.resumed', data: { from: 'e-1', count: 2 } });

    await clock.advance(5000);
    batches.flush();
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(1);
  });

  // `sleep` is a caller-supplied option, so it can reject. A rejection with no
  // handler leaves the endpoint pending forever with no timer left to clear it:
  // recovery never runs and never says so, which is the one outcome this whole
  // deferral is not allowed to produce.
  it('recovers when the grace timer rejects', async () => {
    const { cache, binder, sockets, transport, clock, batches } = harness(
      (_request, call) => (call === 0 ? [{ id: 7, total: 99 }] : [{ id: 7, total: 4242 }]),
      undefined,
      undefined,
      { sleep: () => Promise.reject(new Error('timer unavailable')) },
    );

    cache.subscribe(orderList, undefined, () => undefined);
    binder.subscribe(orderList);
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(1);

    sockets.last().drop();
    await clock.advance(1000);

    batches.flush();
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(2);
  });

  it('refetches immediately when the server reports an unfillable gap', async () => {
    const { cache, binder, sockets, transport, clock, batches } = harness((_request, call) =>
      call === 0 ? [{ id: 7, total: 99 }] : [{ id: 7, total: 4242 }],
    );

    cache.subscribe(orderList, undefined, () => undefined);
    binder.subscribe(orderList);
    await settleMicrotasks();

    sockets.last().drop();
    await clock.advance(1000);

    sockets.last().deliver({ type: 'forge.gap', payload: { reason: 'unresumable' } });
    batches.flush();
    await settleMicrotasks();

    // Recovered without waiting out the grace window.
    expect(transport.calls).toHaveLength(2);
  });

  // The fail-safe. A server that knows nothing about replay says nothing, and
  // must land on exactly the behaviour that predates this deferral.
  it('refetches when no control event arrives', async () => {
    const { cache, binder, sockets, transport, clock, batches } = harness((_request, call) =>
      call === 0 ? [{ id: 7, total: 99 }] : [{ id: 7, total: 4242 }],
    );

    cache.subscribe(orderList, undefined, () => undefined);
    binder.subscribe(orderList);
    await settleMicrotasks();

    sockets.last().drop();
    await clock.advance(1000);

    // Nothing yet: the grace window is still open.
    expect(transport.calls).toHaveLength(1);

    await clock.advance(1000);
    batches.flush();
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(2);
  });

  it('refetches without deferral when resumeGrace is 0', async () => {
    const { cache, binder, sockets, transport, clock, batches } = harness(
      (_request, call) => (call === 0 ? [{ id: 7, total: 99 }] : [{ id: 7, total: 4242 }]),
      undefined,
      undefined,
      { resumeGrace: 0 },
    );

    cache.subscribe(orderList, undefined, () => undefined);
    binder.subscribe(orderList);
    await settleMicrotasks();

    sockets.last().drop();
    await clock.advance(1000);

    batches.flush();
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(2);
  });

  it('recovers when the resumed payload is missing or malformed', async () => {
    const { cache, binder, sockets, transport, clock, batches } = harness((_request, call) =>
      call === 0 ? [{ id: 7, total: 99 }] : [{ id: 7, total: 4242 }],
    );

    cache.subscribe(orderList, undefined, () => undefined);
    binder.subscribe(orderList);
    await settleMicrotasks();

    sockets.last().drop();
    await clock.advance(1000);

    // No payload at all: `decodeFrame` makes the bare envelope its own
    // payload, which has neither `from` nor `count`. Trusting `message` alone
    // would cancel recovery on a frame this loose.
    sockets.last().deliver({ type: 'forge.resumed' });

    await clock.advance(1000);
    batches.flush();
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(2);
  });

  it('does not let an ordinary data frame cancel a pending recovery', async () => {
    const { cache, binder, sockets, transport, clock, batches } = harness((_request, call) =>
      call === 0 ? [{ id: 7, total: 99 }] : [{ id: 7, total: 4242 }],
    );

    cache.subscribe(orderList, undefined, () => undefined);
    binder.subscribe(orderList);
    await settleMicrotasks();

    sockets.last().drop();
    await clock.advance(1000);

    // A frame arrives mid-window. A healthy-looking stream is not the claim
    // "the gap was filled" -- only `forge.resumed` is, and this is not that.
    sockets.last().deliver({ type: 'order.updated', payload: { id: 7, total: 500 } });

    await clock.advance(1000);
    batches.flush();
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(2);
  });

  it('recovers every endpoint independently when more than one socket reconnects', async () => {
    // Two live queries on two channels are two sockets (the default
    // `endpointOf` is the identity function), and a drop takes both down at
    // once. A single unkeyed `pendingRecovery` slot would let the second
    // endpoint's `onReconnect` clobber the first's, so only one of the two
    // would ever recover -- silently, since neither refetch throws.
    const { cache, binder, sockets, transport, clock, batches } = harness(
      (request) =>
        request.meta.path === '/orders' ? [{ id: 7, total: 99 }] : [{ id: 1, name: 'Gadget' }],
      crossChannel,
    );

    cache.subscribe(orderList, undefined, () => undefined);
    cache.subscribe(widgetList, undefined, () => undefined);
    binder.subscribe(orderList);
    binder.subscribe(widgetList);
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(2);

    sockets.last('/ws/orders').drop();
    sockets.last('/ws/widgets').drop();
    await clock.advance(1000);

    // Past the grace window for both: neither server said anything.
    await clock.advance(1000);
    batches.flush();
    await settleMicrotasks();

    // Both refetched. Under the bug, only the endpoint whose `onReconnect`
    // ran last would have.
    expect(transport.calls).toHaveLength(4);
  });

  it('does not let a resume on one endpoint cancel recovery owed to another', async () => {
    const { cache, binder, sockets, transport, clock, batches } = harness(
      (request) =>
        request.meta.path === '/orders' ? [{ id: 7, total: 99 }] : [{ id: 1, name: 'Gadget' }],
      crossChannel,
    );

    cache.subscribe(orderList, undefined, () => undefined);
    cache.subscribe(widgetList, undefined, () => undefined);
    binder.subscribe(orderList);
    binder.subscribe(widgetList);
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(2);

    sockets.last('/ws/orders').drop();
    sockets.last('/ws/widgets').drop();
    await clock.advance(1000);

    // Only the widgets server replays and says so. The orders server, on a
    // different socket, says nothing at all.
    sockets.last('/ws/widgets').deliver({ type: 'forge.resumed', payload: { from: 'e-1', count: 1 } });

    await clock.advance(1000);
    batches.flush();
    await settleMicrotasks();

    // Orders recovered (fail-safe, nothing arrived); widgets did not (its gap
    // was filled). A resume settling by channel/message alone, ignoring which
    // endpoint it arrived on, would have cancelled both or neither.
    expect(transport.calls).toHaveLength(3);
  });
});

describe('principal partitioning', () => {
  it('never writes a frame decoded for the previous identity into the new store', async () => {
    const { cache, binder, sockets, frames, transport } = harness(() => [{ id: 7, total: 99 }]);

    cache.setPrincipal('user-a');
    cache.subscribe(orderList, undefined, () => undefined);
    binder.subscribe(orderList);
    await settleMicrotasks();

    const stale = sockets.last();

    // A frame is decoded and queued, and the identity changes before the frame
    // window closes.
    stale.deliver({ type: 'order.created', payload: { id: 9, total: 5 } });
    expect(binder.pending).toBe(1);

    cache.setPrincipal('user-b');
    frames.flush();

    expect(cache.store.has('Order:9')).toBe(false);
    expect(binder.pending).toBe(0);

    // And the socket itself was repartitioned rather than left to keep pushing
    // one principal's rows at another's store.
    expect(stale.closed).toBe(true);
    expect(sockets.last().context.principal).toBe('user-b');

    stale.deliver({ type: 'order.created', payload: { id: 11, total: 1 } });
    frames.flush();
    expect(cache.store.has('Order:11')).toBe(false);

    // The new socket works.
    await settleMicrotasks();
    sockets.last().deliver({ type: 'order.created', payload: { id: 12, total: 2 } });
    frames.flush();
    expect(cache.store.has('Order:12')).toBe(true);
    expect(transport.calls.length).toBeGreaterThan(0);
  });
});

describe('channel resolution', () => {
  /** The orders channel, plus one for a type `Order` merely *contains*. */
  const nested: readonly StreamBinding[] = [
    ...streams,
    {
      channel: '/ws/customers',
      message: 'customer.updated',
      entity: 'Customer',
      intent: 'patch',
      invalidates: [],
    },
  ];

  it('resolves every channel reachable from the query result type, not just the root', () => {
    const { binder } = harness(() => [], nested);

    // `Order.fields.customer -> Customer`, so an order list holds `Customer`
    // records in its skeleton and a `customer.updated` frame changes what is
    // on screen. Root-only resolution renders that name stale forever with
    // every order around it perfectly current.
    expect([...binder.channelsFor(orderList)].sort()).toEqual(['/ws/customers', '/ws/orders']);

    // And it terminates on a schema that is a graph: `Customer.fields.orders`
    // points back at `Order`.
    expect([...binder.channelsFor({ ...orderList, entity: 'Customer' })].sort()).toEqual([
      '/ws/customers',
      '/ws/orders',
    ]);
  });

  it('applies a frame on a nested entity to a query rooted elsewhere', async () => {
    const { cache, binder, sockets, frames, transport } = harness(
      () => [{ id: 7, total: 99, customer: { id: 'c-3', name: 'Ada' } }],
      nested,
    );

    cache.subscribe(orderList, undefined, () => undefined);
    binder.subscribe(orderList);
    await settleMicrotasks();

    // Two channels, so two sockets: the default `endpointOf` is one per
    // channel, which is what the generated clients do.
    expect(sockets.opened).toHaveLength(2);

    sockets
      .last('/ws/customers')
      .deliver({ type: 'customer.updated', payload: { id: 'c-3', name: 'Grace' } });
    frames.flush();

    expect(cache.store.getRecord('Customer:c-3')?.data['name']).toBe('Grace');
    expect(transport.calls).toHaveLength(1);
  });

  it('subscribes from the declared result type, before the query has ever settled', () => {
    const { binder, sockets } = harness(() => [], nested);

    // No `subscribe` on the cache, no response, no `deps` -- and the channels
    // are resolved anyway, because the rule reads the manifest rather than the
    // settled dependency set. The frames a live query would otherwise miss are
    // precisely the ones that arrive during its first load.
    binder.subscribe(orderList);

    expect(sockets.opened).toHaveLength(2);
  });
});

describe('the cache seam', () => {
  it('registers itself on the cache, so `{live: true}` can find it', () => {
    const { cache, binder, sockets } = harness(() => []);

    expect(cache.live).toBe(binder);

    // Which is the whole of what a framework adapter does: resolve a cache,
    // and ask it. No second provider, and nothing in a generated file that has
    // to know streams exist.
    const release = cache.watchLive(orderList);

    expect(sockets.opened).toHaveLength(1);

    release();
  });

  it('reports rather than silently going deaf when no runtime is attached', () => {
    const reported: { error: unknown; context: string }[] = [];
    const cache = new QueryCache({
      transport: fakeTransport(() => []),
      entities: schema,
      onError: (error, context) => reported.push({ error, context }),
    });

    const release = cache.watchLive(orderList);

    expect(cache.live).toBeUndefined();
    expect(reported).toHaveLength(1);
    expect(reported[0]?.context).toBe('live');
    expect(String((reported[0]?.error as Error).message)).toContain('no stream runtime');

    // And the query itself is unharmed: it fetches, it just does not stream.
    expect(() => {
      release();
    }).not.toThrow();
  });

  it('gives the slot up on dispose, but never one another binder has taken', () => {
    const { cache, binder, manager } = harness(() => []);
    const second = new StreamBinder({ cache, streams, manager });

    expect(cache.live).toBe(second);

    // The first binder is no longer the cache's runtime, and disposing it must
    // not leave every `{live: true}` call site in the application unable to
    // find the one that is.
    binder.dispose();
    expect(cache.live).toBe(second);

    second.dispose();
    expect(cache.live).toBeUndefined();
  });
});

describe('observer event payload', () => {
  it('hands the observer the batch, so a frame can be attributed to its channel', () => {
    const transport = fakeTransport(() => []);
    const cache = new QueryCache({ transport, entities: schema });
    const seen: { channel: string; message: string; intent: string }[] = [];

    cache.observer = (event) => {
      if (event.type !== 'frames') return;

      for (const frame of event.frames) {
        seen.push({
          channel: frame.binding.channel,
          message: frame.binding.message,
          intent: frame.binding.intent,
        });
      }
    };

    applyFrames(cache, [
      {
        binding: {
          channel: '/ws/orders',
          message: 'order.updated',
          entity: 'Order',
          intent: 'upsert',
          invalidates: ['Order[]'],
        },
        payload: { id: 1, total: 99 },
      },
    ]);

    expect(seen).toEqual([{ channel: '/ws/orders', message: 'order.updated', intent: 'upsert' }]);

    cache.observer = undefined;
  });
});

describe('binderSnapshot', () => {
  it('reports the manifest bindings and the mounted live queries per channel', async () => {
    const h = harness(() => []);
    const release = h.binder.subscribe(orderList);

    await settleMicrotasks();

    const snap = binderSnapshot(h.binder);
    const channel = snap.channels.find((entry) => entry.channel === '/ws/orders');

    expect(channel?.bindings.map((binding) => binding.message)).toContain('order.updated');
    expect(snap.live.map((entry) => entry.channel)).toContain('/ws/orders');
    expect(snap.live[0]?.key).toBe(h.cache.key(orderList));
    expect(snap.queued).toBe(0);
    expect(snap.recovering).toEqual([]);

    release();
  });

  it('is a copy, so a panel cannot reach the binder through it', () => {
    const h = harness(() => []);
    const first = binderSnapshot(h.binder);
    const second = binderSnapshot(h.binder);

    expect(first).not.toBe(second);
    expect(first.channels).not.toBe(second.channels);
  });
});
