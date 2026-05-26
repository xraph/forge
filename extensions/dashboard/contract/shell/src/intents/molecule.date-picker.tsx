import * as React from "react";
import { format, parseISO } from "date-fns";
import { Calendar as CalendarIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Calendar } from "@/components/ui/calendar";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { cn } from "@/lib/utils";
import { useFormStateOptional } from "./_form-state";
import { dispatchAction, type Action } from "../runtime/actions";
import type { IntentComponentProps } from "../runtime/registry";

interface DatePickerProps {
  name: string;
  value?: string;
  default?: string;
  mode?: "single" | "range";
  min?: string;
  max?: string;
  disabledDates?: string[];
  format?: string;
  placeholder?: string;
  disabled?: boolean;
  onChange?: Action;
  error?: string;
  className?: string;
}

/**
 * Popover-anchored calendar picker. Wire format is unchanged:
 *   single: "YYYY-MM-DD"
 *   range:  "YYYY-MM-DD/YYYY-MM-DD"
 */
export function DatePicker({ props }: IntentComponentProps<unknown, DatePickerProps>) {
  const form = useFormStateOptional();
  const initial =
    (form?.values[props.name] as string | undefined) ?? props.value ?? props.default ?? "";
  const [local, setLocal] = React.useState(initial);
  const [open, setOpen] = React.useState(false);

  const commit = (next: string) => {
    setLocal(next);
    form?.setValue(props.name, next);
    if (props.onChange) dispatchAction(props.onChange, { value: next });
  };

  const fmt = props.format;
  const disabledFn = parseDisabled(props.disabledDates, props.min, props.max);
  const fromDate = props.min ? parseISO(props.min) : undefined;
  const toDate = props.max ? parseISO(props.max) : undefined;

  if (props.mode === "range") {
    const [start, end] = parseRange(local);
    const selected =
      start && end
        ? { from: parseISO(start), to: parseISO(end) }
        : start
          ? { from: parseISO(start), to: undefined }
          : undefined;
    const label =
      selected?.from && selected.to
        ? `${format(selected.from, fmt ?? "MMM d, yyyy")} – ${format(selected.to, fmt ?? "MMM d, yyyy")}`
        : selected?.from
          ? format(selected.from, fmt ?? "MMM d, yyyy")
          : props.placeholder ?? "Pick a date range";
    return (
      <Picker
        name={props.name}
        open={open}
        setOpen={setOpen}
        disabled={props.disabled}
        error={props.error}
        className={props.className}
        label={label}
        empty={!selected?.from}
      >
        <Calendar
          mode="range"
          selected={selected}
          onSelect={(range) =>
            commit(
              range?.from && range.to
                ? `${formatISO(range.from)}/${formatISO(range.to)}`
                : range?.from
                  ? formatISO(range.from)
                  : "",
            )
          }
          startMonth={fromDate}
          endMonth={toDate}
          disabled={disabledFn}
          numberOfMonths={2}
        />
      </Picker>
    );
  }

  const selected = local ? parseISO(local) : undefined;
  const label = selected
    ? format(selected, fmt ?? "MMM d, yyyy")
    : props.placeholder ?? "Pick a date";

  return (
    <Picker
      name={props.name}
      open={open}
      setOpen={setOpen}
      disabled={props.disabled}
      error={props.error}
      className={props.className}
      label={label}
      empty={!selected}
    >
      <Calendar
        mode="single"
        selected={selected}
        onSelect={(d) => {
          if (d) {
            commit(formatISO(d));
            setOpen(false);
          } else {
            commit("");
          }
        }}
        startMonth={fromDate}
        endMonth={toDate}
        disabled={disabledFn}
      />
    </Picker>
  );
}

function Picker({
  name,
  open,
  setOpen,
  disabled,
  error,
  className,
  label,
  empty,
  children,
}: {
  name: string;
  open: boolean;
  setOpen: (v: boolean) => void;
  disabled?: boolean;
  error?: string;
  className?: string;
  label: string;
  empty: boolean;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1">
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button
            type="button"
            id={name}
            variant="outline"
            disabled={disabled}
            className={cn(
              "w-full justify-start font-normal",
              empty && "text-muted-foreground",
              error && "border-destructive",
              className,
            )}
          >
            <CalendarIcon className="mr-2 h-4 w-4" />
            {label}
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-auto p-0" align="start">
          {children}
        </PopoverContent>
      </Popover>
      {error ? <p className="text-xs text-destructive">{error}</p> : null}
    </div>
  );
}

function formatISO(d: Date): string {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}

function parseRange(s: string): [string, string] {
  if (!s) return ["", ""];
  const i = s.indexOf("/");
  if (i < 0) return [s, ""];
  return [s.slice(0, i), s.slice(i + 1)];
}

function parseDisabled(
  dates: string[] | undefined,
  min: string | undefined,
  max: string | undefined,
): ((date: Date) => boolean) | undefined {
  if (!dates && !min && !max) return undefined;
  const set = new Set((dates ?? []).map((d) => d));
  const lo = min ? parseISO(min) : undefined;
  const hi = max ? parseISO(max) : undefined;
  return (d: Date) => {
    if (set.has(formatISO(d))) return true;
    if (lo && d < lo) return true;
    if (hi && d > hi) return true;
    return false;
  };
}
