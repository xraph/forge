# Slice (d) — React Shell Rendering Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the React/TypeScript shell that consumes the contract endpoints (slices a/b/c), embed it into the dashboard binary, and prove it end-to-end against the pilot's `metric.counter` widget on `/metrics/live`.

**Architecture:** Fresh TypeScript+React+Vite project at `extensions/dashboard/contract/shell/`. Production build to `dist/`, embedded via `//go:embed dist/*` into the dashboard extension and served as a SPA. Contract client + SSE multiplex consumer + intent component registry + slot renderer = the runtime. One concrete intent component (`metric.counter`) plus a `page.shell` wrapper. Slice (e) adds the rest of the vocabulary.

**Tech Stack:** TypeScript 5.x strict mode, React 18, Vite 5, React Router 6.4+ (data router), TanStack Query 5, Zustand 4, Tailwind CSS 3, Vitest + React Testing Library + MSW (Mock Service Worker). pnpm package manager. Go side: `//go:embed`.

---

## Reference

- **Design spec:** [SLICE_D_DESIGN.md](SLICE_D_DESIGN.md).
- **Go-side endpoints to consume** (already shipped):
  - `POST /api/dashboard/v1` — query/command/graph dispatch.
  - `GET /api/dashboard/v1/stream` + `POST /api/dashboard/v1/stream/control` — SSE multiplex.
  - `GET /api/dashboard/v1/csrf` — token issuance.
  - `GET /api/dashboard/v1/capabilities` — version negotiation.
- **New endpoint this slice adds**: `GET /api/dashboard/v1/principal` — current user info.

## Conventions

- TypeScript strict mode (`"strict": true, "noUncheckedIndexedAccess": true`).
- ESLint + Prettier with reasonable defaults; no bikeshedding.
- Vitest test files: `*.test.ts` / `*.test.tsx`, co-located in `test/` directory mirroring `src/`.
- One commit per logical change; no Co-Authored-By trailers.
- Tailwind v3 via PostCSS pipeline; no Tailwind v4 yet.
- All paths in this plan are relative to `/Users/rexraphael/Work/xraph/forge` unless noted.

## File Structure

```
extensions/dashboard/contract/shell/
  package.json
  tsconfig.json
  tsconfig.node.json
  vite.config.ts
  vitest.config.ts
  tailwind.config.ts
  postcss.config.js
  index.html
  .gitignore
  README.md
  embed.go                      # //go:embed dist/*
  src/
    main.tsx
    App.tsx
    index.css
    contract/{types.ts, client.ts, sse.ts, hooks.ts}
    runtime/{registry.ts, renderer.tsx, slots.tsx, fallbacks.tsx, context.ts}
    auth/principal.ts
    intents/{page.shell.tsx, metric.counter.tsx, register.ts}
  test/
    setup.ts
    contract.test.ts
    sse.test.ts
    renderer.test.tsx
    smoke.test.tsx

extensions/dashboard/handlers/principal.go
extensions/dashboard/handlers/principal_test.go
extensions/dashboard/extension.go     # MODIFY
```

## .gitignore for shell/

The repo's top-level .gitignore already excludes `node_modules/`. Add a shell-local `.gitignore` for build artifacts:

```
# extensions/dashboard/contract/shell/.gitignore
node_modules/
dist/
.vite/
*.log
.env
.env.local
coverage/
```

The `dist/` directory is gitignored locally but committed via the Go embed at build time on CI. For local development, contributors run `pnpm build` before `go build`. (A future slice can wire this through a Makefile / build script.)

---

## Phase 0: Project Scaffolding

### Task 0.1: package.json + lockfile + tooling configs

**Files:**
- Create: `extensions/dashboard/contract/shell/package.json`
- Create: `extensions/dashboard/contract/shell/tsconfig.json`
- Create: `extensions/dashboard/contract/shell/tsconfig.node.json`
- Create: `extensions/dashboard/contract/shell/vite.config.ts`
- Create: `extensions/dashboard/contract/shell/vitest.config.ts`
- Create: `extensions/dashboard/contract/shell/tailwind.config.ts`
- Create: `extensions/dashboard/contract/shell/postcss.config.js`
- Create: `extensions/dashboard/contract/shell/.gitignore`
- Create: `extensions/dashboard/contract/shell/README.md`

- [ ] **Step 1: Write package.json**

```json
{
  "name": "@forge/dashboard-shell",
  "private": true,
  "version": "0.1.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "preview": "vite preview",
    "test": "vitest run",
    "test:watch": "vitest",
    "lint": "tsc --noEmit",
    "format": "prettier --write src test"
  },
  "dependencies": {
    "@tanstack/react-query": "^5.40.0",
    "react": "^18.3.0",
    "react-dom": "^18.3.0",
    "react-router-dom": "^6.26.0",
    "zustand": "^4.5.0"
  },
  "devDependencies": {
    "@testing-library/jest-dom": "^6.4.0",
    "@testing-library/react": "^16.0.0",
    "@types/react": "^18.3.0",
    "@types/react-dom": "^18.3.0",
    "@vitejs/plugin-react": "^4.3.0",
    "autoprefixer": "^10.4.0",
    "jsdom": "^25.0.0",
    "msw": "^2.4.0",
    "postcss": "^8.4.0",
    "prettier": "^3.3.0",
    "tailwindcss": "^3.4.0",
    "typescript": "^5.5.0",
    "vite": "^5.4.0",
    "vitest": "^2.1.0"
  }
}
```

- [ ] **Step 2: Write tsconfig.json**

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "lib": ["ES2022", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "moduleResolution": "bundler",
    "jsx": "react-jsx",
    "strict": true,
    "noUncheckedIndexedAccess": true,
    "noFallthroughCasesInSwitch": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "isolatedModules": true,
    "allowImportingTsExtensions": false,
    "resolveJsonModule": true,
    "useDefineForClassFields": true,
    "types": ["vitest/globals", "@testing-library/jest-dom"]
  },
  "include": ["src", "test"],
  "references": [{ "path": "./tsconfig.node.json" }]
}
```

- [ ] **Step 3: Write tsconfig.node.json**

```json
{
  "compilerOptions": {
    "composite": true,
    "module": "ESNext",
    "moduleResolution": "bundler",
    "allowSyntheticDefaultImports": true,
    "strict": true,
    "skipLibCheck": true
  },
  "include": ["vite.config.ts", "vitest.config.ts", "tailwind.config.ts", "postcss.config.js"]
}
```

- [ ] **Step 4: Write vite.config.ts**

```ts
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  base: "/dashboard/contract/static/",
  build: {
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: true,
    target: "es2022",
    rollupOptions: {
      output: {
        manualChunks: {
          "react-vendor": ["react", "react-dom", "react-router-dom"],
          "query-vendor": ["@tanstack/react-query"],
        },
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api/dashboard": {
        target: "http://localhost:8080",
        changeOrigin: false,
      },
      "/dashboard/contract/static": {
        target: "http://localhost:5173",
        bypass: () => "/index.html",
      },
    },
  },
});
```

- [ ] **Step 5: Write vitest.config.ts**

```ts
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  test: {
    globals: true,
    environment: "jsdom",
    setupFiles: ["./test/setup.ts"],
    include: ["test/**/*.test.{ts,tsx}"],
    coverage: { provider: "v8" },
  },
});
```

- [ ] **Step 6: Write tailwind.config.ts**

```ts
import type { Config } from "tailwindcss";

