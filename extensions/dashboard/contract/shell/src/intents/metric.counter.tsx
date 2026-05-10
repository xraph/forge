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
    <div className="rounded-lg border border-gray-200 bg-white p-6 shadow-sm">
      <div className="text-xs font-medium uppercase tracking-wider text-gray-500">{title}</div>
      <div className="mt-2 text-3xl font-semibold text-gray-900">{value}</div>
    </div>
  );
}
