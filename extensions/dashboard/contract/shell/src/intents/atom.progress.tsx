import { ProgressWithLabel } from "@/components/ui/progress";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface ProgressProps {
  value?: number;
  max?: number;
  indeterminate?: boolean;
  label?: string;
  showValue?: boolean;
  className?: string;
}

export function AtomProgress({ props }: IntentComponentProps<unknown, ProgressProps>) {
  const indeterminate = props.indeterminate || props.value === undefined;
  return (
    <ProgressWithLabel
      // Base UI Progress: pass null for indeterminate state.
      value={indeterminate ? null : (props.value ?? 0)}
      max={props.max ?? 100}
      label={props.label}
      showValue={props.showValue}
      className={cn(props.className)}
    />
  );
}
