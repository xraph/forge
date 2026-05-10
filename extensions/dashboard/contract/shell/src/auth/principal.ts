import { create } from "zustand";
import { contractBase } from "../runtime/config";
import type { Principal } from "../contract/types";

interface PrincipalState {
  principal: Principal | null;
  loaded: boolean;
  error: string | null;
  load: (fetcher?: typeof fetch) => Promise<void>;
}

export const usePrincipalStore = create<PrincipalState>((set) => ({
  principal: null,
  loaded: false,
  error: null,
  async load(fetcher = fetch) {
    try {
      const res = await fetcher(`${contractBase}/principal`, { credentials: "include" });
      if (!res.ok) {
        set({ loaded: true, error: `HTTP ${res.status}`, principal: null });
        return;
      }
      const principal = (await res.json()) as Principal;
      set({ principal, loaded: true, error: null });
    } catch (err) {
      set({ loaded: true, error: String(err), principal: null });
    }
  },
}));
