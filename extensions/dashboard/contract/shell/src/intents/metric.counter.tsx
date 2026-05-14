import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useSubscription } from "../contract/hooks";
import type { IntentComponentProps } from "../runtime/registry";

interface CounterProps {
  title?: string;
}

interface CounterPayload {
  totalMetrics?: number;
  cpuPercent?: number;
  [k: string]: unknown;
}

const CONTRIBUTOR = "core-contract";

export function MetricCounter({ node, props }: IntentComponentProps<unknown, CounterProps>) {
  const subscriptionIntent = node.data?.intent;
  const ev = useSubscription<CounterPayload>(CONTRIBUTOR, subscriptionIntent ?? "metrics.summary");

  const value = ev?.payload.totalMetrics ?? ev?.payload.cpuPercent ?? "—";
  const title = props.title ?? node.title ?? subscriptionIntent ?? "Metric";

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
          {title}
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="text-3xl font-semibold tracking-tight">{value}</div>
      </CardContent>
    </Card>
  );
}
