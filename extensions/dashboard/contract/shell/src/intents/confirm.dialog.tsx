import * as React from "react";
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
import { SlotRenderer } from "../runtime/slots";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface ConfirmDialogProps {
  title?: string;
  description?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  variant?: "default" | "destructive";
}

/**
 * confirm.dialog wraps a destructive action behind a confirmation modal.
 * The `trigger` slot is rendered as the visible button; clicking it opens
 * the dialog whose `body` slot renders any additional context (a text
 * note, a form.edit for capturing required input, etc.). The Confirm
 * button at the bottom dispatches the trigger's underlying op when
 * pressed — the trigger is responsible for declaring its op.
 *
 * For pure yes/no confirmations without input, the simpler approach is
 * action.button with `confirm: true`. Use confirm.dialog when the
 * confirmation needs richer body content (e.g. a reason text input).
 */
export function ConfirmDialog({ props, slots }: IntentComponentProps<unknown, ConfirmDialogProps>) {
  const [open, setOpen] = React.useState(false);

  return (
    <AlertDialog open={open} onOpenChange={setOpen}>
      <AlertDialogTrigger asChild>
        <span>
          <SlotRenderer slot="trigger" slots={slots} />
        </span>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          {props.title ? <AlertDialogTitle>{props.title}</AlertDialogTitle> : null}
          {props.description ? <AlertDialogDescription>{props.description}</AlertDialogDescription> : null}
        </AlertDialogHeader>
        <div className="space-y-3">
          <SlotRenderer slot="body" slots={slots} />
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel>{props.cancelLabel ?? "Cancel"}</AlertDialogCancel>
          <AlertDialogAction
            className={cn(
              props.variant === "destructive" &&
                "bg-destructive text-destructive-foreground hover:bg-destructive/90",
            )}
          >
            {props.confirmLabel ?? "Confirm"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
