import { Inbox } from "lucide-react";
import { Icon } from "./atom.icon";
import { SlotRenderer } from "../runtime/slots";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface EmptyStateProps {
  icon?: string;
  title: string;
  description?: string;
  className?: string;
}

export function EmptyState({
  props,
  slots,
}: IntentComponentProps<unknown, EmptyStateProps>) {
  const hasActions = (slots["actions"] ?? []).length > 0;
  return (
    <div
      className={cn(
        "flex flex-col items-center justify-center rounded-lg border border-dashed p-8 text-center",
        props.className,
      )}
    >
      <div className="rounded-full bg-muted p-3 text-muted-foreground">
        {props.icon ? (
          <Icon node={{} as never} slots={{}} props={{ name: props.icon, size: 24 }} />
        ) : (
          <Inbox className="h-6 w-6" />
        )}
      </div>
      <h3 className="mt-4 text-sm font-semibold">{props.title}</h3>
      {props.description ? (
        <p className="mt-1 max-w-sm text-sm text-muted-foreground">{props.description}</p>
      ) : null}
      {hasActions ? (
        <div className="mt-4 flex items-center gap-2">
          <SlotRenderer slot="actions" slots={slots} />
        </div>
      ) : null}
    </div>
  );
}
