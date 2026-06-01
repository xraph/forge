import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface SpacerProps {
  size?: string;
  axis?: "horizontal" | "vertical" | "both";
  className?: string;
}

export function Spacer({ props }: IntentComponentProps<unknown, SpacerProps>) {
  const size = props.size ?? "4";
  const axis = props.axis ?? "vertical";
  const cls =
    axis === "horizontal"
      ? `w-${size}`
      : axis === "both"
        ? `h-${size} w-${size}`
        : `h-${size}`;
  return <div className={cn(cls, props.className)} aria-hidden />;
}
