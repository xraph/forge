import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "@/components/ui/resizable";
import { SlotRenderer } from "../runtime/slots";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface SplitProps {
  direction?: "row" | "column" | "row-reverse" | "column-reverse";
  defaultSize?: number;
  minSize?: number;
  maxSize?: number;
  collapsible?: boolean;
  className?: string;
}

/**
 * Two-pane resizable split, backed by react-resizable-panels. Slots:
 *   - start: leading pane content
 *   - end:   trailing pane content
 *
 * direction=column maps to PanelGroup direction="vertical"; row variants
 * map to horizontal. Reverse variants reverse the slot order visually.
 */
export function Split({ props, slots }: IntentComponentProps<unknown, SplitProps>) {
  const isColumn = props.direction === "column" || props.direction === "column-reverse";
  const isReversed = props.direction === "row-reverse" || props.direction === "column-reverse";
  const defaultSize = clamp(props.defaultSize ?? 50, 0, 100);

  const startPanel = (
    <ResizablePanel
      defaultSize={defaultSize}
      minSize={props.minSize}
      maxSize={props.maxSize}
      collapsible={props.collapsible}
    >
      <div className="h-full w-full overflow-auto">
        <SlotRenderer slot="start" slots={slots} />
      </div>
    </ResizablePanel>
  );

  const endPanel = (
    <ResizablePanel>
      <div className="h-full w-full overflow-auto">
        <SlotRenderer slot="end" slots={slots} />
      </div>
    </ResizablePanel>
  );

  return (
    <ResizablePanelGroup
      direction={isColumn ? "vertical" : "horizontal"}
      className={cn("h-full w-full", props.className)}
    >
      {isReversed ? endPanel : startPanel}
      <ResizableHandle withHandle />
      {isReversed ? startPanel : endPanel}
    </ResizablePanelGroup>
  );
}

function clamp(n: number, lo: number, hi: number): number {
  return Math.min(hi, Math.max(lo, n));
}
