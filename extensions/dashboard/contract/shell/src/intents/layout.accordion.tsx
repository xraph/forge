import {
  Accordion as AccordionPrimitive,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";
import { GraphRenderer } from "../runtime/renderer";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface AccordionItemSpec {
  value: string;
  title: string;
  icon?: string;
  description?: string;
  disabled?: boolean;
}

interface AccordionLayoutProps {
  type?: "single" | "multiple";
  collapsible?: boolean;
  items: AccordionItemSpec[];
  className?: string;
}

export function AccordionLayout({
  props,
  slots,
}: IntentComponentProps<unknown, AccordionLayoutProps>) {
  return (
    <AccordionPrimitive
      // Base UI's Accordion accepts `multiple` to enable multi-open.
      multiple={props.type === "multiple"}
      className={cn("w-full", props.className)}
    >
      {(props.items ?? []).map((item) => {
        const panel = slots[item.value] ?? [];
        return (
          <AccordionItem key={item.value} value={item.value} disabled={item.disabled}>
            <AccordionTrigger>{item.title}</AccordionTrigger>
            <AccordionContent>
              {panel.map((child, i) => (
                <GraphRenderer key={`${child.intent}:${i}`} node={child} />
              ))}
            </AccordionContent>
          </AccordionItem>
        );
      })}
    </AccordionPrimitive>
  );
}
