import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const labelVariants = cva(
  "text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70",
);

/**
 * Label is a thin styled wrapper over the native <label> element. Base UI
 * doesn't ship a Label primitive (htmlFor association is enough), so this
 * matches shadcn/Base UI's recommended pattern of using the platform element
 * directly.
 */
export const Label = React.forwardRef<
  HTMLLabelElement,
  React.LabelHTMLAttributes<HTMLLabelElement> & VariantProps<typeof labelVariants>
>(({ className, ...props }, ref) => (
  // eslint-disable-next-line jsx-a11y/label-has-associated-control
  <label ref={ref} className={cn(labelVariants(), className)} {...props} />
));
Label.displayName = "Label";
