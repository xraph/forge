import { cn } from "@/lib/utils";
import { SlotRenderer } from "../runtime/slots";
import { containerClass, paddingClass, type ContainerSize, type Spacing } from "@/lib/tokens";
import type { IntentComponentProps } from "../runtime/registry";

interface ContainerProps {
  size?: ContainerSize;
  padding?: Spacing;
  className?: string;
}

export function Container({ props, slots }: IntentComponentProps<unknown, ContainerProps>) {
  return (
    <div
      className={cn(
        "mx-auto w-full",
        containerClass(props.size),
        paddingClass(props.padding ?? "4"),
        props.className,
      )}
    >
      <SlotRenderer slot="children" slots={slots} />
    </div>
  );
}
