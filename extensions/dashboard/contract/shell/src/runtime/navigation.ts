import { useContractQuery } from "../contract/hooks";

// Wire shape returned by the core-contract `navigation` query (slice l).
// Mirrors extensions/dashboard/contract/pilot/navigation.go.
export interface NavItem {
  label: string;
  href: string;
  icon?: string;
  badge?: string;
  priority: number;
  // contributor is the contract contributor that owns this route (e.g.
  // "core-contract", "auth"). The shell groups items by contributor when
  // an app switcher is in play, so the active app's nav can be shown in
  // isolation from other contributors' items.
  contributor: string;
}
export interface NavGroup {
  group: string;
  priority: number;
  items: NavItem[];
}
export interface NavigationResponse {
  groups: NavGroup[];
}

const NAV_CONTRIBUTOR = "core-contract";

/**
 * useNavigation fetches the pre-grouped sidebar tree for the dashboard. The
 * 60s staleTime declared in the manifest keeps the UI responsive — the nav
 * tree is effectively static at runtime.
 */
export function useNavigation() {
  return useContractQuery<NavigationResponse>(NAV_CONTRIBUTOR, "navigation");
}