const config: Config = {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: { extend: {} },
  plugins: [],
};

export default config;
```

- [ ] **Step 7: Write postcss.config.js**

```js
export default {
  plugins: {
    tailwindcss: {},
    autoprefixer: {},
  },
};
```

- [ ] **Step 8: Write .gitignore (contents per "Conventions" above)**

- [ ] **Step 9: Write README.md**

```markdown
# Dashboard Contract Shell

The React/TypeScript runtime that consumes the dashboard contract.

## Development

\`\`\`bash
pnpm install
pnpm dev   # Vite dev server on :5173, proxies /api/dashboard/* to :8080
\`\`\`

## Build

\`\`\`bash
pnpm build   # Emits dist/ — embedded into the dashboard Go binary via //go:embed
\`\`\`

## Test

\`\`\`bash
pnpm test
\`\`\`

See [SLICE_D_DESIGN.md](../SLICE_D_DESIGN.md) for the architecture.
```

- [ ] **Step 10: Run `pnpm install`**

```bash
cd extensions/dashboard/contract/shell && pnpm install
```

Expected: clean install, lockfile generated. Lockfile is committed.

- [ ] **Step 11: Verify lint**

```bash
pnpm lint
```

Expected: no errors (no source files yet, so this is just a tsconfig sanity check). Will fail if no `src/` exists; that's fine — Phase 0.2 creates source files.

- [ ] **Step 12: Commit**

```bash
git add extensions/dashboard/contract/shell/
git commit -m "feat(dashboard/contract/shell): scaffold React+TypeScript+Vite project"
```

### Task 0.2: index.html + minimal source skeleton

**Files:**
- Create: `extensions/dashboard/contract/shell/index.html`
- Create: `extensions/dashboard/contract/shell/src/main.tsx`
- Create: `extensions/dashboard/contract/shell/src/App.tsx`
- Create: `extensions/dashboard/contract/shell/src/index.css`
- Create: `extensions/dashboard/contract/shell/test/setup.ts`

- [ ] **Step 1: Write index.html**

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Forge Dashboard</title>
  </head>
  <body class="bg-gray-50 text-gray-900">
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

- [ ] **Step 2: Write src/index.css**

```css
@tailwind base;
@tailwind components;
@tailwind utilities;

html, body, #root {
  height: 100%;
}
```

- [ ] **Step 3: Write src/main.tsx**

```tsx
import React from "react";
import ReactDOM from "react-dom/client";
import "./index.css";
import { App } from "./App";

const root = document.getElementById("root");
if (!root) throw new Error("#root not found");

ReactDOM.createRoot(root).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
```

- [ ] **Step 4: Write src/App.tsx (placeholder)**

```tsx
export function App() {
  return (
    <div className="p-8">
      <h1 className="text-2xl font-semibold">Forge Dashboard Shell</h1>
      <p className="mt-2 text-gray-600">Runtime scaffolded; routes will be wired in Phase 6.</p>
    </div>
  );
}
```

- [ ] **Step 5: Write test/setup.ts**

```ts
import "@testing-library/jest-dom/vitest";
import { afterEach } from "vitest";
import { cleanup } from "@testing-library/react";

afterEach(() => {
  cleanup();
});
```

- [ ] **Step 6: Build the project**

```bash
cd extensions/dashboard/contract/shell && pnpm build
```

Expected: `dist/` directory created with `index.html`, hashed JS/CSS bundles. Build succeeds.

- [ ] **Step 7: Run tests**

```bash
pnpm test
```

Expected: 0 tests pass (no test files yet); exit code 0.

- [ ] **Step 8: Commit**

```bash
git add extensions/dashboard/contract/shell/
git commit -m "feat(dashboard/contract/shell): minimal React app skeleton"
```

---

## Phase 1: Contract Types + Client

### Task 1.1: TypeScript types mirroring the Go envelope

**Files:**
- Create: `extensions/dashboard/contract/shell/src/contract/types.ts`

- [ ] **Step 1: Write types.ts (verbatim from design doc)**

```ts
export type Kind = "graph" | "query" | "command" | "subscribe";

export interface Request<TPayload = unknown> {
  envelope: "v1";
  kind: Kind;
  contributor: string;
  intent: string;
  intentVersion?: number;
  payload?: TPayload;
  params?: Record<string, unknown>;
  context: { route: string; correlationID: string };
  csrf?: string;
  idempotencyKey?: string;
}

export interface ResponseMeta {
  intentVersion?: number;
  deprecation?: { intentVersion: number; removeAfter: string };
  cacheControl?: { staleTime?: string };
  invalidates?: string[];
}

export interface Response<TData = unknown> {
  ok: true;
  envelope: "v1";
  kind: Kind;
  data: TData;
  meta: ResponseMeta;
}

export interface ContractError {
  code: string;
  message?: string;
  details?: Record<string, unknown>;
  retryable?: boolean;
  correlationID?: string;
  redactions?: string[];
}

export interface ErrorResponse {
  ok: false;
  envelope: "v1";
  error: ContractError;
}

export type EnvelopeResponse<T = unknown> = Response<T> | ErrorResponse;

export interface DataBinding {
  queryRef?: string;
  intent?: string;
  params?: Record<string, unknown>;
}

export interface GraphNode {
  intent: string;
  title?: string;
  route?: string;
  data?: DataBinding;
  props?: Record<string, unknown>;
  slots?: Record<string, GraphNode[]>;
  enabledWhen?: Predicate;
  op?: string;
  payload?: Record<string, unknown>;
  component?: string;
  src?: string;
}

export interface Predicate {
  all?: string[];
  any?: string[];
  not?: string[];
  warden?: string;
}

export type SubscriptionMode = "replace" | "append" | "snapshot+delta";

export interface StreamEvent<T = unknown> {
  intent: string;
  mode: SubscriptionMode;
  payload: T;
  seq: number;
}

// Wire shape for GET /api/dashboard/v1/principal
export interface Principal {
  subject: string;
  displayName: string;
  email?: string;
  roles: string[];
  scopes: string[];
}
```

- [ ] **Step 2: Verify types compile**

```bash
cd extensions/dashboard/contract/shell && pnpm lint
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add extensions/dashboard/contract/shell/src/contract/types.ts
git commit -m "feat(dashboard/contract/shell): TypeScript types mirroring the Go envelope"
```

### Task 1.2: ContractClient + tests

**Files:**
- Create: `extensions/dashboard/contract/shell/src/contract/client.ts`
- Create: `extensions/dashboard/contract/shell/test/contract.test.ts`

