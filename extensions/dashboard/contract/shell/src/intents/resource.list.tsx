import * as React from "react";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { useContractQuery } from "../contract/hooks";
import { useContributor, ParentProvider } from "../runtime/context";
import { resolveValue } from "../runtime/bindings";
import { LoadingNode, ErrorNode } from "../runtime/fallbacks";
import { SlotRenderer } from "../runtime/slots";
import type { GraphNode } from "@/contract/types";
import type { IntentComponentProps } from "../runtime/registry";

interface ResourceListProps {
  columns?: string[];
  emptyMessage?: string;
  detailTitle?: string;
}

/**
 * resource.list fetches a list of records via node.data and renders them in a
 * Table. Each row exposes its data as parent context to the rowActions slot
 * (rendered in the last column) and, when clicked, to the detailDrawer slot
 * (rendered in a Sheet).
 */
export function ResourceList({
  node,
  props,
  slots,
}: IntentComponentProps<unknown, ResourceListProps>) {
  const contributor = useContributor();
  const dataIntent = node.data?.intent;
  const dataParams = React.useMemo(() => {
    const out: Record<string, unknown> = {};
    const params = node.data?.params as Record<string, unknown> | undefined;
    if (!params) return out;
    for (const [k, v] of Object.entries(params)) {
      const resolved = resolveValue(v, {});
      if (resolved !== undefined) out[k] = resolved;
    }
    return out;
  }, [node.data?.params]);

  const query = useContractQuery<unknown>(contributor, dataIntent ?? "", undefined, dataParams);
  const [selected, setSelected] = React.useState<Record<string, unknown> | null>(null);

  if (!dataIntent) {
    return <ErrorNode message="resource.list requires a data binding" />;
  }
  if (query.isLoading) return <LoadingNode />;
  if (query.error) return <ErrorNode message={(query.error as Error).message} />;

  const rows = extractRows(query.data);
  const columns = props.columns ?? inferColumns(rows);
  const rowActions = slots["rowActions"] ?? [];
  const detailDrawer = slots["detailDrawer"] ?? [];

  if (rows.length === 0) {
    return (
      <div className="rounded-lg border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
        {props.emptyMessage ?? "No records yet."}
      </div>
    );
  }

  return (
    <div className="rounded-lg border border-border bg-card">
      <Table>
        <TableHeader>
          <TableRow>
            {columns.map((c) => (
              <TableHead key={c} className="font-semibold">
                {c}
              </TableHead>
            ))}
            {rowActions.length > 0 ? <TableHead className="w-24 text-right">Actions</TableHead> : null}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row, i) => (
            <ParentProvider key={rowKey(row, i)} value={row}>
              <TableRow
                className={detailDrawer.length > 0 ? "cursor-pointer" : ""}
                onClick={() => {
                  if (detailDrawer.length > 0) setSelected(row);
                }}
              >
                {columns.map((c) => (
                  <TableCell key={c}>{renderCell(row[c])}</TableCell>
                ))}
                {rowActions.length > 0 ? (
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-2">
                      {rowActions.map((child, j) => (
                        <RenderChild key={`${child.intent}:${j}`} node={child} />
                      ))}
                    </div>
                  </TableCell>
                ) : null}
              </TableRow>
            </ParentProvider>
          ))}
        </TableBody>
      </Table>

      {detailDrawer.length > 0 ? (
        <Sheet open={selected != null} onOpenChange={(open) => !open && setSelected(null)}>
          <SheetContent side="right">
            <SheetHeader>
              <SheetTitle>{props.detailTitle ?? "Details"}</SheetTitle>
            </SheetHeader>
            {selected ? (
              <ParentProvider value={selected}>
                <div className="mt-6">
                  <SlotRenderer slot="detailDrawer" slots={slots} />
                </div>
              </ParentProvider>
            ) : null}
          </SheetContent>
        </Sheet>
      ) : null}
    </div>
  );
}

// extractRows handles three common shapes the contract may return:
//   - array of records
//   - { items: T[] }
//   - { <intentName>: T[] } (e.g., { users: [...] })
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

// Dynamic dispatch via the registry — kept as a thin wrapper to avoid an
// import cycle with the renderer.
function RenderChild({ node }: { node: GraphNode }) {
  // We intentionally don't import GraphRenderer here to avoid a circular
  // import; instead we re-use the renderer via a deferred-import pattern.
  // For slice (e), the slot system already imports renderer through slots.tsx,
  // so we use SlotRenderer with a synthetic single-slot wrapper.
  return <SlotRenderer slot="__row_action__" slots={{ __row_action__: [node] }} />;
}
