import * as React from "react";

/**
 * FilterState is the per-resource.list state used by filter.* intents
 * (filter.search, filter.select, filter.date) to communicate user-driven
 * query parameter changes back to the enclosing resource.list. It
 * mirrors form.edit/FormStateContext but lives independently — a list
 * may have both a header filter row and an inline edit form drawer,
 * each owning its own state.
 */
export interface FilterStateValue {
  /** Current value of each filter param by name. Undefined removes the param. */
  values: Record<string, unknown>;
  /** Setter; passing undefined removes the key. */
  setValue: (name: string, value: unknown) => void;
}

export const FilterStateContext = React.createContext<FilterStateValue | null>(null);

/**
 * useFilterStateOptional returns the current FilterState if the caller
 * is inside a resource.list filters slot. Returns null when standalone
 * so filter components don't blow up if dropped into the wrong slot.
 */
export function useFilterStateOptional() {
  return React.useContext(FilterStateContext);
}