The client lazily fetches the CSRF token, retries once on 401 (token expired), and decodes error envelopes into thrown `ContractClientError`s.

- [ ] **Step 1: Write the failing tests (test/contract.test.ts)**

```ts
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { ContractClient, ContractClientError } from "../src/contract/client";

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
        return HttpResponse.json({ token: "tok123", expiresAt: new Date(Date.now() + 3600_000).toISOString() });
      }),
      http.post("/api/dashboard/v1", async ({ request }) => {
        const body = (await request.json()) as { kind: string; csrf?: string; idempotencyKey?: string };
        expect(body.kind).toBe("command");
        expect(body.csrf).toBe("tok123");
        expect(body.idempotencyKey).toBeTruthy();
        return HttpResponse.json({ ok: true, envelope: "v1", kind: "command", data: null, meta: {} });
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
        HttpResponse.json({ token: `tok-${++attempt}`, expiresAt: new Date(Date.now() + 3600_000).toISOString() }),
      ),
      http.post("/api/dashboard/v1", async ({ request }) => {
        const body = (await request.json()) as { csrf?: string };
        if (body.csrf === "tok-1") {
          return HttpResponse.json(
            { ok: false, envelope: "v1", error: { code: "UNAUTHENTICATED" } },
            { status: 401 },
          );
        }
        return HttpResponse.json({ ok: true, envelope: "v1", kind: "command", data: null, meta: {} });
      }),
    );
    const c = new ContractClient();
    await c.command("users", "do.thing");
    expect(attempt).toBe(2); // refreshed
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
```

- [ ] **Step 2: Run, expect FAIL**

```bash
cd extensions/dashboard/contract/shell && pnpm test
```

Expected: FAIL — module not found (ContractClient).

- [ ] **Step 3: Implement client.ts**

```ts
import type {
  ContractError,
  EnvelopeResponse,
  GraphNode,
  Kind,
  Request,
  Response,
} from "./types";

export class ContractClientError extends Error {
  readonly code: string;
  readonly details?: Record<string, unknown>;
  readonly retryable?: boolean;
  readonly correlationID?: string;

  constructor(err: ContractError) {
    super(err.message ?? err.code);
    this.code = err.code;
    this.details = err.details;
    this.retryable = err.retryable;
    this.correlationID = err.correlationID;
  }
}

export interface ClientOptions {
  baseURL?: string;
  fetcher?: typeof fetch;
}

export class ContractClient {
  private readonly baseURL: string;
  private readonly fetcher: typeof fetch;
  private csrfToken: string | null = null;

  constructor(opts: ClientOptions = {}) {
    this.baseURL = opts.baseURL ?? "/api/dashboard/v1";
    this.fetcher = opts.fetcher ?? fetch;
  }

  async query<T>(contributor: string, intent: string, payload?: unknown, params?: Record<string, unknown>): Promise<T> {
    return this.send<T>({ kind: "query", contributor, intent, payload, params });
  }

  async command<T = unknown>(
    contributor: string,
    intent: string,
    payload?: unknown,
    opts: { idempotencyKey?: string } = {},
  ): Promise<T> {
    return this.send<T>({
      kind: "command",
      contributor,
      intent,
      payload,
      idempotencyKey: opts.idempotencyKey ?? crypto.randomUUID(),
    });
  }

  async graph(contributor: string, route: string): Promise<GraphNode> {
    return this.send<GraphNode>({ kind: "graph", contributor, intent: "page.shell", payload: { route } });
  }

  private async send<T>(input: Omit<Request, "envelope" | "context"> & { context?: Request["context"] }): Promise<T> {
    return this.sendWithRetry<T>(input, /* attempted401Refresh */ false);
  }

  private async sendWithRetry<T>(
    input: Omit<Request, "envelope" | "context"> & { context?: Request["context"] },
    attempted401Refresh: boolean,
  ): Promise<T> {
    if (input.kind === "command" && !this.csrfToken) {
      await this.refreshCSRF();
    }
    const req: Request = {
      envelope: "v1",
      kind: input.kind,
      contributor: input.contributor,
      intent: input.intent,
      intentVersion: input.intentVersion,
      payload: input.payload,
      params: input.params,
      context: input.context ?? { route: typeof window !== "undefined" ? window.location.pathname : "/", correlationID: crypto.randomUUID() },
      csrf: input.kind === "command" ? this.csrfToken ?? undefined : undefined,
      idempotencyKey: input.idempotencyKey,
    };

    const res = await this.fetcher(this.baseURL, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(req),
      credentials: "include",
    });

    let body: EnvelopeResponse<T>;
    try {
      body = (await res.json()) as EnvelopeResponse<T>;
    } catch {
      throw new ContractClientError({ code: "INTERNAL", message: `non-JSON response (status ${res.status})` });
    }

    if (body.ok) {
      return (body as Response<T>).data;
    }

    // Error envelope. Retry once on 401 (CSRF refresh).
    if (!attempted401Refresh && res.status === 401 && input.kind === "command") {
      await this.refreshCSRF();
      return this.sendWithRetry<T>(input, true);
    }

    throw new ContractClientError(body.error);
  }

  private async refreshCSRF(): Promise<void> {
    const res = await this.fetcher(`${this.baseURL}/csrf`, { credentials: "include" });
    if (!res.ok) {
      this.csrfToken = null;
      return;
    }
    const body = (await res.json()) as { token: string };
    this.csrfToken = body.token;
  }
}
```

- [ ] **Step 4: Run tests, expect PASS**

```bash
pnpm test
```

