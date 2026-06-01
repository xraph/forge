import * as React from "react";
import { Checkbox } from "@/components/ui/checkbox";
import { cn } from "@/lib/utils";
import { useFormStateOptional } from "./_form-state";
import { dispatchAction, type Action } from "../runtime/actions";
import type { IntentComponentProps } from "../runtime/registry";

interface CheckboxAtomProps {
  name: string;
  label?: string;
  description?: string;
  checked?: boolean;
  default?: boolean;
  indeterminate?: boolean;
  disabled?: boolean;
  required?: boolean;
  onChange?: Action;
  error?: string;
  className?: string;
}

export function AtomCheckbox({ props }: IntentComponentProps<unknown, CheckboxAtomProps>) {
  const form = useFormStateOptional();
  const initial = (form?.values[props.name] as boolean | undefined) ?? props.checked ?? props.default ?? false;
  const [local, setLocal] = React.useState(initial);

  const onCheckedChange = (next: boolean) => {
    setLocal(next);
    form?.setValue(props.name, next);
    if (props.onChange) dispatchAction(props.onChange, { value: next });
  };

  return (
    <div className={cn("flex items-start gap-2", props.className)}>
      <Checkbox
        id={props.name}
        name={props.name}
        checked={local}
        disabled={props.disabled}
        required={props.required}
        onCheckedChange={onCheckedChange}
        indeterminate={props.indeterminate}
      />
      {(props.label || props.description) && (
        <div className="grid gap-0.5 leading-none">
          {props.label ? (
            <label
              htmlFor={props.name}
              className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
            >
              {props.label}
              {props.required ? <span className="ml-0.5 text-destructive">*</span> : null}
            </label>
          ) : null}
          {props.description ? (
            <p className="text-sm text-muted-foreground">{props.description}</p>
          ) : null}
          {props.error ? <p className="text-xs text-destructive">{props.error}</p> : null}
        </div>
      )}
    </div>
  );
}
