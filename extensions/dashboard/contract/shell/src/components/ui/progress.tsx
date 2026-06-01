import * as React from "react";
import { Progress as BaseProgress } from "@base-ui-components/react/progress";
import { cn } from "@/lib/utils";

export const Progress = React.forwardRef<
  HTMLDivElement,
  React.ComponentPropsWithoutRef<typeof BaseProgress.Root>
>(({ className, ...props }, ref) => (
  <BaseProgress.Root ref={ref} className={cn("relative w-full", className)} {...props}>
    <BaseProgress.Track className="relative h-2 w-full overflow-hidden rounded-full bg-secondary">
      <BaseProgress.Indicator className="h-full bg-primary transition-transform duration-300 ease-out data-[indeterminate]:animate-pulse" />
    </BaseProgress.Track>
  </BaseProgress.Root>
));
Progress.displayName = "Progress";

export const ProgressWithLabel = React.forwardRef<
  HTMLDivElement,
  React.ComponentPropsWithoutRef<typeof BaseProgress.Root> & {
    label?: string;
    showValue?: boolean;
  }
>(({ className, label, showValue, value, ...props }, ref) => (
  <BaseProgress.Root
    ref={ref}
    value={value}
    className={cn("relative w-full", className)}
    {...props}
  >
    {(label || showValue) && (
      <div className="mb-1.5 flex items-center justify-between text-sm">
        {label ? <BaseProgress.Label>{label}</BaseProgress.Label> : <span />}
        {showValue ? (
          <BaseProgress.Value className="text-muted-foreground tabular-nums" />
        ) : null}
      </div>
    )}
    <BaseProgress.Track className="relative h-2 w-full overflow-hidden rounded-full bg-secondary">
      <BaseProgress.Indicator className="h-full bg-primary transition-transform duration-300 ease-out data-[indeterminate]:animate-pulse" />
    </BaseProgress.Track>
  </BaseProgress.Root>
));
ProgressWithLabel.displayName = "ProgressWithLabel";
