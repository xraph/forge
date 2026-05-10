import { SlotRenderer } from "../runtime/slots";
import { usePrincipalStore } from "../auth/principal";
import type { IntentComponentProps } from "../runtime/registry";

export function PageShell({ node, slots }: IntentComponentProps) {
  const principal = usePrincipalStore((s) => s.principal);
  const title = node.title ?? "Dashboard";
  return (
    <div className="flex h-full flex-col">
      <header className="flex items-center justify-between border-b border-gray-200 bg-white px-6 py-3">
        <h1 className="text-lg font-semibold">{title}</h1>
        <div className="text-sm text-gray-600">
          {principal ? <span>{principal.displayName}</span> : <span>Loading…</span>}
        </div>
      </header>
      <main className="flex-1 overflow-auto p-6">
        <SlotRenderer slot="main" slots={slots} />
      </main>
    </div>
  );
}