Expected: 5 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/shell/src/contract/client.ts extensions/dashboard/contract/shell/test/contract.test.ts
git commit -m "feat(dashboard/contract/shell): contract client with auto-CSRF and idempotency"
```

---

## Phase 2: SSE Multiplex Consumer

### Task 2.1: SubscriptionMux + tests

**Files:**
- Create: `extensions/dashboard/contract/shell/src/contract/sse.ts`
- Create: `extensions/dashboard/contract/shell/test/sse.test.ts`

JSDOM does not provide `EventSource`. The mux uses `EventSource` directly; tests substitute via constructor injection.

- [ ] **Step 1: Write the failing tests**

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SubscriptionMux } from "../src/contract/sse";
import type { StreamEvent } from "../src/contract/types";

class FakeEventSource {
  static instances: FakeEventSource[] = [];
  url: string;
  readyState = 0; // CONNECTING
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  private listeners = new Map<string, Array<(ev: MessageEvent) => void>>();

  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
    queueMicrotask(() => {
      this.readyState = 1;
      this.onopen?.();
    });
  }
  addEventListener(name: string, fn: (ev: MessageEvent) => void) {
    const arr = this.listeners.get(name) ?? [];
    arr.push(fn);
    this.listeners.set(name, arr);
  }
  removeEventListener() { /* ignore for tests */ }
  emit(name: string, data: string) {
    const arr = this.listeners.get(name) ?? [];
    arr.forEach(fn => fn({ data } as MessageEvent));
  }
  close() {
    this.readyState = 2;
  }
}

const fetchStub = vi.fn(async (url: string, _init?: RequestInit) => {
  if (url.endsWith("/stream/control")) {
    return new globalThis.Response(JSON.stringify({}), { status: 200 }) as unknown as Response;
  }
  throw new Error("unexpected fetch: " + url);
});

beforeEach(() => {
  FakeEventSource.instances = [];
  fetchStub.mockClear();
});

describe("SubscriptionMux", () => {
  it("dispatches events to the right subscription handler by SSE event name", async () => {
    const mux = new SubscriptionMux({ baseURL: "/api/dashboard/v1", eventSource: FakeEventSource as unknown as typeof EventSource, fetcher: fetchStub as unknown as typeof fetch });
    const received: StreamEvent[] = [];
    const unsub = await mux.subscribe("logs", "audit.tail", {}, ev => { received.push(ev); }, { subscriptionID: "s1" });
    const es = FakeEventSource.instances[0]!;
    es.emit("hello", JSON.stringify({ streamID: "stream-x" }));
    // wait a tick for the hello -> control message round trip
    await Promise.resolve();
    es.emit("s1", JSON.stringify({ intent: "audit.tail", mode: "append", payload: { line: "hi" }, seq: 1 }));
    expect(received).toHaveLength(1);
    expect(received[0]!.payload).toEqual({ line: "hi" });
    unsub();
  });

  it("sends a control message on subscribe and on unsubscribe", async () => {
    const mux = new SubscriptionMux({ baseURL: "/api/dashboard/v1", eventSource: FakeEventSource as unknown as typeof EventSource, fetcher: fetchStub as unknown as typeof fetch });
    const unsub = await mux.subscribe("logs", "audit.tail", {}, () => {}, { subscriptionID: "s1" });
    FakeEventSource.instances[0]!.emit("hello", JSON.stringify({ streamID: "stream-x" }));
    await Promise.resolve();
    expect(fetchStub).toHaveBeenCalledWith(
      "/api/dashboard/v1/stream/control",
      expect.objectContaining({ method: "POST" }),
    );
    const subscribeBody = JSON.parse(fetchStub.mock.calls.find(([, init]) => (init as RequestInit).body)![1]!.body as string);
    expect(subscribeBody.op).toBe("subscribe");
    expect(subscribeBody.subscriptionID).toBe("s1");

    fetchStub.mockClear();
    unsub();
    await Promise.resolve();
    const unsubscribeBody = JSON.parse(fetchStub.mock.calls[0]![1]!.body as string);
    expect(unsubscribeBody.op).toBe("unsubscribe");
  });
});

afterEach(() => {
  FakeEventSource.instances.forEach(es => es.close());
});
```

- [ ] **Step 2: Run, expect FAIL**

```bash
pnpm test
```

Expected: FAIL — module not found.

- [ ] **Step 3: Implement sse.ts**

```ts
import type { StreamEvent } from "./types";

export interface SubscriptionMuxOptions {
  baseURL?: string;
  eventSource?: typeof EventSource;
  fetcher?: typeof fetch;
}

interface PendingSub {
  contributor: string;
  intent: string;
  params: Record<string, unknown>;
  subscriptionID: string;
  handler: (ev: StreamEvent) => void;
}

export class SubscriptionMux {
  private readonly baseURL: string;
  private readonly EventSourceCtor: typeof EventSource;
  private readonly fetcher: typeof fetch;
  private es: EventSource | null = null;
  private streamID: string | null = null;
  private pending: PendingSub[] = [];
  private active = new Map<string, PendingSub>();

  constructor(opts: SubscriptionMuxOptions = {}) {
    this.baseURL = opts.baseURL ?? "/api/dashboard/v1";
    this.EventSourceCtor = opts.eventSource ?? globalThis.EventSource;
    this.fetcher = opts.fetcher ?? globalThis.fetch;
  }

  async subscribe(
    contributor: string,
    intent: string,
    params: Record<string, unknown>,
    handler: (ev: StreamEvent) => void,
    opts: { subscriptionID?: string } = {},
  ): Promise<() => void> {
    const subscriptionID = opts.subscriptionID ?? crypto.randomUUID();
    const sub: PendingSub = { contributor, intent, params, subscriptionID, handler };

    if (!this.es) {
      this.openStream();
    }
    this.attachSubscriptionListener(sub);

    if (this.streamID) {
      await this.sendControl({ op: "subscribe", subscriptionID, contributor, intent, params });
      this.active.set(subscriptionID, sub);
    } else {
      this.pending.push(sub);
    }

    return () => this.unsubscribe(subscriptionID);
  }

  private openStream(): void {
    this.es = new this.EventSourceCtor(`${this.baseURL}/stream`);
    this.es.addEventListener("hello", (ev: MessageEvent) => {
      const { streamID } = JSON.parse(ev.data) as { streamID: string };
      this.streamID = streamID;
      // Drain pending subscriptions in registration order.
      const drain = this.pending.splice(0);
      void Promise.all(
        drain.map(sub => {
          this.active.set(sub.subscriptionID, sub);
          return this.sendControl({
            op: "subscribe",
            subscriptionID: sub.subscriptionID,
            contributor: sub.contributor,
            intent: sub.intent,
            params: sub.params,
          });
        }),
      );
    });
  }

  private attachSubscriptionListener(sub: PendingSub): void {
    if (!this.es) return;
    this.es.addEventListener(sub.subscriptionID, (ev: MessageEvent) => {
      try {
        const parsed = JSON.parse(ev.data) as StreamEvent;
        sub.handler(parsed);
      } catch {
        // Drop malformed events.
      }
    });
  }

  private async unsubscribe(subscriptionID: string): Promise<void> {
    this.active.delete(subscriptionID);
    if (!this.streamID) return;
    await this.sendControl({ op: "unsubscribe", subscriptionID });
  }

  private async sendControl(msg: Record<string, unknown>): Promise<void> {
    if (!this.streamID) return;
    await this.fetcher(`${this.baseURL}/stream/control`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ streamID: this.streamID, ...msg }),
      credentials: "include",
    });
  }

  close(): void {
    this.es?.close();
    this.es = null;
    this.streamID = null;
    this.active.clear();
    this.pending = [];
  }
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
pnpm test
```

Expected: 7 tests PASS (5 contract + 2 sse).

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/contract/shell/src/contract/sse.ts extensions/dashboard/contract/shell/test/sse.test.ts
git commit -m "feat(dashboard/contract/shell): SSE multiplex consumer"
```

---

## Phase 3: Auth Principal Store + React Query Hooks

### Task 3.1: Principal store

**Files:**
- Create: `extensions/dashboard/contract/shell/src/auth/principal.ts`

- [ ] **Step 1: Write principal.ts**

```ts
import { create } from "zustand";
import type { Principal } from "../contract/types";

interface PrincipalState {
  principal: Principal | null;
  loaded: boolean;
  error: string | null;
  load: (fetcher?: typeof fetch) => Promise<void>;
}

