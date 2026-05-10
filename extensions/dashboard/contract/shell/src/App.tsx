import { useEffect } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, Route, Routes, useParams } from "react-router-dom";
import { ContributorProvider, IntentRegistryProvider } from "./runtime/context";
import { buildIntentRegistry } from "./intents/register";
import { GraphRenderer } from "./runtime/renderer";
import { useContractGraph } from "./contract/hooks";
import { LoadingNode, ErrorNode } from "./runtime/fallbacks";
import { usePrincipalStore } from "./auth/principal";
import { useThemeStore } from "@/lib/theme";

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

  if (isLoading) return <LoadingNode />;
  if (error) return <ErrorNode message={(error as Error).message} />;
  if (!data) return <ErrorNode message="empty graph" />;
  return (
    <ContributorProvider value={DEFAULT_CONTRIBUTOR}>
      <GraphRenderer node={data} />
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
        <BrowserRouter basename="/dashboard/contract/app">
          <Routes>
            <Route path="*" element={<PageRoute />} />
          </Routes>
        </BrowserRouter>
      </IntentRegistryProvider>
    </QueryClientProvider>
  );
}
