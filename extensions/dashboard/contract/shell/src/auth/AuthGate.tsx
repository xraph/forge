import * as React from "react";
import { usePrincipalStore } from "./principal";
import { LoginScreen } from "./LoginScreen";
import { LoadingNode } from "../runtime/fallbacks";
import { GraphRenderer } from "../runtime/renderer";
import { ContributorProvider, RouteParamsProvider } from "../runtime/context";
import { useContractGraph } from "../contract/hooks";
import { ContractClientError } from "../contract/client";
import { loginContributor } from "../runtime/config";

interface AuthGateProps {
  children: React.ReactNode;
}

const LOGIN_ROUTE = "/login";

/**
 * AuthGate sits between the router and the dashboard layout. While the
 * principal is loading the gate renders a spinner; once loaded it either
 * passes through (auth disabled or user authenticated) or replaces the tree
 * with a login UI.
 *
 * Slice (l) login UI sourcing — preferred path is the auth extension's
 * contract `/login` graph route under its contributor (default `auth`).
 * The gate fetches it; if the contributor or route is missing the gate
 * falls back to the built-in `LoginScreen` so the shell still works
 * out-of-the-box. This means authsome (or any auth extension) owns the
 * login UI by registering one graph node, no React code required.
 */
export function AuthGate({ children }: AuthGateProps) {
  const loaded = usePrincipalStore((s) => s.loaded);
  const authRequired = usePrincipalStore((s) => s.authRequired);

  if (!loaded) return <LoadingNode />;
  if (authRequired) return <LoginGate />;
  return <>{children}</>;
}

function LoginGate() {
  const { data, error, isLoading } = useContractGraph(loginContributor, LOGIN_ROUTE);

  if (isLoading) return <LoadingNode />;

  // 404 (no contract /login route registered) → fall back to the built-in
  // form. Any other error also falls through; the LoginScreen submission
  // surfaces command-level errors of its own.
  if (error || !data) {
    return <LoginScreen />;
  }

  // The auth extension registered a /login route — render its graph as the
  // login surface. Wrap with the contributor + route-params context the
  // GraphRenderer expects so leaf intents (form.edit submitting auth.login)
  // resolve correctly.
  return (
    <ContributorProvider value={loginContributor}>
      <RouteParamsProvider value={data.routeParams}>
        <GraphRenderer node={data.node} />
      </RouteParamsProvider>
    </ContributorProvider>
  );
}

// Re-export for tests + external callers that need to inspect the error type.
export { ContractClientError };
