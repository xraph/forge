import * as React from "react";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import { useFormStateOptional } from "./_form-state";
import { dispatchAction, type Action } from "../runtime/actions";
import type { IntentComponentProps } from "../runtime/registry";

interface SwitchAtomProps {
  name: string;
  label?: string;
  description?: string;
  checked?: boolean;
  default?: boolean;
  disabled?: boolean;
  onChange?: Action;
  className?: string;
}

export function AtomSwitch({ props }: IntentComponentProps<unknown, SwitchAtomProps>) {
  const form = useFormStateOptional();
  const initial =
    (form?.values[props.name] as boolean | undefined) ?? props.checked ?? props.default ?? false;
  const [local, setLocal] = React.useState(initial);

  const onCheckedChange = (next: boolean) => {
    setLocal(next);
    form?.setValue(props.name, next);
    if (props.onChange) dispatchAction(props.onChange, { value: next });
  };

  return (
    <div className={cn("flex items-center justify-between gap-4", props.className)}>
      {(props.label || props.description) && (
        <div className="grid gap-0.5 leading-none">
          {props.label ? (
            <label htmlFor={props.name} className="text-sm font-medium leading-none">
              {props.label}
            </label>
          ) : null}
          {props.description ? (
            <p className="text-sm text-muted-foreground">{props.description}</p>
          ) : null}
        </div>
      )}
      <Switch
        id={props.name}
        name={props.name}
        checked={local}
        disabled={props.disabled}
        onCheckedChange={onCheckedChange}
      />
    </div>
  );
}
