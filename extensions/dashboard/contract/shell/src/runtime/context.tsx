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