export const usePrincipalStore = create<PrincipalState>(set => ({
  principal: null,
  loaded: false,
  error: null,
  async load(fetcher = fetch) {
    try {
      const res = await fetcher("/api/dashboard/v1/principal", { credentials: "include" });
      if (!res.ok) {
        set({ loaded: true, error: `HTTP ${res.status}`, principal: null });
        return;
      }
      const principal = (await res.json()) as Principal;
      set({ principal, loaded: true, error: null });
    } catch (err) {
      set({ loaded: true, error: String(err), principal: null });
    }
  },
}));
```

(No tests for this in slice (d) — Phase 7 covers the integration via the smoke test.)

- [ ] **Step 2: Verify lint**

```bash
pnpm lint
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add extensions/dashboard/contract/shell/src/auth/principal.ts
git commit -m "feat(dashboard/contract/shell): principal store via Zustand"
```

### Task 3.2: React Query hooks (useGraph, useQuery, useCommand, useSubscription)

**Files:**
- Create: `extensions/dashboard/contract/shell/src/contract/hooks.ts`

The hooks compose React Query and the SubscriptionMux into the public API the intent components use.

- [ ] **Step 1: Write hooks.ts**

```ts
import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery as useRQ } from "@tanstack/react-query";
import { ContractClient } from "./client";
import { SubscriptionMux } from "./sse";
import type { GraphNode, StreamEvent } from "./types";

const sharedClient = new ContractClient();
const sharedMux = new SubscriptionMux();

export function useContractGraph(contributor: string, route: string) {
  return useRQ<GraphNode>({
    queryKey: ["graph", contributor, route],
    queryFn: () => sharedClient.graph(contributor, route),
  });
}

export function useContractQuery<T = unknown>(contributor: string, intent: string, payload?: unknown, params?: Record<string, unknown>) {
  return useRQ<T>({
    queryKey: ["query", contributor, intent, payload, params],
    queryFn: () => sharedClient.query<T>(contributor, intent, payload, params),
  });
}

export function useContractCommand<TPayload = unknown, TResponse = unknown>(contributor: string, intent: string) {
  return useMutation<TResponse, Error, TPayload>({
    mutationFn: payload => sharedClient.command<TResponse>(contributor, intent, payload),
  });
}

export function useSubscription<T = unknown>(contributor: string, intent: string, params: Record<string, unknown> = {}) {
  const [latest, setLatest] = useState<StreamEvent<T> | null>(null);
  const handlerRef = useRef((ev: StreamEvent<T>) => setLatest(ev));

  useEffect(() => {
    let unsub: (() => void) | null = null;
    let cancelled = false;
    void sharedMux.subscribe(contributor, intent, params, ev => handlerRef.current(ev as StreamEvent<T>)).then(u => {
      if (cancelled) {
        u();
        return;
      }
      unsub = u;
    });
    return () => {
      cancelled = true;
      unsub?.();
    };
  }, [contributor, intent, JSON.stringify(params)]);

  return latest;
}
```

- [ ] **Step 2: Verify lint**

```bash
pnpm lint
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add extensions/dashboard/contract/shell/src/contract/hooks.ts
git commit -m "feat(dashboard/contract/shell): React Query hooks for graph, query, command, subscription"
```

---

## Phase 4: Intent Registry + Slot Renderer

### Task 4.1: Registry + context

**Files:**
- Create: `extensions/dashboard/contract/shell/src/runtime/registry.ts`
- Create: `extensions/dashboard/contract/shell/src/runtime/context.ts`

- [ ] **Step 1: Write registry.ts**

```ts
import type { ComponentType } from "react";
import type { GraphNode } from "../contract/types";

export interface IntentComponentProps<TData = unknown, TProps = Record<string, unknown>> {
  node: GraphNode;
  data?: TData;
  props: TProps;
  slots: Record<string, GraphNode[]>;
}

export type IntentComponent = ComponentType<IntentComponentProps<unknown, Record<string, unknown>>>;

export class IntentRegistry {
  private byName = new Map<string, IntentComponent>();

  register(name: string, component: IntentComponent): this {
    if (this.byName.has(name)) {
      throw new Error(`intent ${name} already registered`);
    }
    this.byName.set(name, component);
    return this;
  }

  resolve(name: string): IntentComponent | undefined {
    return this.byName.get(name);
  }

  has(name: string): boolean {
    return this.byName.has(name);
  }
}
```

- [ ] **Step 2: Write context.ts**

```tsx
import { createContext, useContext } from "react";
import type { ReactNode } from "react";
import type { IntentRegistry } from "./registry";

const RegistryContext = createContext<IntentRegistry | null>(null);

export function IntentRegistryProvider({ value, children }: { value: IntentRegistry; children: ReactNode }) {
  return <RegistryContext.Provider value={value}>{children}</RegistryContext.Provider>;
}

export function useIntentRegistry(): IntentRegistry {
  const reg = useContext(RegistryContext);
  if (!reg) throw new Error("useIntentRegistry called outside IntentRegistryProvider");
  return reg;
}
```

- [ ] **Step 3: Verify lint and commit**

```bash
pnpm lint
git add extensions/dashboard/contract/shell/src/runtime/{registry.ts,context.ts}
git commit -m "feat(dashboard/contract/shell): intent registry with React context"
```

### Task 4.2: Renderer + slots + fallbacks + tests

**Files:**
- Create: `extensions/dashboard/contract/shell/src/runtime/fallbacks.tsx`
- Create: `extensions/dashboard/contract/shell/src/runtime/slots.tsx`
- Create: `extensions/dashboard/contract/shell/src/runtime/renderer.tsx`
- Create: `extensions/dashboard/contract/shell/test/renderer.test.tsx`

- [ ] **Step 1: Write fallbacks.tsx**

```tsx
export function UnknownIntent({ intent }: { intent: string }) {
  return (
    <div className="rounded border border-dashed border-amber-400 bg-amber-50 p-3 text-sm text-amber-900">
      Unknown intent: <code>{intent}</code>
    </div>
  );
}

export function LoadingNode() {
  return <div className="rounded border border-gray-200 bg-gray-100 p-3 text-sm text-gray-500">Loading…</div>;
}

export function ErrorNode({ message }: { message: string }) {
  return (
    <div className="rounded border border-red-300 bg-red-50 p-3 text-sm text-red-900">
      Error: {message}
    </div>
  );
}
```

- [ ] **Step 2: Write slots.tsx**

```tsx
import type { GraphNode } from "../contract/types";
import { GraphRenderer } from "./renderer";

export function SlotRenderer({ slot, slots }: { slot: string; slots: Record<string, GraphNode[]> }) {
  const children = slots[slot] ?? [];
  return (
    <>
      {children.map((child, i) => (
        <GraphRenderer key={`${child.intent}:${i}`} node={child} />
      ))}
    </>
  );
}
```

- [ ] **Step 3: Write renderer.tsx**

```tsx
import { useIntentRegistry } from "./context";
import { UnknownIntent } from "./fallbacks";
import type { GraphNode } from "../contract/types";

