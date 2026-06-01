import * as React from "react";
import { ChevronDown } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu as DropdownMenuRoot,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Icon } from "./atom.icon";
import { GraphRenderer } from "../runtime/renderer";
import { cn } from "@/lib/utils";
import { dispatchAction, type Action } from "../runtime/actions";
import type { GraphNode } from "@/contract/types";
import type { ColorVariant } from "@/lib/tokens";
import type { IntentComponentProps } from "../runtime/registry";

interface MenuItem {
  label?: string;
  icon?: string;
  shortcut?: string[];
  variant?: ColorVariant;
  disabled?: boolean;
  separator?: boolean;
  action?: Action;
  items?: MenuItem[];
}

interface DropdownMenuProps {
  triggerLabel?: string;
  triggerIcon?: string;
  items: MenuItem[];
  side?: "top" | "bottom" | "left" | "right";
  align?: "start" | "center" | "end";
  className?: string;
}

export function MoleculeDropdownMenu({
  props,
  slots,
}: IntentComponentProps<unknown, DropdownMenuProps>) {
  const customTrigger = slots["trigger"]?.[0] as GraphNode | undefined;

  return (
    <DropdownMenuRoot>
      <DropdownMenuTrigger asChild>
        {customTrigger ? (
          <span><GraphRenderer node={customTrigger} /></span>
        ) : (
          <Button variant="outline" size="sm" className={cn(props.className)}>
            {props.triggerIcon ? (
              <Icon node={{} as never} slots={{}} props={{ name: props.triggerIcon, size: 14 }} />
            ) : null}
            {props.triggerLabel ?? "Menu"}
            <ChevronDown className="ml-1 h-3 w-3 opacity-60" />
          </Button>
        )}
      </DropdownMenuTrigger>
      <DropdownMenuContent side={props.side} align={props.align ?? "start"}>
        {(props.items ?? []).map((item, i) => renderItem(item, i))}
      </DropdownMenuContent>
    </DropdownMenuRoot>
  );
}

function renderItem(item: MenuItem, key: number | string): React.ReactNode {
  if (item.separator) return <DropdownMenuSeparator key={key} />;
  if (item.items && item.items.length > 0) {
    return (
      <DropdownMenuSub key={key}>
        <DropdownMenuSubTrigger>
          {item.icon ? (
            <Icon node={{} as never} slots={{}} props={{ name: item.icon, size: 14 }} />
          ) : null}
          <span className="ml-2">{item.label ?? ""}</span>
        </DropdownMenuSubTrigger>
        <DropdownMenuSubContent>
          {item.items.map((sub, j) => renderItem(sub, `${key}-${j}`))}
        </DropdownMenuSubContent>
      </DropdownMenuSub>
    );
  }
  if (!item.label) return <DropdownMenuLabel key={key}>—</DropdownMenuLabel>;
  const variantCls =
    item.variant === "destructive" ? "text-destructive focus:text-destructive" : "";
  return (
    <DropdownMenuItem
      key={key}
      disabled={item.disabled}
      className={variantCls}
      onClick={() => item.action && dispatchAction(item.action, {})}
    >
      {item.icon ? (
        <Icon node={{} as never} slots={{}} props={{ name: item.icon, size: 14 }} />
      ) : null}
      <span className={cn(item.icon && "ml-2")}>{item.label}</span>
      {item.shortcut && item.shortcut.length > 0 ? (
        <DropdownMenuShortcut>{item.shortcut.join("+")}</DropdownMenuShortcut>
      ) : null}
    </DropdownMenuItem>
  );
}
