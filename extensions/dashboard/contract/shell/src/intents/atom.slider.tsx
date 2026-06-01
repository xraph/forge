import * as React from "react";
import { Slider } from "@/components/ui/slider";
import { cn } from "@/lib/utils";
import { useFormStateOptional } from "./_form-state";
import { dispatchAction, type Action } from "../runtime/actions";
import type { IntentComponentProps } from "../runtime/registry";

interface SliderAtomProps {
  name: string;
  min?: number;
  max?: number;
  step?: number;
  value?: number;
  default?: number;
  range?: boolean;
  rangeValue?: number[];
  marks?: { value: number; label?: string }[];
  showValue?: boolean;
  disabled?: boolean;
  onChange?: Action;
  className?: string;
}

export function AtomSlider({ props }: IntentComponentProps<unknown, SliderAtomProps>) {
  const form = useFormStateOptional();
  const initial: number | number[] = props.range
    ? (form?.values[props.name] as number[] | undefined) ?? props.rangeValue ?? [props.min ?? 0, props.max ?? 100]
    : (form?.values[props.name] as number | undefined) ?? props.value ?? props.default ?? props.min ?? 0;
  const [local, setLocal] = React.useState<number | number[]>(initial);

  const onValueChange = (next: number | readonly number[]) => {
    const writable = Array.isArray(next) ? Array.from(next) : (next as number);
    setLocal(writable);
    form?.setValue(props.name, writable);
    if (props.onChange) dispatchAction(props.onChange, { value: writable });
  };

  const display = Array.isArray(local) ? local.join(" – ") : String(local);

  return (
    <div className={cn("space-y-2", props.className)}>
      {props.showValue ? (
        <div className="flex justify-end text-xs text-muted-foreground tabular-nums">{display}</div>
      ) : null}
      <Slider
        name={props.name}
        min={props.min ?? 0}
        max={props.max ?? 100}
        step={props.step ?? 1}
        value={local}
        disabled={props.disabled}
        onValueChange={onValueChange}
      />
      {props.marks && props.marks.length > 0 ? (
        <div className="flex justify-between text-xs text-muted-foreground">
          {props.marks.map((m) => (
            <span key={m.value}>{m.label ?? m.value}</span>
          ))}
        </div>
      ) : null}
    </div>
  );
}
