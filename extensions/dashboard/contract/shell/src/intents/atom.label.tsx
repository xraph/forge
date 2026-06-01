import { Label as LabelPrimitive } from "@/components/ui/label";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface LabelProps {
  text: string;
  for?: string;
  required?: boolean;
  optional?: boolean;
  className?: string;
}

export function AtomLabel({ props }: IntentComponentProps<unknown, LabelProps>) {
  return (
    <LabelPrimitive htmlFor={props.for} className={cn(props.className)}>
      {props.text}
      {props.required ? <span className="ml-0.5 text-destructive">*</span> : null}
      {props.optional ? (
        <span className="ml-1 text-xs font-normal text-muted-foreground">(optional)</span>
      ) : null}
    </LabelPrimitive>
  );
}
