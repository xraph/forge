import { AlertOctagon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { dispatchAction, type Action } from "../runtime/actions";
import type { IntentComponentProps } from "../runtime/registry";

interface ErrorStateProps {
  title: string;
  description?: string;
  error?: string;
  onRetry?: Action;
  className?: string;
}

export function ErrorState({ props }: IntentComponentProps<unknown, ErrorStateProps>) {
  return (
    <div
      className={cn(
        "flex flex-col items-center justify-center rounded-lg border border-destructive/30 bg-destructive/5 p-8 text-center",
        props.className,
      )}
    >
      <div className="rounded-full bg-destructive/10 p-3 text-destructive">
        <AlertOctagon className="h-6 w-6" />
      </div>
      <h3 className="mt-4 text-sm font-semibold text-destructive">{props.title}</h3>
      {props.description ? (
        <p className="mt-1 max-w-sm text-sm text-muted-foreground">{props.description}</p>
      ) : null}
      {props.error ? (
        <pre className="mt-3 max-w-md overflow-auto rounded-md bg-muted px-3 py-2 text-left text-xs text-muted-foreground">
          {props.error}
        </pre>
      ) : null}
      {props.onRetry ? (
        <Button
          variant="outline"
          size="sm"
          className="mt-4"
          onClick={() => dispatchAction(props.onRetry, {})}
        >
          Retry
        </Button>
      ) : null}
    </div>
  );
}
