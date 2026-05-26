import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useContractQuery, useSubscription } from "../contract/hooks";
import { useContributor } from "../runtime/context";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface GaugeProps {
  title?: string;
  field?: string;
  /** Min value of the gauge range. Defaults to 0. */
  min?: number;
  /** Max value of the gauge range. Defaults to 100. */
  max?: number;
  /** Numeric thresholds for color zones; e.g. { warn: 70, crit: 90 }. */
  thresholds?: { warn?: number; crit?: number };
  /** Unit suffix appended to the value (e.g. "%", "ms"). */
  unit?: string;
}

interface GaugePayload {
  [k: string]: unknown;
}

/**
 * metric.gauge renders a numeric value as a horizontal gauge bar with
 * color-coded thresholds. Bound to either a query or subscription via
 * node.data; like metric.counter it auto-discovers whether to subscribe
 * based on the intent's declared kind.
 */
export function MetricGauge({ node, props }: IntentComponentProps<unknown, GaugeProps>) {
  const contributor = useContributor();
  const dataIntent = node.data?.intent ?? "";
  const isSubscription = node.data?.kind === "subscription";

  const ev = useSubscription<GaugePayload>(contributor, isSubscription ? dataIntent : "");
  const query = useContractQuery<GaugePayload>(contributor, isSubscription ? "" : dataIntent);

  const payload: GaugePayload | undefined = isSubscription
    ? ev?.payload
    : (query.data as GaugePayload | undefined);

  const raw = props.field && payload ? payload[props.field] : undefined;
  const value = typeof raw === "number" ? raw : 0;
  const min = props.min ?? 0;
  const max = props.max ?? 100;
  const pct = Math.min(100, Math.max(0, ((value - min) / (max - min || 1)) * 100));

  const zoneColor = pickZoneColor(value, props.thresholds);
  const title = props.title ?? node.title ?? dataIntent ?? "Metric";

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
          {title}
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="flex items-baseline justify-between">
          <span className="text-3xl font-semibold tracking-tight">
            {payload ? value : "—"}
            {props.unit ? <span className="ml-1 text-base text-muted-foreground">{props.unit}</span> : null}
          </span>
          <span className="text-xs text-muted-foreground">
            {min} – {max}
            {props.unit ?? ""}
          </span>
        </div>
        <div className="mt-3 h-2 w-full overflow-hidden rounded-full bg-muted">
          <div
            className={cn("h-full transition-all", zoneColor)}
            style={{ width: `${pct}%` }}
          />
        </div>
      </CardContent>
    </Card>
  );
}

function pickZoneColor(
  value: number,
  thresholds: { warn?: number; crit?: number } | undefined,
): string {
  if (!thresholds) return "bg-primary";
  if (thresholds.crit != null && value >= thresholds.crit) return "bg-destructive";
  if (thresholds.warn != null && value >= thresholds.warn) return "bg-amber-500";
  return "bg-primary";
}
