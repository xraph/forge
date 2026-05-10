import "@testing-library/jest-dom/vitest";
import { afterEach } from "vitest";
import { cleanup } from "@testing-library/react";

// jsdom polyfills required by Radix UI primitives.
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
if (typeof globalThis.ResizeObserver === "undefined") {
  (globalThis as unknown as { ResizeObserver: typeof ResizeObserverStub }).ResizeObserver =
    ResizeObserverStub;
}

if (typeof Element !== "undefined" && !Element.prototype.hasPointerCapture) {
  Element.prototype.hasPointerCapture = () => false;
  Element.prototype.releasePointerCapture = () => undefined;
  Element.prototype.setPointerCapture = () => undefined;
  // scrollIntoView used by some Radix portals.
  // eslint-disable-next-line @typescript-eslint/no-empty-function
  Element.prototype.scrollIntoView = function () {};
}

// jsdom has no EventSource; provide a noop class so SubscriptionMux from
// metric.counter / audit.tail mounts in tests don't trigger unhandled
// rejections. Tests that need to drive SSE events override this.
class EventSourceStub {
  url: string;
  readyState = 0;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  constructor(url: string) {
    this.url = url;
  }
  addEventListener() {}
  removeEventListener() {}
  close() {}
}
if (typeof globalThis.EventSource === "undefined") {
  (globalThis as unknown as { EventSource: typeof EventSourceStub }).EventSource =
    EventSourceStub;
}

afterEach(() => {
  cleanup();
});
