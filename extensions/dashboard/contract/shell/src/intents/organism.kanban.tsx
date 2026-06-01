import * as React from "react";
import {
  closestCorners,
  DndContext,
  DragOverlay,
  KeyboardSensor,
  PointerSensor,
  useDraggable,
  useDroppable,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragStartEvent,
} from "@dnd-kit/core";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { GraphRenderer } from "../runtime/renderer";
import { useContractQuery } from "../contract/hooks";
import { useContributor, useParent, useRouteParams, ParentProvider } from "../runtime/context";
import { LoadingNode, ErrorNode } from "../runtime/fallbacks";
import { resolveValue } from "../runtime/bindings";
import { dispatchAction, Toast, type Action } from "../runtime/actions";
import { cn } from "@/lib/utils";
import type { GraphNode } from "@/contract/types";
import type { IntentComponentProps } from "../runtime/registry";

interface KanbanColumn {
  id: string;
  title: string;
  accept?: string[];
  color?: string;
  limit?: number;
}

interface KanbanProps {
  columns: KanbanColumn[];
  statusField: string;
  cardIntent?: string;
  onMove?: Action;
  onCardClick?: Action;
  className?: string;
}

type CardData = Record<string, unknown>;

export function Kanban({ node, props }: IntentComponentProps<unknown, KanbanProps>) {
  const contributor = useContributor();
  const parent = useParent();
  const route = useRouteParams();

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
  const [activeCard, setActiveCard] = React.useState<CardData | null>(null);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor),
  );

  if (!node.data?.intent && !node.data?.queryRef) {
    return <ErrorNode message="Kanban requires a data binding" />;
  }
  if (query.isLoading) return <LoadingNode />;
  if (query.error) return <ErrorNode message={(query.error as Error).message} />;

  const cards = extractCards(query.data);
  const byColumn = groupByColumn(cards, props.columns, props.statusField);

  const cardLookup = new Map<string, { card: CardData; col: string }>();
  for (const [colId, list] of byColumn) {
    for (const c of list) {
      const id = String(c.id ?? "");
      if (id) cardLookup.set(id, { card: c, col: colId });
    }
  }

  const onDragStart = (e: DragStartEvent) => {
    const found = cardLookup.get(String(e.active.id));
    if (found) setActiveCard(found.card);
  };

  const onDragEnd = (e: DragEndEvent) => {
    setActiveCard(null);
    if (!e.over) return;
    const found = cardLookup.get(String(e.active.id));
    if (!found) return;
    const to = String(e.over.id);
    if (to === found.col) return;
    const target = props.columns.find((c) => c.id === to);
    if (!target) return;
    const currentSize = byColumn.get(to)?.length ?? 0;
    if (target.limit && currentSize >= target.limit) {
      dispatchAction(Toast(`${target.title} is full (${target.limit})`), {});
      return;
    }
    if (props.onMove) {
      dispatchAction(props.onMove, {
        value: { id: found.card.id, from: found.col, to, card: found.card },
      });
    }
  };

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={closestCorners}
      onDragStart={onDragStart}
      onDragEnd={onDragEnd}
    >
      <div className={cn("flex h-full min-h-[400px] gap-4 overflow-x-auto pb-2", props.className)}>
        {props.columns.map((col) => {
          const list = byColumn.get(col.id) ?? [];
          return (
            <KanbanColumnView
              key={col.id}
              col={col}
              cards={list}
              cardIntent={props.cardIntent}
              onCardClick={props.onCardClick}
            />
          );
        })}
      </div>
      <DragOverlay>{activeCard ? <DefaultCard card={activeCard} dragging /> : null}</DragOverlay>
    </DndContext>
  );
}

function KanbanColumnView({
  col,
  cards,
  cardIntent,
  onCardClick,
}: {
  col: KanbanColumn;
  cards: CardData[];
  cardIntent?: string;
  onCardClick?: Action;
}) {
  const { setNodeRef, isOver } = useDroppable({ id: col.id });
  const overLimit = col.limit && cards.length > col.limit;

  return (
    <div className="flex w-72 shrink-0 flex-col">
      <div className="mb-2 flex items-center justify-between rounded-md bg-muted/50 px-3 py-2">
        <div className="flex items-center gap-2">
          <span className="text-sm font-semibold">{col.title}</span>
          <Badge variant="secondary" size="sm">
            {cards.length}
          </Badge>
        </div>
        {col.limit ? (
          <Badge variant={overLimit ? "destructive" : "outline"} size="sm">
            {cards.length}/{col.limit}
          </Badge>
        ) : null}
      </div>
      <div
        ref={setNodeRef}
        className={cn(
          "flex min-h-[80px] flex-1 flex-col gap-2 overflow-y-auto rounded-md transition-colors",
          isOver && "bg-accent/30 outline-2 outline-dashed outline-accent",
        )}
      >
        {cards.map((card, i) => (
          <ParentProvider key={String(card.id ?? i)} value={card}>
            <DraggableCard
              card={card}
              cardIntent={cardIntent}
              onCardClick={onCardClick}
            />
          </ParentProvider>
        ))}
        {cards.length === 0 ? (
          <div className="rounded-md border border-dashed py-6 text-center text-xs text-muted-foreground">
            Drop here
          </div>
        ) : null}
      </div>
    </div>
  );
}

function DraggableCard({
  card,
  cardIntent,
  onCardClick,
}: {
  card: CardData;
  cardIntent?: string;
  onCardClick?: Action;
}) {
  const id = String(card.id ?? "");
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({ id });
  return (
    <div
      ref={setNodeRef}
      {...attributes}
      {...listeners}
      onClick={() => onCardClick && dispatchAction(onCardClick, { value: card })}
      className={cn(
        "cursor-grab outline-none active:cursor-grabbing focus-visible:ring-2 focus-visible:ring-ring",
        isDragging && "opacity-30",
      )}
    >
      {cardIntent ? (
        <GraphRenderer node={{ intent: cardIntent, props: { row: card } } as GraphNode} />
      ) : (
        <DefaultCard card={card} />
      )}
    </div>
  );
}

function DefaultCard({ card, dragging }: { card: CardData; dragging?: boolean }) {
  const title = String(card.title ?? card.name ?? card.id ?? "Untitled");
  const description = card.description ? String(card.description) : null;
  return (
    <Card className={cn(dragging && "shadow-xl ring-2 ring-ring")}>
      <CardHeader className="p-3">
        <CardTitle className="text-sm">{title}</CardTitle>
      </CardHeader>
      {description ? (
        <CardContent className="p-3 pt-0">
          <p className="text-xs text-muted-foreground">{description}</p>
        </CardContent>
      ) : null}
    </Card>
  );
}

function extractCards(data: unknown): CardData[] {
  if (Array.isArray(data)) return data as CardData[];
  if (data && typeof data === "object") {
    const obj = data as Record<string, unknown>;
    for (const k of ["items", "cards", "rows", "data"]) {
      if (Array.isArray(obj[k])) return obj[k] as CardData[];
    }
  }
  return [];
}

function groupByColumn(
  cards: CardData[],
  columns: KanbanColumn[],
  field: string,
): Map<string, CardData[]> {
  const map = new Map<string, CardData[]>();
  for (const col of columns) map.set(col.id, []);
  for (const card of cards) {
    const status = String(card[field] ?? "");
    let target: KanbanColumn | undefined;
    for (const col of columns) {
      if (col.id === status) {
        target = col;
        break;
      }
      if (col.accept && col.accept.includes(status)) {
        target = col;
        break;
      }
    }
    if (target) map.get(target.id)!.push(card);
  }
  return map;
}
