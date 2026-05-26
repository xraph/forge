import * as React from "react";
import { useLocation, useNavigate } from "react-router-dom";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Checkbox } from "@/components/ui/checkbox";
import { useContractQuery } from "../contract/hooks";
import { useContributor, ParentProvider } from "../runtime/context";
import { resolveValue } from "../runtime/bindings";
import { LoadingNode, ErrorNode } from "../runtime/fallbacks";
import { SlotRenderer } from "../runtime/slots";
import { FilterStateContext } from "./_filter-state";
import { titleCase, cn } from "@/lib/utils";
import type { GraphNode } from "@/contract/types";
import type { IntentComponentProps } from "../runtime/registry";

interface ColumnSpec {
  field: string;
  label?: string;
}

interface ResourceListProps {
  columns?: (string | ColumnSpec)[];
  emptyMessage?: string;
  /**
   * Detail navigation target on row click. See hrefForRow for the two
   * supported forms. Empty string disables navigation entirely.
   */
  detailHref?: string;
  disableRowNav?: boolean;
  /**
   * When true, a leading checkbox column appears for row selection.
   * Auto-enabled when the bulkActions slot has at least one fill.
   */
  selectable?: boolean;
}

interface ResolvedColumn {
  field: string;
  label: string;
}

function resolveColumns(specs: (string | ColumnSpec)[]): ResolvedColumn[] {
  return specs.map((c) =>
    typeof c === "string"
      ? { field: c, label: titleCase(c) }
      : { field: c.field, label: c.label ?? titleCase(c.field) },
  );
}

function hrefForRow(
  row: Record<string, unknown>,
  currentPath: string,
  template: string | undefined,
): string | null {
  if (template === "") return null;
  if (template) {
    let out = template;
    let aborted = false;
    out = out.replace(/:(\w+)/g, (_, field: string) => {
      const v = row[field];
      if (v === undefined || v === null || v === "") {
        aborted = true;
        return "";
      }
      return encodeURIComponent(String(v));
    });
    return aborted ? null : out;
  }
  const id = row.id;
  if (typeof id !== "string" && typeof id !== "number") return null;
  const base = currentPath.endsWith("/") ? currentPath.slice(0, -1) : currentPath;
  return `${base}/${encodeURIComponent(String(id))}`;
}

/**
 * resource.list fetches a list of records via node.data and renders them
 * in a Table. The data fetch reacts to the FilterStateContext provided
 * to descendants (via the `filters` slot) so filter components can drive
 * the query params without prop drilling.
 *
 * Slot layout:
 *   header       — optional content above the filters row (CTAs, breadcrumbs)
 *   filters      — search/select/date inputs that mutate filter state
 *   bulkActions  — toolbar visible when rows are selected
 *   (table)
 *   pagination   — page controls below the table
 *   rowActions   — per-row buttons rendered in the last column
 */
