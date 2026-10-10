import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Workbench } from "./Workbench";
import {
  RequestError,
  type API,
  type Project,
  type Settings,
  type Op,
  type Progress,
} from "./api";
const base: Project = {
  name: "atlas",
  settings: {
    hash: "settings-1",
    revision: 0,
    store: { backend: "files" },
    files: [
      { path: ".forge.yml", hash: "file-1", content: "deploy:\n  version: 2" },
    ],
    deploy: {
      version: 2,
      defaults: { target: "local", environment: "dev" },
      services: {
        api: { app: "api", kind: "web" },
        worker: { app: "worker", kind: "worker" },
      },
      resources: { primary: { type: "postgres", version: "16" } },
      targets: {
        local: { provider: "compose" },
        production: {
          provider: "kubernetes",
          context: "prod",
          namespace: "atlas",
        },
        render: {
          provider: "render",
          build: {
            delivery: "git",
            repo: "https://github.com/example/atlas",
            branch: "main",
          },
        },
      },
      environments: {
        dev: {
          target: "local",
          resources: { primary: { lifecycle: "container" } },
        },
        prod: { target: "production" },
        preview: { target: "render" },
      },
    },
  },
  apps: [{ name: "api" }, { name: "worker" }],
  suggestions: [],
  diagnostics: [],
  providers: [
    { name: "compose", level: "apply" },
    { name: "kubernetes", level: "apply" },
    { name: "render", level: "renderable" },
  ],
};
function fixture(initial = base) {
  let project = structuredClone(initial);
  let reject = false;
  let listener: (event: Progress) => void = () => {};
  const calls = vi.fn();
  const api: API = {
    async request<T>(path: string, body?: unknown) {
      calls(path, body);
      if (path === "project") return structuredClone(project) as T;
      if (path === "files" && body) {
        if (reject)
          throw new RequestError(
            "Configuration changed; reload before saving",
            6,
          );
        const { ops } = body as { ops: Op[] };
        for (const op of ops) {
          const keys = op.path.replace(/^deploy\./, "").split(".");
          let object: Record<string, unknown> = project.settings
            .deploy as unknown as Record<string, unknown>;
          for (const key of keys.slice(0, -1)) {
            object[key] ??= {};
            object = object[key] as Record<string, unknown>;
          }
          object[keys.at(-1)!] = op.value;
        }
        project.settings.hash = "settings-2";
        return structuredClone(project.settings) as T;
      }
      if (path === "init") {
        project.settings.deploy = structuredClone(base.settings.deploy);
        return {} as T;
      }
      if (path === "plan")
        return {
          plan: {
            hash: "a".repeat(64),
            target: "local",
            environment: "dev",
            target_spec: { provider: "compose" },
            operations: [
              {
                id: "rollout-api",
                kind: "rollout",
                service: "api",
                destructive: false,
                detail: "Roll out api",
              },
            ],
            deployment: { services: [{ name: "api" }], resources: [] },
            diagnostics: [],
          },
          artifacts: { "compose.yaml": "services:\n  api: {}" },
        } as T;
      if (path === "status?target=local&env=dev")
        return {
          overall: "failed",
          failed_operation: "rollout-api",
          services: {},
          resources: {},
          routes: [],
        } as T;
      if (path.startsWith("history"))
        return {
          status: "failed",
          failed_operation: "rollout-api",
          releases: [],
          resources: {},
        } as T;
      if (path === "runs" || path === "connections") return [] as T;
      return {} as T;
    },
    async events(fn) {
      listener = fn;
    },
  };
  return {
    api,
    calls,
    reject() {
      reject = true;
    },
    accept() {
      reject = false;
    },
    emit(event: Progress) {
      listener(event);
    },
    settings(): Settings {
      return project.settings;
    },
    refresh() {
      project = structuredClone(base);
    },
  };
}
beforeEach(() => {
  localStorage.clear();
});
async function selectLocal(api: API) {
  const user = userEvent.setup();
  render(<Workbench api={api} />);
  await user.click(await screen.findByRole("button", { name: /Select local/ }));
  return user;
}
describe("deployment workspace", () => {
  it("clears a recovered status error after deployment completes", async () => {
    const f = fixture();
    let deployed = false;
    const request = f.api.request.bind(f.api);
    f.api.request = async (path, body) => {
      if (path.startsWith("status")) {
        if (!deployed) throw new RequestError("No recorded deployment", 4);
        return {
          overall: "healthy",
          services: {},
          resources: {},
          routes: [],
        } as never;
      }
      return request(path, body);
    };
    const user = await selectLocal(f.api);
    await user.click(screen.getByRole("button", { name: "Activity" }));
    expect(await screen.findByText("No recorded deployment")).toBeVisible();
    deployed = true;
    act(() =>
      f.emit({ id: 1, type: "completed", target: "local", environment: "dev" }),
    );
    await waitFor(() =>
      expect(
        screen.queryByText("No recorded deployment"),
      ).not.toBeInTheDocument(),
    );
  });

  it("gates the sidebar on a target and persists the profile selection", async () => {
    const f = fixture();
    const user = await selectLocal(f.api);
    expect(
      await screen.findByRole("navigation", { name: "Deployment sections" }),
    ).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Manage targets" }));
    await user.click(screen.getByRole("button", { name: /Select production/ }));
    expect(localStorage.getItem("forge-deploy:atlas:profile")).toContain(
      "production",
    );
  });
  it("saves an optional service subset through authoritative CAS", async () => {
    const f = fixture();
    const user = await selectLocal(f.api);
    await user.click(
      await screen.findByRole("checkbox", { name: "Deploy worker" }),
    );
    await user.click(
      screen.getByRole("button", { name: "Save configuration" }),
    );
    await waitFor(() =>
      expect(f.calls).toHaveBeenCalledWith(
        "files",
        expect.objectContaining({
          expected: "settings-1",
          ops: expect.arrayContaining([
            { path: "deploy.environments.dev.services", value: ["api"] },
          ]),
        }),
      ),
    );
  });
  it("keeps exact approval invalid after a service edit", async () => {
    const f = fixture();
    const user = await selectLocal(f.api);
    await user.click(screen.getByRole("button", { name: "Build plan" }));
    await user.click(
      await screen.findByRole("checkbox", { name: "Approve this exact plan" }),
    );
    expect(screen.getByRole("button", { name: "Apply plan" })).toBeEnabled();
    await user.click(screen.getByRole("button", { name: "Services" }));
    await user.click(screen.getByRole("checkbox", { name: "Deploy worker" }));
    expect(screen.getByRole("button", { name: "Apply plan" })).toBeDisabled();
  });
  it("preserves a dirty draft on conflict and gives you an explicit reload", async () => {
    const f = fixture();
    f.reject();
    const user = await selectLocal(f.api);
    await user.click(screen.getByRole("checkbox", { name: "Deploy worker" }));
    await user.click(
      screen.getByRole("button", { name: "Save configuration" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Configuration changed",
    );
    expect(
      screen.getByRole("checkbox", { name: "Deploy worker" }),
    ).not.toBeChecked();
    expect(
      screen.getByRole("button", { name: "Reload configuration" }),
    ).toBeVisible();
  });
  it("offers separate image registry and Git source settings", async () => {
    const f = fixture();
    const user = await selectLocal(f.api);
    await user.click(screen.getByRole("button", { name: "Images & delivery" }));
    expect(await screen.findByLabelText("Delivery method")).toBeVisible();
    await user.selectOptions(screen.getByLabelText("Delivery method"), "git");
    expect(screen.getByLabelText("Source repository")).toBeVisible();
    await user.selectOptions(
      screen.getByLabelText("Delivery method"),
      "registry",
    );
    expect(screen.getByLabelText("Registry host")).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Authenticate registry" }),
    ).toBeVisible();
  });
  it("creates a named profile in project settings", async () => {
    const f = fixture();
    const user = await selectLocal(f.api);
    await user.click(screen.getByRole("button", { name: "Manage targets" }));
    await user.click(screen.getByRole("button", { name: "Create target" }));
    await user.type(screen.getByLabelText("Target name"), "staging");
    await user.click(screen.getByRole("button", { name: "Save target" }));
    await waitFor(() =>
      expect(f.calls).toHaveBeenCalledWith(
        "files",
        expect.objectContaining({
          ops: expect.arrayContaining([
            {
              path: "deploy.targets.staging",
              value: expect.objectContaining({ provider: "compose" }),
            },
          ]),
        }),
      ),
    );
  });
  it("shows recorded failure and recovery actions from status", async () => {
    const f = fixture();
    const user = await selectLocal(f.api);
    await user.click(screen.getByRole("button", { name: "Activity" }));
    expect(await screen.findByText("rollout-api")).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Refresh status" }),
    ).toBeVisible();
  });
});

