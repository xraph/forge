import { useContractQuery, useSubscription } from "../contract/hooks";
import { useContributor } from "../runtime/context";
import { StatCard } from "./molecule.stat-card";
import type { IntentComponentProps } from "../runtime/registry";

interface DashboardStatProps {
  /** Label shown above the value (e.g. "Active users"). */
  label: string;
  /** Field name to pluck out of the bound query/subscription payload. */
  valueField?: string;
  /** Field on the payload to use as a delta percentage. */
  deltaField?: string;
  /** Field on the payload to use as the trend ("up"|"down"|"flat"). */
  trendField?: string;
  /** Leading icon name (lucide). */
  icon?: string;
  /** Short helper text under the value. */
  description?: string;
  className?: string;
}

/**
 * dashboard.stat is a query-bound wrapper over molecule.stat-card. It
 * fetches its data via node.data and projects a single field into the
 * card's value. Suitable for the dashboard.grid widget slot — each tile
 * renders one number from one query without the manifest having to
 * declare a full nested DataBinding tree per card.
 */
export function DashboardStat({
  node,
  props,
}: IntentComponentProps<unknown, DashboardStatProps>) {
  const contributor = useContributor();
  const dataIntent = node.data?.intent ?? "";
  const isSubscription = node.data?.kind === "subscription";

  const ev = useSubscription<Record<string, unknown>>(
    contributor,
    isSubscription ? dataIntent : "",
  );
  const query = useContractQuery<Record<string, unknown>>(
    contributor,
    isSubscription ? "" : dataIntent,
  );

  const payload = isSubscription ? ev?.payload : query.data;
  const loading = !isSubscription && dataIntent ? query.isLoading : false;

  const rawValue = props.valueField && payload ? payload[props.valueField] : undefined;
  const value = formatStatValue(rawValue);
  const delta = numericField(payload, props.deltaField);
  const trend = stringField(payload, props.trendField) as "up" | "down" | "flat" | undefined;

  return (
    <StatCard
      node={{} as never}
      slots={{}}
      props={{
        label: props.label,
        value,
        delta: delta ?? undefined,
        deltaFormat: delta !== null ? "percent" : undefined,
        trend,
        icon: props.icon,
        description: props.description,
        loading,
        className: props.className,
      }}
    />
  );
}

function formatStatValue(v: unknown): string {
  if (v == null) return "—";
  if (typeof v === "number") return v.toLocaleString();
  return String(v);
}

function numericField(payload: Record<string, unknown> | undefined, field?: string): number | null {
  if (!payload || !field) return null;
  const v = payload[field];
  return typeof v === "number" ? v : null;
}

function stringField(payload: Record<string, unknown> | undefined, field?: string): string | undefined {
  if (!payload || !field) return undefined;
  const v = payload[field];
  return typeof v === "string" ? v : undefined;
}
