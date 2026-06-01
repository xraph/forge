import * as React from "react";
import {
  flexRender,
  getCoreRowModel,
  getExpandedRowModel,
  getFilteredRowModel,
  getGroupedRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  useReactTable,
  type ColumnDef,
  type ColumnPinningState,
  type GroupingState,
  type PaginationState,
  type Row,
  type SortingState,
  type Updater,
  type VisibilityState,
} from "@tanstack/react-table";
import {
  ArrowDown,
  ArrowUp,
  ChevronRight,
  Columns3,
  Download,
  Filter,
  MoreVertical,
  Pin,
  PinOff,
  Rows3,
} from "lucide-react";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Badge } from "@/components/ui/badge";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { GraphRenderer } from "../runtime/renderer";
import { Input } from "@/components/ui/input";
import { Search } from "lucide-react";
import { MoleculePagination } from "./molecule.pagination";
import { EmptyState } from "./molecule.empty-state";
import { Icon } from "./atom.icon";
import { useContractQuery } from "../contract/hooks";
import { useContributor, useParent, useRouteParams, ParentProvider } from "../runtime/context";
import { usePrincipalStore } from "../auth/principal";
import { LoadingNode, ErrorNode } from "../runtime/fallbacks";
import { evaluatePredicate, type Predicate } from "../runtime/predicate";
import { dispatchAction, type Action } from "../runtime/actions";
import { resolveValue } from "../runtime/bindings";
import { cn } from "@/lib/utils";
import type { GraphNode } from "@/contract/types";
import type { IntentComponentProps } from "../runtime/registry";

type Row_ = Record<string, unknown>;

interface ColumnSpec {
  id: string;
  field: string;
  header: string;
  type?: string;
  format?: string;
  width?: string;
  minWidth?: string;
  maxWidth?: string;
  sortable?: boolean;
  filterable?: boolean;
  hideable?: boolean;
  resizable?: boolean;
  pinned?: "" | "left" | "right";
  aggregation?: "sum" | "avg" | "min" | "max" | "count";
  align?: "start" | "center" | "end";
  truncate?: boolean;
  cell?: { intent: string; props?: Record<string, unknown> };
  filterType?: string;
  description?: string;
  hidden?: boolean;
}

interface SelectionConfig {
  mode: "none" | "single" | "multi";
  onChange?: Action;
}

interface SortingConfig {
  mode?: "client" | "server";
  initial?: { column: string; direction: "asc" | "desc" }[];
  multi?: boolean;
}

interface FilteringConfig {
  mode?: "client" | "server";
  global?: boolean;
  perCol?: boolean;
}

interface PaginationConfig {
  mode?: "client" | "server";
  pageSize?: number;
  pageSizeOptions?: number[];
  initialPage?: number;
}

interface GridRowAction {
  id: string;
  label: string;
  icon?: string;
  variant?: "default" | "destructive" | "secondary" | "outline" | "ghost";
  disabled?: Predicate;
  visible?: Predicate;
  action?: Action;
}

interface GridBulkAction {
  id: string;
  label: string;
  icon?: string;
  variant?: "default" | "destructive" | "secondary" | "outline" | "ghost";
  action?: Action;
}

interface ToolbarConfig {
  search?: boolean;
  columnPicker?: boolean;
  densityToggle?: boolean;
  exportButton?: boolean;
  refresh?: boolean;
  title?: string;
}

interface DataGridProps {
  columns: ColumnSpec[];
  rowId?: string;
  selection?: SelectionConfig;
  sorting?: SortingConfig;
  filtering?: FilteringConfig;
  pagination?: PaginationConfig;
  grouping?: { by?: string[] };
  pinning?: { left?: string[]; right?: string[] };
  virtualization?: { enabled?: boolean; overscan?: number };
  density?: "compact" | "normal" | "spacious";
  stickyHeader?: boolean;
  emptyState?: { icon?: string; title?: string; description?: string };
  rowActions?: GridRowAction[];
  bulkActions?: GridBulkAction[];
  toolbar?: ToolbarConfig;
  masterDetail?: { intent: string };
  onRowClick?: Action;
  height?: string;
  resizable?: boolean;
  className?: string;
}

