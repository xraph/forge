import * as React from "react";
import { Star } from "lucide-react";
import { cn } from "@/lib/utils";
import { useFormStateOptional } from "./_form-state";
import { dispatchAction, type Action } from "../runtime/actions";
import type { Size } from "@/lib/tokens";
import type { IntentComponentProps } from "../runtime/registry";

interface RatingProps {
  name?: string;
  value?: number;
  default?: number;
  max?: number;
  readOnly?: boolean;
  precision?: number; // 1 | 0.5
  size?: Size;
  onChange?: Action;
  className?: string;
}

const sizePx: Record<string, number> = {
  xs: 12,
  sm: 14,
  default: 18,
  lg: 22,
  xl: 28,
  icon: 18,
};

export function Rating({ props }: IntentComponentProps<unknown, RatingProps>) {
  const form = useFormStateOptional();
  const stored = props.name ? (form?.values[props.name] as number | undefined) : undefined;
  const initial: number = stored ?? props.value ?? props.default ?? 0;
  const [local, setLocal] = React.useState<number>(initial);
  const [hover, setHover] = React.useState<number | null>(null);
  const max = props.max ?? 5;
  const precision = props.precision ?? 1;
  const px = sizePx[props.size ?? "default"];

  const commit = (next: number) => {
    setLocal(next);
    if (props.name) form?.setValue(props.name, next);
    if (props.onChange) dispatchAction(props.onChange, { value: next });
  };

  const display = hover ?? local;

  return (
    <div className={cn("inline-flex items-center gap-0.5", props.className)} role="radiogroup">
      {Array.from({ length: max }).map((_, i) => {
        const star = i + 1;
        const full = display >= star;
        const half = precision === 0.5 && !full && display >= star - 0.5;
        return (
          <button
            key={i}
            type="button"
            role="radio"
            aria-checked={local === star}
            disabled={props.readOnly}
            onMouseEnter={() => !props.readOnly && setHover(star)}
            onMouseLeave={() => !props.readOnly && setHover(null)}
            onClick={() => !props.readOnly && commit(star)}
            className={cn(
              "relative inline-block transition-transform",
              !props.readOnly && "cursor-pointer hover:scale-110",
              props.readOnly && "cursor-default",
            )}
            aria-label={`${star} of ${max}`}
          >
            <Star
              size={px}
              className={cn(
                "transition-colors",
                full ? "fill-primary text-primary" : "text-muted-foreground/40",
              )}
            />
            {half ? (
              <Star
                size={px}
                className="absolute left-0 top-0 fill-primary text-primary"
                style={{ clipPath: "inset(0 50% 0 0)" }}
              />
            ) : null}
          </button>
        );
      })}
    </div>
  );
}
