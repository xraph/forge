import * as React from "react";
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  ComposedChart,
  Line,
  LineChart,
  Pie,
  PieChart,
  Scatter,
  ScatterChart,
  XAxis,
  YAxis,
  ZAxis,
} from "recharts";
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart";
import { useContractQuery } from "../contract/hooks";
import { useContributor, useParent, useRouteParams } from "../runtime/context";
import { LoadingNode, ErrorNode } from "../runtime/fallbacks";
import { resolveValue } from "../runtime/bindings";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface ChartSeries {
  key: string;
  label?: string;
  type?: "line" | "bar" | "area";
  colorToken?: string;
}

interface ChartProps {
  type: "line" | "bar" | "area" | "pie" | "scatter" | "composed";
  xAxis?: string;
  yAxis?: string;
  series?: ChartSeries[];
  showGrid?: boolean;
  showLegend?: boolean;
  showTooltip?: boolean;
  height?: number;
  stacked?: boolean;
  className?: string;
}

/**
 * Recharts-backed chart organism. Colors come from shadcn chart-N tokens
 * via the ChartConfig CSS-variable bridge — no raw hex values.
 */
export function Chart({ node, props }: IntentComponentProps<unknown, ChartProps>) {
  const contributor = useContributor();
  const parent = useParent();
  const route = useRouteParams();
  const height = props.height ?? 300;

  const dataParams = React.useMemo(() => {
    const out: Record<string, unknown> = {};
    if (node.data?.params) {
      for (const [k, v] of Object.entries(node.data.params)) {
        const r = resolveValue(v, { parent, route });
        if (r !== undefined) out[k] = r;
      }
    }
    return out;
  }, [node.data?.params, parent, route]);

  const query = useContractQuery<unknown>(contributor, node.data?.intent ?? "", undefined, dataParams);

  if (!node.data?.intent && !node.data?.queryRef) {
    return <ErrorNode message="Chart requires a data binding" />;
  }
  if (query.isLoading) return <LoadingNode />;
  if (query.error) return <ErrorNode message={(query.error as Error).message} />;

  const rows = extractRows(query.data);
  if (rows.length === 0) {
    return (
      <div
        className={cn("flex items-center justify-center rounded-lg border border-dashed", props.className)}
        style={{ height }}
      >
        <p className="text-sm text-muted-foreground">No data</p>
      </div>
    );
  }

  const xAxis = props.xAxis ?? "x";
  const series = props.series ?? inferSeries(rows, xAxis);
  const config = buildChartConfig(series);

  if (props.type === "pie") {
    return (
      <ChartContainer
        config={config}
        className={cn("aspect-square", props.className)}
        style={{ height }}
      >
        <PieChart>
          {props.showTooltip !== false ? (
            <ChartTooltip content={<ChartTooltipContent hideLabel />} />
          ) : null}
          <Pie
            data={rows}
            dataKey={series[0]?.key ?? "value"}
            nameKey={xAxis}
            outerRadius="80%"
          >
            {rows.map((_r, i) => (
              <Cell key={i} fill={`var(--chart-${(i % 5) + 1})`} />
            ))}
          </Pie>
          {props.showLegend !== false ? (
            <ChartLegend content={<ChartLegendContent />} />
          ) : null}
        </PieChart>
      </ChartContainer>
    );
  }

  if (props.type === "scatter") {
    return (
      <ChartContainer config={config} className={props.className} style={{ height }}>
        <ScatterChart>
          {props.showGrid !== false ? <CartesianGrid /> : null}
          <XAxis type="number" dataKey={xAxis} />
          <YAxis type="number" dataKey={props.yAxis ?? series[0]?.key ?? "y"} />
          <ZAxis range={[60, 60]} />
          {props.showTooltip !== false ? (
            <ChartTooltip content={<ChartTooltipContent />} />
          ) : null}
          {series.map((s) => (
            <Scatter key={s.key} name={s.label ?? s.key} data={rows} fill={`var(--color-${s.key})`} />
          ))}
          {props.showLegend !== false ? (
            <ChartLegend content={<ChartLegendContent />} />
          ) : null}
        </ScatterChart>
      </ChartContainer>
    );
  }

  const stackId = props.stacked ? "a" : undefined;

  const commonAxes = (
    <>
      {props.showGrid !== false ? <CartesianGrid vertical={false} /> : null}
      <XAxis dataKey={xAxis} tickLine={false} axisLine={false} tickMargin={8} />
      <YAxis tickLine={false} axisLine={false} tickMargin={8} />
      {props.showTooltip !== false ? <ChartTooltip content={<ChartTooltipContent />} /> : null}
      {props.showLegend !== false ? <ChartLegend content={<ChartLegendContent />} /> : null}
    </>
  );

  if (props.type === "composed") {
    return (
      <ChartContainer config={config} className={props.className} style={{ height }}>
        <ComposedChart data={rows}>
          {commonAxes}
          {series.map((s) => {
            const color = `var(--color-${s.key})`;
            if (s.type === "bar")
              return <Bar key={s.key} dataKey={s.key} fill={color} stackId={stackId} radius={3} />;
            if (s.type === "area")
              return (
                <Area
                  key={s.key}
                  dataKey={s.key}
                  fill={color}
                  stroke={color}
                  fillOpacity={0.3}
                  stackId={stackId}
                />
              );
            return <Line key={s.key} dataKey={s.key} stroke={color} dot={false} strokeWidth={2} />;
          })}
        </ComposedChart>
      </ChartContainer>
    );
  }

  if (props.type === "bar") {
    return (
      <ChartContainer config={config} className={props.className} style={{ height }}>
        <BarChart data={rows}>
          {commonAxes}
          {series.map((s) => (
            <Bar
              key={s.key}
              dataKey={s.key}
              fill={`var(--color-${s.key})`}
              stackId={stackId}
              radius={3}
            />
          ))}
        </BarChart>
      </ChartContainer>
    );
  }

  if (props.type === "area") {
    return (
      <ChartContainer config={config} className={props.className} style={{ height }}>
        <AreaChart data={rows}>
          {commonAxes}
          {series.map((s) => (
            <Area
              key={s.key}
              dataKey={s.key}
              fill={`var(--color-${s.key})`}
              stroke={`var(--color-${s.key})`}
              fillOpacity={0.3}
              stackId={stackId}
            />
          ))}
        </AreaChart>
      </ChartContainer>
    );
  }

  // Default: line
  return (
    <ChartContainer config={config} className={props.className} style={{ height }}>
      <LineChart data={rows}>
        {commonAxes}
        {series.map((s) => (
          <Line
            key={s.key}
            dataKey={s.key}
            stroke={`var(--color-${s.key})`}
            dot={false}
            strokeWidth={2}
          />
        ))}
      </LineChart>
    </ChartContainer>
  );
}

function buildChartConfig(series: ChartSeries[]): ChartConfig {
  const out: ChartConfig = {};
  series.forEach((s, i) => {
    out[s.key] = {
      label: s.label ?? s.key,
      color: `var(--${s.colorToken ?? `chart-${(i % 5) + 1}`})`,
    };
  });
  return out;
}

function inferSeries(rows: Record<string, unknown>[], xAxis: string): ChartSeries[] {
  if (rows.length === 0) return [];
  const keys = Object.keys(rows[0]!).filter(
    (k) => k !== xAxis && typeof rows[0]![k] === "number",
  );
  return keys.map((k, i) => ({ key: k, label: k, colorToken: `chart-${(i % 5) + 1}` }));
}

function extractRows(data: unknown): Record<string, unknown>[] {
  if (Array.isArray(data)) return data as Record<string, unknown>[];
  if (data && typeof data === "object") {
    const obj = data as Record<string, unknown>;
    for (const k of ["items", "rows", "data", "series", "values"]) {
      if (Array.isArray(obj[k])) return obj[k] as Record<string, unknown>[];
    }
  }
  return [];
}
