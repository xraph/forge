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

interface RowProps {
  gap?: Spacing;
  align?: Align;
  justify?: Justify;
  wrap?: boolean;
  padding?: Spacing;
  className?: string;
}

export function Row({ props, slots }: IntentComponentProps<unknown, RowProps>) {
  return (
    <div
      className={cn(
        "flex flex-row",
        props.wrap && "flex-wrap",
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
