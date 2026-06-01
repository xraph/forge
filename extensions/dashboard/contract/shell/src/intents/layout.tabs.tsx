import * as React from "react";
import { Tabs as TabsPrimitive, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Icon } from "./atom.icon";
import { Badge } from "@/components/ui/badge";
import { GraphRenderer } from "../runtime/renderer";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface TabItem {
  value: string;
  label: string;
  icon?: string;
  badge?: string;
  description?: string;
  disabled?: boolean;
}

interface TabsLayoutProps {
  defaultValue?: string;
  orientation?: "horizontal" | "vertical";
  items: TabItem[];
  variant?: "default" | "underline" | "pills";
  className?: string;
}

export function TabsLayout({
  props,
  slots,
}: IntentComponentProps<unknown, TabsLayoutProps>) {
  const items = props.items ?? [];
  const [value, setValue] = React.useState(
    props.defaultValue ?? items[0]?.value ?? "",
  );

  return (
    <TabsPrimitive
      value={value}
      onValueChange={setValue}
      orientation={props.orientation ?? "horizontal"}
      className={cn("w-full", props.className)}
    >
      <TabsList>
        {items.map((item) => (
          <TabsTrigger key={item.value} value={item.value} disabled={item.disabled}>
            {item.icon ? (
              <Icon
                node={{} as never}
                slots={{}}
                props={{ name: item.icon, size: 16 }}
              />
            ) : null}
            <span>{item.label}</span>
            {item.badge ? (
              <Badge variant="secondary" size="sm">
                {item.badge}
              </Badge>
            ) : null}
          </TabsTrigger>
        ))}
      </TabsList>
      {items.map((item) => {
        const panel = slots[item.value] ?? [];
        return (
          <TabsContent key={item.value} value={item.value}>
            {panel.map((child, i) => (
              <GraphRenderer key={`${child.intent}:${i}`} node={child} />
            ))}
          </TabsContent>
        );
      })}
    </TabsPrimitive>
  );
}
