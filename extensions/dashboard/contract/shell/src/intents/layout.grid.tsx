import { cn } from "@/lib/utils";
import { SlotRenderer } from "../runtime/slots";
import {
  colGapClass,
  gapClass,
  gridColsClass,
  paddingClass,
  rowGapClass,
  type ResponsiveCols,
  type Spacing,
} from "@/lib/tokens";
import type { IntentComponentProps } from "../runtime/registry";

interface GridProps {
  cols?: ResponsiveCols | number;
  gap?: Spacing;
  rowGap?: Spacing;
  colGap?: Spacing;
  padding?: Spacing;
  className?: string;
}

export function Grid({ props, slots }: IntentComponentProps<unknown, GridProps>) {
  return (
    <div
      className={cn(
        "grid",
        gridColsClass(props.cols),
        gapClass(props.gap),
        rowGapClass(props.rowGap),
        colGapClass(props.colGap),
        paddingClass(props.padding),
        props.className,
      )}
    >
      <SlotRenderer slot="children" slots={slots} />
    </div>
  );
}
