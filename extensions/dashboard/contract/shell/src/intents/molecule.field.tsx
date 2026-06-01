import { SlotRenderer } from "../runtime/slots";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface FieldProps {
  label?: string;
  description?: string;
  helpText?: string;
  required?: boolean;
  optional?: boolean;
  error?: string;
  for?: string;
  className?: string;
}

export function MoleculeField({
  props,
  slots,
}: IntentComponentProps<unknown, FieldProps>) {
  return (
    <div className={cn("space-y-1.5", props.className)}>
      {props.label ? (
        <label htmlFor={props.for} className="text-sm font-medium leading-none">
          {props.label}
          {props.required ? <span className="ml-0.5 text-destructive">*</span> : null}
          {props.optional ? (
            <span className="ml-1 text-xs font-normal text-muted-foreground">(optional)</span>
          ) : null}
        </label>
      ) : null}
      {props.description ? (
        <p className="text-sm text-muted-foreground">{props.description}</p>
      ) : null}
      <div>
        <SlotRenderer slot="control" slots={slots} />
      </div>
      {props.helpText && !props.error ? (
        <p className="text-xs text-muted-foreground">{props.helpText}</p>
      ) : null}
      {props.error ? <p className="text-xs text-destructive">{props.error}</p> : null}
    </div>
  );
}
