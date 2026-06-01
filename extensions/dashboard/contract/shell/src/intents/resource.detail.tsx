import * as React from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useContractQuery } from "../contract/hooks";
import {
  useContributor,
  useParent,
  ParentProvider,
  useRouteParams,
} from "../runtime/context";
import { resolveValue } from "../runtime/bindings";
import { LoadingNode, ErrorNode } from "../runtime/fallbacks";
import { SlotRenderer } from "../runtime/slots";
import { usePrincipalStore } from "../auth/principal";
import type { IntentComponentProps } from "../runtime/registry";

interface ResourceDetailProps {
  title?: string;
  fields?: string[];
}

/**
 * resource.detail renders a single record. It pulls data from node.data
 * when present (typically a query intent like users.detail), falling back
 * to the nearest parent context (so a list row's detailDrawer can reuse
 * the row data without re-fetching).
 *
 * Layout:
 *  - When any of the rich slots (`header`, `sections`, `actions`,
 *    `extensions`) are set, the loaded record is provided as parent
 *    context to descendants and slots are rendered in this order:
 *      header → actions row → sections → extensions
 *  - When no slots are set, the bare-fields definition-list fallback
 *    renders for backward compatibility with the pilot's existing
 *    manifests.
 */
export function ResourceDetail({
  node,
  props,
  slots,
}: IntentComponentProps<unknown, ResourceDetailProps>) {
  const contributor = useContributor();
  const parent = useParent();
  const route = useRouteParams();
  const principal = usePrincipalStore((s) => s.principal);

  const dataIntent = node.data?.intent;
  const dataParams = React.useMemo(() => {
    const out: Record<string, unknown> = {};
    const params = node.data?.params as Record<string, unknown> | undefined;
    if (!params) return out;
    for (const [k, v] of Object.entries(params)) {
      const resolved = resolveValue(v, { parent, session: { user: principal }, route });
      if (resolved !== undefined) out[k] = resolved;
    }
    return out;
  }, [node.data?.params, parent, principal, route]);

  const query = useContractQuery<Record<string, unknown>>(
    contributor,
    dataIntent ?? "",
    undefined,
    dataParams,
  );

  const fromQuery = dataIntent ? query.data : undefined;
  const record = fromQuery ?? (parent as Record<string, unknown> | null) ?? null;

  if (dataIntent && query.isLoading) return <LoadingNode />;
  if (dataIntent && query.error) return <ErrorNode message={(query.error as Error).message} />;
  if (!record) return <ErrorNode message="resource.detail has no data to render" />;

  const hasRichLayout = Boolean(
    slots.header?.length || slots.sections?.length ||
    slots.actions?.length || slots.extensions?.length,
  );

  if (hasRichLayout) {
    return (
      <ParentProvider value={record}>
        <div className="space-y-6">
          {slots.header?.length ? (
            <div>
              <SlotRenderer slot="header" slots={slots} />
            </div>
          ) : null}
          {slots.actions?.length ? (
            <div className="flex flex-wrap items-center gap-2">
              <SlotRenderer slot="actions" slots={slots} />
            </div>
          ) : null}
          {slots.sections?.length ? (
            <div className="space-y-6">
              <SlotRenderer slot="sections" slots={slots} />
            </div>
          ) : null}
          {slots.extensions?.length ? (
            <div className="space-y-6">
              <SlotRenderer slot="extensions" slots={slots} />
            </div>
          ) : null}
        </div>
      </ParentProvider>
    );
  }

  const fieldNames = props.fields ?? Object.keys(record);
  const title = props.title ?? node.title;

  return (
    <Card>
      {title ? (
        <CardHeader>
          <CardTitle>{title}</CardTitle>
        </CardHeader>
      ) : null}
      <CardContent>
        <dl className="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-3 text-sm">
          {fieldNames.map((name) => (
            <React.Fragment key={name}>
              <dt className="font-medium text-muted-foreground">{name}</dt>
              <dd className="break-all">{renderValue(record[name])}</dd>
            </React.Fragment>
          ))}
        </dl>
      </CardContent>
    </Card>
  );
}

function renderValue(v: unknown): React.ReactNode {
  if (v == null) return <span className="text-muted-foreground">—</span>;
  if (typeof v === "boolean") return v ? "Yes" : "No";
  if (typeof v === "object") return <code className="font-mono text-xs">{JSON.stringify(v)}</code>;
  return String(v);
}
