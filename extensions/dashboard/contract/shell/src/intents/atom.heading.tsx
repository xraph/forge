import * as React from "react";
import { cn } from "@/lib/utils";
import { textVariantClass, type TextVariant } from "@/lib/tokens";
import type { IntentComponentProps } from "../runtime/registry";

interface HeadingProps {
  text: string;
  level?: number; // 1-6
  variant?: TextVariant;
  className?: string;
}

const sizeForLevel: Record<number, string> = {
  1: "text-3xl font-bold tracking-tight",
  2: "text-2xl font-semibold tracking-tight",
  3: "text-xl font-semibold",
  4: "text-lg font-semibold",
  5: "text-base font-semibold",
  6: "text-sm font-semibold uppercase tracking-wide",
};

export function AtomHeading({ props }: IntentComponentProps<unknown, HeadingProps>) {
  const level = Math.min(6, Math.max(1, props.level ?? 2));
  const Tag = `h${level}` as keyof React.JSX.IntrinsicElements;
  return React.createElement(
    Tag,
    {
      className: cn(sizeForLevel[level], textVariantClass(props.variant), props.className),
    },
    props.text,
  );
}
