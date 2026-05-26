import { TrendingDown, TrendingUp, Minus } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Icon } from "./atom.icon";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface StatCardProps {
  label: string;
  value: string;
  previousValue?: string;
  delta?: number;
  deltaFormat?: "percent" | "absolute";
  icon?: string;
  trend?: "up" | "down" | "flat";
  description?: string;
  loading?: boolean;
  className?: string;
}

export function StatCard({ props }: IntentComponentProps<unknown, StatCardProps>) {
  const TrendIcon =
    props.trend === "up" ? TrendingUp : props.trend === "down" ? TrendingDown : Minus;

  const trendColor =
    props.trend === "up"
      ? "text-foreground"
      : props.trend === "down"
        ? "text-destructive"
        : "text-muted-foreground";

  const deltaText =
    props.delta !== undefined
      ? props.deltaFormat === "percent"
        ? `${props.delta > 0 ? "+" : ""}${props.delta}%`
        : `${props.delta > 0 ? "+" : ""}${props.delta}`
      : null;

  return (
    <Card className={cn("overflow-hidden", props.className)}>
      <CardContent className="p-6">
        <div className="flex items-center justify-between">
          <p className="text-sm font-medium text-muted-foreground">{props.label}</p>
          {props.icon ? (
            <div className="text-muted-foreground">
              <Icon node={{} as never} slots={{}} props={{ name: props.icon, size: 16 }} />
            </div>
          ) : null}
        </div>
        <div className="mt-2 flex items-baseline justify-between gap-3">
          {props.loading ? (
            <Skeleton className="h-8 w-24" />
          ) : (
            <p className="text-2xl font-semibold tabular-nums">{props.value}</p>
          )}
          {!props.loading && deltaText ? (
            <span className={cn("inline-flex items-center gap-1 text-xs", trendColor)}>
              <TrendIcon className="h-3 w-3" />
              <span className="tabular-nums">{deltaText}</span>
            </span>
          ) : null}
        </div>
        {props.description ? (
          <p className="mt-2 text-xs text-muted-foreground">{props.description}</p>
        ) : null}
        {props.previousValue ? (
          <p className="mt-1 text-xs text-muted-foreground">vs. {props.previousValue}</p>
        ) : null}
      </CardContent>
    </Card>
  );
}
