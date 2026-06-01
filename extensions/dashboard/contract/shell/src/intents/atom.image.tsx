import { cn } from "@/lib/utils";
import { radiusClass, type Radius } from "@/lib/tokens";
import type { IntentComponentProps } from "../runtime/registry";

interface ImageProps {
  src: string;
  alt?: string;
  aspectRatio?: "square" | "video" | "auto";
  objectFit?: "cover" | "contain" | "fill" | "none";
  radius?: Radius;
  width?: string;
  height?: string;
  className?: string;
}

const aspectClass: Record<string, string> = {
  square: "aspect-square",
  video: "aspect-video",
  auto: "",
};

const objectClass: Record<string, string> = {
  cover: "object-cover",
  contain: "object-contain",
  fill: "object-fill",
  none: "object-none",
};

export function AtomImage({ props }: IntentComponentProps<unknown, ImageProps>) {
  return (
    <img
      src={props.src}
      alt={props.alt ?? ""}
      style={{ width: props.width, height: props.height }}
      className={cn(
        aspectClass[props.aspectRatio ?? "auto"],
        objectClass[props.objectFit ?? "cover"],
        radiusClass(props.radius),
        props.className,
      )}
    />
  );
}
