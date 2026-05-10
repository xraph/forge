import { IntentRegistry, type IntentComponent } from "../runtime/registry";
import { PageShell } from "./page.shell";
import { MetricCounter } from "./metric.counter";
import { ActionButton } from "./action.button";
import { ActionMenu } from "./action.menu";
import { ActionDivider } from "./action.divider";
import { FormEdit } from "./form.edit";
import { FormField } from "./form.field";
import { ResourceList } from "./resource.list";
import { ResourceDetail } from "./resource.detail";
import { DashboardGrid } from "./dashboard.grid";
import { AuditTail } from "./audit.tail";
import { AuthLoginForm } from "../auth/AuthLoginForm";

export function buildIntentRegistry(): IntentRegistry {
  const reg = new IntentRegistry();
  reg.register("page.shell", PageShell as unknown as IntentComponent);
  reg.register("metric.counter", MetricCounter as unknown as IntentComponent);
  reg.register("action.button", ActionButton as unknown as IntentComponent);
  reg.register("action.menu", ActionMenu as unknown as IntentComponent);
  reg.register("action.divider", ActionDivider as unknown as IntentComponent);
  reg.register("form.edit", FormEdit as unknown as IntentComponent);
  reg.register("form.field", FormField as unknown as IntentComponent);
  reg.register("resource.list", ResourceList as unknown as IntentComponent);
  reg.register("resource.detail", ResourceDetail as unknown as IntentComponent);
  reg.register("dashboard.grid", DashboardGrid as unknown as IntentComponent);
  reg.register("audit.tail", AuditTail as unknown as IntentComponent);
  // Slice (l.5): `auth.login.form` is the dashboard's contract login intent.
  // Authsome's manifest binds it at /login with a per-app config query so
  // the form auto-reflects configured social providers + branding.
  reg.register("auth.login.form", AuthLoginForm as unknown as IntentComponent);
  return reg;
}
