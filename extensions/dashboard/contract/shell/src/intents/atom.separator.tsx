import { Separator } from "@/components/ui/separator";
import type { IntentComponentProps } from "../runtime/registry";

interface SeparatorProps {
  orientation?: "horizontal" | "vertical";
  decorative?: boolean;
  className?: string;
}

export function AtomSeparator({ props }: IntentComponentProps<unknown, SeparatorProps>) {
  return (
    <Separator
      orientation={props.orientation ?? "horizontal"}
      aria-hidden={props.decorative}
      className={props.className}
    />
  );
}
