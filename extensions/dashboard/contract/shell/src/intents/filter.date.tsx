import { Input } from "@/components/ui/input";
import { useFilterStateOptional } from "./_filter-state";
import type { IntentComponentProps } from "../runtime/registry";

interface FilterDateProps {
  paramName: string;
  label?: string;
  /** Single date or a from/to range. v1 ships single only. */
  mode?: "single" | "range";
  className?: string;
}

/**
 * filter.date renders a native date input that writes to the enclosing
 * resource.list's FilterStateContext under props.paramName. The value is
 * an ISO YYYY-MM-DD string. Range mode is reserved for a follow-up
 * (needs the daterangepicker forgeui addition); v1 ships single only.
 */
export function FilterDate({ props }: IntentComponentProps<unknown, FilterDateProps>) {
  const filter = useFilterStateOptional();
  const value = (filter?.values[props.paramName] as string | undefined) ?? "";

  return (
    <Input
      type="date"
      value={value}
      onChange={(e) => filter?.setValue(props.paramName, e.target.value || undefined)}
      aria-label={props.label ?? props.paramName}
      className={props.className ?? "w-44"}
    />
  );
}
