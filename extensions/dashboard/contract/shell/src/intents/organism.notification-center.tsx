import * as React from "react";
import { Bell, Check } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { Icon } from "./atom.icon";
import { Link } from "react-router-dom";
import { useContractQuery } from "../contract/hooks";
import { useContributor, useParent, useRouteParams } from "../runtime/context";
import { resolveValue } from "../runtime/bindings";
import { dispatchAction, type Action } from "../runtime/actions";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface NotificationCenterProps {
  titleField?: string;
  bodyField?: string;
  timestampField?: string;
  readField?: string;
  iconField?: string;
  hrefField?: string;
  onMarkRead?: Action;
  onMarkAllRead?: Action;
  onClick?: Action;
  unreadOnlyDefault?: boolean;
  className?: string;
}

export function NotificationCenter({
  node,
  props,
}: IntentComponentProps<unknown, NotificationCenterProps>) {
  const contributor = useContributor();
  const parent = useParent();
  const route = useRouteParams();
  const titleField = props.titleField ?? "title";
  const bodyField = props.bodyField ?? "body";
  const tsField = props.timestampField ?? "timestamp";
  const readField = props.readField ?? "read";

  const [unreadOnly, setUnreadOnly] = React.useState(props.unreadOnlyDefault ?? false);

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

  const items = extractItems(query.data);
  const visible = unreadOnly ? items.filter((i) => !i[readField]) : items;
  const unreadCount = items.filter((i) => !i[readField]).length;

  return (
    <Sheet>
      <SheetTrigger asChild>
        <Button variant="ghost" size="icon" className={cn("relative", props.className)} aria-label="Notifications">
          <Bell className="h-5 w-5" />
          {unreadCount > 0 ? (
            <Badge
              variant="destructive"
              size="sm"
              className="absolute -right-1 -top-1 h-4 min-w-[16px] justify-center rounded-full px-1 text-[10px]"
            >
              {unreadCount > 99 ? "99+" : unreadCount}
            </Badge>
          ) : null}
        </Button>
      </SheetTrigger>
      <SheetContent className="w-[min(420px,100vw)] sm:max-w-md">
        <SheetHeader>
          <SheetTitle>Notifications</SheetTitle>
        </SheetHeader>
        <div className="mt-2 flex items-center justify-between">
          <button
            type="button"
            className="text-xs underline-offset-4 hover:underline"
            onClick={() => setUnreadOnly((s) => !s)}
          >
            {unreadOnly ? "Show all" : "Unread only"}
          </button>
          {unreadCount > 0 && props.onMarkAllRead ? (
            <Button variant="ghost" size="sm" onClick={() => dispatchAction(props.onMarkAllRead, {})}>
              <Check className="mr-1 h-3 w-3" /> Mark all read
            </Button>
          ) : null}
        </div>
        <div className="mt-4 space-y-1">
          {query.isLoading ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : visible.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">
              {unreadOnly ? "No unread notifications." : "No notifications yet."}
            </p>
          ) : (
            visible.map((n, i) => (
              <NotificationRow
                key={String(n.id ?? i)}
                n={n}
                titleField={titleField}
                bodyField={bodyField}
                tsField={tsField}
                readField={readField}
                iconField={props.iconField}
                hrefField={props.hrefField}
                onMarkRead={props.onMarkRead}
                onClick={props.onClick}
              />
            ))
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}

function NotificationRow({
  n,
  titleField,
  bodyField,
  tsField,
  readField,
  iconField,
  hrefField,
  onMarkRead,
  onClick,
}: {
  n: Record<string, unknown>;
  titleField: string;
  bodyField: string;
  tsField: string;
  readField: string;
  iconField?: string;
  hrefField?: string;
  onMarkRead?: Action;
  onClick?: Action;
}) {
  const isRead = !!n[readField];
  const href = hrefField ? (n[hrefField] as string | undefined) : undefined;
  const body = (
    <div
      className={cn(
        "flex gap-3 rounded-md p-3 transition-colors hover:bg-accent",
        !isRead && "bg-accent/40",
      )}
      onClick={() => onClick && dispatchAction(onClick, { value: n })}
    >
      {iconField && n[iconField] ? (
        <span className="mt-0.5 shrink-0 text-muted-foreground">
          <Icon node={{} as never} slots={{}} props={{ name: String(n[iconField]), size: 16 }} />
        </span>
      ) : (
        <span className="mt-1.5 inline-block h-2 w-2 shrink-0 rounded-full bg-primary" />
      )}
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline justify-between gap-2">
          <p className={cn("truncate text-sm", isRead ? "font-normal" : "font-medium")}>
            {String(n[titleField] ?? "")}
          </p>
          {n[tsField] ? (
            <span className="shrink-0 text-xs text-muted-foreground">
              {formatRelative(n[tsField])}
            </span>
          ) : null}
        </div>
        {n[bodyField] ? (
          <p className="line-clamp-2 text-xs text-muted-foreground">{String(n[bodyField])}</p>
        ) : null}
      </div>
      {!isRead && onMarkRead ? (
        <Button
          variant="ghost"
          size="icon"
          className="h-6 w-6 shrink-0"
          onClick={(e) => {
            e.stopPropagation();
            dispatchAction(onMarkRead, { value: n });
          }}
          aria-label="Mark as read"
        >
          <Check className="h-3 w-3" />
        </Button>
      ) : null}
    </div>
  );
  return href ? <Link to={href}>{body}</Link> : body;
}

function extractItems(data: unknown): Record<string, unknown>[] {
  if (Array.isArray(data)) return data as Record<string, unknown>[];
  if (data && typeof data === "object") {
    const obj = data as Record<string, unknown>;
    for (const k of ["items", "notifications", "data"]) {
      if (Array.isArray(obj[k])) return obj[k] as Record<string, unknown>[];
    }
  }
  return [];
}

function formatRelative(raw: unknown): string {
  const d = new Date(String(raw));
  if (Number.isNaN(d.getTime())) return "";
  const diff = (Date.now() - d.getTime()) / 1000;
  if (diff < 60) return "just now";
  if (diff < 3600) return `${Math.floor(diff / 60)}m`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h`;
  if (diff < 86400 * 7) return `${Math.floor(diff / 86400)}d`;
  return d.toLocaleDateString();
}
