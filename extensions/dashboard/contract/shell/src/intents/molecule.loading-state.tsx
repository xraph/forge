import { Loader2 } from "lucide-react";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface LoadingStateProps {
  label?: string;
  className?: string;
}

export function LoadingState({ props }: IntentComponentProps<unknown, LoadingStateProps>) {
  return (
    <div className={cn("flex items-center justify-center gap-2 p-8 text-muted-foreground", props.className)}>
      <Loader2 className="h-4 w-4 animate-spin" />
      {props.label ? <span className="text-sm">{props.label}</span> : null}
    </div>
  );
}
