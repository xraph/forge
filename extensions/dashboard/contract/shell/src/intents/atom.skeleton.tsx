import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { radiusClass, type Radius } from "@/lib/tokens";
import type { IntentComponentProps } from "../runtime/registry";

interface SkeletonProps {
  width?: string;
  height?: string;
  radius?: Radius;
  circle?: boolean;
  className?: string;
}

export function AtomSkeleton({ props }: IntentComponentProps<unknown, SkeletonProps>) {
  return (
    <Skeleton
      style={{ width: props.width, height: props.height }}
      className={cn(props.circle ? "rounded-full" : radiusClass(props.radius), props.className)}
    />
  );
}
