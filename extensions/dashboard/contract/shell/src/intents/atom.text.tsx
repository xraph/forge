import { cn } from "@/lib/utils";
import {
  textSizeClass,
  textVariantClass,
  type TextSize,
  type TextVariant,
} from "@/lib/tokens";
import type { IntentComponentProps } from "../runtime/registry";

interface TextProps {
  text: string;
  size?: TextSize;
  variant?: TextVariant;
  weight?: "normal" | "medium" | "semibold" | "bold";
  italic?: boolean;
  underline?: boolean;
  truncate?: boolean;
  mono?: boolean;
  className?: string;
}

export function AtomText({ props }: IntentComponentProps<unknown, TextProps>) {
  return (
    <span
      className={cn(
        textSizeClass(props.size),
        textVariantClass(props.variant),
        props.weight && `font-${props.weight}`,
        props.italic && "italic",
        props.underline && "underline underline-offset-4",
        props.truncate && "truncate",
        props.mono && "font-mono",
        props.className,
      )}
    >
      {props.text}
    </span>
  );
}
