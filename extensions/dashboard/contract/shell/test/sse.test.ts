import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SubscriptionMux } from "../src/contract/sse";
import type { StreamEvent } from "../src/contract/types";

class FakeEventSource {
  static instances: FakeEventSource[] = [];
  url: string;
  readyState = 0;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  private listeners = new Map<string, Array<(ev: MessageEvent) => void>>();

  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
    queueMicrotask(() => {
      this.readyState = 1;
      this.onopen?.();
    });
  }
  addEventListener(name: string, fn: (ev: MessageEvent) => void) {
    const arr = this.listeners.get(name) ?? [];
    arr.push(fn);
    this.listeners.set(name, arr);
  }
  removeEventListener() {
    /* noop */
  }
  emit(name: string, data: string) {
    const arr = this.listeners.get(name) ?? [];
    arr.forEach((fn) => fn({ data } as MessageEvent));
  }
  close() {
    this.readyState = 2;
  }
}

const fetchStub = vi.fn(async (url: string, _init?: RequestInit) => {
  if (url.endsWith("/stream/control")) {
    return new globalThis.Response(JSON.stringify({}), { status: 200 }) as unknown as Response;
  }
  throw new Error("unexpected fetch: " + url);
});

beforeEach(() => {
  FakeEventSource.instances = [];
  fetchStub.mockClear();
});

afterEach(() => {
  FakeEventSource.instances.forEach((es) => es.close());
});

describe("SubscriptionMux", () => {
  it("dispatches events to the right subscription handler by SSE event name", async () => {
    const mux = new SubscriptionMux({
      baseURL: "/api/dashboard/v1",
      eventSource: FakeEventSource as unknown as typeof EventSource,
      fetcher: fetchStub as unknown as typeof fetch,
    });
    const received: StreamEvent[] = [];
    const unsub = await mux.subscribe(
      "logs",
      "audit.tail",
      {},
      (ev) => {
        received.push(ev);
      },
      { subscriptionID: "s1" },
    );
    const es = FakeEventSource.instances[0]!;
    es.emit("hello", JSON.stringify({ streamID: "stream-x" }));
    await Promise.resolve();
    es.emit(
      "s1",
      JSON.stringify({ intent: "audit.tail", mode: "append", payload: { line: "hi" }, seq: 1 }),
    );
    expect(received).toHaveLength(1);
    expect(received[0]!.payload).toEqual({ line: "hi" });
    unsub();
  });

  it("sends a control message on subscribe and on unsubscribe", async () => {
    const mux = new SubscriptionMux({
      baseURL: "/api/dashboard/v1",
      eventSource: FakeEventSource as unknown as typeof EventSource,
      fetcher: fetchStub as unknown as typeof fetch,
    });
    const unsub = await mux.subscribe(
      "logs",
      "audit.tail",
      {},
      () => {},
      { subscriptionID: "s1" },
    );
    FakeEventSource.instances[0]!.emit("hello", JSON.stringify({ streamID: "stream-x" }));
    await Promise.resolve();
    expect(fetchStub).toHaveBeenCalledWith(
      "/api/dashboard/v1/stream/control",
      expect.objectContaining({ method: "POST" }),
    );
    const subscribeBody = JSON.parse(
      fetchStub.mock.calls.find(([, init]) => (init as RequestInit).body)![1]!.body as string,
    );
    expect(subscribeBody.op).toBe("subscribe");
    expect(subscribeBody.subscriptionID).toBe("s1");

    fetchStub.mockClear();
    unsub();
    await Promise.resolve();
    const unsubscribeBody = JSON.parse(fetchStub.mock.calls[0]![1]!.body as string);
    expect(unsubscribeBody.op).toBe("unsubscribe");
  });
});
