import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { cn } from "@/lib/utils";
import type { Size } from "@/lib/tokens";
import type { IntentComponentProps } from "../runtime/registry";

interface AvatarProps {
  src?: string;
  alt?: string;
  fallback?: string;
  size?: Size;
  status?: "online" | "offline" | "busy" | "away";
  className?: string;
}

const sizeClass: Record<string, string> = {
  xs: "h-6 w-6 text-xs",
  sm: "h-8 w-8 text-xs",
  default: "h-10 w-10 text-sm",
  lg: "h-12 w-12 text-base",
  xl: "h-16 w-16 text-lg",
  icon: "h-10 w-10 text-sm",
};

const statusClass: Record<string, string> = {
  online: "bg-primary",
  offline: "bg-muted",
  busy: "bg-destructive",
  away: "bg-muted-foreground",
};

export function AtomAvatar({ props }: IntentComponentProps<unknown, AvatarProps>) {
  const initials = (props.fallback ?? props.alt ?? "").slice(0, 2).toUpperCase();
  return (
    <div className={cn("relative inline-block", props.className)}>
      <Avatar className={sizeClass[props.size ?? "default"]}>
        {props.src ? <AvatarImage src={props.src} alt={props.alt} /> : null}
        <AvatarFallback>{initials || "?"}</AvatarFallback>
      </Avatar>
      {props.status ? (
        <span
          className={cn(
            "absolute bottom-0 right-0 h-2.5 w-2.5 rounded-full ring-2 ring-background",
            statusClass[props.status],
          )}
        />
      ) : null}
    </div>
  );
}
