import { cn } from "@/lib/utils";
import { SlotRenderer } from "../runtime/slots";
import type { GraphNode } from "@/contract/types";
import type { IntentComponentProps } from "../runtime/registry";

interface SectionProps {
  description?: string;
  compact?: boolean;
  className?: string;
}

export function Section({
  node,
  props,
  slots,
}: IntentComponentProps<unknown, SectionProps>) {
  const actions = slots["actions"] ?? ([] as GraphNode[]);
  return (
    <section className={cn(props.compact ? "space-y-2" : "space-y-4", props.className)}>
      {(node.title || props.description || actions.length > 0) && (
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-1">
            {node.title ? (
              <h2 className="text-lg font-semibold leading-none tracking-tight">{node.title}</h2>
            ) : null}
            {props.description ? (
              <p className="text-sm text-muted-foreground">{props.description}</p>
            ) : null}
          </div>
          {actions.length > 0 ? (
            <div className="flex items-center gap-2">
              <SlotRenderer slot="actions" slots={slots} />
            </div>
          ) : null}
        </div>
      )}
      <div>
        <SlotRenderer slot="content" slots={slots} />
      </div>
    </section>
  );
}
