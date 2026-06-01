import * as React from "react";
import { icons } from "lucide-react";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface IconProps {
  name: string;
  size?: number;
  strokeWidth?: number;
  className?: string;
}

/**
 * atom.icon resolves a lucide icon by name. Unknown names render a
 * placeholder square so missing icons are obvious in dev without
 * crashing the renderer.
 */
export function Icon({ props }: IntentComponentProps<unknown, IconProps>) {
  const Resolved = resolve(props.name);
  if (!Resolved) {
    return (
      <span
        className={cn("inline-block rounded border border-dashed border-muted-foreground/40", props.className)}
        style={{
          width: props.size ?? 16,
          height: props.size ?? 16,
        }}
        aria-label={`unknown icon ${props.name}`}
      />
    );
  }
  return (
    <Resolved
      size={props.size ?? 16}
      strokeWidth={props.strokeWidth ?? 2}
      className={props.className}
    />
  );
}

function resolve(name: string): React.ComponentType<{ size?: number; strokeWidth?: number; className?: string }> | null {
  if (!name) return null;
  // Normalize "user-plus" → "UserPlus", "user" → "User".
  const pascal = name
    .split(/[-_\s]+/)
    .filter(Boolean)
    .map((p) => p.charAt(0).toUpperCase() + p.slice(1))
    .join("");
  const Comp = (icons as Record<string, unknown>)[pascal];
  return (Comp as React.ComponentType<{ size?: number; strokeWidth?: number; className?: string }>) ?? null;
}
