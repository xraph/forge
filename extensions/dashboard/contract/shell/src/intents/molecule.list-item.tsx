import { Link } from "react-router-dom";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Icon } from "./atom.icon";
import { SlotRenderer } from "../runtime/slots";
import { cn } from "@/lib/utils";
import { dispatchAction, type Action } from "../runtime/actions";
import type { IntentComponentProps } from "../runtime/registry";

interface ListItemProps {
  title: string;
  description?: string;
  icon?: string;
  avatarSrc?: string;
  avatarFallback?: string;
  href?: string;
  selected?: boolean;
  disabled?: boolean;
  onClick?: Action;
  className?: string;
}

export function ListItem({
  props,
  slots,
}: IntentComponentProps<unknown, ListItemProps>) {
  const Cls = cn(
    "group flex items-center gap-3 rounded-md border border-transparent px-3 py-2 transition-colors",
    props.selected && "border-border bg-accent text-accent-foreground",
    !props.disabled && !props.selected && "hover:bg-accent hover:text-accent-foreground",
    props.disabled && "cursor-not-allowed opacity-50",
    props.href && !props.disabled && "cursor-pointer",
    props.className,
  );

  const inner = (
    <>
      {props.avatarSrc || props.avatarFallback ? (
        <Avatar className="h-8 w-8 shrink-0">
          {props.avatarSrc ? <AvatarImage src={props.avatarSrc} alt={props.title} /> : null}
          <AvatarFallback>{(props.avatarFallback ?? props.title).slice(0, 2).toUpperCase()}</AvatarFallback>
        </Avatar>
      ) : props.icon ? (
        <span className="shrink-0 text-muted-foreground">
          <Icon node={{} as never} slots={{}} props={{ name: props.icon, size: 18 }} />
        </span>
      ) : null}
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium">{props.title}</p>
        {props.description ? (
          <p className="truncate text-xs text-muted-foreground">{props.description}</p>
        ) : null}
      </div>
      <div className="flex items-center gap-2">
        <SlotRenderer slot="trailing" slots={slots} />
      </div>
    </>
  );

  if (props.href && !props.disabled) {
    return (
      <Link to={props.href} className={Cls}>
        {inner}
      </Link>
    );
  }
  return (
    <div
      role={props.onClick ? "button" : undefined}
      tabIndex={props.onClick && !props.disabled ? 0 : undefined}
      onClick={() => !props.disabled && props.onClick && dispatchAction(props.onClick, {})}
      className={Cls}
    >
      {inner}
    </div>
  );
}
