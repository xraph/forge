import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const buttonVariants = cva(
  "inline-flex items-center justify-center whitespace-nowrap rounded-md text-sm font-medium ring-offset-background transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground hover:bg-primary/90",
        destructive: "bg-destructive text-destructive-foreground hover:bg-destructive/90",
        outline: "border border-input bg-background hover:bg-accent hover:text-accent-foreground",
        secondary: "bg-secondary text-secondary-foreground hover:bg-secondary/80",
        ghost: "hover:bg-accent hover:text-accent-foreground",
        link: "text-primary underline-offset-4 hover:underline",
      },
      size: {
        default: "h-10 px-4 py-2",
        sm: "h-9 rounded-md px-3",
        lg: "h-11 rounded-md px-8",
        icon: "h-10 w-10",
      },
    },
    defaultVariants: { variant: "default", size: "default" },
  },
);

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

/**
 * Slot clones the single child element and merges className + props onto it.
 * Replaces @radix-ui/react-slot for the asChild pattern. Base UI doesn't ship
 * a Slot equivalent; most Base UI primitives expose a `render` prop instead,
 * but Button needs the simpler asChild ergonomic for action.button et al.
 */
function Slot({
  children,
  className,
  ...rest
}: { children: React.ReactNode; className?: string } & Record<string, unknown>) {
  if (!React.isValidElement(children)) return null;
  const child = children as React.ReactElement<Record<string, unknown>>;
  const childClassName =
    typeof child.props.className === "string" ? child.props.className : undefined;
  return React.cloneElement(child, {
    ...rest,
    ...child.props,
    className: cn(className, childClassName),
  });
}

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, children, ...props }, ref) => {
    const cls = cn(buttonVariants({ variant, size, className }));
    if (asChild) {
      return (
        <Slot className={cls} {...(props as Record<string, unknown>)}>
          {children}
        </Slot>
      );
    }
    return (
      <button ref={ref} className={cls} {...props}>
        {children}
      </button>
    );
  },
);
Button.displayName = "Button";

export { buttonVariants };
