import { Label } from "@/components/ui/label";
import { CodeEditor } from "./organism.code-editor";
import { useFormState } from "./form.edit";
import type { IntentComponentProps } from "../runtime/registry";

interface FieldJsonProps {
  name: string;
  label?: string;
  description?: string;
  required?: boolean;
  /** Editor height (CSS value). Defaults to 240px. */
  height?: string;
}

/**
 * field.json is a form.edit field that surfaces a JSON value through the
 * organism.code-editor primitive. It participates in form-state under
 * props.name; the value is stored as a string in the form (so the
 * eventual submit handler can choose to JSON.parse it server-side, or
 * accept a raw string per its intent schema).
 */
export function FieldJson({ props }: IntentComponentProps<unknown, FieldJsonProps>) {
  const form = useFormState();
  const id = `field-json-${props.name}`;
  const value = typeof form.values[props.name] === "string"
    ? (form.values[props.name] as string)
    : "";

  return (
    <div className="space-y-2">
      <Label htmlFor={id}>
        {props.label ?? props.name}
        {props.required ? <span className="ml-0.5 text-destructive">*</span> : null}
      </Label>
      <CodeEditor
        node={{} as never}
        slots={{}}
        props={{
          name: props.name,
          language: "json",
          height: props.height ?? "240px",
          value,
        }}
      />
      {props.description ? (
        <p className="text-sm text-muted-foreground">{props.description}</p>
      ) : null}
    </div>
  );
}
