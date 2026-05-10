import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { ContractClient } from "../src/contract/client";

const server = setupServer();
beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe("ContractClient", () => {
  it("query: returns response.data on success", async () => {
    server.use(
      http.post("/api/dashboard/v1", async ({ request }) => {
        const body = (await request.json()) as { kind: string; intent: string };
        expect(body.kind).toBe("query");
        expect(body.intent).toBe("users.list");
        return HttpResponse.json({
          ok: true,
          envelope: "v1",
          kind: "query",
          data: { users: ["alice"] },
          meta: { intentVersion: 1 },
        });
      }),
    );
    const c = new ContractClient();
    const data = await c.query<{ users: string[] }>("users", "users.list");
    expect(data.users).toEqual(["alice"]);
  });

  it("error envelope: throws ContractClientError carrying the wire error", async () => {
    server.use(
      http.post("/api/dashboard/v1", () =>
        HttpResponse.json(
          { ok: false, envelope: "v1", error: { code: "NOT_FOUND", message: "no" } },
          { status: 404 },
        ),
      ),
    );
    const c = new ContractClient();
    await expect(c.query("x", "y")).rejects.toMatchObject({ code: "NOT_FOUND" });
  });

  it("command: auto-attaches CSRF token (fetched lazily) and idempotency key", async () => {
    let csrfFetched = 0;
    server.use(
      http.get("/api/dashboard/v1/csrf", () => {
        csrfFetched++;
        return HttpResponse.json({
          token: "tok123",
          expiresAt: new Date(Date.now() + 3600_000).toISOString(),
        });
      }),
      http.post("/api/dashboard/v1", async ({ request }) => {
        const body = (await request.json()) as {
          kind: string;
          csrf?: string;
          idempotencyKey?: string;
        };
        expect(body.kind).toBe("command");
        expect(body.csrf).toBe("tok123");
        expect(body.idempotencyKey).toBeTruthy();
        return HttpResponse.json({
          ok: true,
          envelope: "v1",
          kind: "command",
          data: null,
          meta: {},
        });
      }),
    );
    const c = new ContractClient();
    await c.command("users", "user.disable", { id: "u1" });
    expect(csrfFetched).toBe(1);
  });

  it("command: refreshes CSRF on 401 and retries once", async () => {
    let attempt = 0;
    server.use(
      http.get("/api/dashboard/v1/csrf", () =>
        HttpResponse.json({
          token: `tok-${++attempt}`,
          expiresAt: new Date(Date.now() + 3600_000).toISOString(),
        }),
      ),
      http.post("/api/dashboard/v1", async ({ request }) => {
        const body = (await request.json()) as { csrf?: string };
        if (body.csrf === "tok-1") {
          return HttpResponse.json(
            { ok: false, envelope: "v1", error: { code: "UNAUTHENTICATED" } },
            { status: 401 },
          );
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
    const c = new ContractClient();
    await c.command("users", "do.thing");
    expect(attempt).toBe(2);
  });

  it("graph: returns the graph tree on success", async () => {
    server.use(
      http.post("/api/dashboard/v1", () =>
        HttpResponse.json({
          ok: true,
          envelope: "v1",
          kind: "graph",
          data: { intent: "page.shell", route: "/x" },
          meta: {},
        }),
      ),
    );
    const c = new ContractClient();
    const node = await c.graph("core-contract", "/x");
    expect(node.intent).toBe("page.shell");
  });
});
