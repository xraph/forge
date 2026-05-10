import { LayoutDashboard } from "lucide-react";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Separator } from "@/components/ui/separator";
import { ThemeToggle } from "@/components/theme-toggle";
import { SlotRenderer } from "../runtime/slots";
import { usePrincipalStore } from "../auth/principal";
import type { IntentComponentProps } from "../runtime/registry";

function initialsOf(name: string): string {
  const parts = name.trim().split(/\s+/);
  if (parts.length === 0) return "";
  if (parts.length === 1) return parts[0]!.slice(0, 2).toUpperCase();
  return (parts[0]![0]! + parts[parts.length - 1]![0]!).toUpperCase();
}

export function PageShell({ node, slots }: IntentComponentProps) {
  const principal = usePrincipalStore((s) => s.principal);
  const title = node.title ?? "Dashboard";
  return (
    <div className="flex h-full flex-col bg-background text-foreground">
      <header className="flex h-14 items-center justify-between border-b border-border bg-card px-6">
        <div className="flex items-center gap-3">
          <LayoutDashboard className="h-5 w-5 text-muted-foreground" />
          <h1 className="text-base font-semibold">{title}</h1>
        </div>
        <div className="flex items-center gap-3">
          <ThemeToggle />
          <Separator orientation="vertical" className="h-6" />
          {principal ? (
            <div className="flex items-center gap-2">
              <Avatar className="h-8 w-8">
                <AvatarFallback>{initialsOf(principal.displayName)}</AvatarFallback>
              </Avatar>
              <span className="text-sm font-medium">{principal.displayName}</span>
            </div>
          ) : (
            <span className="text-sm text-muted-foreground">Loading…</span>
          )}
        </div>
      </header>
      <main className="flex-1 overflow-auto p-6">
        <SlotRenderer slot="main" slots={slots} />
      </main>
    </div>
  );
}
