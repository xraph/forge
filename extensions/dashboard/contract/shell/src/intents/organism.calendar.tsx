import * as React from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useContractQuery } from "../contract/hooks";
import { useContributor, useParent, useRouteParams } from "../runtime/context";
import { LoadingNode, ErrorNode } from "../runtime/fallbacks";
import { resolveValue } from "../runtime/bindings";
import { dispatchAction, type Action } from "../runtime/actions";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface CalendarProps {
  view?: "month" | "week" | "day";
  titleField?: string;
  startField?: string;
  endField?: string;
  colorField?: string;
  onEventClick?: Action;
  onCreate?: Action;
  className?: string;
}

export function Calendar({ node, props }: IntentComponentProps<unknown, CalendarProps>) {
  const contributor = useContributor();
  const parent = useParent();
  const route = useRouteParams();
  const titleField = props.titleField ?? "title";
  const startField = props.startField ?? "start";

  const [cursor, setCursor] = React.useState(() => firstOfMonth(new Date()));

  const dataParams = React.useMemo(() => {
    const out: Record<string, unknown> = {};
    if (node.data?.params) {
      for (const [k, v] of Object.entries(node.data.params)) {
        const r = resolveValue(v, { parent, route });
        if (r !== undefined) out[k] = r;
      }
    }
    out.month = cursor.toISOString();
    return out;
  }, [node.data?.params, parent, route, cursor]);

  const query = useContractQuery<unknown>(contributor, node.data?.intent ?? "", undefined, dataParams);

  if (!node.data?.intent && !node.data?.queryRef) {
    return <ErrorNode message="Calendar requires a data binding" />;
  }
  if (query.isLoading) return <LoadingNode />;
  if (query.error) return <ErrorNode message={(query.error as Error).message} />;

  const events = extractEvents(query.data);
  const eventsByDay = groupByDay(events, startField);

  const monthName = cursor.toLocaleString(undefined, { month: "long", year: "numeric" });
  const weeks = monthWeeks(cursor);

  return (
    <div className={cn("space-y-3", props.className)}>
      <div className="flex items-center justify-between">
        <h3 className="text-base font-semibold">{monthName}</h3>
        <div className="flex items-center gap-1">
          <Button variant="outline" size="icon" onClick={() => setCursor(addMonths(cursor, -1))}>
            <ChevronLeft className="h-4 w-4" />
          </Button>
          <Button variant="outline" size="sm" onClick={() => setCursor(firstOfMonth(new Date()))}>
            Today
          </Button>
          <Button variant="outline" size="icon" onClick={() => setCursor(addMonths(cursor, 1))}>
            <ChevronRight className="h-4 w-4" />
          </Button>
        </div>
      </div>
      <div className="grid grid-cols-7 gap-px rounded-md border bg-border">
        {["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"].map((d) => (
          <div key={d} className="bg-card px-2 py-1 text-center text-xs font-semibold text-muted-foreground">
            {d}
          </div>
        ))}
        {weeks.flat().map((day, i) => {
          const isCurrentMonth = day.getMonth() === cursor.getMonth();
          const isToday = sameDate(day, new Date());
          const list = eventsByDay.get(dateKey(day)) ?? [];
          return (
            <button
              key={i}
              type="button"
              onClick={() => props.onCreate && dispatchAction(props.onCreate, { value: day.toISOString() })}
              className={cn(
                "flex min-h-[88px] flex-col gap-1 bg-card p-1 text-left",
                !isCurrentMonth && "bg-muted/30 text-muted-foreground",
                "hover:bg-accent/30",
              )}
            >
              <span
                className={cn(
                  "ml-auto inline-flex h-5 w-5 items-center justify-center rounded-full text-xs",
                  isToday && "bg-primary text-primary-foreground",
                )}
              >
                {day.getDate()}
              </span>
              <div className="flex flex-col gap-0.5">
                {list.slice(0, 3).map((ev, j) => (
                  <button
                    key={j}
                    type="button"
                    onClick={(e) => {
                      e.stopPropagation();
                      if (props.onEventClick) dispatchAction(props.onEventClick, { value: ev });
                    }}
                    className="truncate rounded bg-primary/15 px-1 py-0.5 text-left text-xs hover:bg-primary/25"
                  >
                    {String(ev[titleField] ?? "Untitled")}
                  </button>
                ))}
                {list.length > 3 ? (
                  <span className="text-xs text-muted-foreground">+{list.length - 3} more</span>
                ) : null}
              </div>
            </button>
          );
        })}
      </div>
    </div>
  );
}

function firstOfMonth(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth(), 1);
}
function addMonths(d: Date, n: number): Date {
  return new Date(d.getFullYear(), d.getMonth() + n, 1);
}
function monthWeeks(cursor: Date): Date[][] {
  const first = firstOfMonth(cursor);
  // Week starts Monday; getDay returns 0=Sun..6=Sat.
  const offset = (first.getDay() + 6) % 7;
  const start = new Date(first);
  start.setDate(first.getDate() - offset);
  const weeks: Date[][] = [];
  for (let w = 0; w < 6; w++) {
    const row: Date[] = [];
    for (let d = 0; d < 7; d++) {
      const day = new Date(start);
      day.setDate(start.getDate() + w * 7 + d);
      row.push(day);
    }
    weeks.push(row);
    if (w >= 4 && weeks[w]![6]!.getMonth() !== cursor.getMonth()) break;
  }
  return weeks;
}
function sameDate(a: Date, b: Date): boolean {
  return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();
}
function dateKey(d: Date): string {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}
function pad(n: number): string {
  return String(n).padStart(2, "0");
}

function extractEvents(data: unknown): Record<string, unknown>[] {
  if (Array.isArray(data)) return data as Record<string, unknown>[];
  if (data && typeof data === "object") {
    const obj = data as Record<string, unknown>;
    for (const k of ["events", "items", "data"]) {
      if (Array.isArray(obj[k])) return obj[k] as Record<string, unknown>[];
    }
  }
  return [];
}

function groupByDay(events: Record<string, unknown>[], startField: string): Map<string, Record<string, unknown>[]> {
  const out = new Map<string, Record<string, unknown>[]>();
  for (const ev of events) {
    const raw = ev[startField];
    if (!raw) continue;
    const d = new Date(String(raw));
    if (Number.isNaN(d.getTime())) continue;
    const key = dateKey(d);
    if (!out.has(key)) out.set(key, []);
    out.get(key)!.push(ev);
  }
  return out;
}
