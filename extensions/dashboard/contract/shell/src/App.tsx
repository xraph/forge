import * as React from "react";
import { useEffect } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, Route, Routes, useLocation, useParams } from "react-router-dom";
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
import { useApps } from "./runtime/apps";
import { useThemeStore } from "@/lib/theme";
import { shellBase } from "./runtime/config";

const DEFAULT_CONTRIBUTOR = "core-contract";

// parseAppPath splits a URL pathname into the app slug and the
// contributor-local route. URLs are namespaced as /@<slug><route>; the
// slug is read from the first segment when it starts with "@". A path
// without a slug prefix means the root app — PageRoute dispatches it
// to whichever contributor has app.root: true (the platform / pilot).
//
//   parsePath("/@authsome/users")   → { slug: "authsome", route: "/users" }
//   parsePath("/health")            → { slug: null,       route: "/health" }
//   parsePath("/")                  → { slug: null,       route: "/" }
function parseAppPath(pathname: string): { slug: string | null; route: string } {
  const trimmed = pathname.startsWith("/") ? pathname.slice(1) : pathname;
  if (!trimmed.startsWith("@")) {
    return { slug: null, route: pathname || "/" };
  }
  const slashIdx = trimmed.indexOf("/");
  if (slashIdx === -1) {
    return { slug: trimmed.slice(1), route: "/" };
  }
  return { slug: trimmed.slice(1, slashIdx), route: trimmed.slice(slashIdx) };
}

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
  const location = useLocation();
  const params = useParams();
  // useParams() captures everything after the BrowserRouter basename. We
  // pull the slug + route via location.pathname to keep the slug-parsing
  // logic in one helper, but the splat in `params["*"]` is the same value.
  const { slug, route } = parseAppPath(location.pathname || `/${params["*"] ?? ""}`);
  const apps = useApps();

  // Resolve which contributor owns this URL.
  //  - slug present → look up the app with that slug
  //  - slug absent → route to the root app (the one with app.root: true);
  //    falls back to DEFAULT_CONTRIBUTOR when apps.list hasn't loaded yet
  //    or no app declares root: true (shouldn't happen in a sane deploy
  //    but keeps the shell usable rather than 404ing the bare URL).
  const contributor = React.useMemo(() => {
    const list = apps.data?.apps ?? [];
    if (slug) {
      const hit = list.find((a) => a.slug === slug);
      return hit?.contributor ?? DEFAULT_CONTRIBUTOR;
    }
    const rootApp = list.find((a) => a.root);
    return rootApp?.contributor ?? DEFAULT_CONTRIBUTOR;
  }, [apps.data?.apps, slug]);

  const { data, isLoading, error } = useContractGraph(contributor, route);

  // While the apps list is loading we don't know which contributor owns
  // a prefixed URL — surface a spinner so we don't fire a misrouted
  // graph query whose 404 would then snap to the default contributor.
  if (slug && apps.isLoading) {
    return (
      <DashboardLayout>
        <LoadingNode />
      </DashboardLayout>
    );
  }
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
    <ContributorProvider value={contributor}>
      <RouteParamsProvider value={data.routeParams}>
        <DashboardLayout title={data.node.title}>
          <GraphRenderer node={data.node} />
        </DashboardLayout>
      </RouteParamsProvider>
    </ContributorProvider>
  );
}


// AppErrorBoundary catches render-time exceptions so a bad component
// surfaces as a visible error instead of a blank page. Without it, a
// throw deep in the tree (e.g. a Base UI primitive used outside its
// required context) unmounts everything and leaves the user looking at
// nothing — historically very hard to diagnose. The fallback shows the
// error message + a recover button that re-mounts the tree.
class AppErrorBoundary extends React.Component<
  { children: React.ReactNode },
  { error: Error | null }
> {
  constructor(props: { children: React.ReactNode }) {
    super(props);
    this.state = { error: null };
  }
  static getDerivedStateFromError(error: Error) {
    return { error };
  }
  componentDidCatch(error: Error, info: React.ErrorInfo) {
    // Log to the console so the actual stack trace is preserved for
    // browser DevTools — the visible fallback only shows the message.
    // eslint-disable-next-line no-console
    console.error("dashboard shell render error:", error, info);
  }
  render() {
    if (this.state.error) {
      return (
        <div className="flex min-h-svh items-center justify-center bg-background p-6">
          <div className="flex w-full max-w-md flex-col gap-4 rounded-lg border border-destructive/40 bg-card p-6 text-sm shadow-sm">
            <div className="font-semibold text-destructive">Dashboard render error</div>
            <pre className="overflow-auto whitespace-pre-wrap break-words rounded bg-muted p-3 text-xs">
              {this.state.error.message}
            </pre>
            <button
              type="button"
              className="self-end rounded-md border border-border bg-background px-3 py-1.5 text-xs font-medium hover:bg-muted"
              onClick={() => this.setState({ error: null })}
            >
              Try again
            </button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
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
          <AppErrorBoundary>
            <AuthGate>
              <Routes>
                <Route path="*" element={<PageRoute />} />
              </Routes>
            </AuthGate>
          </AppErrorBoundary>
        </BrowserRouter>
      </IntentRegistryProvider>
    </QueryClientProvider>
  );
}
