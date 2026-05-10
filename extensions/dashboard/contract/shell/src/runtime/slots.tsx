import type { GraphNode } from "../contract/types";
import { GraphRenderer } from "./renderer";

export function SlotRenderer({
  slot,
  slots,
}: {
  slot: string;
  slots: Record<string, GraphNode[]>;
}) {
  const children = slots[slot] ?? [];
  return (
    <>
      {children.map((child, i) => (
        <GraphRenderer key={`${child.intent}:${i}`} node={child} />
      ))}
    </>
  );
}
