import { Badge as BadgePrimitive } from "@/components/ui/badge";
import { Icon } from "./atom.icon";
import { cn } from "@/lib/utils";
import type { ColorVariant } from "@/lib/tokens";
import type { IntentComponentProps } from "../runtime/registry";

interface BadgeProps {
  label: string;
  variant?: ColorVariant;
  icon?: string;
  dot?: boolean;
  className?: string;
}

function resolveVariant(v?: ColorVariant): "default" | "secondary" | "destructive" | "outline" | "muted" | "accent" | "ghost" | "link" {
  if (!v) return "default";
  return v as "default" | "secondary" | "destructive" | "outline" | "muted" | "accent" | "ghost" | "link";
}

export function AtomBadge({ props }: IntentComponentProps<unknown, BadgeProps>) {
  return (
    <BadgePrimitive variant={resolveVariant(props.variant)} className={cn(props.className)}>
      {props.dot ? <span className="h-1.5 w-1.5 rounded-full bg-current" /> : null}
      {props.icon ? (
        <Icon node={{} as never} slots={{}} props={{ name: props.icon, size: 12 }} />
      ) : null}
      {props.label}
    </BadgePrimitive>
  );
}
