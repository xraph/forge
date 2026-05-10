// Runtime configuration injected by the Go server before the bundle loads.
//
// The dashboard extension may be mounted at any base path (e.g. /dashboard,
// /admin, /ops) — even rebased behind a reverse proxy. The Go SPA handler
// inlines a <script> tag in index.html that sets window.__FORGE_DASHBOARD__
// with the resolved paths. The shell reads it once at module load and uses
// the values for the API client baseURL and the React Router basename.
//
// Falls back to /dashboard so unit tests, Vite dev mode, and direct module
// imports keep working without server-side injection.

interface InjectedConfig {
  basePath?: string;
  contractBase?: string;
  shellBase?: string;
  // Slice (l): auth toggles. authEnabled controls whether the shell renders
  // the LoginScreen on a 401 from /principal. loginPath is the absolute URL
  // the shell should fall back to when no contract /login route exists in
  // the manifest. loginOp is the contract command name the built-in
  // LoginScreen issues on submit.
  authEnabled?: boolean;
  loginPath?: string;
  loginOp?: string;
}

declare global {
  interface Window {
    __FORGE_DASHBOARD__?: InjectedConfig;
  }
}

const FALLBACK_BASE = "/dashboard";

function readInjected(): InjectedConfig {
  if (typeof window === "undefined") return {};
  return window.__FORGE_DASHBOARD__ ?? {};
}

const injected = readInjected();

export const basePath: string = injected.basePath ?? FALLBACK_BASE;
export const contractBase: string = injected.contractBase ?? `${FALLBACK_BASE}/api/dashboard/v1`;
export const shellBase: string = injected.shellBase ?? `${FALLBACK_BASE}/contract/app`;
export const authEnabled: boolean = injected.authEnabled ?? false;
export const loginPath: string = injected.loginPath ?? `${FALLBACK_BASE}/login`;
export const loginOp: string = injected.loginOp ?? "auth.login";
