import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { AuthGate } from "../src/auth/AuthGate";
import { LoginScreen } from "../src/auth/LoginScreen";
import { usePrincipalStore } from "../src/auth/principal";
import { IntentRegistryProvider } from "../src/runtime/context";
import { buildIntentRegistry } from "../src/intents/register";

const server = setupServer();
const intentRegistry = buildIntentRegistry();

// AuthGate's LoginGate reads the current location to decide which auth graph
// (/login, /signup, …) to render, so tests must mount it inside a router.
function withProviders(ui: React.ReactElement, initialPath = "/") {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 0 } },
  });
  return (
    <QueryClientProvider client={qc}>
      <IntentRegistryProvider value={intentRegistry}>
        <MemoryRouter initialEntries={[initialPath]}>{ui}</MemoryRouter>
      </IntentRegistryProvider>
    </QueryClientProvider>
  );
}

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
      withProviders(
        <AuthGate>
          <div>protected-content</div>
        </AuthGate>,
      ),
    );
    expect(screen.getByText("protected-content")).toBeInTheDocument();
  });

  it("falls back to built-in LoginScreen when no contract /login route is registered", async () => {
    server.use(
      // No /login graph route → 404 envelope. AuthGate should fall through.
      http.post("/api/dashboard/v1", () =>
        HttpResponse.json(
          {
            ok: false,
            envelope: "v1",
            error: { code: "NOT_FOUND", message: "no /login" },
          },
          { status: 404 },
        ),
      ),
    );
    usePrincipalStore.setState({
      loaded: true,
      authRequired: true,
      principal: null,
    });
    render(
      withProviders(
        <AuthGate>
          <div>protected-content</div>
        </AuthGate>,
      ),
    );
    expect(screen.queryByText("protected-content")).not.toBeInTheDocument();
    await waitFor(() => {
      expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
    });
  });

  it("renders the auth extension's contract /login graph when registered", async () => {
    server.use(
      http.post("/api/dashboard/v1", () =>
        HttpResponse.json({
          ok: true,
          envelope: "v1",
          kind: "graph",
          data: {
            intent: "page.shell",
            route: "/login",
            slots: {
              main: [
                {
                  intent: "custom",
                  component: "extension-login-marker",
                  props: { label: "extension-login-here" },
                },
              ],
            },
          },
          meta: {},
        }),
      ),
    );
    usePrincipalStore.setState({
      loaded: true,
      authRequired: true,
      principal: null,
    });
    render(
      withProviders(
        <AuthGate>
          <div>protected-content</div>
        </AuthGate>,
      ),
    );
    // Built-in form should not appear when the contract owns /login.
    await waitFor(() => {
      expect(screen.queryByLabelText(/email/i)).not.toBeInTheDocument();
    });
  });

  it("requests the /signup graph when the location is /signup", async () => {
    let requestedRoute: string | undefined;
    server.use(
      http.post("/api/dashboard/v1", async ({ request }) => {
        const body = (await request.json()) as { payload?: { route?: string } };
        requestedRoute = body.payload?.route;
        return HttpResponse.json({
          ok: true,
          envelope: "v1",
          kind: "graph",
          data: { intent: "page.shell", route: "/signup", slots: { main: [] } },
          meta: {},
        });
      }),
    );
    usePrincipalStore.setState({
      loaded: true,
      authRequired: true,
      principal: null,
    });
    render(
      withProviders(
        <AuthGate>
          <div>protected-content</div>
        </AuthGate>,
        "/signup",
      ),
    );
    // The gate is location-aware: at /signup it fetches the signup graph
    // rather than the hardcoded /login route.
    await waitFor(() => {
      expect(requestedRoute).toBe("/signup");
    });
    expect(screen.queryByText("protected-content")).not.toBeInTheDocument();
  });

  it("redirects an authenticated principal off a public auth route to the app root", async () => {
    usePrincipalStore.setState({
      loaded: true,
      authRequired: false,
      principal: { subject: "x", displayName: "X", roles: [], scopes: [] },
    });
    render(
      withProviders(
        <AuthGate>
          <div>protected-content</div>
        </AuthGate>,
        "/signup",
      ),
    );
    // Just-authenticated user parked on /signup is sent to "/" where the
    // authenticated shell (children) renders.
    await waitFor(() => {
      expect(screen.getByText("protected-content")).toBeInTheDocument();
    });
  });
});

describe("LoginScreen", () => {
  it("issues an auth.login command and reloads principal on success", async () => {
    let postBody: unknown;
    server.use(
      // CSRF prefetch — return a token so command sends.
      http.get("/api/dashboard/v1/csrf", () =>
        HttpResponse.json({ token: "tk" }),
      ),
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
    fireEvent.change(screen.getByLabelText(/email/i), {
      target: { value: "alice@example.com" },
    });
    fireEvent.change(screen.getByLabelText(/password/i), {
      target: { value: "secret" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^login$/i }));

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
      http.get("/api/dashboard/v1/csrf", () =>
        HttpResponse.json({ token: "tk" }),
      ),
      http.post("/api/dashboard/v1", () =>
        HttpResponse.json({
          ok: false,
          envelope: "v1",
          error: { code: "PERMISSION_DENIED", message: "bad credentials" },
        }),
      ),
    );

    render(<LoginScreen />);
    fireEvent.change(screen.getByLabelText(/email/i), {
      target: { value: "x@y.z" },
    });
    fireEvent.change(screen.getByLabelText(/password/i), {
      target: { value: "wrong" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^login$/i }));

    await waitFor(() => {
      expect(screen.getByText(/bad credentials/i)).toBeInTheDocument();
    });
  });
});

describe("AuthGate access-denied", () => {
  it("renders the access-denied panel when accessDenied is true", () => {
    usePrincipalStore.setState({
      loaded: true,
      authRequired: false,
      accessDenied: true,
      accessDeniedMessage: "missing role",
      requiredRoles: ["dashboard.admin"],
      principal: null,
    });
    render(
      withProviders(
        <AuthGate>
          <div>protected-content</div>
        </AuthGate>,
      ),
    );
    expect(screen.queryByText("protected-content")).not.toBeInTheDocument();
    expect(screen.getByText(/access denied/i)).toBeInTheDocument();
    expect(screen.getByText(/missing role/i)).toBeInTheDocument();
    expect(screen.getByText(/dashboard\.admin/)).toBeInTheDocument();
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

  it("sets accessDenied on 403 with required roles", async () => {
    server.use(
      http.get("/api/dashboard/v1/principal", () =>
        HttpResponse.json(
          {
            code: "PERMISSION_DENIED",
            message: "missing role",
            requiredRoles: ["dashboard.admin"],
          },
          { status: 403 },
        ),
      ),
    );
    await usePrincipalStore.getState().load();
    const s = usePrincipalStore.getState();
    expect(s.accessDenied).toBe(true);
    expect(s.requiredRoles).toEqual(["dashboard.admin"]);
    expect(s.authRequired).toBe(false);
    expect(s.principal).toBeNull();
  });
});
