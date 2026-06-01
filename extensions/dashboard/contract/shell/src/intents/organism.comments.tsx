import * as React from "react";
import { Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { useContractQuery } from "../contract/hooks";
import { useContributor, useParent, useRouteParams } from "../runtime/context";
import { LoadingNode, ErrorNode } from "../runtime/fallbacks";
import { resolveValue } from "../runtime/bindings";
import { dispatchAction, type Action } from "../runtime/actions";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface CommentsProps {
  authorField?: string;
  avatarField?: string;
  bodyField?: string;
  timestampField?: string;
  onPost?: Action;
  onDelete?: Action;
  currentUserId?: string;
  readOnly?: boolean;
  className?: string;
}

export function Comments({ node, props }: IntentComponentProps<unknown, CommentsProps>) {
  const contributor = useContributor();
  const parent = useParent();
  const route = useRouteParams();
  const authorField = props.authorField ?? "author";
  const avatarField = props.avatarField ?? "avatar";
  const bodyField = props.bodyField ?? "body";
  const tsField = props.timestampField ?? "timestamp";

  const [draft, setDraft] = React.useState("");

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
    return <ErrorNode message="Comments requires a data binding" />;
  }
  if (query.isLoading) return <LoadingNode />;
  if (query.error) return <ErrorNode message={(query.error as Error).message} />;

  const items = extractItems(query.data);

  const post = () => {
    if (!draft.trim() || !props.onPost) return;
    dispatchAction(props.onPost, { value: { body: draft } });
    setDraft("");
  };

  return (
    <div className={cn("space-y-4", props.className)}>
      <ol className="space-y-4">
        {items.map((c, i) => {
          const author = c[authorField] as { name?: string; id?: string } | string | undefined;
          const authorName =
            typeof author === "object" && author?.name ? author.name : String(author ?? "Anonymous");
          const authorID = typeof author === "object" ? author?.id : undefined;
          const avatar = c[avatarField] as string | undefined;
          const isMine = props.currentUserId && authorID === props.currentUserId;
          return (
            <li key={String(c.id ?? i)} className="flex gap-3">
              <Avatar className="h-8 w-8 shrink-0">
                {avatar ? <AvatarImage src={avatar} alt={authorName} /> : null}
                <AvatarFallback>{authorName.slice(0, 2).toUpperCase()}</AvatarFallback>
              </Avatar>
              <div className="min-w-0 flex-1 rounded-md border bg-card px-3 py-2">
                <div className="flex items-baseline justify-between gap-2">
                  <span className="text-sm font-medium">{authorName}</span>
                  <span className="text-xs text-muted-foreground">
                    {c[tsField] ? formatRelative(c[tsField]) : ""}
                  </span>
                </div>
                <p className="mt-1 whitespace-pre-wrap text-sm">{String(c[bodyField] ?? "")}</p>
                {isMine && props.onDelete ? (
                  <div className="mt-2 flex justify-end">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => dispatchAction(props.onDelete, { value: c })}
                      className="h-7 text-xs text-destructive hover:bg-destructive/10 hover:text-destructive"
                    >
                      <Trash2 className="mr-1 h-3 w-3" /> Delete
                    </Button>
                  </div>
                ) : null}
              </div>
            </li>
          );
        })}
        {items.length === 0 ? (
          <p className="py-4 text-center text-sm text-muted-foreground">No comments yet.</p>
        ) : null}
      </ol>
      {!props.readOnly && props.onPost ? (
        <div className="space-y-2">
          <Textarea
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder="Write a comment…"
            rows={3}
          />
          <div className="flex justify-end">
            <Button onClick={post} disabled={!draft.trim()}>
              Post
            </Button>
          </div>
        </div>
      ) : null}
    </div>
  );
}

function extractItems(data: unknown): Record<string, unknown>[] {
  if (Array.isArray(data)) return data as Record<string, unknown>[];
  if (data && typeof data === "object") {
    const obj = data as Record<string, unknown>;
    for (const k of ["items", "comments", "data"]) {
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
