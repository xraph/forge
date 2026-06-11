import "@testing-library/jest-dom/vitest";
import { afterEach } from "vitest";
import { cleanup } from "@testing-library/react";

// Pin the injected runtime config so tests target /api/dashboard/v1
// (matching the existing MSW handlers) instead of inheriting the production
// fallback /dashboard/api/dashboard/v1 from runtime/config.ts. Must run before
// any module under test imports runtime/config; setupFiles guarantees that.
(
  window as unknown as { __FORGE_DASHBOARD__: Record<string, string> }
).__FORGE_DASHBOARD__ = {
  basePath: "",
  contractBase: "/api/dashboard/v1",
  shellBase: "/dashboard/ui",
};

// jsdom polyfills required by Radix UI primitives.
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
if (typeof globalThis.ResizeObserver === "undefined") {
  (
    globalThis as unknown as { ResizeObserver: typeof ResizeObserverStub }
  ).ResizeObserver = ResizeObserverStub;
}

if (typeof Element !== "undefined" && !Element.prototype.hasPointerCapture) {
  Element.prototype.hasPointerCapture = () => false;
  Element.prototype.releasePointerCapture = () => undefined;
  Element.prototype.setPointerCapture = () => undefined;
  // scrollIntoView used by some Radix portals.
  // eslint-disable-next-line @typescript-eslint/no-empty-function
  Element.prototype.scrollIntoView = function () {};
}

// jsdom doesn't ship matchMedia; the slice (l) Sidebar uses it for the
// mobile breakpoint check. Static "false" is fine — tests never resize.
if (typeof window !== "undefined" && typeof window.matchMedia !== "function") {
  (
    window as unknown as { matchMedia: (q: string) => MediaQueryList }
  ).matchMedia = (query: string) =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addListener: () => undefined,
      removeListener: () => undefined,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
      dispatchEvent: () => false,
    }) as unknown as MediaQueryList;
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
  (
    globalThis as unknown as { EventSource: typeof EventSourceStub }
  ).EventSource = EventSourceStub;
}

afterEach(() => {
  cleanup();
});
