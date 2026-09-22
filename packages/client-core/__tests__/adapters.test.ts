import { describe, expect, it, vi } from 'vitest';

import { channelMessages, eventSourceConnection, webSocketConnection } from '../src/stream';
import type { EventSourceLike, StreamBinding, WebSocketLike } from '../src/stream';

function fakeSocket(): WebSocketLike & { sent: string[]; closes: number } {
  return {
    onmessage: null,
    onclose: null,
    onerror: null,
    sent: [],
    closes: 0,
    send(data) {
      this.sent.push(data);
    },
    close() {
      this.closes += 1;
    },
  };
}

describe('webSocketConnection', () => {
  it('parses JSON text frames and passes anything else through', () => {
    const socket = fakeSocket();
    const conn = webSocketConnection(socket);
    const seen: unknown[] = [];

    conn.onMessage((m) => seen.push(m));
    socket.onmessage?.({ data: '{"type":"order.created","payload":{"id":7}}' });
    socket.onmessage?.({ data: { already: 'parsed' } });

    expect(seen).toEqual([{ type: 'order.created', payload: { id: 7 } }, { already: 'parsed' }]);
  });

  it('reports a frame that does not parse and keeps the socket', () => {
    const socket = fakeSocket();
    const conn = webSocketConnection(socket);
    const errors: unknown[] = [];

    conn.onMessage(() => undefined);
    conn.onError?.((e) => errors.push(e));
    socket.onmessage?.({ data: '{not json' });

    expect(errors).toHaveLength(1);
    expect(socket.closes).toBe(0);
  });

  it('encodes what it sends, and reports a peer close once', () => {
    const socket = fakeSocket();
    const conn = webSocketConnection(socket);
    const closed = vi.fn();

    conn.onClose(closed);
    conn.send?.({ type: 'system', event: 'pong' });
    conn.send?.('raw');
    socket.onclose?.('gone');
    socket.onclose?.('gone again');

    expect(socket.sent).toEqual(['{"type":"system","event":"pong"}', 'raw']);
    expect(closed).toHaveBeenCalledTimes(1);
  });

  it('does not report a close it asked for', () => {
    const socket = fakeSocket();
    const conn = webSocketConnection(socket);
    const closed = vi.fn();

    conn.onClose(closed);
    conn.close();
    socket.onclose?.();

    expect(socket.closes).toBe(1);
    expect(closed).not.toHaveBeenCalled();
  });
});

function fakeSource(): EventSourceLike & {
  listeners: Map<string, (event: { data: string; lastEventId?: string }) => void>;
  closes: number;
} {
  return {
    listeners: new Map(),
    closes: 0,
    onerror: null,
    addEventListener(type, listener) {
      this.listeners.set(type, listener);
    },
    close() {
      this.closes += 1;
    },
  };
}

describe('eventSourceConnection', () => {
  it('listens for the named events and the control events, delivering decodable frames', () => {
    const source = fakeSource();
    const conn = eventSourceConnection(source, { events: ['order.created'] });
    const seen: unknown[] = [];

    conn.onMessage((m) => seen.push(m));

    expect([...source.listeners.keys()]).toEqual(['order.created', 'forge.resumed', 'forge.gap']);

    source.listeners.get('order.created')?.({ data: '{"id":7}', lastEventId: '41' });

    expect(seen).toEqual([{ event: 'order.created', data: { id: 7 }, id: '41' }]);
  });

  it('treats an error as a drop: reports it, closes the source, and says so once', () => {
    const source = fakeSource();
    const conn = eventSourceConnection(source, { events: [] });
    const errors: unknown[] = [];
    const closed = vi.fn();

    conn.onError?.((e) => errors.push(e));
    conn.onClose(closed);
    source.onerror?.('network');
    source.onerror?.('network');

    expect(errors).toEqual(['network']);
    expect(source.closes).toBe(1);
    expect(closed).toHaveBeenCalledTimes(1);
  });
});

describe('channelMessages', () => {
  it('reads one channel\'s message names out of the streams table, once each', () => {
    const row = (channel: string, message: string): StreamBinding => ({
      channel,
      message,
      entity: 'Order',
      intent: 'patch',
      invalidates: [],
    });
    const streams = [
      row('/sse/orders', 'order.created'),
      row('/sse/orders', 'order.deleted'),
      row('/sse/orders', 'order.created'),
      { kind: 'duplex', channel: '/sse/orders', send: 'command', receive: 'event' } as const,
      row('/sse/other', 'x'),
    ];

    expect(channelMessages(streams, '/sse/orders')).toEqual(['order.created', 'order.deleted']);
  });
});
