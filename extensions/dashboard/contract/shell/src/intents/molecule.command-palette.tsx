import * as React from "react";
import { Dialog as BaseDialog } from "@base-ui-components/react/dialog";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandGroupLabel,
  CommandInput,
  CommandItem,
  CommandList,
  CommandShortcut,
} from "@/components/ui/command";
import { Icon } from "./atom.icon";
import { cn } from "@/lib/utils";
import { dispatchAction, type Action } from "../runtime/actions";
import type { IntentComponentProps } from "../runtime/registry";

interface PaletteItem {
  id: string;
  label: string;
  icon?: string;
  shortcut?: string[];
  disabled?: boolean;
  action?: Action;
}

interface PaletteGroup {
  label?: string;
  items: PaletteItem[];
}

interface CommandPaletteProps {
  placeholder?: string;
  groups: PaletteGroup[];
  triggerShortcut?: string[];
  emptyMessage?: string;
  className?: string;
}

export function CommandPalette({
  props,
}: IntentComponentProps<unknown, CommandPaletteProps>) {
  const [open, setOpen] = React.useState(false);

  React.useEffect(() => {
    if (!props.triggerShortcut || props.triggerShortcut.length === 0) return;
    const handler = (e: KeyboardEvent) => {
      const wants = new Set(props.triggerShortcut!.map((k) => k.toLowerCase()));
      const has = new Set<string>();
      if (e.metaKey) has.add("meta").add("cmd");
      if (e.ctrlKey) has.add("ctrl");
      if (e.altKey) has.add("alt").add("option");
      if (e.shiftKey) has.add("shift");
      has.add(e.key.toLowerCase());
      for (const w of wants) {
        if (!has.has(w)) return;
      }
      e.preventDefault();
      setOpen((v) => !v);
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [props.triggerShortcut]);

  const flat = React.useMemo(
    () => props.groups.flatMap((g) => g.items),
    [props.groups],
  );

  const run = (item: PaletteItem) => {
    if (item.action) dispatchAction(item.action, {});
    setOpen(false);
  };

  return (
    <BaseDialog.Root open={open} onOpenChange={setOpen}>
      <BaseDialog.Portal>
        <BaseDialog.Backdrop className="fixed inset-0 z-50 bg-background/80 backdrop-blur-sm data-[open]:animate-in data-[closed]:animate-out data-[closed]:fade-out-0 data-[open]:fade-in-0" />
        <BaseDialog.Popup
          className={cn(
            "fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 overflow-hidden rounded-lg border bg-popover text-popover-foreground shadow-lg data-[open]:animate-in data-[closed]:animate-out",
            props.className,
          )}
        >
          <Command items={flat}>
            <CommandInput placeholder={props.placeholder ?? "Type a command or search…"} />
            <CommandList className="max-h-[400px] overflow-y-auto">
              <CommandEmpty>{props.emptyMessage ?? "No results."}</CommandEmpty>
              {props.groups.map((group, gi) => (
                <CommandGroup key={`g-${gi}`}>
                  {group.label ? <CommandGroupLabel>{group.label}</CommandGroupLabel> : null}
                  {group.items.map((item) => (
                    <CommandItem
                      key={item.id}
                      value={item.id}
                      disabled={item.disabled}
                      onClick={() => run(item)}
                    >
                      {item.icon ? (
                        <Icon node={{} as never} slots={{}} props={{ name: item.icon, size: 14 }} />
                      ) : null}
                      <span>{item.label}</span>
                      {item.shortcut && item.shortcut.length > 0 ? (
                        <CommandShortcut>{item.shortcut.join("+")}</CommandShortcut>
                      ) : null}
                    </CommandItem>
                  ))}
                </CommandGroup>
              ))}
            </CommandList>
          </Command>
        </BaseDialog.Popup>
      </BaseDialog.Portal>
    </BaseDialog.Root>
  );
}
