import * as React from "react";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { useFormStateOptional } from "./_form-state";
import { dispatchAction, type Action } from "../runtime/actions";
import type { IntentComponentProps } from "../runtime/registry";

interface TextareaAtomProps {
  name: string;
  placeholder?: string;
  value?: string;
  default?: string;
  rows?: number;
  maxRows?: number;
  autosize?: boolean;
  disabled?: boolean;
  readOnly?: boolean;
  required?: boolean;
  minLength?: number;
  maxLength?: number;
  onChange?: Action;
  error?: string;
  className?: string;
}

export function AtomTextarea({ props }: IntentComponentProps<unknown, TextareaAtomProps>) {
  const form = useFormStateOptional();
  const initial =
    (form?.values[props.name] as string | undefined) ?? props.value ?? props.default ?? "";
  const [local, setLocal] = React.useState(initial);

  const onChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setLocal(e.target.value);
    form?.setValue(props.name, e.target.value);
    if (props.onChange) dispatchAction(props.onChange, { value: e.target.value });
  };

  return (
    <div className="space-y-1">
      <Textarea
        name={props.name}
        placeholder={props.placeholder}
        value={local}
        rows={props.rows}
        disabled={props.disabled}
        readOnly={props.readOnly}
        required={props.required}
        minLength={props.minLength}
        maxLength={props.maxLength}
        onChange={onChange}
        className={cn(props.error && "border-destructive", props.className)}
        aria-invalid={!!props.error}
      />
      {props.error ? <p className="text-xs text-destructive">{props.error}</p> : null}
    </div>
  );
}
