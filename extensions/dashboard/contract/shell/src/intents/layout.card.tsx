import { cn } from "@/lib/utils";
import {
  Card as CardPrimitive,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { SlotRenderer } from "../runtime/slots";
import type { IntentComponentProps } from "../runtime/registry";

interface CardLayoutProps {
  description?: string;
  compact?: boolean;
  noPadding?: boolean;
  bordered?: boolean;
  className?: string;
}

export function Card({
  node,
  props,
  slots,
}: IntentComponentProps<unknown, CardLayoutProps>) {
  const hasHeaderText = !!(node.title || props.description);
  const headerNodes = slots["header"] ?? [];
  const footerNodes = slots["footer"] ?? [];
  const actionNodes = slots["actions"] ?? [];

  return (
    <CardPrimitive
      className={cn(
        props.bordered === false && "border-0 shadow-none",
        props.className,
      )}
    >
      {hasHeaderText || headerNodes.length > 0 || actionNodes.length > 0 ? (
        <CardHeader className={cn(props.compact && "p-4")}>
          <div className="flex items-start justify-between gap-4">
            <div className="space-y-1.5">
              {node.title ? <CardTitle>{node.title}</CardTitle> : null}
              {props.description ? (
                <CardDescription>{props.description}</CardDescription>
              ) : null}
              {headerNodes.length > 0 ? (
                <div className="pt-2">
                  <SlotRenderer slot="header" slots={slots} />
                </div>
              ) : null}
            </div>
            {actionNodes.length > 0 ? (
              <div className="flex items-center gap-2">
                <SlotRenderer slot="actions" slots={slots} />
              </div>
            ) : null}
          </div>
        </CardHeader>
      ) : null}
      <CardContent
        className={cn(props.compact && "p-4 pt-0", props.noPadding && "p-0")}
      >
        <SlotRenderer slot="content" slots={slots} />
      </CardContent>
      {footerNodes.length > 0 ? (
        <CardFooter className={cn(props.compact && "p-4 pt-0")}>
          <SlotRenderer slot="footer" slots={slots} />
        </CardFooter>
      ) : null}
    </CardPrimitive>
  );
}
