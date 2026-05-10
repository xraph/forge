import * as React from "react";
import { Button } from "@/components/ui/button";
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
import { useContractCommand } from "../contract/hooks";
import { useContributor, useParent } from "../runtime/context";
import { usePrincipalStore } from "../auth/principal";
import { resolvePayload } from "../runtime/bindings";
import type { IntentComponentProps } from "../runtime/registry";

interface ActionButtonProps {
  label?: string;
  variant?: "default" | "destructive" | "outline" | "secondary" | "ghost";
  size?: "default" | "sm" | "lg";
  /** When set, opens a confirmation dialog before issuing the command. */
  confirm?: string;
  /** Override the contributor for the op. Defaults to the current ContributorContext. */
  contributor?: string;
}

export function ActionButton({
  node,
  props,
}: IntentComponentProps<unknown, ActionButtonProps>) {
  const fallbackContributor = useContributor();
  const parent = useParent();
  const principal = usePrincipalStore((s) => s.principal);

  const op = node.op;
  const label = props.label ?? node.title ?? op ?? "Action";
  const targetContributor = props.contributor ?? fallbackContributor;

  const command = useContractCommand(targetContributor ?? "core-contract", op ?? "");
  const [open, setOpen] = React.useState(false);

  const run = () => {
    const resolved = resolvePayload(node.payload, {
      parent,
      session: { user: principal },
    });
    command.mutate(resolved);
  };

  const buttonEl = (
    <Button
      variant={props.variant ?? "default"}
      size={props.size ?? "sm"}
      disabled={command.isPending || !op}
      onClick={(e) => {
        e.stopPropagation();
        if (props.confirm) {
          setOpen(true);
        } else {
          run();
        }
      }}
    >
      {label}
    </Button>
  );

  if (!props.confirm) return buttonEl;

  return (
    <AlertDialog open={open} onOpenChange={setOpen}>
      <AlertDialogTrigger asChild>{buttonEl}</AlertDialogTrigger>
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
