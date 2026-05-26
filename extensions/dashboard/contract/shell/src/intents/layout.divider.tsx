import { Separator } from "@/components/ui/separator";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface DividerProps {
  orientation?: "horizontal" | "vertical";
  decorative?: boolean;
  label?: string;
  className?: string;
}

export function Divider({ props }: IntentComponentProps<unknown, DividerProps>) {
  if (props.label) {
    return (
      <div className={cn("relative flex items-center", props.className)}>
        <Separator className="flex-1" />
        <span className="px-3 text-xs uppercase tracking-wide text-muted-foreground">
          {props.label}
        </span>
        <Separator className="flex-1" />
      </div>
    );
  }
  return (
    <Separator
      orientation={props.orientation ?? "horizontal"}
      aria-hidden={props.decorative}
      className={props.className}
    />
  );
}
