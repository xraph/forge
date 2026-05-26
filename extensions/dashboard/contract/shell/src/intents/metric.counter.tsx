import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useContractQuery, useSubscription } from "../contract/hooks";
import { useContributor } from "../runtime/context";
import type { IntentComponentProps } from "../runtime/registry";

interface CounterProps {
  title?: string;
  // field names the value to pluck out of the bound query / subscription
  // payload (e.g. props.field = "totalServices" for the pilot's overview
  // counter). Falls back to the legacy metrics.summary fields when the
  // payload is one of those (totalMetrics / cpuPercent) and field is unset.
  field?: string;
}

interface CounterPayload {
  totalMetrics?: number;
  cpuPercent?: number;
  [k: string]: unknown;
}

export function MetricCounter({ node, props }: IntentComponentProps<unknown, CounterProps>) {
  const contributor = useContributor();
  const dataIntent = node.data?.intent ?? "";
  // The dashboard registry stamps DataBinding.kind at merge time from the
  // referenced intent's declared kind. metric.counter supports both:
  // subscription-bound counters (live SSE updates from e.g. metrics.summary)
  // and query-bound counters (one-shot fetch with react-query caching, used
  // by the pilot's overview page via queries.overview).
  //
  // Both hooks are called every render to satisfy the rules of hooks; the
  // unused side passes intent="" which the hook's internal guard turns into
  // a no-op (no network call, no cache entry).
  const isSubscription = node.data?.kind === "subscription";
  const ev = useSubscription<CounterPayload>(
    contributor,
    isSubscription ? dataIntent : "",
  );
  const query = useContractQuery<CounterPayload>(
    contributor,
    isSubscription ? "" : dataIntent,
  );

  const payload: CounterPayload | undefined = isSubscription
    ? ev?.payload
    : (query.data as CounterPayload | undefined);

  let value: unknown = "—";
  if (payload) {
    if (props.field && props.field in payload) {
      value = payload[props.field] ?? "—";
    } else {
      value = payload.totalMetrics ?? payload.cpuPercent ?? "—";
    }
  }

  const title = props.title ?? node.title ?? dataIntent ?? "Metric";

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
          {title}
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="text-3xl font-semibold tracking-tight">{String(value)}</div>
      </CardContent>
    </Card>
  );
}
