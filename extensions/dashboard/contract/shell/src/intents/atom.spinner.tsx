import { Loader2 } from "lucide-react";
import { cn } from "@/lib/utils";
import { textVariantClass, type TextVariant } from "@/lib/tokens";
import type { Size } from "@/lib/tokens";
import type { IntentComponentProps } from "../runtime/registry";

interface SpinnerProps {
  size?: Size;
  label?: string;
  variant?: TextVariant;
  className?: string;
}

const sizePx: Record<string, number> = {
  xs: 12,
  sm: 14,
  default: 16,
  lg: 20,
  xl: 24,
  icon: 16,
};

export function AtomSpinner({ props }: IntentComponentProps<unknown, SpinnerProps>) {
  return (
    <span className={cn("inline-flex items-center gap-2", textVariantClass(props.variant), props.className)}>
      <Loader2 size={sizePx[props.size ?? "default"]} className="animate-spin" />
      {props.label ? <span className="text-sm">{props.label}</span> : null}
    </span>
  );
}
