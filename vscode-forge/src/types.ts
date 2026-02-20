// Types mirroring the Go debug_protocol.go structs.

export type DebugMessageType = 'snapshot' | 'metrics' | 'health' | 'lifecycle' | 'pong';

export interface DebugMessage {
  type: DebugMessageType;
  ts: number;
  app: string;
  payload: unknown;
}

export interface DebugAppInfo {
  name: string;
  version: string;
  environment: string;
  http_addr: string;
  debug_addr: string;
  uptime_ms: number;
}

export interface DebugRoute {
  name?: string;
  method: string;
  path: string;
  tags?: string[];
  summary?: string;
  description?: string;
}

export interface DebugExtInfo {
  name: string;
  version: string;
  description: string;
  dependencies: string[];
  healthy: boolean;
}

export interface DebugCheckResult {
  status: string;
  message?: string;
  response_ms?: number;
}

export interface DebugHealth {
  overall: string;
  checks: Record<string, DebugCheckResult>;
}

export interface DebugMetrics {
  raw: string;
}

export interface DebugSnapshot {
  app: DebugAppInfo;
  config: Record<string, unknown>;
  services: string[];
  routes: DebugRoute[];
  extensions: DebugExtInfo[];
  health?: DebugHealth;
}

export interface ServerEntry {
  pid: number;
  app_name: string;
  app_version: string;
  debug_addr: string;
  app_addr: string;
  workspace_dir: string;
  started_at: string;
}

export interface AppState {
  entry: ServerEntry;
  snapshot?: DebugSnapshot;
  health?: DebugHealth;
  metricsRaw?: string;
  connected: boolean;
  lastUpdate: number;
}
