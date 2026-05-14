import { useEffect } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, Route, Routes, useParams } from "react-router-dom";
import {
  ContributorProvider,
  IntentRegistryProvider,
  RouteParamsProvider,
} from "./runtime/context";
import { buildIntentRegistry } from "./intents/register";
import { GraphRenderer } from "./runtime/renderer";
import { useContractGraph } from "./contract/hooks";
import { LoadingNode, ErrorNode } from "./runtime/fallbacks";
import { usePrincipalStore } from "./auth/principal";
import { AuthGate } from "./auth/AuthGate";
import { DashboardLayout } from "./runtime/layout";
import { useThemeStore } from "@/lib/theme";
import { shellBase } from "./runtime/config";

const DEFAULT_CONTRIBUTOR = "core-contract";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5_000,
      refetchOnWindowFocus: false,
      retry: false,
    },
  },
});

const registry = buildIntentRegistry();

function PageRoute() {
  const params = useParams();
  const route = `/${params["*"] ?? ""}`;
  const { data, isLoading, error } = useContractGraph(DEFAULT_CONTRIBUTOR, route);

  if (isLoading) {
    return (
      <DashboardLayout>
        <LoadingNode />
      </DashboardLayout>
    );
  }
  if (error) {
    return (
      <DashboardLayout>
        <ErrorNode message={(error as Error).message} />
      </DashboardLayout>
    );
  }
  if (!data) {
    return (
      <DashboardLayout>
        <ErrorNode message="empty graph" />
      </DashboardLayout>
    );
  }
  return (
    <ContributorProvider value={DEFAULT_CONTRIBUTOR}>
      <RouteParamsProvider value={data.routeParams}>
        <DashboardLayout title={data.node.title}>
          <GraphRenderer node={data.node} />
        </DashboardLayout>
      </RouteParamsProvider>
    </ContributorProvider>
  );
}

export function App() {
  const loadPrincipal = usePrincipalStore((s) => s.load);
  const initTheme = useThemeStore((s) => s.init);
  useEffect(() => {
    initTheme();
    void loadPrincipal();
  }, [loadPrincipal, initTheme]);

  return (
    <QueryClientProvider client={queryClient}>
      <IntentRegistryProvider value={registry}>
        <BrowserRouter basename={shellBase}>
          <AuthGate>
            <Routes>
              <Route path="*" element={<PageRoute />} />
            </Routes>
          </AuthGate>
        </BrowserRouter>
      </IntentRegistryProvider>
    </QueryClientProvider>
  );
}
