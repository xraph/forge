import * as React from "react";
import { Separator as BaseSeparator } from "@base-ui-components/react/separator";
import { cn } from "@/lib/utils";

export const Separator = React.forwardRef<
  HTMLDivElement,
  React.ComponentPropsWithoutRef<typeof BaseSeparator>
>(({ className, orientation = "horizontal", ...props }, ref) => (
  <BaseSeparator
    ref={ref}
    orientation={orientation}
    className={cn(
      "shrink-0 bg-border",
      orientation === "horizontal" ? "h-[1px] w-full" : "h-full w-[1px]",
      className,
    )}
    {...props}
  />
));
Separator.displayName = "Separator";
