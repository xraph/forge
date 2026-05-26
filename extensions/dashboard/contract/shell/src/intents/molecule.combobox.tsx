import * as React from "react";
import { Check, ChevronsUpDown } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { useFormStateOptional } from "./_form-state";
import { dispatchAction, type Action } from "../runtime/actions";
import type { IntentComponentProps } from "../runtime/registry";

interface ComboOption {
  label: string;
  value: string;
  description?: string;
  icon?: string;
  disabled?: boolean;
}

interface ComboboxProps {
  name: string;
  placeholder?: string;
  options?: ComboOption[];
  value?: string;
  multiple?: boolean;
  values?: string[];
  searchable?: boolean;
  creatable?: boolean;
  clearable?: boolean;
  onSearch?: Action;
  onChange?: Action;
  disabled?: boolean;
  error?: string;
  className?: string;
}

export function Combobox({ props }: IntentComponentProps<unknown, ComboboxProps>) {
  const form = useFormStateOptional();
  const opts = props.options ?? [];

  const initial: string | string[] = props.multiple
    ? (form?.values[props.name] as string[] | undefined) ?? props.values ?? []
    : (form?.values[props.name] as string | undefined) ?? props.value ?? "";
  const [local, setLocal] = React.useState<string | string[]>(initial);
  const [open, setOpen] = React.useState(false);

  const commit = (next: string | string[]) => {
    setLocal(next);
    form?.setValue(props.name, next);
    if (props.onChange) dispatchAction(props.onChange, { value: next });
  };

  const selected = Array.isArray(local)
    ? opts.filter((o) => local.includes(o.value))
    : opts.find((o) => o.value === local);

  const triggerLabel = (() => {
    if (Array.isArray(selected)) {
      if (selected.length === 0) return props.placeholder ?? "Select…";
      if (selected.length <= 2) return selected.map((s) => s.label).join(", ");
      return `${selected.length} selected`;
    }
    return selected?.label ?? props.placeholder ?? "Select…";
  })();

  const onSelect = (value: string) => {
    if (props.multiple) {
      const arr = Array.isArray(local) ? local : [];
      commit(arr.includes(value) ? arr.filter((v) => v !== value) : [...arr, value]);
    } else {
      commit(value);
      setOpen(false);
    }
  };

  const isSelected = (value: string) =>
    Array.isArray(local) ? local.includes(value) : local === value;

  return (
    <div className="space-y-1">
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button
            variant="outline"
            role="combobox"
            aria-expanded={open}
            disabled={props.disabled}
            className={cn(
              "w-full justify-between",
              props.error && "border-destructive",
              props.className,
            )}
          >
            <span className="truncate text-left">{triggerLabel}</span>
            <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-[--radix-popover-trigger-width] p-0">
          <Command
            items={opts}
            onValueChange={(v) => {
              if (props.onSearch) dispatchAction(props.onSearch, { value: v });
            }}
          >
            {props.searchable !== false ? (
              <CommandInput placeholder="Search…" />
            ) : null}
            <CommandList>
              <CommandEmpty>No results.</CommandEmpty>
              <CommandGroup>
                {opts.map((opt) => (
                  <CommandItem
                    key={opt.value}
                    value={opt.value}
                    disabled={opt.disabled}
                    onClick={() => onSelect(opt.value)}
                  >
                    <Check
                      className={cn(
                        "mr-2 h-4 w-4",
                        isSelected(opt.value) ? "opacity-100" : "opacity-0",
                      )}
                    />
                    <span className="flex flex-col">
                      <span>{opt.label}</span>
                      {opt.description ? (
                        <span className="text-xs text-muted-foreground">{opt.description}</span>
                      ) : null}
                    </span>
                  </CommandItem>
                ))}
              </CommandGroup>
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
      {props.multiple && Array.isArray(local) && local.length > 0 ? (
        <div className="flex flex-wrap gap-1">
          {selected && Array.isArray(selected)
            ? selected.map((s) => (
                <Badge key={s.value} variant="secondary">
                  {s.label}
                </Badge>
              ))
            : null}
        </div>
      ) : null}
      {props.error ? <p className="text-xs text-destructive">{props.error}</p> : null}
    </div>
  );
}
