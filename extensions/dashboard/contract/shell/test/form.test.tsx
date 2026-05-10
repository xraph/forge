import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { ContributorProvider } from "../src/runtime/context";
import { IntentRegistryProvider } from "../src/runtime/context";
import { buildIntentRegistry } from "../src/intents/register";
import { GraphRenderer } from "../src/runtime/renderer";
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

describe("form.edit + form.field", () => {
  it("renders fields, captures input, and submits the gathered values via the op command", async () => {
    type Sent = { intent: string; payload: Record<string, unknown> };
    const captured: { value?: Sent } = {};
    server.use(
      http.post("/api/dashboard/v1", async ({ request }) => {
        captured.value = (await request.json()) as Sent;
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
      intent: "form.edit",
      op: "user.create",
      slots: {
        fields: [
          {
            intent: "form.field",
            props: { name: "email", label: "Email", kind: "text", required: true },
          },
          {
            intent: "form.field",
            props: { name: "active", label: "Active", kind: "checkbox" },
          },
        ],
      },
    };
    withApp(node);

    fireEvent.change(screen.getByLabelText(/email/i), {
      target: { value: "alice@example.com" },
    });
    fireEvent.click(screen.getByRole("checkbox", { name: /active/i }));
    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => {
      expect(captured.value?.intent).toBe("user.create");
    });
    expect(captured.value?.payload).toMatchObject({
      email: "alice@example.com",
      active: true,
    });
  });

  it("preloads from a query intent before rendering", async () => {
    server.use(
      http.post("/api/dashboard/v1", async ({ request }) => {
        const body = (await request.json()) as { kind: string; intent: string };
        if (body.kind === "query" && body.intent === "user.detail") {
          return HttpResponse.json({
            ok: true,
            envelope: "v1",
            kind: "query",
            data: { email: "preloaded@example.com" },
            meta: {},
          });
        }
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
      intent: "form.edit",
      op: "user.update",
      data: { intent: "user.detail" },
      slots: {
        fields: [{ intent: "form.field", props: { name: "email", label: "Email" } }],
      },
    };
    withApp(node);

    const input = (await screen.findByLabelText(/email/i)) as HTMLInputElement;
    await waitFor(() => {
      expect(input.value).toBe("preloaded@example.com");
    });
  });
});
