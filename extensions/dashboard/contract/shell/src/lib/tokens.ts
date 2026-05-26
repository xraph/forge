/**
 * Token → Tailwind translation layer.
 *
 * The Go contract emits string tokens (e.g., "4" for spacing, "center" for
 * alignment) that map to Tailwind utility classes. Keeping this mapping in
 * one place enforces the rule that only canonical shadcn semantic tokens
 * leak into rendered class names — no success / warning / info, no custom
 * brand hex codes.
 */

export type Spacing =
  | "0"
  | "0.5"
  | "1"
  | "1.5"
  | "2"
  | "2.5"
  | "3"
  | "4"
  | "5"
  | "6"
  | "8"
  | "10"
  | "12"
  | "16"
  | "20"
  | "24";

export type Align = "start" | "center" | "end" | "stretch" | "baseline";
export type Justify =
  | "start"
  | "center"
  | "end"
  | "between"
  | "around"
  | "evenly";

export type Direction = "row" | "column" | "row-reverse" | "column-reverse";
export type Orientation = "horizontal" | "vertical";
export type ContainerSize = "sm" | "md" | "lg" | "xl" | "2xl" | "full";
export type Radius = "none" | "sm" | "md" | "lg" | "xl" | "full";
export type ColorVariant =
  | "default"
  | "secondary"
  | "destructive"
  | "outline"
  | "ghost"
  | "link"
  | "muted"
  | "accent";

export type TextSize = "xs" | "sm" | "base" | "lg" | "xl";
export type TextVariant =
  | "default"
  | "muted"
  | "destructive"
  | "primary"
  | "secondary";
export type Size = "xs" | "sm" | "default" | "lg" | "xl" | "icon";

export interface ResponsiveCols {
  base?: number;
  sm?: number;
  md?: number;
  lg?: number;
  xl?: number;
  "2xl"?: number;
}

export function gapClass(s: Spacing | undefined): string {
  if (!s) return "";
  return `gap-${s}`;
}

export function rowGapClass(s: Spacing | undefined): string {
  return s ? `gap-y-${s}` : "";
}

export function colGapClass(s: Spacing | undefined): string {
  return s ? `gap-x-${s}` : "";
}

export function paddingClass(s: Spacing | undefined): string {
  return s ? `p-${s}` : "";
}

export function alignClass(a: Align | undefined): string {
  if (!a) return "";
  return {
    start: "items-start",
    center: "items-center",
    end: "items-end",
    stretch: "items-stretch",
    baseline: "items-baseline",
  }[a];
}

export function justifyClass(j: Justify | undefined): string {
  if (!j) return "";
  return {
    start: "justify-start",
    center: "justify-center",
    end: "justify-end",
    between: "justify-between",
    around: "justify-around",
    evenly: "justify-evenly",
  }[j];
}

export function directionClass(d: Direction | undefined): string {
  if (!d) return "";
  return {
    row: "flex-row",
    column: "flex-col",
    "row-reverse": "flex-row-reverse",
    "column-reverse": "flex-col-reverse",
  }[d];
}

export function containerClass(s: ContainerSize | undefined): string {
  if (!s) return "max-w-7xl";
  return {
    sm: "max-w-screen-sm",
    md: "max-w-screen-md",
    lg: "max-w-screen-lg",
    xl: "max-w-screen-xl",
    "2xl": "max-w-screen-2xl",
    full: "max-w-full",
  }[s];
}

export function radiusClass(r: Radius | undefined): string {
  if (!r) return "";
  return {
    none: "rounded-none",
    sm: "rounded-sm",
    md: "rounded-md",
    lg: "rounded-lg",
    xl: "rounded-xl",
    full: "rounded-full",
  }[r];
}

export function textSizeClass(s: TextSize | undefined): string {
  if (!s) return "";
  return { xs: "text-xs", sm: "text-sm", base: "text-base", lg: "text-lg", xl: "text-xl" }[s];
}

export function textVariantClass(v: TextVariant | undefined): string {
  if (!v) return "";
  return {
    default: "text-foreground",
    muted: "text-muted-foreground",
    destructive: "text-destructive",
    primary: "text-primary",
    secondary: "text-secondary-foreground",
  }[v];
}

export function gridColsClass(cols: ResponsiveCols | number | undefined): string {
  if (cols === undefined || cols === null) return "";
  if (typeof cols === "number") return colsForN(cols, "");
  const out: string[] = [];
  if (cols.base !== undefined) out.push(colsForN(cols.base, ""));
  if (cols.sm !== undefined) out.push(colsForN(cols.sm, "sm:"));
  if (cols.md !== undefined) out.push(colsForN(cols.md, "md:"));
  if (cols.lg !== undefined) out.push(colsForN(cols.lg, "lg:"));
  if (cols.xl !== undefined) out.push(colsForN(cols.xl, "xl:"));
  if (cols["2xl"] !== undefined) out.push(colsForN(cols["2xl"], "2xl:"));
  return out.filter(Boolean).join(" ");
}

function colsForN(n: number, prefix: string): string {
  if (n <= 0) return "";
  if (n > 12) return `${prefix}grid-cols-12`;
  return `${prefix}grid-cols-${n}`;
}
