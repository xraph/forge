import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { AuthGate } from "../src/auth/AuthGate";
import { LoginScreen } from "../src/auth/LoginScreen";
import { usePrincipalStore } from "../src/auth/principal";

const server = setupServer();

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
  server.resetHandlers();
  // Reset principal store between tests since it's a singleton.
  usePrincipalStore.setState({
    principal: null,
    loaded: false,
    error: null,
    authRequired: false,
    loginPath: null,
  });
});
afterAll(() => server.close());

describe("AuthGate", () => {
  it("renders children when principal is loaded and auth not required", () => {
    usePrincipalStore.setState({
      loaded: true,
      authRequired: false,
      principal: { subject: "x", displayName: "X", roles: [], scopes: [] },
    });
    render(
      <AuthGate>
        <div>protected-content</div>
      </AuthGate>,
    );
    expect(screen.getByText("protected-content")).toBeInTheDocument();
  });

  it("renders LoginScreen when principal is loaded and auth required", () => {
    usePrincipalStore.setState({
      loaded: true,
      authRequired: true,
      principal: null,
    });
    render(
      <AuthGate>
        <div>protected-content</div>
      </AuthGate>,
    );
    expect(screen.queryByText("protected-content")).not.toBeInTheDocument();
    // The login form has an email field — distinct enough to confirm it rendered.
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
  });
});

describe("LoginScreen", () => {
  it("issues an auth.login command and reloads principal on success", async () => {
    let postBody: unknown;
    server.use(
      // CSRF prefetch — return a token so command sends.
      http.get("/api/dashboard/v1/csrf", () => HttpResponse.json({ token: "tk" })),
      http.post("/api/dashboard/v1", async ({ request }) => {
        postBody = await request.json();
        return HttpResponse.json({
          ok: true,
          envelope: "v1",
          kind: "command",
          data: { ok: true },
          meta: {},
        });
      }),
      http.get("/api/dashboard/v1/principal", () =>
        HttpResponse.json({
          authenticated: true,
          subject: "alice",
          displayName: "Alice",
          roles: [],
          scopes: [],
        }),
      ),
    );

    render(<LoginScreen />);
    fireEvent.change(screen.getByLabelText(/email/i), { target: { value: "alice@example.com" } });
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "secret" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    await waitFor(() => {
      expect(usePrincipalStore.getState().principal?.subject).toBe("alice");
    });

    // Confirm the command envelope carried the credentials.
    const env = postBody as { intent?: string; payload?: { email?: string } };
    expect(env.intent).toBe("auth.login");
    expect(env.payload?.email).toBe("alice@example.com");
  });

  it("displays an error message when login fails", async () => {
    server.use(
      http.get("/api/dashboard/v1/csrf", () => HttpResponse.json({ token: "tk" })),
      http.post("/api/dashboard/v1", () =>
        HttpResponse.json({
          ok: false,
          envelope: "v1",
          error: { code: "PERMISSION_DENIED", message: "bad credentials" },
        }),
      ),
    );

    render(<LoginScreen />);
    fireEvent.change(screen.getByLabelText(/email/i), { target: { value: "x@y.z" } });
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "wrong" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    await waitFor(() => {
      expect(screen.getByText(/bad credentials/i)).toBeInTheDocument();
    });
  });
});

describe("usePrincipalStore", () => {
  it("sets authRequired on 401 with loginPath envelope", async () => {
    server.use(
      http.get("/api/dashboard/v1/principal", () =>
        HttpResponse.json(
          { code: "UNAUTHENTICATED", loginPath: "/dashboard/login" },
          { status: 401 },
        ),
      ),
    );
    await usePrincipalStore.getState().load();
    const s = usePrincipalStore.getState();
    expect(s.authRequired).toBe(true);
    expect(s.loginPath).toBe("/dashboard/login");
    expect(s.principal).toBeNull();
  });

  it("treats authenticated:false 200 as auth-disabled", async () => {
    server.use(
      http.get("/api/dashboard/v1/principal", () =>
        HttpResponse.json({ authenticated: false }),
      ),
    );
    await usePrincipalStore.getState().load();
    const s = usePrincipalStore.getState();
    expect(s.authRequired).toBe(false);
    expect(s.principal).toBeNull();
    expect(s.loaded).toBe(true);
  });
});
