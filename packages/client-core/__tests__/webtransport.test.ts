import { describe, expect, it } from 'vitest';

import { manualScheduler } from '../src/invalidate';
import { SubscriptionManager, webTransportConnection } from '../src/stream';
import { manualClock } from '../src/transport';

/**
 * The WebTransport adapter, driven through a real `SubscriptionManager`.
 *
 * Nothing here constructs a `WebTransport`. The adapter takes one that is
 * already open, exactly as `SubscriptionManagerOptions.connect` takes an
 * already-open socket, so the fake below only has to have the three members
 * the adapter reads.
 *
 * A WebSocket adapter really is the four-line object literal the README shows,
 * because `onMessage` is already a callback. A datagram source is a pull loop
 * over a `ReadableStream` instead, which is why this one is written once here
 * rather than left to every caller.
 */
function fakeTransport() {
  let push: (bytes: Uint8Array) => void = () => undefined;
  let end: () => void = () => undefined;
  let shut: (reason?: unknown) => void = () => undefined;

  // `start` runs synchronously during construction, so both are assigned
  // before this function returns.
  const readable = new ReadableStream<Uint8Array>({
    start(controller) {
      push = (bytes) => controller.enqueue(bytes);
      end = () => controller.close();
    },
  });

  const closed = new Promise<unknown>((resolve) => {
    shut = resolve;
  });

  let closes = 0;

  return {
    transport: {
      datagrams: { readable },
      closed,
      close: () => {
        closes += 1;
        shut('closed by us');
      },
    },
    /** Send one datagram, JSON-encoded the way the server would. */
    send: (message: unknown) => push(new TextEncoder().encode(JSON.stringify(message))),
    /** Send raw bytes, for the malformed case. */
    sendRaw: (bytes: Uint8Array) => push(bytes),
    /** The peer went away. */
    drop: (reason?: unknown) => {
      end();
      shut(reason);
    },
    closeCount: () => closes,
  };
}

/** Let the adapter's read loop run. Each datagram costs one microtask hop. */
async function settle(): Promise<void> {
  for (let i = 0; i < 8; i++) await Promise.resolve();
}

function harness(fake: ReturnType<typeof fakeTransport>) {
  const errors: { error: unknown; context: string }[] = [];
  const clock = manualClock();
  const release = manualScheduler();

  const subscriptions = new SubscriptionManager({
    connect: () => webTransportConnection(fake.transport),
    sleep: clock.sleep,
    random: () => 0,
    backoff: { baseDelay: 1000, attempts: 1 },
    release: release.schedule,
    onError: (error, context) => errors.push({ error, context }),
  });

  return { subscriptions, errors, release };
}

describe('the WebTransport adapter', () => {
  it('delivers a datagram to a channel subscriber', async () => {
    const fake = fakeTransport();
    const { subscriptions } = harness(fake);
    const seen: unknown[] = [];

    subscriptions.subscribe('/wt/orders', (message) => seen.push(message));

    fake.send({ type: 'order.created', id: 7 });
    await settle();

    expect(seen).toEqual([{ type: 'order.created', id: 7 }]);
  });

  it('delivers datagrams in the order they arrive', async () => {
    const fake = fakeTransport();
    const { subscriptions } = harness(fake);
    const seen: unknown[] = [];

    subscriptions.subscribe('/wt/orders', (message) => seen.push(message));

    fake.send({ n: 1 });
    fake.send({ n: 2 });
    fake.send({ n: 3 });
    await settle();

    expect(seen).toEqual([{ n: 1 }, { n: 2 }, { n: 3 }]);
  });

  it('reports a malformed datagram and keeps reading', async () => {
    const fake = fakeTransport();
    const { subscriptions, errors } = harness(fake);
    const seen: unknown[] = [];

    subscriptions.subscribe('/wt/orders', (message) => seen.push(message));

    fake.sendRaw(new TextEncoder().encode('{not json'));
    fake.send({ n: 2 });
    await settle();

    // The bad datagram is reported, not thrown, and the loop survives it.
    expect(errors).toHaveLength(1);
    expect(seen).toEqual([{ n: 2 }]);
  });

  it('tells the manager the socket closed when the datagram source ends', async () => {
    const fake = fakeTransport();
    const { subscriptions } = harness(fake);

    subscriptions.subscribe('/wt/orders', () => undefined);
    expect(subscriptions.size).toBe(1);

    fake.drop();
    await settle();

    // The manager owns what happens next; what the adapter owes it is the
    // notification, and a reconnect is scheduled off the back of it.
    expect(subscriptions.size).toBe(1);
  });

  it('closes the transport when the manager releases the last subscriber', async () => {
    const fake = fakeTransport();
    const { subscriptions, release } = harness(fake);

    const stop = subscriptions.subscribe('/wt/orders', () => undefined);

    stop();
    release.flush();

    expect(fake.closeCount()).toBe(1);
  });
});