it("renders fresh provider history with nullable wire arrays", async () => {
  const initial = structuredClone(base);
  initial.diagnostics = null as unknown as typeof initial.diagnostics;
  const f = fixture(initial);
  await selectLocal(f.api);
  expect(
    await screen.findByRole("checkbox", { name: "Deploy api" }),
  ).toBeVisible();
});

it("initializes deployment settings for an existing Forge project", async () => {
  const initial = structuredClone(base);
  initial.settings.deploy = null;
  const f = fixture(initial);
  const user = userEvent.setup();
  render(<Workbench api={f.api} />);
  await user.click(
    await screen.findByRole("button", { name: "Initialize deployment" }),
  );
  expect(
    await screen.findByRole("button", { name: "Select local" }),
  ).toBeVisible();
  expect(f.calls).toHaveBeenCalledWith("init", { answers: {}, force: false });
});

it("revokes approval when the server rejects a stale plan", async () => {
  const f = fixture();
  const api: API = {
    ...f.api,
    async request<T>(path: string, body?: unknown) {
      if (path === "apply") throw new RequestError("Plan inputs changed", 6);
      return f.api.request<T>(path, body);
    },
  };
  const user = await selectLocal(api);
  await user.click(screen.getByRole("button", { name: "Build plan" }));
  await user.click(
    await screen.findByRole("checkbox", { name: "Approve this exact plan" }),
  );
  await user.click(screen.getByRole("button", { name: "Apply plan" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Plan inputs changed",
  );
  expect(screen.getByRole("button", { name: "Apply plan" })).toBeDisabled();
  expect(
    screen.getByRole("checkbox", { name: "Approve this exact plan" }),
  ).not.toBeChecked();
});

it("returns to target selection when a saved profile is removed externally", async () => {
  const f = fixture();
  const user = await selectLocal(f.api);
  await user.click(screen.getByRole("checkbox", { name: "Deploy worker" }));
  f.reject();
  delete f.settings().deploy!.targets.local;
  await user.click(screen.getByRole("button", { name: "Save configuration" }));
  await user.click(
    await screen.findByRole("button", { name: "Reload configuration" }),
  );
  expect(
    await screen.findByRole("button", { name: "Select production" }),
  ).toBeVisible();
  expect(
    screen.queryByRole("navigation", { name: "Deployment sections" }),
  ).not.toBeInTheDocument();
});

it("saves provider Git source and trigger values used by the engine", async () => {
  const f = fixture();
  const user = await selectLocal(f.api);
  await user.click(screen.getByRole("button", { name: "Images & delivery" }));
  await user.selectOptions(screen.getByLabelText("Delivery method"), "git");
  await user.type(
    screen.getByLabelText("Source repository"),
    "https://github.com/example/atlas",
  );
  await user.selectOptions(
    screen.getByLabelText("Deploy trigger"),
    "checksPass",
  );
  await user.click(screen.getByRole("button", { name: "Save configuration" }));
  await waitFor(() =>
    expect(f.calls).toHaveBeenCalledWith(
      "files",
      expect.objectContaining({
        ops: [
          {
            path: "deploy.targets.local.build",
            value: expect.objectContaining({
              source: "git",
              trigger: "checksPass",
            }),
          },
        ],
      }),
    ),
  );
});

it("copies a plan handoff that works with SQL authority", async () => {
  const initial = structuredClone(base);
  initial.settings.store = { backend: "sqlite", reference: ".forge/deploy.db" };
  const user = await selectLocal(fixture(initial).api);
  await user.click(screen.getByRole("button", { name: "Build plan" }));
  await user.click(await screen.findByRole("tab", { name: "CLI & AI" }));
  const command = screen.getByText(/forge deploy apply/);
  expect(command).toHaveTextContent(`--plan ${"a".repeat(64)}`);
  expect(command).not.toHaveTextContent(".forge/plans/");
});

it("ignores late status responses after switching targets", async () => {
  const f = fixture();
  let resolveStatus!: (value: unknown) => void;
  const delayed = new Promise((resolve) => {
    resolveStatus = resolve;
  });
  const api: API = {
    ...f.api,
    async request<T>(path: string, body?: unknown) {
      if (path === "status?target=local&env=dev") return delayed as Promise<T>;
      if (path === "status?target=production&env=prod")
        return {
          overall: "healthy",
          services: {},
          resources: {},
          routes: [],
        } as T;
      return f.api.request<T>(path, body);
    },
  };
  const user = await selectLocal(api);
  await user.click(screen.getByRole("button", { name: "Activity" }));
  await user.click(screen.getByRole("button", { name: "Manage targets" }));
  await user.click(screen.getByRole("button", { name: "Select production" }));
  await user.click(screen.getByRole("button", { name: "Activity" }));
  await screen.findByText("healthy");
  await act(async () =>
    resolveStatus({
      overall: "failed",
      failed_operation: "old-target-rollout",
      services: {},
      resources: {},
      routes: [],
    }),
  );
  expect(screen.queryByText("old-target-rollout")).not.toBeInTheDocument();
  expect(screen.getByText("healthy")).toBeVisible();
});

it("plans a resource-only companion target with no application selection", async () => {
  const initial = structuredClone(base);
  initial.settings.deploy!.targets.local.resource_only = true;
  initial.settings.deploy!.environments.dev.services = [];
  const f = fixture(initial);
  const user = await selectLocal(f.api);
  expect(screen.getByRole("checkbox", { name: "Deploy api" })).toHaveAttribute(
    "aria-disabled",
    "true",
  );
  await user.click(screen.getByRole("checkbox", { name: "Deploy api" }));
  expect(screen.getByRole("checkbox", { name: "Deploy api" })).toHaveAttribute(
    "aria-checked",
    "false",
  );
  await user.click(screen.getByRole("button", { name: "Build plan" }));
  await waitFor(() =>
    expect(f.calls).toHaveBeenCalledWith(
      "plan",
      expect.objectContaining({ services: [] }),
    ),
  );
});

it("reloads connection text before using the fresh CAS revision", async () => {
  const f = fixture();
  const user = await selectLocal(f.api);
  await user.click(screen.getByRole("button", { name: "Connections" }));
  await user.type(screen.getByLabelText("Connection overrides"), " ");
  await user.click(
    screen.getByRole("button", { name: "Update connections draft" }),
  );
  f.reject();
  await user.click(screen.getByRole("button", { name: "Save configuration" }));
  await screen.findByRole("alert");
  const connections = [
    { from: "api", to: "worker", port: "http", timeout: 5000000000 },
  ];
  f.settings().deploy!.connections = connections;
  f.settings().hash = "concurrent-settings";
  f.accept();
  await user.click(
    screen.getByRole("button", { name: "Reload configuration" }),
  );
  await waitFor(() =>
    expect(screen.getByLabelText("Connection overrides")).toHaveValue(
      JSON.stringify(connections, null, 2),
    ),
  );
  await user.click(
    screen.getByRole("button", { name: "Update connections draft" }),
  );
  await user.click(screen.getByRole("button", { name: "Save configuration" }));
  await waitFor(() =>
    expect(f.settings().deploy!.connections).toEqual(connections),
  );
});
