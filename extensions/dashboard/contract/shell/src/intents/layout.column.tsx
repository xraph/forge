import { cn } from "@/lib/utils";
import { SlotRenderer } from "../runtime/slots";
import {
  alignClass,
  gapClass,
  justifyClass,
  paddingClass,
  type Align,
  type Justify,
  type Spacing,
} from "@/lib/tokens";
import type { IntentComponentProps } from "../runtime/registry";

interface ColumnProps {
  gap?: Spacing;
  align?: Align;
  justify?: Justify;
  padding?: Spacing;
  className?: string;
}

export function Column({ props, slots }: IntentComponentProps<unknown, ColumnProps>) {
  return (
    <div
      className={cn(
        "flex flex-col",
        gapClass(props.gap),
        alignClass(props.align),
        justifyClass(props.justify),
        paddingClass(props.padding),
        props.className,
      )}
    >
      <SlotRenderer slot="children" slots={slots} />
    </div>
  );
}
