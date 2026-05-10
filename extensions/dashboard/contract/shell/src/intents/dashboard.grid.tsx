import { SlotRenderer } from "../runtime/slots";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface DashboardGridProps {
  /** Number of columns at md+ breakpoint. Defaults to 3. */
  columns?: 1 | 2 | 3 | 4;
}

/**
 * dashboard.grid lays out widget children in a responsive CSS grid. The
 * widgets slot fills the grid; each child intent renders in its own cell.
 */
export function DashboardGrid({ props, slots }: IntentComponentProps<unknown, DashboardGridProps>) {
  const cols = props.columns ?? 3;
  const colsClass =
    cols === 1
      ? "md:grid-cols-1"
      : cols === 2
        ? "md:grid-cols-2"
        : cols === 3
          ? "md:grid-cols-3"
          : "md:grid-cols-4";

  return (
    <div className={cn("grid grid-cols-1 gap-4", colsClass)}>
      <SlotRenderer slot="widgets" slots={slots} />
    </div>
  );
}
