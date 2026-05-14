import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { DashboardLayout } from "../src/runtime/layout";
import { usePrincipalStore } from "../src/auth/principal";

const server = setupServer();

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
  server.resetHandlers();
  usePrincipalStore.setState({
    principal: null,
    loaded: false,
    error: null,
    authRequired: false,
    loginPath: null,
  });
});
afterAll(() => server.close());

function renderWithProviders(ui: React.ReactElement) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 0 } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={["/health"]}>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("DashboardLayout", () => {
  it("renders the breadcrumb title and the navigation groups from the contract", async () => {
    server.use(
      http.post("/api/dashboard/v1", () =>
        HttpResponse.json({
          ok: true,
          envelope: "v1",
          kind: "query",
          data: {
            groups: [
              {
                group: "Overview",
                priority: 0,
                items: [
                  { label: "Health", href: "/health", icon: "heart-pulse", priority: 1 },
                  { label: "Metrics", href: "/metrics", icon: "chart-bar", priority: 2 },
                ],
              },
              {
                group: "Operations",
                priority: 6,
                items: [{ label: "Audit", href: "/audit", icon: "history", priority: 23 }],
              },
            ],
          },
          meta: {},
        }),
      ),
    );

    renderWithProviders(
      <DashboardLayout title="Health">
        <div>page-body</div>
      </DashboardLayout>,
    );

    expect(screen.getByText("page-body")).toBeInTheDocument();
    expect(screen.getByText("Health")).toBeInTheDocument(); // breadcrumb

    await waitFor(() => {
      expect(screen.getByText("Overview")).toBeInTheDocument();
      expect(screen.getByText("Operations")).toBeInTheDocument();
      expect(screen.getByText("Metrics")).toBeInTheDocument();
      expect(screen.getByText("Audit")).toBeInTheDocument();
    });
  });

  it("falls back to a prettified path label when no title is provided", () => {
    server.use(
      http.post("/api/dashboard/v1", () =>
        HttpResponse.json({
          ok: true,
          envelope: "v1",
          kind: "query",
          data: { groups: [] },
          meta: {},
        }),
      ),
    );

    renderWithProviders(
      <DashboardLayout>
        <div>x</div>
      </DashboardLayout>,
    );
    // initialEntries is /health -> "Health"
    expect(screen.getByText("Health")).toBeInTheDocument();
  });
});