const densityRowCls: Record<string, string> = {
  compact: "h-9 [&_td]:py-1 [&_th]:py-1",
  normal: "h-12",
  spacious: "h-14 [&_td]:py-3 [&_th]:py-3",
};

export function DataGrid({ node, props }: IntentComponentProps<unknown, DataGridProps>) {
  const contributor = useContributor();
  const parent = useParent();
  const route = useRouteParams();
  const principal = usePrincipalStore((s) => s.principal);

  const dataBinding = node.data;
  const dataIntent = dataBinding?.intent;

  // ---- Toolbar / control state ----
  const [globalFilter, setGlobalFilter] = React.useState("");
  const [sorting, setSorting] = React.useState<SortingState>(
    (props.sorting?.initial ?? []).map((r) => ({ id: r.column, desc: r.direction === "desc" })),
  );
  const [pagination, setPagination] = React.useState<PaginationState>({
    pageIndex: (props.pagination?.initialPage ?? 1) - 1,
    pageSize: props.pagination?.pageSize ?? 25,
  });
  const [rowSelection, setRowSelection] = React.useState<Record<string, boolean>>({});
  const [columnVisibility, setColumnVisibility] = React.useState<VisibilityState>(() => {
    const out: VisibilityState = {};
    for (const c of props.columns) if (c.hidden) out[c.id] = false;
    return out;
  });
  const [columnPinning, setColumnPinning] = React.useState<ColumnPinningState>({
    left: props.pinning?.left ?? props.columns.filter((c) => c.pinned === "left").map((c) => c.id),
    right: props.pinning?.right ?? props.columns.filter((c) => c.pinned === "right").map((c) => c.id),
  });
  const [grouping, setGrouping] = React.useState<GroupingState>(props.grouping?.by ?? []);
  const [density, setDensity] = React.useState<"compact" | "normal" | "spacious">(
    props.density ?? "normal",
  );
  const [expanded, setExpanded] = React.useState<Record<string, boolean>>({});

  // ---- Data fetch ----
  const dataParams = React.useMemo(() => {
    const out: Record<string, unknown> = {};
    if (dataBinding?.params) {
      for (const [k, v] of Object.entries(dataBinding.params)) {
        const resolved = resolveValue(v, { parent, route });
        if (resolved !== undefined) out[k] = resolved;
      }
    }
    // Server-mode: push state into params.
    if (props.pagination?.mode === "server") {
      out.page = pagination.pageIndex + 1;
      out.pageSize = pagination.pageSize;
    }
    if (props.sorting?.mode === "server" && sorting.length > 0) {
      out.sort = sorting.map((s) => ({ column: s.id, direction: s.desc ? "desc" : "asc" }));
    }
    if (props.filtering?.mode === "server" && globalFilter) {
      out.search = globalFilter;
    }
    return out;
  }, [dataBinding, parent, route, pagination, sorting, globalFilter, props.pagination?.mode, props.sorting?.mode, props.filtering?.mode]);

  const query = useContractQuery<unknown>(contributor, dataIntent ?? "", undefined, dataParams);
  const rows = extractRows(query.data);
  const serverTotal = extractTotal(query.data);

  // ---- Column defs ----
  const columnDefs = React.useMemo<ColumnDef<Row_>[]>(() => {
    const out: ColumnDef<Row_>[] = [];

    if (props.selection?.mode === "multi") {
      out.push({
        id: "__select__",
        size: 32,
        enableSorting: false,
        enableHiding: false,
        header: ({ table }) => (
          <Checkbox
            checked={table.getIsAllPageRowsSelected()}
            indeterminate={table.getIsSomePageRowsSelected() && !table.getIsAllPageRowsSelected()}
            onCheckedChange={(c) => table.toggleAllPageRowsSelected(!!c)}
            aria-label="Select all"
          />
        ),
        cell: ({ row }) => (
          <Checkbox
            checked={row.getIsSelected()}
            onCheckedChange={(c) => row.toggleSelected(!!c)}
            onClick={(e) => e.stopPropagation()}
            aria-label="Select row"
          />
        ),
      });
    } else if (props.selection?.mode === "single") {
      out.push({
        id: "__select__",
        size: 32,
        enableSorting: false,
        enableHiding: false,
        header: () => null,
        cell: ({ row, table }) => (
          <input
            type="radio"
            checked={row.getIsSelected()}
            onChange={() => {
              table.resetRowSelection();
              row.toggleSelected(true);
            }}
            onClick={(e) => e.stopPropagation()}
          />
        ),
      });
    }

    if (props.masterDetail) {
      out.push({
        id: "__expand__",
        size: 32,
        enableSorting: false,
        enableHiding: false,
        header: () => null,
        cell: ({ row }) => (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              setExpanded((s) => ({ ...s, [row.id]: !s[row.id] }));
            }}
            className={cn(
              "inline-flex h-6 w-6 items-center justify-center rounded transition-transform",
              expanded[row.id] && "rotate-90",
            )}
            aria-label={expanded[row.id] ? "Collapse" : "Expand"}
          >
            <ChevronRight className="h-4 w-4 text-muted-foreground" />
          </button>
        ),
      });
    }

    for (const c of props.columns) {
      const def: ColumnDef<Row_> = {
        id: c.id,
        accessorFn: (row) => readPath(row, c.field),
        header: c.header,
        enableSorting: c.sortable ?? false,
        enableHiding: c.hideable ?? true,
        enableColumnFilter: c.filterable ?? false,
        meta: { spec: c },
        cell: ({ getValue, row }) => renderCell(c, getValue() as unknown, row),
      };
      out.push(def);
    }

    if (props.rowActions && props.rowActions.length > 0) {
      out.push({
        id: "__actions__",
        size: 48,
        enableSorting: false,
        enableHiding: false,
        header: () => null,
        cell: ({ row }) => <RowActionsMenu row={row.original} actions={props.rowActions!} ctx={{ parent, route, session: { user: principal } }} />,
      });
    }

    return out;
  }, [props.columns, props.selection, props.masterDetail, props.rowActions, parent, route, principal, expanded]);

  // ---- Table instance ----
  const table = useReactTable({
    data: rows,
    columns: columnDefs,
    state: {
      sorting,
      globalFilter,
      pagination,
      rowSelection,
      columnVisibility,
      columnPinning,
      grouping,
    },
    getRowId: (row, i) => {
      const k = props.rowId ?? "id";
      const v = (row as Row_)[k];
      return v === undefined || v === null ? String(i) : String(v);
    },
    enableRowSelection: props.selection?.mode !== "none",
    enableMultiRowSelection: props.selection?.mode === "multi",
    manualPagination: props.pagination?.mode === "server",
    pageCount: props.pagination?.mode === "server" && serverTotal !== undefined
      ? Math.max(1, Math.ceil(serverTotal / pagination.pageSize))
      : undefined,
    manualSorting: props.sorting?.mode === "server",
    manualFiltering: props.filtering?.mode === "server",
    onSortingChange: setSorting,
    onGlobalFilterChange: setGlobalFilter,
    onPaginationChange: setPagination,
    onRowSelectionChange: (updater: Updater<Record<string, boolean>>) => {
      setRowSelection((prev) => (typeof updater === "function" ? updater(prev) : updater));
      if (props.selection?.onChange) {
        const next = typeof updater === "function" ? updater(rowSelection) : updater;
        dispatchAction(props.selection.onChange, { value: Object.keys(next).filter((k) => next[k]) });
      }
    },
    onColumnVisibilityChange: setColumnVisibility,
    onColumnPinningChange: setColumnPinning,
    onGroupingChange: setGrouping,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: props.sorting?.mode === "server" ? undefined : getSortedRowModel(),
    getFilteredRowModel: props.filtering?.mode === "server" ? undefined : getFilteredRowModel(),
    getPaginationRowModel: props.pagination?.mode === "server" ? undefined : getPaginationRowModel(),
    getGroupedRowModel: getGroupedRowModel(),
    getExpandedRowModel: getExpandedRowModel(),
  });

  const selectedIds = Object.keys(rowSelection).filter((k) => rowSelection[k]);
  const showBulk = (props.bulkActions ?? []).length > 0 && selectedIds.length > 0;

  if (!dataIntent && !dataBinding?.queryRef) {
    return <ErrorNode message="DataGrid requires a data binding (node.data)" />;
  }
  if (query.isLoading) return <LoadingNode />;
  if (query.error) return <ErrorNode message={(query.error as Error).message} />;

  // ---- Render ----
  const totalRows = props.pagination?.mode === "server" && serverTotal !== undefined ? serverTotal : rows.length;
  const rowsToRender = table.getRowModel().rows;

  if (rowsToRender.length === 0) {
    return (
      <div className={cn("space-y-3", props.className)}>
        <Toolbar
          props={props}
          globalFilter={globalFilter}
          onSearch={setGlobalFilter}
          density={density}
          setDensity={setDensity}
          table={table}
        />
        <EmptyState
          node={{} as never}
          slots={{}}
          props={{
            icon: props.emptyState?.icon ?? "inbox",
            title: props.emptyState?.title ?? "No results",
            description: props.emptyState?.description,
          }}
        />
      </div>
    );
  }

  return (
    <div className={cn("space-y-3", props.className)}>
      <Toolbar
        props={props}
        globalFilter={globalFilter}
        onSearch={setGlobalFilter}
        density={density}
        setDensity={setDensity}
        table={table}
      />

      {showBulk ? (
        <BulkBar
          count={selectedIds.length}
          actions={props.bulkActions!}
          onClear={() => setRowSelection({})}
          ctx={{ ids: selectedIds, parent, route, session: { user: principal } }}
        />
      ) : null}

      <div
        className={cn("relative overflow-auto rounded-lg border bg-card")}
        style={{ height: props.height }}
      >
        <Table className={cn(densityRowCls[density])}>
          <TableHeader className={cn(props.stickyHeader && "sticky top-0 z-10 bg-card")}>
            {table.getHeaderGroups().map((hg) => (
              <TableRow key={hg.id}>
                {hg.headers.map((h) => {
                  const meta = (h.column.columnDef.meta as { spec?: ColumnSpec } | undefined) ?? {};
                  const spec = meta.spec;
                  const isPinnedLeft = h.column.getIsPinned() === "left";
                  const isPinnedRight = h.column.getIsPinned() === "right";
                  return (
                    <TableHead
                      key={h.id}
                      style={{ width: spec?.width, minWidth: spec?.minWidth, maxWidth: spec?.maxWidth }}
                      className={cn(
                        spec?.align === "end" && "text-right",
                        spec?.align === "center" && "text-center",
                        (isPinnedLeft || isPinnedRight) && "sticky z-10 bg-card shadow-[inset_-1px_0_0_hsl(var(--border))]",
                        isPinnedLeft && "left-0",
                        isPinnedRight && "right-0",
                      )}
                    >
                      {h.isPlaceholder ? null : (
                        <div className="flex items-center justify-between gap-2">
                          {h.column.getCanSort() ? (
                            <button
                              type="button"
                              onClick={h.column.getToggleSortingHandler()}
                              className="-ml-2 inline-flex h-7 items-center gap-1 rounded px-2 hover:bg-muted/50"
                            >
                              {flexRender(h.column.columnDef.header, h.getContext())}
                              {h.column.getIsSorted() === "asc" ? (
                                <ArrowUp className="h-3 w-3" />
                              ) : h.column.getIsSorted() === "desc" ? (
                                <ArrowDown className="h-3 w-3" />
                              ) : null}
                            </button>
                          ) : (
                            flexRender(h.column.columnDef.header, h.getContext())
                          )}
                          {spec?.filterable || spec?.aggregation ? (
                            <DropdownMenu>
                              <DropdownMenuTrigger asChild>
                                <Button variant="ghost" size="icon" className="h-6 w-6 -mr-1">
                                  <Filter className="h-3 w-3 opacity-50" />
                                </Button>
                              </DropdownMenuTrigger>
                              <DropdownMenuContent align="start">
                                {h.column.getCanPin() ? (
                                  <>
                                    <DropdownMenuItem onClick={() => h.column.pin("left")}>
                                      <Pin className="mr-2 h-3 w-3" /> Pin left
                                    </DropdownMenuItem>
                                    <DropdownMenuItem onClick={() => h.column.pin("right")}>
                                      <Pin className="mr-2 h-3 w-3" /> Pin right
                                    </DropdownMenuItem>
                                    <DropdownMenuItem onClick={() => h.column.pin(false)}>
                                      <PinOff className="mr-2 h-3 w-3" /> Unpin
                                    </DropdownMenuItem>
                                    <DropdownMenuSeparator />
                                  </>
                                ) : null}
                                {h.column.getCanHide() ? (
                                  <DropdownMenuItem onClick={() => h.column.toggleVisibility(false)}>
                                    Hide column
                                  </DropdownMenuItem>
                                ) : null}
                              </DropdownMenuContent>
                            </DropdownMenu>
                          ) : null}
                        </div>
                      )}
                    </TableHead>
                  );
                })}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {rowsToRender.map((row) => (
              <React.Fragment key={row.id}>
                <ParentProvider value={row.original as Record<string, unknown>}>
                  <TableRow
                    data-state={row.getIsSelected() ? "selected" : undefined}
                    className={cn(
                      props.onRowClick && "cursor-pointer",
                      row.getIsSelected() && "bg-muted/50",
                    )}
                    onClick={() => props.onRowClick && dispatchAction(props.onRowClick, { value: row.original })}
                  >
                    {row.getVisibleCells().map((cell) => {
                      const meta = (cell.column.columnDef.meta as { spec?: ColumnSpec } | undefined) ?? {};
                      const spec = meta.spec;
                      const isPinnedLeft = cell.column.getIsPinned() === "left";
                      const isPinnedRight = cell.column.getIsPinned() === "right";
                      return (
                        <TableCell
                          key={cell.id}
                          className={cn(
                            spec?.align === "end" && "text-right",
                            spec?.align === "center" && "text-center",
                            spec?.truncate && "max-w-0 truncate",
                            (isPinnedLeft || isPinnedRight) && "sticky z-[1] bg-card shadow-[inset_-1px_0_0_hsl(var(--border))]",
                            isPinnedLeft && "left-0",
                            isPinnedRight && "right-0",
                          )}
                        >
                          {flexRender(cell.column.columnDef.cell, cell.getContext())}
                        </TableCell>
                      );
                    })}
                  </TableRow>
                  {props.masterDetail && expanded[row.id] ? (
                    <TableRow>
                      <TableCell colSpan={row.getVisibleCells().length} className="bg-muted/30 p-4">
                        <GraphRenderer
                          node={{ intent: props.masterDetail.intent, props: { row: row.original } } as GraphNode}
                        />
                      </TableCell>
                    </TableRow>
                  ) : null}
                </ParentProvider>
              </React.Fragment>
            ))}
          </TableBody>
        </Table>
      </div>

      <MoleculePagination
        node={{} as never}
        slots={{}}
        props={{
          page: pagination.pageIndex + 1,
          pageSize: pagination.pageSize,
          total: totalRows,
          pageSizeOptions: props.pagination?.pageSizeOptions ?? [10, 25, 50, 100],
          showPageSize: true,
          showFirstLast: true,
          onPageChange: undefined,
          onPageSizeChange: undefined,
        }}
      />
    </div>
  );
}

// ---- Helpers ----

function extractRows(data: unknown): Row_[] {
  if (Array.isArray(data)) return data as Row_[];
  if (data && typeof data === "object") {
    const obj = data as Record<string, unknown>;
    if (Array.isArray(obj.items)) return obj.items as Row_[];
    if (Array.isArray(obj.rows)) return obj.rows as Row_[];
    if (Array.isArray(obj.data)) return obj.data as Row_[];
    for (const v of Object.values(obj)) {
      if (Array.isArray(v)) return v as Row_[];
    }
  }
  return [];
}

function extractTotal(data: unknown): number | undefined {
  if (!data || typeof data !== "object") return undefined;
  const obj = data as Record<string, unknown>;
  for (const k of ["total", "totalCount", "count"]) {
    const v = obj[k];
    if (typeof v === "number") return v;
  }
  return undefined;
}

function readPath(row: Row_, path: string): unknown {
  if (!path.includes(".")) return row[path];
  let cur: unknown = row;
  for (const seg of path.split(".")) {
    if (cur == null || typeof cur !== "object") return undefined;
    cur = (cur as Row_)[seg];
  }
  return cur;
}

function renderCell(col: ColumnSpec, value: unknown, row: Row<Row_>): React.ReactNode {
  if (col.cell?.intent) {
    return (
      <GraphRenderer
        node={{
          intent: col.cell.intent,
          props: { ...(col.cell.props ?? {}), value, row: row.original, column: col.id },
        } as GraphNode}
      />
    );
  }
  if (value == null) return <span className="text-muted-foreground">—</span>;

  switch (col.type) {
    case "boolean":
      return value ? "Yes" : "No";
    case "number":
      return <span className="tabular-nums">{formatNumber(value, col.format)}</span>;
    case "currency":
      return <span className="tabular-nums">{formatCurrency(value, col.format ?? "USD")}</span>;
    case "date":
      return <span>{formatDate(value, false)}</span>;
    case "datetime":
      return <span>{formatDate(value, true)}</span>;
    case "badge":
      return <Badge variant="secondary">{String(value)}</Badge>;
    case "avatar": {
      const v = value as { src?: string; alt?: string; fallback?: string } | string;
      const isObj = typeof v === "object" && v !== null;
      return (
        <Avatar className="h-7 w-7">
          {isObj && v.src ? <AvatarImage src={v.src} alt={v.alt} /> : null}
          <AvatarFallback>
            {(isObj ? v.fallback ?? v.alt ?? "" : String(v))
              .slice(0, 2)
              .toUpperCase()}
          </AvatarFallback>
        </Avatar>
      );
    }
    case "link":
      return (
        <a className="text-primary underline-offset-4 hover:underline" href={String(value)}>
          {String(value)}
        </a>
      );
    default:
      if (typeof value === "object") return <span className="font-mono text-xs">{JSON.stringify(value)}</span>;
      return String(value);
  }
}

function formatNumber(v: unknown, fmt?: string): string {
  if (typeof v !== "number") return String(v);
  if (!fmt) return v.toLocaleString();
  const digits = Number.parseInt(fmt, 10);
  return Number.isFinite(digits) ? v.toFixed(digits) : v.toLocaleString();
}

function formatCurrency(v: unknown, currency: string): string {
  if (typeof v !== "number") return String(v);
  try {
    return new Intl.NumberFormat(undefined, { style: "currency", currency }).format(v);
  } catch {
    return v.toLocaleString();
  }
}

function formatDate(v: unknown, withTime: boolean): string {
  if (!v) return "";
  const d = typeof v === "number" ? new Date(v) : new Date(String(v));
  if (Number.isNaN(d.getTime())) return String(v);
  return withTime ? d.toLocaleString() : d.toLocaleDateString();
}

// ---- Sub-components ----

function Toolbar({
  props,
  globalFilter,
  onSearch,
  density,
  setDensity,
  table,
}: {
  props: DataGridProps;
  globalFilter: string;
  onSearch: (v: string) => void;
  density: "compact" | "normal" | "spacious";
  setDensity: (d: "compact" | "normal" | "spacious") => void;
  table: ReturnType<typeof useReactTable<Row_>>;
}) {
  const tc = props.toolbar;
  if (!tc) return null;
  return (
    <div className="flex flex-wrap items-center justify-between gap-2">
      <div className="flex flex-1 items-center gap-2">
        {tc.title ? <h3 className="text-sm font-semibold">{tc.title}</h3> : null}
        {tc.search ? (
          <div className="relative max-w-sm flex-1">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              type="search"
              placeholder="Search rows…"
              value={globalFilter}
              onChange={(e) => onSearch(e.target.value)}
              className="pl-9"
            />
          </div>
        ) : null}
      </div>
      <div className="flex items-center gap-1">
        {tc.densityToggle ? (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="icon" aria-label="Density">
                <Rows3 className="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuLabel>Density</DropdownMenuLabel>
              {(["compact", "normal", "spacious"] as const).map((d) => (
                <DropdownMenuCheckboxItem
                  key={d}
                  checked={density === d}
                  onCheckedChange={() => setDensity(d)}
                >
                  {d[0]!.toUpperCase() + d.slice(1)}
                </DropdownMenuCheckboxItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        ) : null}
        {tc.columnPicker ? (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="icon" aria-label="Columns">
                <Columns3 className="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="max-h-80 overflow-y-auto">
              <DropdownMenuLabel>Columns</DropdownMenuLabel>
              {table
                .getAllLeafColumns()
                .filter((c) => c.getCanHide())
                .map((c) => (
                  <DropdownMenuCheckboxItem
                    key={c.id}
                    checked={c.getIsVisible()}
                    onCheckedChange={(v) => c.toggleVisibility(!!v)}
                  >
                    {(c.columnDef.header as string) ?? c.id}
                  </DropdownMenuCheckboxItem>
                ))}
            </DropdownMenuContent>
          </DropdownMenu>
        ) : null}
        {tc.exportButton ? (
          <Button variant="outline" size="icon" aria-label="Export" onClick={() => exportCSV(table)}>
            <Download className="h-4 w-4" />
          </Button>
        ) : null}
      </div>
    </div>
  );
}

function BulkBar({
  count,
  actions,
  onClear,
  ctx,
}: {
  count: number;
  actions: GridBulkAction[];
  onClear: () => void;
  ctx: { ids: string[]; parent: unknown; route: Record<string, string>; session: unknown };
}) {
  return (
    <div className="flex items-center justify-between rounded-md border bg-muted/50 px-3 py-2 text-sm">
      <div>
        <strong>{count}</strong> selected
        <Button variant="link" size="sm" onClick={onClear} className="ml-2 h-auto p-0">
          Clear
        </Button>
      </div>
      <div className="flex items-center gap-2">
        {actions.map((a) => (
          <Button
            key={a.id}
            variant={(a.variant as never) ?? "outline"}
            size="sm"
            onClick={() => a.action && dispatchAction(a.action, { value: ctx.ids })}
          >
            {a.icon ? (
              <Icon node={{} as never} slots={{}} props={{ name: a.icon, size: 14 }} />
            ) : null}
            {a.label}
          </Button>
        ))}
      </div>
    </div>
  );
}

function RowActionsMenu({
  row,
  actions,
  ctx,
}: {
  row: Row_;
  actions: GridRowAction[];
  ctx: { parent: unknown; route: Record<string, string>; session: { user: unknown } };
}) {
  const visible = actions.filter((a) =>
    !a.visible
      ? true
      : evaluatePredicate(a.visible, { form: row, parent: ctx.parent, route: ctx.route, session: ctx.session as { user: import("@/contract/types").Principal | null } }),
  );
  if (visible.length === 0) return null;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" onClick={(e) => e.stopPropagation()}>
          <MoreVertical className="h-4 w-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {visible.map((a) => {
          const disabled = a.disabled
            ? evaluatePredicate(a.disabled, { form: row, parent: ctx.parent, route: ctx.route, session: ctx.session as { user: import("@/contract/types").Principal | null } })
            : false;
          return (
            <DropdownMenuItem
              key={a.id}
              disabled={disabled}
              className={a.variant === "destructive" ? "text-destructive focus:text-destructive" : ""}
              onClick={(e) => {
                e.stopPropagation();
                if (a.action) dispatchAction(a.action, { value: row });
              }}
            >
              {a.icon ? (
                <Icon node={{} as never} slots={{}} props={{ name: a.icon, size: 14 }} />
              ) : null}
              <span className={cn(a.icon && "ml-2")}>{a.label}</span>
            </DropdownMenuItem>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function exportCSV(table: ReturnType<typeof useReactTable<Row_>>) {
  const cols = table.getAllLeafColumns().filter((c) => c.getIsVisible() && !c.id.startsWith("__"));
  const header = cols.map((c) => (c.columnDef.header as string) ?? c.id);
  const lines = [header.map(csvCell).join(",")];
  for (const row of table.getRowModel().rows) {
    const cells = cols.map((c) => csvCell(row.getValue(c.id)));
    lines.push(cells.join(","));
  }
  const blob = new Blob([lines.join("\n")], { type: "text/csv;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = "data-grid.csv";
  a.click();
  URL.revokeObjectURL(url);
}

function csvCell(v: unknown): string {
  if (v == null) return "";
  const s = typeof v === "object" ? JSON.stringify(v) : String(v);
  if (/[",\n]/.test(s)) return `"${s.replace(/"/g, '""')}"`;
  return s;
}