export function ResourceList({
  node,
  props,
  slots,
}: IntentComponentProps<unknown, ResourceListProps>) {
  const contributor = useContributor();
  const navigate = useNavigate();
  const location = useLocation();

  // Filter state lives in this component so children (filter.search,
  // pagination.cursor) can drive the bound query without re-mounting it.
  const [filterValues, setFilterValues] = React.useState<Record<string, unknown>>({});
  const setFilterValue = React.useCallback((name: string, value: unknown) => {
    setFilterValues((prev) => {
      const next = { ...prev };
      if (value === undefined) delete next[name];
      else next[name] = value;
      return next;
    });
  }, []);

  const dataIntent = node.data?.intent;
  const baseParams = React.useMemo(() => {
    const out: Record<string, unknown> = {};
    const params = node.data?.params as Record<string, unknown> | undefined;
    if (!params) return out;
    for (const [k, v] of Object.entries(params)) {
      const resolved = resolveValue(v, {});
      if (resolved !== undefined) out[k] = resolved;
    }
    return out;
  }, [node.data?.params]);

  // Merge base manifest params with live filter values. Filter values
  // win on conflict so users can override manifest defaults.
  const dataParams = React.useMemo(
    () => ({ ...baseParams, ...stripInternals(filterValues) }),
    [baseParams, filterValues],
  );

  const query = useContractQuery<unknown>(contributor, dataIntent ?? "", undefined, dataParams);

  // Expose the payload's nextCursor / total back into filter state so
  // pagination components can read them. This is opt-in — only set when
  // present on the payload, which keeps stable references for queries
  // that never return paging metadata.
  React.useEffect(() => {
    if (!query.data || typeof query.data !== "object") return;
    const obj = query.data as Record<string, unknown>;
    if (typeof obj.nextCursor === "string" && obj.nextCursor !== filterValues["__nextCursor__"]) {
      setFilterValue("__nextCursor__", obj.nextCursor);
    }
    if (typeof obj.total === "number" && obj.total !== filterValues["__total__"]) {
      setFilterValue("__total__", obj.total);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query.data]);

  const [selected, setSelected] = React.useState<Set<string>>(new Set());

  if (!dataIntent) {
    return <ErrorNode message="resource.list requires a data binding" />;
  }
  if (query.isLoading) return <LoadingNode />;
  if (query.error) return <ErrorNode message={(query.error as Error).message} />;

  const rows = extractRows(query.data);
  const columns = resolveColumns(props.columns ?? inferColumns(rows));
  const rowActions = slots["rowActions"] ?? [];
  const filtersSlot = slots["filters"] ?? [];
  const bulkSlot = slots["bulkActions"] ?? [];
  const headerSlot = slots["header"] ?? [];
  const paginationSlot = slots["pagination"] ?? [];

  const selectable = (props.selectable ?? false) || bulkSlot.length > 0;
  const navDisabled = props.disableRowNav === true;

  const toggleRow = (key: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };
  const toggleAll = (on: boolean) => {
    if (!on) {
      setSelected(new Set());
      return;
    }
    setSelected(new Set(rows.map((r, i) => rowKey(r, i))));
  };

  const body = (
    <div className="space-y-3">
      {headerSlot.length > 0 ? (
        <div>
          <SlotRenderer slot="header" slots={slots} />
        </div>
      ) : null}
      {filtersSlot.length > 0 ? (
        <div className="flex flex-wrap items-center gap-2">
          <SlotRenderer slot="filters" slots={slots} />
        </div>
      ) : null}
      {selectable && selected.size > 0 && bulkSlot.length > 0 ? (
        <div className="flex items-center gap-3 rounded-md border bg-muted/40 px-3 py-2 text-sm">
          <span className="font-medium">{selected.size} selected</span>
          <div className="flex items-center gap-2">
            <SlotRenderer slot="bulkActions" slots={slots} />
          </div>
        </div>
      ) : null}
      {rows.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
          {props.emptyMessage ?? "No records yet."}
        </div>
      ) : (
        <div className="rounded-lg border border-border bg-card">
          <Table>
            <TableHeader>
              <TableRow>
                {selectable ? (
                  <TableHead className="w-10">
                    <Checkbox
                      checked={selected.size > 0 && selected.size === rows.length}
                      onCheckedChange={(c) => toggleAll(Boolean(c))}
                      aria-label="Select all"
                    />
                  </TableHead>
                ) : null}
                {columns.map((c) => (
                  <TableHead key={c.field} className="font-semibold">
                    {c.label}
                  </TableHead>
                ))}
                {rowActions.length > 0 ? <TableHead className="w-24 text-right">Actions</TableHead> : null}
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((row, i) => {
                const key = rowKey(row, i);
                const href = navDisabled ? null : hrefForRow(row, location.pathname, props.detailHref);
                const isSelected = selected.has(key);
                return (
                  <ParentProvider key={key} value={row}>
                    <TableRow
                      className={cn(href ? "cursor-pointer" : "cursor-default", isSelected && "bg-muted/40")}
                      onClick={() => {
                        if (href) navigate(href);
                      }}
                    >
                      {selectable ? (
                        <TableCell onClick={(e) => e.stopPropagation()}>
                          <Checkbox
                            checked={isSelected}
                            onCheckedChange={() => toggleRow(key)}
                            aria-label={`Select row ${key}`}
                          />
                        </TableCell>
                      ) : null}
                      {columns.map((c) => (
                        <TableCell key={c.field}>{renderCell(row[c.field])}</TableCell>
                      ))}
                      {rowActions.length > 0 ? (
                        <TableCell className="text-right" onClick={(e) => e.stopPropagation()}>
                          <div className="flex justify-end gap-2">
                            {rowActions.map((child, j) => (
                              <RenderChild key={`${child.intent}:${j}`} node={child} />
                            ))}
                          </div>
                        </TableCell>
                      ) : null}
                    </TableRow>
                  </ParentProvider>
                );
              })}
            </TableBody>
          </Table>
        </div>
      )}
      {paginationSlot.length > 0 ? (
        <div>
          <SlotRenderer slot="pagination" slots={slots} />
        </div>
      ) : null}
    </div>
  );

  return (
    <FilterStateContext.Provider value={{ values: filterValues, setValue: setFilterValue }}>
      {body}
    </FilterStateContext.Provider>
  );
}

function stripInternals(values: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(values)) {
    if (k.startsWith("__") && k.endsWith("__")) continue;
    out[k] = v;
  }
  return out;
}

function extractRows(data: unknown): Record<string, unknown>[] {
  if (Array.isArray(data)) return data as Record<string, unknown>[];
  if (data && typeof data === "object") {
    const obj = data as Record<string, unknown>;
    for (const v of Object.values(obj)) {
      if (Array.isArray(v)) return v as Record<string, unknown>[];
    }
  }
  return [];
}

function inferColumns(rows: Record<string, unknown>[]): string[] {
  if (rows.length === 0) return [];
  return Object.keys(rows[0]!).slice(0, 6);
}

function rowKey(row: Record<string, unknown>, i: number): string {
  if (typeof row.id === "string" || typeof row.id === "number") return String(row.id);
  if (typeof row.name === "string") return row.name;
  return `row-${i}`;
}

function renderCell(value: unknown): React.ReactNode {
  if (value == null) return <span className="text-muted-foreground">—</span>;
  if (typeof value === "boolean") return value ? "Yes" : "No";
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}

function RenderChild({ node }: { node: GraphNode }) {
  return <SlotRenderer slot="__row_action__" slots={{ __row_action__: [node] }} />;
}
