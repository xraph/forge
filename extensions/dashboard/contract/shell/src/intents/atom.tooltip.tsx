import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { GraphRenderer } from "../runtime/renderer";
import type { GraphNode } from "@/contract/types";
import type { IntentComponentProps } from "../runtime/registry";

interface TooltipAtomProps {
  content: string;
  side?: "top" | "bottom" | "left" | "right";
  delayMs?: number;
  className?: string;
}

export function AtomTooltip({
  props,
  slots,
}: IntentComponentProps<unknown, TooltipAtomProps>) {
  const trigger = slots["trigger"]?.[0] as GraphNode | undefined;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        {trigger ? <span><GraphRenderer node={trigger} /></span> : <span />}
      </TooltipTrigger>
      <TooltipContent side={props.side} className={props.className}>
        {props.content}
      </TooltipContent>
    </Tooltip>
  );
}
