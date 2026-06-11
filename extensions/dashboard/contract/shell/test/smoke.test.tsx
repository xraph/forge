import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { App } from "../src/App";

const server = setupServer(
  http.get("/api/dashboard/v1/principal", () =>
    HttpResponse.json({
      authenticated: true,
      subject: "alice",
      displayName: "Alice",
      roles: [],
      scopes: [],
    }),
  ),
  http.post("/api/dashboard/v1", () =>
    HttpResponse.json({
      ok: true,
      envelope: "v1",
      kind: "graph",
      data: {
        intent: "page.shell",
        title: "Live Metrics",
        slots: {
          main: [
            {
              intent: "metric.counter",
              title: "Total Metrics",
              data: { intent: "metrics.summary" },
            },
          ],
        },
      },
      meta: {},
    }),
  ),
);

beforeAll(() => {
  // jsdom has no EventSource; provide a noop class so SubscriptionMux doesn't crash
  // when metric.counter subscribes.
  (globalThis as unknown as { EventSource: unknown }).EventSource = class {
    constructor(public url: string) {}
    addEventListener() {}
    removeEventListener() {}
    close() {}
    onopen: (() => void) | null = null;
    onerror: (() => void) | null = null;
  };
  history.pushState({}, "", "/dashboard/ui/metrics/live");
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe("App smoke", () => {
  it("renders page.shell with metric.counter from a fetched graph", async () => {
    render(<App />);
    expect(
      await screen.findByText("Live Metrics", undefined, { timeout: 5000 }),
    ).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.getByText("Total Metrics")).toBeInTheDocument();
    });
    await waitFor(() => {
      expect(screen.getByText("Alice")).toBeInTheDocument();
    });
  }, 10000);
});
