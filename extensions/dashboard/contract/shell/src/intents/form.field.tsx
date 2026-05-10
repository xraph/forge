import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Checkbox } from "@/components/ui/checkbox";
import { useFormState } from "./form.edit";
import type { IntentComponentProps } from "../runtime/registry";

interface FormFieldProps {
  name: string;
  label?: string;
  kind?: "text" | "email" | "number" | "password" | "textarea" | "checkbox";
  placeholder?: string;
  description?: string;
  required?: boolean;
  disabled?: boolean;
}

export function FormField({ props }: IntentComponentProps<unknown, FormFieldProps>) {
  const form = useFormState();
  const value = form.values[props.name];
  const id = `form-field-${props.name}`;
  const kind = props.kind ?? "text";

  const commonProps = {
    id,
    name: props.name,
    placeholder: props.placeholder,
    required: props.required,
    disabled: props.disabled || form.submitting,
  };

  return (
    <div className="space-y-2">
      <Label htmlFor={id}>
        {props.label ?? props.name}
        {props.required ? <span className="ml-0.5 text-destructive">*</span> : null}
      </Label>
      {kind === "textarea" ? (
        <Textarea
          {...commonProps}
          value={typeof value === "string" ? value : ""}
          onChange={(e) => form.setValue(props.name, e.target.value)}
        />
      ) : kind === "checkbox" ? (
        <div className="flex items-center gap-2">
          <Checkbox
            id={id}
            checked={Boolean(value)}
            disabled={commonProps.disabled}
            onCheckedChange={(checked) => form.setValue(props.name, Boolean(checked))}
          />
          {props.description ? (
            <span className="text-sm text-muted-foreground">{props.description}</span>
          ) : null}
        </div>
      ) : (
        <Input
          {...commonProps}
          type={kind}
          value={typeof value === "string" || typeof value === "number" ? String(value) : ""}
          onChange={(e) => {
            const raw = e.target.value;
            form.setValue(props.name, kind === "number" ? Number(raw) : raw);
          }}
        />
      )}
      {kind !== "checkbox" && props.description ? (
        <p className="text-sm text-muted-foreground">{props.description}</p>
      ) : null}
    </div>
  );
}