export function GraphRenderer({ node }: { node: GraphNode }) {
  const registry = useIntentRegistry();
  const Component = registry.resolve(node.intent);
  if (!Component) return <UnknownIntent intent={node.intent} />;
  return (
    <Component
      node={node}
      props={node.props ?? {}}
      slots={node.slots ?? {}}
      data={undefined}
    />
  );
}
```

(Data binding resolution is intentionally minimal here. Slice (e)'s vocabulary components handle their own data via the React Query hooks; the renderer just passes the node through. The `data` prop is reserved for future enhancements that pre-resolve queries via the data router.)

- [ ] **Step 4: Write the failing tests**

```tsx
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { IntentRegistry } from "../src/runtime/registry";
import { IntentRegistryProvider } from "../src/runtime/context";
import { GraphRenderer } from "../src/runtime/renderer";
import { SlotRenderer } from "../src/runtime/slots";
import type { GraphNode } from "../src/contract/types";

function setup(node: GraphNode, registry: IntentRegistry) {
  return render(
    <IntentRegistryProvider value={registry}>
      <GraphRenderer node={node} />
    </IntentRegistryProvider>,
  );
}

describe("GraphRenderer", () => {
  it("renders a registered intent's component", () => {
    const reg = new IntentRegistry();
    reg.register("hello", () => <p>hello world</p>);
    setup({ intent: "hello" }, reg);
    expect(screen.getByText("hello world")).toBeInTheDocument();
  });

  it("renders UnknownIntent fallback when intent is not registered", () => {
    const reg = new IntentRegistry();
    setup({ intent: "missing" }, reg);
    expect(screen.getByText(/Unknown intent/i)).toBeInTheDocument();
    expect(screen.getByText("missing")).toBeInTheDocument();
  });

  it("recursively renders slot children via SlotRenderer", () => {
    const reg = new IntentRegistry();
    reg.register("parent", ({ slots }) => (
      <div data-testid="parent">
        <SlotRenderer slot="main" slots={slots} />
      </div>
    ));
    reg.register("leaf", ({ node }) => <span>leaf-{node.intent}</span>);
    const node: GraphNode = {
      intent: "parent",
      slots: { main: [{ intent: "leaf" }, { intent: "leaf" }] },
    };
    setup(node, reg);
    expect(screen.getByTestId("parent")).toBeInTheDocument();
    expect(screen.getAllByText(/leaf-leaf/)).toHaveLength(2);
  });
});

describe("IntentRegistry", () => {
  it("rejects double registration", () => {
    const reg = new IntentRegistry();
    reg.register("x", () => null);
    expect(() => reg.register("x", () => null)).toThrow(/already registered/);
  });
});
```

- [ ] **Step 5: Run, expect PASS**

```bash
pnpm test
```

Expected: 11 tests PASS (5 contract + 2 sse + 4 renderer).

- [ ] **Step 6: Commit**

```bash
git add extensions/dashboard/contract/shell/src/runtime/{fallbacks.tsx,slots.tsx,renderer.tsx} extensions/dashboard/contract/shell/test/renderer.test.tsx
git commit -m "feat(dashboard/contract/shell): graph renderer with slot expansion and fallbacks"
```

---

## Phase 5: page.shell + metric.counter Components

### Task 5.1: page.shell

**Files:**
- Create: `extensions/dashboard/contract/shell/src/intents/page.shell.tsx`

- [ ] **Step 1: Write page.shell.tsx**

```tsx
import { SlotRenderer } from "../runtime/slots";
import { usePrincipalStore } from "../auth/principal";
import type { IntentComponentProps } from "../runtime/registry";

export function PageShell({ node, slots }: IntentComponentProps) {
  const principal = usePrincipalStore(s => s.principal);
  const title = node.title ?? "Dashboard";
  return (
    <div className="flex h-full flex-col">
      <header className="flex items-center justify-between border-b border-gray-200 bg-white px-6 py-3">
        <h1 className="text-lg font-semibold">{title}</h1>
        <div className="text-sm text-gray-600">
          {principal ? <span>{principal.displayName}</span> : <span>Loading…</span>}
        </div>
      </header>
      <main className="flex-1 overflow-auto p-6">
        <SlotRenderer slot="main" slots={slots} />
      </main>
    </div>
  );
}
```

### Task 5.2: metric.counter

**Files:**
- Create: `extensions/dashboard/contract/shell/src/intents/metric.counter.tsx`

The component subscribes to the `metric.summary` intent (or whatever the data binding declares) and displays a value. For slice (d), we hardcode the contributor and the metric name from `node.data.intent` so the pilot's `/metrics/live` page renders cleanly.

- [ ] **Step 1: Write metric.counter.tsx**

```tsx
import { useSubscription } from "../contract/hooks";
import type { IntentComponentProps } from "../runtime/registry";

interface CounterProps {
  title?: string;
}

interface CounterPayload {
  totalMetrics?: number;
  cpuPercent?: number;
  [k: string]: unknown;
}

const CONTRIBUTOR = "core-contract";

export function MetricCounter({ node, props }: IntentComponentProps<unknown, CounterProps>) {
  const subscriptionIntent = node.data?.intent;
  const ev = useSubscription<CounterPayload>(CONTRIBUTOR, subscriptionIntent ?? "metrics.summary");

  const value = ev?.payload.totalMetrics ?? ev?.payload.cpuPercent ?? "—";
  const title = props.title ?? node.title ?? subscriptionIntent ?? "Metric";

  return (
    <div className="rounded-lg border border-gray-200 bg-white p-6 shadow-sm">
      <div className="text-xs font-medium uppercase tracking-wider text-gray-500">{title}</div>
      <div className="mt-2 text-3xl font-semibold text-gray-900">{value}</div>
    </div>
  );
}
```

> **Note**: the contributor name `core-contract` matches the pilot from slice (c). Slice (e) generalizes this — the data binding's params will carry the contributor.

### Task 5.3: register.ts + tests

**Files:**
- Create: `extensions/dashboard/contract/shell/src/intents/register.ts`
- Modify: `extensions/dashboard/contract/shell/test/renderer.test.tsx` (add a smoke test for built-ins)

- [ ] **Step 1: Write register.ts**

```ts
import { IntentRegistry } from "../runtime/registry";
import { PageShell } from "./page.shell";
import { MetricCounter } from "./metric.counter";

export function buildIntentRegistry(): IntentRegistry {
  const reg = new IntentRegistry();
  reg.register("page.shell", PageShell as any);
  reg.register("metric.counter", MetricCounter as any);
  return reg;
}
```

- [ ] **Step 2: Add a built-ins test**

Append to `test/renderer.test.tsx`:

```tsx
import { buildIntentRegistry } from "../src/intents/register";

