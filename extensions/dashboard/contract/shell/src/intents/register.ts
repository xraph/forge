import { IntentRegistry, type IntentComponent } from "../runtime/registry";
import { PageShell } from "./page.shell";
import { MetricCounter } from "./metric.counter";

export function buildIntentRegistry(): IntentRegistry {
  const reg = new IntentRegistry();
  reg.register("page.shell", PageShell as unknown as IntentComponent);
  reg.register("metric.counter", MetricCounter as unknown as IntentComponent);
  return reg;
}
