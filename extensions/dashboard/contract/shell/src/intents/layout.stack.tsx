import * as React from "react";
import { cn } from "@/lib/utils";
import { GraphRenderer } from "../runtime/renderer";
import { Separator } from "@/components/ui/separator";
import {
  directionClass,
  gapClass,
  paddingClass,
  type Direction,
  type Spacing,
} from "@/lib/tokens";
import type { IntentComponentProps } from "../runtime/registry";

interface StackProps {
  direction?: Direction;
  gap?: Spacing;
  divider?: boolean;
  padding?: Spacing;
  className?: string;
}

export function Stack({ props, slots }: IntentComponentProps<unknown, StackProps>) {
  const children = slots["children"] ?? [];
  return (
    <div
      className={cn(
        "flex",
        directionClass(props.direction ?? "column"),
        gapClass(props.gap),
        paddingClass(props.padding),
        props.className,
      )}
    >
      {children.map((child, i) => (
        <React.Fragment key={`${child.intent}:${i}`}>
          <GraphRenderer node={child} />
          {props.divider && i < children.length - 1 ? (
            <Separator
              orientation={
                props.direction === "row" || props.direction === "row-reverse"
                  ? "vertical"
                  : "horizontal"
              }
            />
          ) : null}
        </React.Fragment>
      ))}
    </div>
  );
}
