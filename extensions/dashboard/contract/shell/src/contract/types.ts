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
  // Slice (j): server-extracted :name route params for the matched manifest
  // route, e.g. /traces/:id matched against /traces/abc -> { id: "abc" }.
  routeParams?: Record<string, string>;
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
  // kind is stamped by the dashboard registry at merge time from the
  // referenced intent's declared kind. The shell branches on this in
  // metric.counter to pick useContractQuery vs useSubscription; absent
  // for legacy / hand-crafted graph responses, in which case components
  // default to the query path.
  kind?: "query" | "command" | "subscription" | "graph";
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

export interface Principal {
  subject: string;
  displayName: string;
  email?: string;
  roles: string[];
  scopes: string[];
}
