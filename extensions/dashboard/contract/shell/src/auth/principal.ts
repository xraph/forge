import { create } from "zustand";
import { contractBase } from "../runtime/config";
import type { Principal } from "../contract/types";

interface PrincipalState {
  principal: Principal | null;
  loaded: boolean;
  error: string | null;
  // Slice (l): set true when /principal returns 401 with the
  // {code:"UNAUTHENTICATED"} envelope. The shell uses this to render the
  // built-in LoginScreen (or the auth extension's contract /login route).
  // Stays false when auth is disabled server-side (200 anonymous response).
  authRequired: boolean;
  loginPath: string | null;
  load: (fetcher?: typeof fetch) => Promise<void>;
}

interface PrincipalEnvelope {
  authenticated: boolean;
  subject?: string;
  displayName?: string;
  email?: string;
  roles?: string[];
  scopes?: string[];
}

interface UnauthEnvelope {
  code?: string;
  loginPath?: string;
}

export const usePrincipalStore = create<PrincipalState>((set) => ({
  principal: null,
  loaded: false,
  error: null,
  authRequired: false,
  loginPath: null,
  async load(fetcher = fetch) {
    try {
      const res = await fetcher(`${contractBase}/principal`, { credentials: "include" });
      if (res.status === 401) {
        const body = (await res.json().catch(() => ({}))) as UnauthEnvelope;
        set({
          loaded: true,
          error: null,
          principal: null,
          authRequired: true,
          loginPath: body.loginPath ?? null,
        });
        return;
      }
      if (!res.ok) {
        set({
          loaded: true,
          error: `HTTP ${res.status}`,
          principal: null,
          authRequired: false,
          loginPath: null,
        });
        return;
      }
      const env = (await res.json()) as PrincipalEnvelope;
      if (!env.authenticated) {
        set({
          principal: null,
          loaded: true,
          error: null,
          authRequired: false,
          loginPath: null,
        });
        return;
      }
      set({
        principal: {
          subject: env.subject ?? "",
          displayName: env.displayName ?? env.subject ?? "",
          email: env.email,
          roles: env.roles ?? [],
          scopes: env.scopes ?? [],
        },
        loaded: true,
        error: null,
        authRequired: false,
        loginPath: null,
      });
    } catch (err) {
      set({
        loaded: true,
        error: String(err),
        principal: null,
        authRequired: false,
        loginPath: null,
      });
    }
  },
}));