describe("buildIntentRegistry", () => {
  it("registers page.shell and metric.counter", () => {
    const reg = buildIntentRegistry();
    expect(reg.has("page.shell")).toBe(true);
    expect(reg.has("metric.counter")).toBe(true);
  });
});
```

- [ ] **Step 3: Run, expect PASS**

```bash
pnpm test
```

Expected: 12 tests PASS.

- [ ] **Step 4: Commit**

```bash
git add extensions/dashboard/contract/shell/src/intents/ extensions/dashboard/contract/shell/test/renderer.test.tsx
git commit -m "feat(dashboard/contract/shell): page.shell and metric.counter intent components"
```

---

## Phase 6: App Routing + Smoke Test

### Task 6.1: App.tsx — providers + router

**Files:**
- Modify: `extensions/dashboard/contract/shell/src/App.tsx`

- [ ] **Step 1: Replace App.tsx with the wired version**

```tsx
import { useEffect } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, Route, Routes, useParams } from "react-router-dom";
import { IntentRegistryProvider } from "./runtime/context";
import { buildIntentRegistry } from "./intents/register";
import { GraphRenderer } from "./runtime/renderer";
import { useContractGraph } from "./contract/hooks";
import { LoadingNode, ErrorNode } from "./runtime/fallbacks";
import { usePrincipalStore } from "./auth/principal";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 5_000, refetchOnWindowFocus: false },
  },
});

const registry = buildIntentRegistry();

function PageRoute() {
  const params = useParams();
  const route = `/${params["*"] ?? ""}`;
  const { data, isLoading, error } = useContractGraph("core-contract", route);

  if (isLoading) return <LoadingNode />;
  if (error) return <ErrorNode message={(error as Error).message} />;
  if (!data) return <ErrorNode message="empty graph" />;
  return <GraphRenderer node={data} />;
}

export function App() {
  const loadPrincipal = usePrincipalStore(s => s.load);
  useEffect(() => { void loadPrincipal(); }, [loadPrincipal]);

  return (
    <QueryClientProvider client={queryClient}>
      <IntentRegistryProvider value={registry}>
        <BrowserRouter basename="/dashboard/contract/app">
          <Routes>
            <Route path="*" element={<PageRoute />} />
          </Routes>
        </BrowserRouter>
      </IntentRegistryProvider>
    </QueryClientProvider>
  );
}
```

- [ ] **Step 2: Verify build**

```bash
pnpm build
```

Expected: clean build.

- [ ] **Step 3: Verify lint**

```bash
pnpm lint
```

Expected: clean.

### Task 6.2: Smoke test

**Files:**
- Create: `extensions/dashboard/contract/shell/test/smoke.test.tsx`

The smoke test mounts the App, intercepts `POST /api/dashboard/v1` (returns a graph for `/metrics/live`), and verifies the metric.counter renders. SSE is mocked but doesn't fire events in this test — the title rendering is enough to prove the runtime resolves and renders.

- [ ] **Step 1: Write smoke.test.tsx**

```tsx
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { App } from "../src/App";

