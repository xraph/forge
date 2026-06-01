import * as React from "react";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Icon } from "./atom.icon";
import { cn } from "@/lib/utils";
import { useFormStateOptional } from "./_form-state";
import { dispatchAction, type Action } from "../runtime/actions";
import type { IntentComponentProps } from "../runtime/registry";

interface SelectOption {
  label: string;
  value: string;
  description?: string;
  icon?: string;
  disabled?: boolean;
}

interface SelectAtomProps {
  name: string;
  placeholder?: string;
  options: SelectOption[];
  value?: string;
  default?: string;
  disabled?: boolean;
  required?: boolean;
  clearable?: boolean;
  onChange?: Action;
  error?: string;
  className?: string;
}

export function AtomSelect({ props }: IntentComponentProps<unknown, SelectAtomProps>) {
  const form = useFormStateOptional();
  const initial = (form?.values[props.name] as string | undefined) ?? props.value ?? props.default ?? "";
  const [local, setLocal] = React.useState(initial);

  const onValueChange = (next: string | null) => {
    const s = next ?? "";
    setLocal(s);
    form?.setValue(props.name, s);
    if (props.onChange) dispatchAction(props.onChange, { value: s });
  };

  return (
    <div className="space-y-1">
      <Select value={local} onValueChange={onValueChange} disabled={props.disabled}>
        <SelectTrigger className={cn(props.error && "border-destructive", props.className)}>
          <SelectValue>{local ? undefined : props.placeholder ?? "Select…"}</SelectValue>
        </SelectTrigger>
        <SelectContent>
          {(props.options ?? []).map((opt) => (
            <SelectItem key={opt.value} value={opt.value} disabled={opt.disabled}>
              <span className="flex items-center gap-2">
                {opt.icon ? (
                  <Icon node={{} as never} slots={{}} props={{ name: opt.icon, size: 14 }} />
                ) : null}
                <span>{opt.label}</span>
              </span>
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {props.error ? <p className="text-xs text-destructive">{props.error}</p> : null}
    </div>
  );
}
