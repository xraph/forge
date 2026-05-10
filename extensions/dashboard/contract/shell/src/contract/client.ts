import type {
  ContractError,
  EnvelopeResponse,
  GraphNode,
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

  async query<T>(
    contributor: string,
    intent: string,
    payload?: unknown,
    params?: Record<string, unknown>,
  ): Promise<T> {
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
    return this.send<GraphNode>({
      kind: "graph",
      contributor,
      intent: "page.shell",
      payload: { route },
    });
  }

  private async send<T>(
    input: Omit<Request, "envelope" | "context"> & { context?: Request["context"] },
  ): Promise<T> {
    return this.sendWithRetry<T>(input, false);
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
      context: input.context ?? {
        route: typeof window !== "undefined" ? window.location.pathname : "/",
        correlationID: crypto.randomUUID(),
      },
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
      throw new ContractClientError({
        code: "INTERNAL",
        message: `non-JSON response (status ${res.status})`,
      });
    }

    if (body.ok) {
      return (body as Response<T>).data;
    }

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
