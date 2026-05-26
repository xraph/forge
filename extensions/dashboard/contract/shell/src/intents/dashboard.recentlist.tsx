import * as React from "react";
import { useNavigate } from "react-router-dom";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useContractQuery } from "../contract/hooks";
import { useContributor } from "../runtime/context";
import { LoadingNode, ErrorNode } from "../runtime/fallbacks";
import { titleCase, cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface ColumnSpec {
  field: string;
  label?: string;
}

interface DashboardRecentListProps {
  title?: string;
  columns?: (string | ColumnSpec)[];
  /** Max rows to render. Defaults to 5. */
  limit?: number;
  /** If set, renders a "View all" link in the header pointing here. */
  viewAllHref?: string;
  /** Optional row-click target template (`/users/:id` etc.). */
  detailHref?: string;
  emptyMessage?: string;
  className?: string;
}

/**
 * dashboard.recentlist is a compact list widget for the overview page.
 * It binds to a list query and renders the top N rows in a borderless
 * mini-table inside a Card. Used in dashboard.grid.widgets to surface
 * "Recent signups", "Recent activity", etc., alongside stat tiles.
 */
export function DashboardRecentList({
  node,
  props,
}: IntentComponentProps<unknown, DashboardRecentListProps>) {
  const contributor = useContributor();
  const navigate = useNavigate();
  const dataIntent = node.data?.intent ?? "";
  const query = useContractQuery<unknown>(contributor, dataIntent);

  if (!dataIntent) {
    return <ErrorNode message="dashboard.recentlist requires a data binding" />;
  }
  if (query.isLoading) return <LoadingNode />;
  if (query.error) return <ErrorNode message={(query.error as Error).message} />;

  const rows = extractRows(query.data).slice(0, props.limit ?? 5);
  const columns = resolveColumns(props.columns ?? inferColumns(rows));
  const title = props.title ?? node.title ?? "Recent";

  return (
    <Card className={cn("col-span-full", props.className)}>
      <CardHeader className="flex flex-row items-center justify-between">
        <CardTitle className="text-base font-semibold">{title}</CardTitle>
        {props.viewAllHref ? (
          <Button variant="ghost" size="sm" onClick={() => navigate(props.viewAllHref!)}>
            View all
          </Button>
        ) : null}
      </CardHeader>
      <CardContent>
        {rows.length === 0 ? (
          <p className="py-6 text-center text-sm text-muted-foreground">
            {props.emptyMessage ?? "Nothing to show yet."}
          </p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                {columns.map((c) => (
                  <TableHead key={c.field}>{c.label}</TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((row, i) => {
                const href = hrefForRow(row, props.detailHref);
                return (
                  <TableRow
                    key={rowKey(row, i)}
                    className={cn(href ? "cursor-pointer" : "")}
                    onClick={() => {
                      if (href) navigate(href);
                    }}
                  >
                    {columns.map((c) => (
                      <TableCell key={c.field}>{renderCell(row[c.field])}</TableCell>
                    ))}
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
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
  return Object.keys(rows[0]!).slice(0, 4);
}

function resolveColumns(specs: (string | ColumnSpec)[]): { field: string; label: string }[] {
  return specs.map((c) =>
    typeof c === "string"
      ? { field: c, label: titleCase(c) }
      : { field: c.field, label: c.label ?? titleCase(c.field) },
  );
}

function rowKey(row: Record<string, unknown>, i: number): string {
  if (typeof row.id === "string" || typeof row.id === "number") return String(row.id);
  return `row-${i}`;
}

function hrefForRow(row: Record<string, unknown>, template: string | undefined): string | null {
  if (!template) return null;
  let aborted = false;
  const out = template.replace(/:(\w+)/g, (_, field: string) => {
    const v = row[field];
    if (v === undefined || v === null || v === "") {
      aborted = true;
      return "";
    }
    return encodeURIComponent(String(v));
  });
  return aborted ? null : out;
}

function renderCell(value: unknown): React.ReactNode {
  if (value == null) return <span className="text-muted-foreground">—</span>;
  if (typeof value === "boolean") return value ? "Yes" : "No";
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}
