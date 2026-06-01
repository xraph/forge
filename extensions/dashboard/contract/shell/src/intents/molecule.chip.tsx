import { X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Icon } from "./atom.icon";
import { cn } from "@/lib/utils";
import { dispatchAction, type Action } from "../runtime/actions";
import type { ColorVariant } from "@/lib/tokens";
import type { IntentComponentProps } from "../runtime/registry";

interface ChipProps {
  label: string;
  icon?: string;
  variant?: ColorVariant;
  removable?: boolean;
  onRemove?: Action;
  onClick?: Action;
  disabled?: boolean;
  className?: string;
}

function resolveVariant(v?: ColorVariant) {
  if (!v) return "secondary" as const;
  return v;
}

export function Chip({ props }: IntentComponentProps<unknown, ChipProps>) {
  return (
    <Badge
      variant={resolveVariant(props.variant)}
      className={cn(
        "gap-1",
        props.disabled && "opacity-50",
        props.onClick && !props.disabled && "cursor-pointer",
        props.className,
      )}
      onClick={() => !props.disabled && props.onClick && dispatchAction(props.onClick, {})}
    >
      {props.icon ? (
        <Icon node={{} as never} slots={{}} props={{ name: props.icon, size: 12 }} />
      ) : null}
      <span>{props.label}</span>
      {props.removable ? (
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="-mr-1 h-4 w-4 rounded-full hover:bg-foreground/10"
          onClick={(e) => {
            e.stopPropagation();
            if (!props.disabled && props.onRemove) dispatchAction(props.onRemove, {});
          }}
          disabled={props.disabled}
          aria-label={`Remove ${props.label}`}
        >
          <X className="h-3 w-3" />
        </Button>
      ) : null}
    </Badge>
  );
}
