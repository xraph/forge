import * as React from "react";
import { Button as ButtonPrimitive } from "@/components/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Loader2 } from "lucide-react";
import { Icon } from "./atom.icon";
import { dispatchAction, type Action } from "../runtime/actions";
import { useContractCommand } from "../contract/hooks";
import { useContributor, useParent, useRouteParams } from "../runtime/context";
import { usePrincipalStore } from "../auth/principal";
import { resolvePayload } from "../runtime/bindings";
import type { ColorVariant, Size } from "@/lib/tokens";
import type { IntentComponentProps } from "../runtime/registry";

interface AtomButtonProps {
  label?: string;
  variant?: ColorVariant;
  size?: Size;
  leadingIcon?: string;
  trailingIcon?: string;
  loading?: boolean;
  disabled?: boolean;
  fullWidth?: boolean;
  onClick?: Action;
  confirm?: string;
  className?: string;
}

// Pass-through variants that map 1:1 to the shadcn Button primitive.
type ShadcnVariant = "default" | "destructive" | "outline" | "secondary" | "ghost" | "link";
type ShadcnSize = "default" | "sm" | "lg" | "icon";

function resolveVariant(v?: ColorVariant): ShadcnVariant {
  if (!v) return "default";
  if (v === "muted" || v === "accent") return "secondary";
  return v as ShadcnVariant;
}

function resolveSize(s?: Size): ShadcnSize {
  if (s === "sm" || s === "lg" || s === "icon") return s;
  return "default";
}

export function AtomButton({
  node,
  props,
}: IntentComponentProps<unknown, AtomButtonProps>) {
  const fallbackContributor = useContributor();
  const parent = useParent();
  const route = useRouteParams();
  const principal = usePrincipalStore((s) => s.principal);
  const op = node.op;
  const command = useContractCommand(fallbackContributor ?? "core-contract", op ?? "");
  const [open, setOpen] = React.useState(false);

  const label = props.label ?? node.title ?? "Action";
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
      dispatchAction(props.onClick, {
        parent,
        session: { user: principal },
        route,
      });
    }
  };

  const inner = (
    <ButtonPrimitive
      variant={resolveVariant(props.variant)}
      size={resolveSize(props.size)}
      disabled={isBusy || props.disabled || (!op && !props.onClick)}
      className={props.fullWidth ? `w-full ${props.className ?? ""}` : props.className}
      onClick={(e) => {
        e.stopPropagation();
        if (props.confirm) setOpen(true);
        else run();
      }}
    >
      {isBusy ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
      {!isBusy && props.leadingIcon ? (
        <Icon node={{} as never} slots={{}} props={{ name: props.leadingIcon, size: 16 }} />
      ) : null}
      {label}
      {props.trailingIcon ? (
        <Icon node={{} as never} slots={{}} props={{ name: props.trailingIcon, size: 16 }} />
      ) : null}
    </ButtonPrimitive>
  );

  if (!props.confirm) return inner;

  return (
    <AlertDialog open={open} onOpenChange={setOpen}>
      <AlertDialogTrigger asChild>{inner}</AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Confirm</AlertDialogTitle>
          <AlertDialogDescription>{props.confirm}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction onClick={run}>{label}</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
