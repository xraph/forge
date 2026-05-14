import { SlotRenderer } from "../runtime/slots";
import type { IntentComponentProps } from "../runtime/registry";

/**
 * page.shell after slice (l) only renders its `main` slot. The dashboard
 * chrome (sidebar, topbar, breadcrumb, theme toggle, user info) lives one
 * level up in DashboardLayout so it persists across route navigations and
 * doesn't re-mount per page.
 */
export function PageShell({ slots }: IntentComponentProps) {
  return (
    <div className="space-y-6">
      <SlotRenderer slot="main" slots={slots} />
    </div>
  );
}
