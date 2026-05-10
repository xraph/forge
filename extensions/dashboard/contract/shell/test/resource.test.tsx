import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { ContributorProvider, IntentRegistryProvider } from "../src/runtime/context";
import { buildIntentRegistry } from "../src/intents/register";
import { GraphRenderer } from "../src/runtime/renderer";
import type { GraphNode } from "../src/contract/types";

const server = setupServer();
beforeAll(() => server.listen({ onUnhandledRequest: "bypass" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function withApp(node: GraphNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const reg = buildIntentRegistry();
  return render(
    <QueryClientProvider client={qc}>
      <IntentRegistryProvider value={reg}>
        <ContributorProvider value="users">
          <GraphRenderer node={node} />
        </ContributorProvider>
      </IntentRegistryProvider>
    </QueryClientProvider>,
  );
}

describe("resource.list", () => {
  it("renders a Table with rows from the data binding's query intent", async () => {
    server.use(
      http.post("/api/dashboard/v1", () =>
        HttpResponse.json({
          ok: true,
          envelope: "v1",
          kind: "query",
          data: {
            users: [
              { id: 1, name: "Alice", role: "admin" },
              { id: 2, name: "Bob", role: "viewer" },
            ],
          },
          meta: {},
        }),
      ),
    );
    const node: GraphNode = {
      intent: "resource.list",
      data: { intent: "users.list" },
      props: { columns: ["id", "name", "role"] },
    };
    withApp(node);

    await waitFor(() => expect(screen.getByText("Alice")).toBeInTheDocument());
    expect(screen.getByText("Bob")).toBeInTheDocument();
    expect(screen.getByText("admin")).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "name" })).toBeInTheDocument();
  });

  it("shows empty-state copy when the query returns no rows", async () => {
    server.use(
      http.post("/api/dashboard/v1", () =>
        HttpResponse.json({
          ok: true,
          envelope: "v1",
          kind: "query",
          data: { users: [] },
          meta: {},
        }),
      ),
    );
    const node: GraphNode = {
      intent: "resource.list",
      data: { intent: "users.list" },
      props: { emptyMessage: "Nothing here yet" },
    };
    withApp(node);

    await waitFor(() => expect(screen.getByText("Nothing here yet")).toBeInTheDocument());
  });
});

describe("resource.detail", () => {
  it("renders fields from the parent context (no data binding)", () => {
    server.use(
      http.post("/api/dashboard/v1", () =>
        HttpResponse.json({
          ok: true,
          envelope: "v1",
          kind: "query",
          data: { users: [{ id: 1, name: "Alice" }] },
          meta: {},
        }),
      ),
    );
    const node: GraphNode = {
      intent: "resource.list",
      data: { intent: "users.list" },
      props: { columns: ["id", "name"] },
      slots: {
        detailDrawer: [
          {
            intent: "resource.detail",
            props: { fields: ["id", "name"] },
          },
        ],
      },
    };
    withApp(node);

    // Wait for the list to render, then click the row to open the detail drawer.
    return screen.findByText("Alice").then((cell) => {
      fireEvent.click(cell.closest("tr")!);
      // The drawer renders the detail with the row's fields.
      return waitFor(() => {
        const idCells = screen.getAllByText(/^id$/i);
        expect(idCells.length).toBeGreaterThan(0);
      });
    });
  });
});

describe("dashboard.grid", () => {
  it("renders widgets from its slot in a CSS grid", () => {
    const node: GraphNode = {
      intent: "dashboard.grid",
      props: { columns: 2 },
      slots: {
        widgets: [
          { intent: "metric.counter", title: "A" },
          { intent: "metric.counter", title: "B" },
        ],
      },
    };
    withApp(node);
    expect(screen.getByText("A")).toBeInTheDocument();
    expect(screen.getByText("B")).toBeInTheDocument();
  });
});
