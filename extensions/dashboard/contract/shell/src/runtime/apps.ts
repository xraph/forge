import { create } from "zustand";
import { useContractQuery } from "../contract/hooks";

// Wire shape returned by the core-contract `apps.list` query. Mirrors
// extensions/dashboard/contract/pilot/apps.go. The shell uses this to
// populate the sidebar's app switcher and to filter the nav tree to the
// active app's items.
export interface AppInfo {
  contributor: string;
  displayName: string;
  // slug is the URL namespace this app's routes live under: a route
  // declared as `/users` is served at `/@<slug>/users` on the wire and
  // the navigation handler returns the prefixed href. Empty when the
  // app is `root: true` — root apps own the bare URL space.
  slug?: string;
  // root marks this as the platform app: its routes are NOT prefixed,
  // so /, /health, etc. stay bare. PageRoute falls back to the root
  // app's contributor when the URL has no /@<slug> prefix.
  root?: boolean;
  icon?: string;
  priority: number;
  // home is the route the switcher navigates to. Already prefixed for
  // non-root apps (`/@authsome/users`); unprefixed for root apps (`/`).
  home?: string;
}
export interface AppsListResponse {
  apps: AppInfo[];
}

const APPS_CONTRIBUTOR = "core-contract";

/** useApps fetches the switcher catalog. Cached per the pilot manifest's queries.appsList staleTime. */
export function useApps() {
  return useContractQuery<AppsListResponse>(APPS_CONTRIBUTOR, "apps.list");
}

// Selected-app state. Persisted to localStorage so refreshes don't reset
// the workspace to the default app. URL synchronization (`?app=auth`) is
// handled at the layout level so the store stays purely about state.
const STORAGE_KEY = "forge-dashboard:selected-app";

interface AppStore {
  /** Contributor name of the active app (e.g. "core-contract", "auth"). */
  selected: string | null;
  /** Replace the active app and persist to localStorage. Null clears. */
  setSelected: (contributor: string | null) => void;
}

function loadInitial(): string | null {
  if (typeof window === "undefined") return null;
  // URL beats localStorage on first load so deep-linking into a different
  // app (e.g. /dashboard/contract/app/users) wins over a stale selection.
  const params = new URLSearchParams(window.location.search);
  const fromURL = params.get("app");
  if (fromURL) return fromURL;
  try {
    return window.localStorage.getItem(STORAGE_KEY);
  } catch {
    // localStorage can throw in private-mode browsers; treat as no-state.
    return null;
  }
}

export const useAppStore = create<AppStore>((set) => ({
  selected: loadInitial(),
  setSelected: (contributor) => {
    set({ selected: contributor });
    if (typeof window === "undefined") return;
    try {
      if (contributor === null) {
        window.localStorage.removeItem(STORAGE_KEY);
      } else {
        window.localStorage.setItem(STORAGE_KEY, contributor);
      }
    } catch {
      // ignore; selection still lives in memory for the session
    }
  },
}));
