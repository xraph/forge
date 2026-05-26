import * as React from "react";
import { cn } from "@/lib/utils";
import type { Size } from "@/lib/tokens";
import type { IntentComponentProps } from "../runtime/registry";

interface KbdProps {
  keys: string[];
  size?: Size;
  className?: string;
}

const sizeClass: Record<string, string> = {
  xs: "h-4 px-1 text-[10px]",
  sm: "h-5 px-1.5 text-xs",
  default: "h-6 px-1.5 text-xs",
  lg: "h-7 px-2 text-sm",
  xl: "h-8 px-2.5 text-sm",
  icon: "h-6 px-1.5 text-xs",
};

export function AtomKbd({ props }: IntentComponentProps<unknown, KbdProps>) {
  const cls = sizeClass[props.size ?? "default"];
  return (
    <span className={cn("inline-flex items-center gap-1", props.className)}>
      {props.keys.map((key, i) => (
        <React.Fragment key={`${key}-${i}`}>
          <kbd
            className={cn(
              "inline-flex items-center justify-center rounded border border-border bg-muted font-mono font-medium text-muted-foreground shadow-sm",
              cls,
            )}
          >
            {key}
          </kbd>
          {i < props.keys.length - 1 ? <span className="text-xs text-muted-foreground">+</span> : null}
        </React.Fragment>
      ))}
    </span>
  );
}
