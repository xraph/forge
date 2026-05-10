import * as React from "react";
import { usePrincipalStore } from "./principal";
import { LoginScreen } from "./LoginScreen";
import { LoadingNode } from "../runtime/fallbacks";

interface AuthGateProps {
  children: React.ReactNode;
}

/**
 * AuthGate sits between the router and the dashboard layout. While the
 * principal is loading the gate renders a spinner; once loaded it either
 * passes through (auth disabled or user authenticated) or replaces the tree
 * with the built-in LoginScreen (auth enabled but unauthenticated).
 *
 * This is the slice (l) auth gate. Auth extensions that ship a contract
 * /login route bypass the LoginScreen by handling the login envelope on the
 * server side so the next /principal call succeeds; the gate is therefore
 * also the integration seam — no React-side hook required for the extension.
 */
export function AuthGate({ children }: AuthGateProps) {
  const loaded = usePrincipalStore((s) => s.loaded);
  const authRequired = usePrincipalStore((s) => s.authRequired);

  if (!loaded) return <LoadingNode />;
  if (authRequired) return <LoginScreen />;
  return <>{children}</>;
}
