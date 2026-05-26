import * as React from "react";
import { Check } from "lucide-react";
import { Button } from "@/components/ui/button";
import { GraphRenderer } from "../runtime/renderer";
import { cn } from "@/lib/utils";
import { dispatchAction, type Action } from "../runtime/actions";
import type { IntentComponentProps } from "../runtime/registry";

interface StepperStep {
  id: string;
  title: string;
  description?: string;
  icon?: string;
  optional?: boolean;
}

interface StepperProps {
  steps: StepperStep[];
  initial?: string;
  orientation?: "horizontal" | "vertical";
  onNext?: Action;
  onPrev?: Action;
  onComplete?: Action;
  className?: string;
}

export function Stepper({ props, slots }: IntentComponentProps<unknown, StepperProps>) {
  const steps = props.steps ?? [];
  const initialIdx = Math.max(
    0,
    steps.findIndex((s) => s.id === (props.initial ?? steps[0]?.id)),
  );
  const [current, setCurrent] = React.useState(initialIdx);
  const isHorizontal = (props.orientation ?? "horizontal") === "horizontal";

  const activeStep = steps[current];
  const isLast = current === steps.length - 1;

  const next = () => {
    if (isLast) {
      if (props.onComplete) dispatchAction(props.onComplete, { value: activeStep?.id });
      return;
    }
    if (props.onNext) dispatchAction(props.onNext, { value: activeStep?.id });
    setCurrent((c) => Math.min(steps.length - 1, c + 1));
  };

  const prev = () => {
    if (current === 0) return;
    if (props.onPrev) dispatchAction(props.onPrev, { value: activeStep?.id });
    setCurrent((c) => Math.max(0, c - 1));
  };

  return (
    <div
      className={cn(
        "gap-6",
        isHorizontal ? "flex flex-col" : "grid grid-cols-[240px_1fr]",
        props.className,
      )}
    >
      <nav
        aria-label="Progress"
        className={cn(isHorizontal ? "flex w-full justify-between" : "flex flex-col gap-1")}
      >
        {steps.map((step, i) => {
          const done = i < current;
          const isActive = i === current;
          return (
            <div
              key={step.id}
              className={cn(
                "flex items-center gap-3",
                isHorizontal && "flex-1 last:flex-none",
              )}
            >
              <button
                type="button"
                onClick={() => setCurrent(i)}
                className={cn(
                  "inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-full border-2 text-xs font-medium transition-colors",
                  done && "border-primary bg-primary text-primary-foreground",
                  isActive && "border-primary text-primary",
                  !done && !isActive && "border-border text-muted-foreground",
                )}
                aria-current={isActive ? "step" : undefined}
              >
                {done ? <Check className="h-4 w-4" /> : i + 1}
              </button>
              <div className="min-w-0 flex-1">
                <div className={cn("text-sm font-medium", isActive ? "text-foreground" : "text-muted-foreground")}>
                  {step.title}
                  {step.optional ? (
                    <span className="ml-1 text-xs font-normal text-muted-foreground">(optional)</span>
                  ) : null}
                </div>
                {step.description && !isHorizontal ? (
                  <div className="text-xs text-muted-foreground">{step.description}</div>
                ) : null}
              </div>
              {isHorizontal && i < steps.length - 1 ? (
                <div
                  className={cn(
                    "h-px flex-1",
                    done ? "bg-primary" : "bg-border",
                  )}
                />
              ) : null}
            </div>
          );
        })}
      </nav>

      <div>
        <div className="min-h-[200px]">
          {activeStep ? (
            <div className="space-y-4">
              {(slots[activeStep.id] ?? []).map((child, i) => (
                <GraphRenderer key={`${child.intent}:${i}`} node={child} />
              ))}
            </div>
          ) : null}
        </div>
        <div className="mt-6 flex items-center justify-between">
          <Button variant="outline" onClick={prev} disabled={current === 0}>
            Previous
          </Button>
          <Button onClick={next}>{isLast ? "Finish" : "Next"}</Button>
        </div>
      </div>
    </div>
  );
}
