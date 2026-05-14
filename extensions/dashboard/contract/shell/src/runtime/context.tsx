import { createContext, useContext } from "react";
import type { ReactNode } from "react";
import type { IntentRegistry } from "./registry";

const RegistryContext = createContext<IntentRegistry | null>(null);

export function IntentRegistryProvider({
  value,
  children,
}: {
  value: IntentRegistry;
  children: ReactNode;
}) {
  return <RegistryContext.Provider value={value}>{children}</RegistryContext.Provider>;
}

export function useIntentRegistry(): IntentRegistry {
  const reg = useContext(RegistryContext);
  if (!reg) throw new Error("useIntentRegistry called outside IntentRegistryProvider");
  return reg;
}

// ContributorContext threads the contributor name through the renderer so leaf
// intents (action.button, form.edit, etc.) know which contributor to address
// when issuing commands or queries from a relative `op` reference.
const ContributorContext = createContext<string | null>(null);

export function ContributorProvider({
  value,
  children,
}: {
  value: string;
  children: ReactNode;
}) {
  return <ContributorContext.Provider value={value}>{children}</ContributorContext.Provider>;
}

/** useContributor returns the contributor name owning the current graph subtree. */
export function useContributor(): string {
  const c = useContext(ContributorContext);
  if (!c) throw new Error("useContributor called outside ContributorProvider");
  return c;
}

// ParentContext exposes the nearest enclosing data row's value to children, so
// that nodes referencing { from: parent.<field> } can resolve those values
// without a custom binding system. resource.list sets parent.* to the row
// object when rendering rowActions and detailDrawer slots.
const ParentContext = createContext<Record<string, unknown> | null>(null);

export function ParentProvider({
  value,
  children,
}: {
  value: Record<string, unknown>;
  children: ReactNode;
}) {
  return <ParentContext.Provider value={value}>{children}</ParentContext.Provider>;
}

/** useParent returns the nearest enclosing parent record, or null when not within one. */
export function useParent(): Record<string, unknown> | null {
  return useContext(ParentContext);
}

// RouteParamsContext exposes the :name route placeholders the server matched
// for the current page. Slice (j) added this so payload bindings of the form
// `{ from: route.id }` resolve when the user lands on /traces/abc123 directly.
// Empty for routes without :name segments.
const RouteParamsContext = createContext<Record<string, string>>({});

export function RouteParamsProvider({
  value,
  children,
}: {
  value: Record<string, string>;
  children: ReactNode;
}) {
  return <RouteParamsContext.Provider value={value}>{children}</RouteParamsContext.Provider>;
}

/** useRouteParams returns the active route's :name placeholder values. */
export function useRouteParams(): Record<string, string> {
  return useContext(RouteParamsContext);
}
