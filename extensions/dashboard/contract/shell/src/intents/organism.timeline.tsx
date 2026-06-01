import * as React from "react";
import { GraphRenderer } from "../runtime/renderer";
import { useContractQuery } from "../contract/hooks";
import { useContributor, useParent, useRouteParams } from "../runtime/context";
import { LoadingNode, ErrorNode } from "../runtime/fallbacks";
import { Icon } from "./atom.icon";
import { resolveValue } from "../runtime/bindings";
import { cn } from "@/lib/utils";
import type { GraphNode } from "@/contract/types";
import type { IntentComponentProps } from "../runtime/registry";

interface TimelineProps {
  titleField?: string;
  descriptionField?: string;
  timestampField?: string;
  iconField?: string;
  groupBy?: string;
  renderItem?: string;
  className?: string;
}

export function Timeline({ node, props }: IntentComponentProps<unknown, TimelineProps>) {
  const contributor = useContributor();
  const parent = useParent();
  const route = useRouteParams();
  const titleField = props.titleField ?? "title";
  const descField = props.descriptionField ?? "description";
  const tsField = props.timestampField ?? "timestamp";

  const dataParams = React.useMemo(() => {
    const out: Record<string, unknown> = {};
    if (node.data?.params) {
      for (const [k, v] of Object.entries(node.data.params)) {
        const r = resolveValue(v, { parent, route });
        if (r !== undefined) out[k] = r;
      }
    }
    return out;
  }, [node.data?.params, parent, route]);

  const query = useContractQuery<unknown>(contributor, node.data?.intent ?? "", undefined, dataParams);

  if (!node.data?.intent && !node.data?.queryRef) {
    return <ErrorNode message="Timeline requires a data binding" />;
  }
  if (query.isLoading) return <LoadingNode />;
  if (query.error) return <ErrorNode message={(query.error as Error).message} />;

  const items = extractItems(query.data);
  const groups = props.groupBy ? groupItems(items, props.groupBy) : [{ label: "", items }];

  return (
    <div className={cn("space-y-6", props.className)}>
      {groups.map((g, gi) => (
        <div key={`g-${gi}`} className="space-y-3">
          {g.label ? (
            <div className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{g.label}</div>
          ) : null}
          <ol className="relative space-y-3 border-l border-border pl-4">
            {g.items.map((item, i) => (
              <li key={String(item.id ?? `${gi}-${i}`)} className="relative">
                <span className="absolute -left-[21px] top-1 inline-flex h-3 w-3 items-center justify-center rounded-full border-2 border-background bg-primary" />
                {props.renderItem ? (
                  <GraphRenderer node={{ intent: props.renderItem, props: { row: item } } as GraphNode} />
                ) : (
                  <div className="space-y-1">
                    <div className="flex items-center gap-2">
                      {props.iconField && item[props.iconField] ? (
                        <Icon
                          node={{} as never}
                          slots={{}}
                          props={{ name: String(item[props.iconField]), size: 14 }}
                        />
                      ) : null}
                      <span className="text-sm font-medium">{String(item[titleField] ?? "")}</span>
                      {item[tsField] ? (
                        <span className="text-xs text-muted-foreground">
                          · {formatTimestamp(item[tsField])}
                        </span>
                      ) : null}
                    </div>
                    {item[descField] ? (
                      <p className="text-sm text-muted-foreground">{String(item[descField])}</p>
                    ) : null}
                  </div>
                )}
              </li>
            ))}
          </ol>
        </div>
      ))}
    </div>
  );
}

function extractItems(data: unknown): Record<string, unknown>[] {
  if (Array.isArray(data)) return data as Record<string, unknown>[];
  if (data && typeof data === "object") {
    const obj = data as Record<string, unknown>;
    for (const k of ["items", "events", "data", "entries"]) {
      if (Array.isArray(obj[k])) return obj[k] as Record<string, unknown>[];
    }
  }
  return [];
}

function groupItems(items: Record<string, unknown>[], field: string) {
  const map = new Map<string, Record<string, unknown>[]>();
  for (const item of items) {
    const key = String(item[field] ?? "");
    if (!map.has(key)) map.set(key, []);
    map.get(key)!.push(item);
  }
  return Array.from(map.entries()).map(([label, items]) => ({ label, items }));
}

function formatTimestamp(raw: unknown): string {
  const d = new Date(String(raw));
  if (Number.isNaN(d.getTime())) return String(raw);
  return d.toLocaleString();
}
