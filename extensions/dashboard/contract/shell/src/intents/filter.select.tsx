import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useContractQuery } from "../contract/hooks";
import { useContributor } from "../runtime/context";
import { useFilterStateOptional } from "./_filter-state";
import type { IntentComponentProps } from "../runtime/registry";

interface FilterSelectProps {
  paramName: string;
  label?: string;
  /** Literal options. Pass `optionsQuery` instead for data-bound options. */
  options?: { label: string; value: string }[];
  /** Query intent name to fetch options from. Expects rows of { label, value }. */
  optionsQuery?: string;
  /** Placeholder shown when no value is selected. */
  placeholder?: string;
  /** Include an "All" item that clears the filter. */
  clearable?: boolean;
  className?: string;
}

/**
 * filter.select renders a single-select dropdown that writes to the
 * enclosing resource.list's FilterStateContext under props.paramName.
 * Options can be literal (props.options) or data-bound (props.optionsQuery)
 * — the latter is useful when the option list itself comes from an intent
 * (e.g. "role" filter populated from roles.list).
 */
export function FilterSelect({ props }: IntentComponentProps<unknown, FilterSelectProps>) {
  const filter = useFilterStateOptional();
  const contributor = useContributor();
  const query = useContractQuery<{ items?: { label: string; value: string }[] }>(
    contributor,
    props.optionsQuery ?? "",
  );

  const options = props.optionsQuery
    ? (query.data?.items ?? extractOptions(query.data))
    : (props.options ?? []);

  const value = (filter?.values[props.paramName] as string | undefined) ?? "";

  const onChange = (next: string | null) => {
    if (!filter) return;
    if (!next || next === "__clear__") filter.setValue(props.paramName, undefined);
    else filter.setValue(props.paramName, next);
  };

  const placeholder = props.placeholder ?? props.label ?? "Select…";

  return (
    <Select value={value || undefined} onValueChange={onChange}>
      <SelectTrigger className={props.className ?? "w-48"}>
        <SelectValue>{value ? undefined : placeholder}</SelectValue>
      </SelectTrigger>
      <SelectContent>
        {props.clearable ? <SelectItem value="__clear__">All</SelectItem> : null}
        {options.map((o) => (
          <SelectItem key={o.value} value={o.value}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function extractOptions(data: unknown): { label: string; value: string }[] {
  if (Array.isArray(data)) return data as { label: string; value: string }[];
  if (data && typeof data === "object") {
    for (const v of Object.values(data as Record<string, unknown>)) {
      if (Array.isArray(v)) return v as { label: string; value: string }[];
    }
  }
  return [];
}
