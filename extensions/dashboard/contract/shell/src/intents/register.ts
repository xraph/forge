import { IntentRegistry, type IntentComponent } from "../runtime/registry";
import { PageShell } from "./page.shell";
import { MetricCounter } from "./metric.counter";
import { ActionButton } from "./action.button";
import { ActionMenu } from "./action.menu";
import { ActionDivider } from "./action.divider";

export function buildIntentRegistry(): IntentRegistry {
  const reg = new IntentRegistry();
  reg.register("page.shell", PageShell as unknown as IntentComponent);
  reg.register("metric.counter", MetricCounter as unknown as IntentComponent);
  reg.register("action.button", ActionButton as unknown as IntentComponent);
  reg.register("action.menu", ActionMenu as unknown as IntentComponent);
  reg.register("action.divider", ActionDivider as unknown as IntentComponent);
  return reg;
}
