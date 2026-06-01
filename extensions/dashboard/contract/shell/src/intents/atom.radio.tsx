import * as React from "react";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { cn } from "@/lib/utils";
import { useFormStateOptional } from "./_form-state";
import { dispatchAction, type Action } from "../runtime/actions";
import type { IntentComponentProps } from "../runtime/registry";

interface RadioOption {
  label: string;
  value: string;
  description?: string;
  icon?: string;
  disabled?: boolean;
}

interface RadioAtomProps {
  name: string;
  options: RadioOption[];
  value?: string;
  default?: string;
  orientation?: "horizontal" | "vertical";
  disabled?: boolean;
  required?: boolean;
  onChange?: Action;
  error?: string;
  className?: string;
}

export function AtomRadio({ props }: IntentComponentProps<unknown, RadioAtomProps>) {
  const form = useFormStateOptional();
  const initial = (form?.values[props.name] as string | undefined) ?? props.value ?? props.default ?? "";
  const [local, setLocal] = React.useState(initial);

  const onValueChange = (next: unknown) => {
    const s = String(next ?? "");
    setLocal(s);
    form?.setValue(props.name, s);
    if (props.onChange) dispatchAction(props.onChange, { value: s });
  };

  return (
    <div className="space-y-2">
      <RadioGroup
        name={props.name}
        value={local}
        disabled={props.disabled}
        required={props.required}
        onValueChange={onValueChange}
        className={cn(
          props.orientation === "horizontal" ? "flex gap-4" : "grid gap-2",
          props.className,
        )}
      >
        {props.options.map((opt) => (
          <div key={opt.value} className="flex items-start gap-2">
            <RadioGroupItem id={`${props.name}-${opt.value}`} value={opt.value} disabled={opt.disabled} />
            <div className="grid gap-0.5 leading-none">
              <label
                htmlFor={`${props.name}-${opt.value}`}
                className="text-sm font-medium leading-none"
              >
                {opt.label}
              </label>
              {opt.description ? (
                <p className="text-sm text-muted-foreground">{opt.description}</p>
              ) : null}
            </div>
          </div>
        ))}
      </RadioGroup>
      {props.error ? <p className="text-xs text-destructive">{props.error}</p> : null}
    </div>
  );
}
