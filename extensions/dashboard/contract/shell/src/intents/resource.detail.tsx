import * as React from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useContractQuery } from "../contract/hooks";
import { useContributor, useParent, useRouteParams } from "../runtime/context";
import { resolveValue } from "../runtime/bindings";
import { LoadingNode, ErrorNode } from "../runtime/fallbacks";
import { usePrincipalStore } from "../auth/principal";
import type { IntentComponentProps } from "../runtime/registry";

interface ResourceDetailProps {
  title?: string;
  fields?: string[];
}

/**
 * resource.detail renders a single record. It pulls data from node.data when
 * present (typically a query intent like user.detail), falling back to the
 * nearest parent context (so a list row's detailDrawer can reuse the row data
 * without re-fetching). Fields are rendered in a definition list; props.fields
 * narrows and orders them.
 */
export function ResourceDetail({
  node,
  props,
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
  const record = fromQuery ?? parent ?? null;

  if (dataIntent && query.isLoading) return <LoadingNode />;
  if (dataIntent && query.error) return <ErrorNode message={(query.error as Error).message} />;
  if (!record) return <ErrorNode message="resource.detail has no data to render" />;

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
