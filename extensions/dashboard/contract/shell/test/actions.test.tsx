import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { ContributorProvider, ParentProvider } from "../src/runtime/context";
import { ActionButton } from "../src/intents/action.button";
import { ActionDivider } from "../src/intents/action.divider";
import type { GraphNode } from "../src/contract/types";

const server = setupServer(
  http.get("/api/dashboard/v1/csrf", () =>
    HttpResponse.json({
      token: "tok",
      expiresAt: new Date(Date.now() + 3600_000).toISOString(),
    }),
  ),
);
beforeAll(() => server.listen({ onUnhandledRequest: "bypass" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function withProviders(ui: React.ReactNode, parent?: Record<string, unknown>) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ContributorProvider value="users">
        {parent ? <ParentProvider value={parent}>{ui}</ParentProvider> : ui}
      </ContributorProvider>
    </QueryClientProvider>,
  );
}

describe("ActionButton", () => {
  it("renders label and is enabled when op is set", () => {
    const node: GraphNode = { intent: "action.button", op: "user.disable" };
    withProviders(
      <ActionButton
        node={node}
        props={{ label: "Disable" }}
        slots={{}}
        data={undefined}
      />,
    );
    const btn = screen.getByRole("button", { name: "Disable" });
    expect(btn).toBeInTheDocument();
    expect(btn).not.toBeDisabled();
  });

  it("issues a command on click and resolves parent.<field> in payload", async () => {
    type Captured = { intent: string; payload: unknown; csrf?: string };
    const captured: { value?: Captured } = {};
    server.use(
      http.post("/api/dashboard/v1", async ({ request }) => {
        captured.value = (await request.json()) as Captured;
        return HttpResponse.json({
          ok: true,
          envelope: "v1",
          kind: "command",
          data: null,
          meta: {},
        });
      }),
    );
    const node: GraphNode = {
      intent: "action.button",
      op: "user.disable",
      payload: { id: { from: "parent.id" } },
    };
    withProviders(
      <ActionButton
        node={node}
        props={{ label: "Disable" }}
        slots={{}}
        data={undefined}
      />,
      { id: "u_42", name: "Alice" },
    );
    fireEvent.click(screen.getByRole("button", { name: "Disable" }));
    await waitFor(() => {
      expect(captured.value?.intent).toBe("user.disable");
    });
    expect(captured.value?.payload).toEqual({ id: "u_42" });
    expect(captured.value?.csrf).toBe("tok");
  });

  it("opens a confirmation dialog when confirm is set; only fires after confirming", async () => {
    let calls = 0;
    server.use(
      http.post("/api/dashboard/v1", () => {
        calls++;
        return HttpResponse.json({
          ok: true,
          envelope: "v1",
          kind: "command",
          data: null,
          meta: {},
        });
      }),
    );
    const node: GraphNode = { intent: "action.button", op: "user.delete" };
    withProviders(
      <ActionButton
        node={node}
        props={{ label: "Delete", variant: "destructive", confirm: "Are you sure?" }}
        slots={{}}
        data={undefined}
      />,
    );
    // First click opens the dialog; doesn't fire the command yet.
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    await screen.findByText("Are you sure?");
    expect(calls).toBe(0);

    // Confirming fires the command.
    const dialogActions = screen.getAllByRole("button", { name: "Delete" });
    // The trigger and the action both have name "Delete"; the action is the
    // one inside the AlertDialog.
    fireEvent.click(dialogActions[dialogActions.length - 1]!);
    await waitFor(() => expect(calls).toBe(1));
  });
});

describe("ActionDivider", () => {
  it("renders a horizontal separator", () => {
    const { container } = render(<ActionDivider />);
    const sep = container.querySelector('[role="none"], [data-orientation="horizontal"]');
    expect(sep).toBeTruthy();
  });
});
