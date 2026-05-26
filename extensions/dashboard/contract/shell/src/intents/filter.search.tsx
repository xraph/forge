import * as React from "react";
import { Input } from "@/components/ui/input";
import { Search } from "lucide-react";
import { useFilterStateOptional } from "./_filter-state";
import type { IntentComponentProps } from "../runtime/registry";

interface FilterSearchProps {
  /** Query parameter name (defaults to "q"). */
  paramName?: string;
  placeholder?: string;
  /** Debounce in ms (default 250). Applied before pushing to filter state. */
  debounceMs?: number;
  className?: string;
}

/**
 * filter.search renders a search input that writes its value into the
 * enclosing resource.list's FilterStateContext under the named param.
 * The resource.list re-fetches whenever filter state changes. When used
 * outside a resource.list (no FilterStateContext provider), the input
 * is still rendered but its value is held locally — manifest authors
 * should only place it inside `resource.list.filters`.
 */
export function FilterSearch({ props }: IntentComponentProps<unknown, FilterSearchProps>) {
  const filter = useFilterStateOptional();
  const param = props.paramName ?? "q";
  const debounceMs = props.debounceMs ?? 250;

  const [local, setLocal] = React.useState<string>(
    (filter?.values[param] as string | undefined) ?? "",
  );

  React.useEffect(() => {
    if (!filter) return;
    const id = setTimeout(() => filter.setValue(param, local || undefined), debounceMs);
    return () => clearTimeout(id);
  }, [local, filter, param, debounceMs]);

  return (
    <div className={`relative ${props.className ?? ""}`}>
      <Search className="pointer-events-none absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
      <Input
        type="search"
        placeholder={props.placeholder ?? "Search…"}
        value={local}
        onChange={(e) => setLocal(e.target.value)}
        className="pl-9"
      />
    </div>
  );
}
