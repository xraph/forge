export type Diagnostic = {
  code: string;
  severity: string;
  message: string;
  field?: string;
  file?: string;
  fix?: string;
};
export type Op = { path: string; value?: unknown; delete?: boolean };
export type Service = {
  app: string;
  kind: string;
  replicas?: number;
  ports?: Record<
    string,
    { port: number; exposure?: string; protocol?: string }
  >;
  calls?: string[];
  discovery?: boolean;
  bindings?: {
    resource: string;
    extension: string;
    database?: string;
    store?: string;
    metadata_database?: string;
  }[];
  health?: Record<string, unknown>;
  config?: string[];
  migrate?: string;
};
export type Resource = {
  type: string;
  version?: string;
  features?: string[];
  bucket?: string;
};
export type Placement = {
  lifecycle?: string;
  target?: string;
  secret?: string;
  recipe?: string;
};
export type Registry = {
  host?: string;
  namespace?: string;
  auth?: string;
  visibility?: string;
  username?: string;
  pull_secret?: string;
  secret_ref?: string;
};
export type Build = {
  source?: string;
  delivery?: string;
  builder?: string;
  platforms?: string[];
  registry?: Registry;
  images?: Record<string, string>;
  repo?: string;
  branch?: string;
  commit?: string;
  trigger?: string;
  config_path?: string;
  services?: Record<
    string,
    {
      root_dir?: string;
      dockerfile?: string;
      build_command?: string;
      start_command?: string;
    }
  >;
};
export type Target = {
  provider: string;
  context?: string;
  namespace?: string;
  region?: string;
  docker_context?: string;
  local_cluster?: string;
  network_policy?: boolean;
  ingress_class?: string;
  managed_databases?: Record<string, string>;
  secret_keys?: Record<string, string>;
  gateway?: string;
  build?: Build;
  release?: {
    mode?: string;
    repo?: string;
    branch?: string;
    path?: string;
    approval?: string;
    controller?: string;
  };
  [key: string]: unknown;
};
export type Environment = {
  target: string;
  services?: string[];
  purpose?: string;
  environment_action?: string;
  resources?: Record<string, Placement>;
  replicas?: Record<string, number>;
  ingress?: { host: string; tls?: string };
  external_services?: Record<string, { url: string; auth?: string }>;
};
export type Deploy = {
  version: number;
  registry?: string;
  defaults?: { target?: string; environment?: string };
  services: Record<string, Service>;
  resources?: Record<string, Resource>;
  targets: Record<string, Target>;
  environments: Record<string, Environment>;
  connections?: {
    from: string;
    to: string;
    port?: string;
    config_key?: string;
    timeout?: number;
    retry?: { attempts?: number };
  }[];
  secrets?: {
    resolver?: string;
    file?: string;
    references?: Record<string, string>;
  };
};
export type Settings = {
  hash: string;
  revision: number;
  store: { backend: string; reference?: string; project?: string };
  files: { path: string; hash: string; content: string }[];
  deploy: Deploy | null;
};
export type Suggestion = {
  kind: string;
  path: string;
  value: unknown;
  source: string;
  confidence: string;
  question?: string;
  options?: string[];
};
export type Project = {
  name: string;
  settings: Settings;
  apps: { name: string; config_paths?: string[] }[];
  suggestions: Suggestion[];
  decisions: Suggestion[];
  diagnostics: Diagnostic[];
  providers: { name: string; level: string }[];
};
export type Plan = {
  hash: string;
  target: string;
  environment: string;
  target_spec: Target;
  operations: {
    id: string;
    kind: string;
    service?: string;
    resource?: string;
    destructive: boolean;
    detail: string;
  }[];
  deployment: { services: { name: string }[]; resources: { name: string }[] };
  diagnostics: Diagnostic[];
};
export type Planned = { plan: Plan; artifacts: Record<string, string> };
export type Run = {
  publication?: {
    plan_hash: string;
    images: Record<string, { repository: string; digest: string }>;
  };
  id: string;
  action: string;
  target: string;
  environment: string;
  status: string;
  error?: { message: string };
  started_at: string;
};
export type Status = {
  overall: string;
  failed_operation?: string;
  services: Record<
    string,
    {
      ready: number;
      desired: number;
      message?: string;
      image?: { ref?: string };
    }
  >;
  resources: Record<string, string>;
  routes: string[];
};
export type History = {
  status: string;
  active_plan_hash?: string;
  failed_operation?: string;
  releases: {
    id: string;
    plan_hash: string;
    status: string;
    applied_at: string;
  }[];
  resources: Record<
    string,
    { name: string; type: string; lifecycle: string; provider_id: string }
  >;
};
export type Lifecycle = {
  hash: string;
  request: {
    action: string;
    target: string;
    env: string;
    release?: string;
    delete_data?: boolean;
  };
  expires: string;
  state: { hash: string; snapshot: History; recorded_plan: Plan };
};
export type Connection = {
  name: string;
  kind: string;
  host?: string;
  username?: string;
  connected: boolean;
};
export type Progress = {
  id: number;
  type: string;
  run?: string;
  target?: string;
  environment?: string;
  operation?: { op: string; status: string; message: string };
  error?: { message: string };
};
export class RequestError extends Error {
  constructor(
    message: string,
    readonly code: number,
    readonly diagnostics: Diagnostic[] = [],
  ) {
    super(message);
  }
}
export interface API {
  request<T>(path: string, body?: unknown, signal?: AbortSignal): Promise<T>;
  events(
    onEvent: (event: Progress) => void,
    signal: AbortSignal,
  ): Promise<void>;
}
export const apiClient: API = {
  async request<T>(path: string, body?: unknown, signal?: AbortSignal) {
    const response = await fetch("/api/" + path, {
      method: body === undefined ? "GET" : "POST",
      credentials: "same-origin",
      headers: {
        "X-Forge-Workbench": "1",
        ...(body !== undefined ? { "Content-Type": "application/json" } : {}),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal,
    });
    const result = await response.json();
    if (!response.ok || !result.ok) {
      throw new RequestError(
        result.error?.message ?? "Request failed",
        result.error?.code ?? response.status,
        result.error?.diagnostics,
      );
    }
    return result.data as T;
  },
  async events(onEvent, signal) {
    let last = 0;
    while (!signal.aborted) {
      try {
        const response = await fetch("/api/events?after=" + last, {
          credentials: "same-origin",
          headers: { "X-Forge-Workbench": "1" },
          signal,
        });
        if (!response.ok || !response.body) {
          throw new Error("Progress stream unavailable");
        }
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";
        try {
          while (!signal.aborted) {
            const chunk = await reader.read();
            if (chunk.done) break;
            buffer += decoder.decode(chunk.value, { stream: true });
            let end;
            while ((end = buffer.indexOf("\n\n")) !== -1) {
              const message = buffer.slice(0, end);
              buffer = buffer.slice(end + 2);
              const data = message
                .split("\n")
                .find((line) => line.startsWith("data: "))
                ?.slice(6);
              if (data) {
                if (message.includes("event: reset")) {
                  last = 0;
                  onEvent({ id: 0, type: "reset" });
                } else {
                  const event = JSON.parse(data) as Progress;
                  last = event.id;
                  onEvent(event);
                }
              }
            }
          }
        } finally {
          await reader.cancel().catch(() => {});
        }
      } catch (error) {
        if (signal.aborted) return;
        onEvent({
          id: last,
          type: "disconnected",
          error: {
            message:
              error instanceof Error ? error.message : "Progress disconnected",
          },
        });
      }
      await new Promise<void>((resolve) => {
        const timer = setTimeout(done, 1500);
        function done() {
          clearTimeout(timer);
          signal.removeEventListener("abort", done);
          resolve();
        }
        signal.addEventListener("abort", done, { once: true });
        if (signal.aborted) done();
      });
    }
  },
};
