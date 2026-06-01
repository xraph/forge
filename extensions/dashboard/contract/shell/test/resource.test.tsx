import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
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

// LocationProbe renders the current pathname into the DOM so navigation
// tests can assert where useNavigate sent the user.
function LocationProbe() {
  const { pathname } = useLocation();
  return <div data-testid="location-probe">{pathname}</div>;
}

function withApp(node: GraphNode, initialPath = "/users") {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const reg = buildIntentRegistry();
  return render(
    <QueryClientProvider client={qc}>
      <IntentRegistryProvider value={reg}>
        <ContributorProvider value="users">
          <MemoryRouter initialEntries={[initialPath]}>
            <Routes>
              <Route
                path="*"
                element={
                  <>
                    <GraphRenderer node={node} />
                    <LocationProbe />
                  </>
                }
              />
            </Routes>
          </MemoryRouter>
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
    // Column headers are title-cased; the underlying field is still "name".
    expect(screen.getByRole("columnheader", { name: "Name" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "ID" })).toBeInTheDocument();
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

describe("resource.list row navigation", () => {
  function withUsers() {
    server.use(
      http.post("/api/dashboard/v1", () =>
        HttpResponse.json({
          ok: true,
          envelope: "v1",
          kind: "query",
          data: { users: [{ id: "u_1", name: "Alice" }, { id: "u_2", name: "Bob" }] },
          meta: {},
        }),
      ),
    );
  }

  it("navigates to <currentPath>/<row.id> by default", async () => {
    withUsers();
    withApp(
      {
        intent: "resource.list",
        data: { intent: "users.list" },
        props: { columns: ["id", "name"] },
      },
      "/users",
    );

    const cell = await screen.findByText("Alice");
    fireEvent.click(cell.closest("tr")!);
    await waitFor(() =>
      expect(screen.getByTestId("location-probe").textContent).toBe("/users/u_1"),
    );
  });

  it("substitutes :field tokens from props.detailHref", async () => {
    server.use(
      http.post("/api/dashboard/v1", () =>
        HttpResponse.json({
          ok: true,
          envelope: "v1",
          kind: "query",
          data: { settings: [{ key: "password.min_length", type: "int" }] },
          meta: {},
        }),
      ),
    );
    withApp(
      {
        intent: "resource.list",
        data: { intent: "settings.list" },
        props: { columns: ["key", "type"], detailHref: "/settings/:key" },
      },
      "/settings",
    );

    const cell = await screen.findByText("password.min_length");
    fireEvent.click(cell.closest("tr")!);
    await waitFor(() =>
      expect(screen.getByTestId("location-probe").textContent).toBe(
        "/settings/password.min_length",
      ),
    );
  });

  it("disables row navigation when props.disableRowNav is true", async () => {
    withUsers();
    withApp(
      {
        intent: "resource.list",
        data: { intent: "users.list" },
        props: { columns: ["id", "name"], disableRowNav: true },
      },
      "/users",
    );

    const cell = await screen.findByText("Alice");
    fireEvent.click(cell.closest("tr")!);
    // Pathname stays put — no navigation fired.
    expect(screen.getByTestId("location-probe").textContent).toBe("/users");
  });

  it("does not navigate when clicking inside the action cell", async () => {
    server.use(
      http.post("/api/dashboard/v1", () =>
        HttpResponse.json({
          ok: true,
          envelope: "v1",
          kind: "query",
          data: { users: [{ id: "u_1", name: "Alice" }] },
          meta: {},
        }),
      ),
    );
    withApp(
      {
        intent: "resource.list",
        data: { intent: "users.list" },
        props: { columns: ["id", "name"] },
        slots: {
          rowActions: [
            {
              intent: "action.button",
              title: "Ban",
              // op intentionally omitted — we only test propagation, not
              // the action's own dispatch (covered separately).
            },
          ],
        },
      },
      "/users",
    );

    const banBtn = await screen.findByText("Ban");
    fireEvent.click(banBtn);
    // Pathname unchanged: stopPropagation on the action cell prevented
    // the row's navigate handler from firing.
    expect(screen.getByTestId("location-probe").textContent).toBe("/users");
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