const server = setupServer(
  http.get("/api/dashboard/v1/principal", () =>
    HttpResponse.json({ subject: "alice", displayName: "Alice", roles: [], scopes: [] }),
  ),
  http.post("/api/dashboard/v1", () =>
    HttpResponse.json({
      ok: true, envelope: "v1", kind: "graph",
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
  // jsdom has no EventSource; provide a noop class to keep SubscriptionMux from crashing
  // when buildIntentRegistry's metric.counter mounts and subscribes.
  (globalThis as any).EventSource = class {
    constructor(public url: string) {}
    addEventListener() {}
    removeEventListener() {}
    close() {}
    onopen: (() => void) | null = null;
    onerror: (() => void) | null = null;
  };
  // Override window.location.pathname to a known route.
  history.pushState({}, "", "/dashboard/contract/app/metrics/live");
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe("App smoke", () => {
  it("renders page.shell with metric.counter from a fetched graph", async () => {
    render(<App />);
    expect(await screen.findByText("Live Metrics")).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.getByText("Total Metrics")).toBeInTheDocument();
    });
    expect(screen.getByText("Alice")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run, expect PASS**

```bash
pnpm test
```

Expected: 13 tests PASS.

- [ ] **Step 3: Commit**

```bash
git add extensions/dashboard/contract/shell/src/App.tsx extensions/dashboard/contract/shell/test/smoke.test.tsx
git commit -m "feat(dashboard/contract/shell): App routing + smoke test through page.shell + metric.counter"
```

---

## Phase 7: Go-side Wire-up

### Task 7.1: Principal endpoint

**Files:**
- Create: `extensions/dashboard/handlers/principal.go`
- Create: `extensions/dashboard/handlers/principal_test.go`

- [ ] **Step 1: Write the failing test**

```go
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
)

func TestHandleAPIPrincipal_OK(t *testing.T) {
	user := &dashauth.UserInfo{Subject: "alice", DisplayName: "Alice", Roles: []string{"admin"}, Scopes: []string{"users.read"}}
	ctx := dashauth.WithUser(context.Background(), user)
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/v1/principal", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	HandleAPIPrincipalHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["subject"] != "alice" || body["displayName"] != "Alice" {
		t.Errorf("unexpected body: %v", body)
	}
}

func TestHandleAPIPrincipal_Unauthenticated(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/v1/principal", nil)
	w := httptest.NewRecorder()
	HandleAPIPrincipalHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}
```

> **Note**: confirm the `dashauth.WithUser` helper exists. If the auth package doesn't expose a `WithUser(ctx, user) context.Context`, use the existing `UserFromContext` lookup pattern and find the corresponding setter (likely `SetUserOnContext` or unexported). If no public setter exists, the test can construct a request whose context has the user via direct context.WithValue using `dashauth`'s context key. Adjust as needed; keep behavior identical.

- [ ] **Step 2: Run, expect FAIL**

```bash
go test ./extensions/dashboard/handlers/...
```

Expected: FAIL — undefined HandleAPIPrincipalHTTP.

- [ ] **Step 3: Implement principal.go**

```go
package handlers

import (
	"encoding/json"
	"net/http"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
)

// principalResponse is the wire shape for GET /api/dashboard/v1/principal.
type principalResponse struct {
	Subject     string   `json:"subject"`
	DisplayName string   `json:"displayName"`
	Email       string   `json:"email,omitempty"`
	Roles       []string `json:"roles"`
	Scopes      []string `json:"scopes"`
}

// HandleAPIPrincipalHTTP returns the current user's principal info as JSON.
// 401 when no user is in context.
func HandleAPIPrincipalHTTP(w http.ResponseWriter, r *http.Request) {
	user := dashauth.UserFromContext(r.Context())
	if user == nil {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	resp := principalResponse{
		Subject:     user.Subject,
		DisplayName: user.DisplayName,
		Email:       user.Email,
		Roles:       append([]string{}, user.Roles...),
		Scopes:      append([]string{}, user.Scopes...),
	}
	if resp.DisplayName == "" {
		resp.DisplayName = resp.Subject
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
```

- [ ] **Step 4: Run, expect PASS**

```bash
go test ./extensions/dashboard/handlers/...
```

Expected: 2 new tests PASS.

- [ ] **Step 5: Commit**

```bash
git add extensions/dashboard/handlers/principal.go extensions/dashboard/handlers/principal_test.go
git commit -m "feat(dashboard): principal endpoint exposing UserInfo to the React shell"
```

### Task 7.2: Embed shell + register routes

**Files:**
- Create: `extensions/dashboard/contract/shell/embed.go`
- Modify: `extensions/dashboard/extension.go`

- [ ] **Step 1: Write embed.go**

```go
package shell

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// FS returns the production-built shell's static files.
// Files live under "dist/" within the embedded FS; the returned fs.FS strips
// that prefix so the static handler sees a flat root.
func FS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
```

> **Build dependency**: this file refers to `dist/` which is created by `pnpm build`. For the Go build to succeed, `pnpm install && pnpm build` must run inside `shell/` first. CI updates and a future `Makefile` target are out of scope for this slice but flagged as a follow-up.

- [ ] **Step 2: Add a placeholder dist directory so the embed compiles**

```bash
mkdir -p extensions/dashboard/contract/shell/dist
echo "<!doctype html><html><body><div id=\"root\"></div></body></html>" > extensions/dashboard/contract/shell/dist/index.html
```

This file is gitignored locally; CI builds the real one.

- [ ] **Step 3: Modify extension.go — register principal endpoint and SPA static handler**

In `extensions/dashboard/extension.go`, find where the contract routes are registered (search for `e.handleContractCapabilities`). Add three new route registrations alongside:

```go
import (
	"net/http"
	"path"
	"strings"

	"github.com/xraph/forge/extensions/dashboard/contract/shell"
	"github.com/xraph/forge/extensions/dashboard/handlers"
)

// Inside the contract registration block:
must(router.GET(base+"/api/dashboard/v1/principal", handlers.HandleAPIPrincipalHTTP))

// Static + SPA fallback for the React shell:
shellFS, err := shell.FS()
if err != nil {
	return fmt.Errorf("dashboard: load shell embed: %w", err)
}
must(router.GET(base+"/contract/static/*filepath", e.makeShellStaticHandler(shellFS)))
must(router.GET(base+"/contract/app", e.makeShellSPAHandler(shellFS)))
must(router.GET(base+"/contract/app/*filepath", e.makeShellSPAHandler(shellFS)))
```

Add the helpers at the bottom of `extension.go` (or in a new `extension_shell.go`):

```go
func (e *Extension) makeShellStaticHandler(shellFS fs.FS) http.HandlerFunc {
	fileServer := http.FileServer(http.FS(shellFS))
	return func(w http.ResponseWriter, r *http.Request) {
		// Strip the /dashboard/contract/static prefix so fileServer sees a clean path.
		prefix := e.config.BasePath + "/contract/static"
		r2 := *r
		r2.URL = &url.URL{Path: strings.TrimPrefix(r.URL.Path, prefix)}
		// Aggressive cache for hashed assets.
		if strings.Contains(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileServer.ServeHTTP(w, &r2)
	}
}

func (e *Extension) makeShellSPAHandler(shellFS fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, err := shellFS.Open("index.html")
		if err != nil {
			http.Error(w, "shell index missing — has the shell been built?", http.StatusInternalServerError)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = io.Copy(w, f)
	}
}
```

Add imports: `"io"`, `"io/fs"`, `"net/url"`, `"strings"`.

> **Notes on router shape**: the `*filepath` syntax depends on the underlying router (BunRouter). Confirm via `grep "filepath\|wildcard" internal/router/`. If `*filepath` doesn't work, use whatever wildcard the router supports (e.g., `*` alone). The intent is: any path under `/dashboard/contract/static/` resolves a file from the embedded FS.

- [ ] **Step 4: Build the whole module**

```bash
go build ./...
```

Expected: clean build. The placeholder `dist/index.html` is enough for the embed to satisfy the compiler.

- [ ] **Step 5: Test**

```bash
go test ./extensions/dashboard/...
```

Expected: all prior tests pass; the new principal test added in Task 7.1 also passes.

- [ ] **Step 6: Vet**

```bash
go vet ./extensions/dashboard/...
```

Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add extensions/dashboard/contract/shell/embed.go extensions/dashboard/extension.go
git commit -m "feat(dashboard): embed React shell and serve static + SPA routes"
```

---

## Phase 8: Final Verification

- [ ] **Step 1: Build the React shell for real (replaces the placeholder dist)**

```bash
cd extensions/dashboard/contract/shell
pnpm install   # first time / lockfile change
pnpm build
```

Expected: `dist/` populated with hashed JS/CSS bundles + `index.html`.

- [ ] **Step 2: Build the Go binary with the real shell embedded**

```bash
cd /Users/rexraphael/Work/xraph/forge
go build ./...
```

Expected: clean build; the binary's `embed.FS` now contains the real bundle.

- [ ] **Step 3: Run all tests**

```bash
go test -count=1 ./extensions/dashboard/...
go test -race -count=1 ./extensions/dashboard/contract/...
go vet ./extensions/dashboard/...
cd extensions/dashboard/contract/shell && pnpm test
```

All four must be clean.

- [ ] **Step 4: Manual smoke (optional, post-merge)**

Start the dashboard, browse to `http://localhost:8080/dashboard/contract/app/metrics/live`. Should render the page shell with the Total Metrics counter. CSRF token + stream subscription should connect within 1s.

- [ ] **Step 5: Final commit if anything's still dangling**

```bash
git status
```

If clean, slice (d) is complete.

## Self-Review Notes

- **Spec coverage:** Every section of SLICE_D_DESIGN.md maps to a phase. Project layout → Phase 0; contract types → Phase 1.1; client → Phase 1.2; SSE → Phase 2; principal store → Phase 3.1; React Query hooks → Phase 3.2; intent registry + context → Phase 4.1; renderer + slots + fallbacks → Phase 4.2; page.shell + metric.counter + register → Phase 5; App routing + smoke → Phase 6; principal endpoint → Phase 7.1; embed + static handler + SPA route → Phase 7.2; verification → Phase 8.
- **Spec deviations**: none functionally. The plan's data binding is intentionally minimal in renderer.tsx; data flows through React Query hooks at the leaf component, which is the simpler v1 shape. Slice (e) can layer pre-fetching via the data router.
- **No placeholders**: every TDD cycle has real test code + real implementation. Two informational notes in Phase 7 ask the implementer to verify dashauth/router shape before using the verbatim code — these are honest verify-before-using notes, not unfinished spec.
- **Type consistency**: `GraphNode`, `Request`, `Response`, `StreamEvent`, `IntentComponent`, `IntentRegistry` are defined once and used identically across phases. The TS types mirror the Go envelope shapes exactly.
- **Out-of-scope items honored**: Slice (e) vocabulary, slice (f) migrations, iframe escape hatch, browser E2E, login UI — all stay where the design says.
