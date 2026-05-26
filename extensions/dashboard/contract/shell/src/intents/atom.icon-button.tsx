import { Button as ButtonPrimitive } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { Loader2 } from "lucide-react";
import { Icon } from "./atom.icon";
import { dispatchAction, type Action } from "../runtime/actions";
import { useContractCommand } from "../contract/hooks";
import { useContributor, useParent, useRouteParams } from "../runtime/context";
import { usePrincipalStore } from "../auth/principal";
import { resolvePayload } from "../runtime/bindings";
import type { ColorVariant, Size } from "@/lib/tokens";
import type { IntentComponentProps } from "../runtime/registry";

interface IconButtonProps {
  icon: string;
  variant?: ColorVariant;
  size?: Size;
  tooltip?: string;
  ariaLabel?: string;
  loading?: boolean;
  disabled?: boolean;
  onClick?: Action;
  confirm?: string;
  className?: string;
}

function resolveVariant(v?: ColorVariant): "default" | "destructive" | "outline" | "secondary" | "ghost" | "link" {
  if (!v) return "ghost";
  if (v === "muted" || v === "accent") return "secondary";
  return v as "default" | "destructive" | "outline" | "secondary" | "ghost" | "link";
}

export function IconButton({
  node,
  props,
}: IntentComponentProps<unknown, IconButtonProps>) {
  const fallbackContributor = useContributor();
  const parent = useParent();
  const route = useRouteParams();
  const principal = usePrincipalStore((s) => s.principal);
  const op = node.op;
  const command = useContractCommand(fallbackContributor ?? "core-contract", op ?? "");
  const isBusy = props.loading || command.isPending;

  const run = () => {
    if (op) {
      const payload = resolvePayload(node.payload, {
        parent,
        session: { user: principal },
        route,
      });
      command.mutate(payload);
    } else if (props.onClick) {
      dispatchAction(props.onClick, { parent, session: { user: principal }, route });
    }
  };

  const button = (
    <ButtonPrimitive
      variant={resolveVariant(props.variant)}
      size="icon"
      disabled={isBusy || props.disabled || (!op && !props.onClick)}
      aria-label={props.ariaLabel ?? props.tooltip ?? "action"}
      className={props.className}
      onClick={(e) => {
        e.stopPropagation();
        run();
      }}
    >
      {isBusy ? (
        <Loader2 className="h-4 w-4 animate-spin" />
      ) : (
        <Icon node={{} as never} slots={{}} props={{ name: props.icon, size: 16 }} />
      )}
    </ButtonPrimitive>
  );

  if (!props.tooltip) return button;
  return (
    <Tooltip>
      <TooltipTrigger asChild>{button}</TooltipTrigger>
      <TooltipContent>{props.tooltip}</TooltipContent>
    </Tooltip>
  );
}
