import * as React from "react";
import { X } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Icon } from "./atom.icon";
import { dispatchAction, type Action } from "../runtime/actions";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface AlertMoleculeProps {
  variant?: "default" | "destructive" | "muted" | "accent";
  title?: string;
  description?: string;
  icon?: string;
  dismissible?: boolean;
  onDismiss?: Action;
  className?: string;
}

export function MoleculeAlert({ props }: IntentComponentProps<unknown, AlertMoleculeProps>) {
  const [open, setOpen] = React.useState(true);
  if (!open) return null;

  const dismiss = () => {
    setOpen(false);
    if (props.onDismiss) dispatchAction(props.onDismiss, {});
  };

  return (
    <Alert variant={props.variant ?? "default"} className={cn("relative", props.className)}>
      {props.icon ? (
        <Icon node={{} as never} slots={{}} props={{ name: props.icon, size: 16 }} />
      ) : null}
      {props.title ? <AlertTitle>{props.title}</AlertTitle> : null}
      {props.description ? <AlertDescription>{props.description}</AlertDescription> : null}
      {props.dismissible ? (
        <Button
          variant="ghost"
          size="icon"
          onClick={dismiss}
          className="absolute right-2 top-2 h-6 w-6"
          aria-label="Dismiss"
        >
          <X className="h-3.5 w-3.5" />
        </Button>
      ) : null}
    </Alert>
  );
}
