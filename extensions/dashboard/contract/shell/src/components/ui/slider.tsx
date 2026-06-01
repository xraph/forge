import * as React from "react";
import { Slider as BaseSlider } from "@base-ui-components/react/slider";
import { cn } from "@/lib/utils";

export const Slider = React.forwardRef<
  HTMLDivElement,
  React.ComponentPropsWithoutRef<typeof BaseSlider.Root>
>(({ className, ...props }, ref) => (
  <BaseSlider.Root
    ref={ref}
    className={cn("relative flex w-full touch-none select-none items-center", className)}
    {...props}
  >
    <BaseSlider.Control className="relative h-2 w-full grow overflow-hidden rounded-full bg-secondary">
      <BaseSlider.Track className="absolute inset-0">
        <BaseSlider.Indicator className="absolute h-full bg-primary" />
      </BaseSlider.Track>
    </BaseSlider.Control>
    <BaseSlider.Thumb className="block h-5 w-5 rounded-full border-2 border-primary bg-background ring-offset-background transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50" />
  </BaseSlider.Root>
));
Slider.displayName = "Slider";
