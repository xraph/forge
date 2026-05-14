import * as React from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useSubscription } from "../contract/hooks";
import { useContributor } from "../runtime/context";
import type { IntentComponentProps } from "../runtime/registry";

interface AuditTailProps {
  title?: string;
  /** How many events to keep in the buffer; older events drop off the top. */
  bufferSize?: number;
  /** Render each entry. By default JSON-stringifies the payload. */
  format?: "json" | "line";
}

interface BufferEntry {
  seq: number;
  payload: unknown;
}

/**
 * audit.tail subscribes to an append-mode subscription intent and renders the
 * last N events in a scrollable monospace pane. Auto-scroll-to-bottom unless
 * the user has scrolled up (sticky-bottom UX). Buffer trimmed to bufferSize.
 */
export function AuditTail({ node, props }: IntentComponentProps<unknown, AuditTailProps>) {
  const contributor = useContributor();
  const subscriptionIntent = node.data?.intent ?? "";
  const event = useSubscription<unknown>(contributor, subscriptionIntent);
  const [buffer, setBuffer] = React.useState<BufferEntry[]>([]);
  const bufferSize = props.bufferSize ?? 200;
  const viewportRef = React.useRef<HTMLDivElement>(null);
  const stickyBottomRef = React.useRef(true);

  // Append new events to the buffer and trim.
  React.useEffect(() => {
    if (!event) return;
    setBuffer((prev) => {
      const next = [...prev, { seq: event.seq, payload: event.payload }];
      if (next.length > bufferSize) {
        return next.slice(next.length - bufferSize);
      }
      return next;
    });
  }, [event, bufferSize]);

  // Maintain sticky-bottom scroll behavior.
  React.useEffect(() => {
    if (!stickyBottomRef.current) return;
    const el = viewportRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [buffer]);

  const onScroll = (e: React.UIEvent<HTMLDivElement>) => {
    const el = e.currentTarget;
    const distanceToBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    stickyBottomRef.current = distanceToBottom < 8;
  };

  const title = props.title ?? node.title ?? subscriptionIntent ?? "Audit tail";
  const format = props.format ?? "json";

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">{title}</CardTitle>
      </CardHeader>
      <CardContent className="p-0">
        <ScrollArea className="h-72 px-4 pb-4">
          <div
            ref={viewportRef}
            onScroll={onScroll}
            className="h-full overflow-auto"
            data-testid="audit-tail-viewport"
          >
            {buffer.length === 0 ? (
              <p className="py-4 text-sm text-muted-foreground">
                Waiting for events…
              </p>
            ) : (
              <ul className="space-y-1 font-mono text-xs">
                {buffer.map((entry) => (
                  <li key={entry.seq} className="border-b border-border/50 py-1 last:border-0">
                    {format === "line" && typeof entry.payload === "object" && entry.payload && "line" in (entry.payload as Record<string, unknown>)
                      ? String((entry.payload as Record<string, unknown>).line)
                      : JSON.stringify(entry.payload)}
                  </li>
                ))}
              </ul>
            )}
          </div>
        </ScrollArea>
      </CardContent>
    </Card>
  );
}
