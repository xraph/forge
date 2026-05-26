import * as React from "react";
import { ChevronRight } from "lucide-react";
import { GraphRenderer } from "../runtime/renderer";
import { useContractQuery } from "../contract/hooks";
import { useContributor, useParent, useRouteParams } from "../runtime/context";
import { LoadingNode, ErrorNode } from "../runtime/fallbacks";
import { Icon } from "./atom.icon";
import { resolveValue } from "../runtime/bindings";
import { dispatchAction, type Action } from "../runtime/actions";
import { cn } from "@/lib/utils";
import type { GraphNode } from "@/contract/types";
import type { IntentComponentProps } from "../runtime/registry";

interface TreeViewProps {
  childrenField?: string;
  labelField?: string;
  iconField?: string;
  nodeIntent?: string;
  defaultExpand?: number;
  onSelect?: Action;
  className?: string;
}

interface TreeNode {
  id: string;
  data: Record<string, unknown>;
  children: TreeNode[];
}

export function TreeView({ node, props }: IntentComponentProps<unknown, TreeViewProps>) {
  const contributor = useContributor();
  const parent = useParent();
  const route = useRouteParams();
  const childrenField = props.childrenField ?? "children";
  const labelField = props.labelField ?? "label";
  const defaultExpand = props.defaultExpand ?? 1;

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
  const [expanded, setExpanded] = React.useState<Record<string, boolean>>({});

  if (!node.data?.intent && !node.data?.queryRef) {
    return <ErrorNode message="TreeView requires a data binding" />;
  }
  if (query.isLoading) return <LoadingNode />;
  if (query.error) return <ErrorNode message={(query.error as Error).message} />;

  const roots = normalize(query.data, childrenField);

  const toggle = (id: string) => setExpanded((s) => ({ ...s, [id]: !s[id] }));

  return (
    <ul className={cn("space-y-0.5 text-sm", props.className)}>
      {roots.map((r) => (
        <TreeNodeRow
          key={r.id}
          node={r}
          depth={0}
          expanded={expanded}
          toggle={toggle}
          defaultExpand={defaultExpand}
          labelField={labelField}
          iconField={props.iconField}
          nodeIntent={props.nodeIntent}
          onSelect={props.onSelect}
        />
      ))}
    </ul>
  );
}

function TreeNodeRow({
  node,
  depth,
  expanded,
  toggle,
  defaultExpand,
  labelField,
  iconField,
  nodeIntent,
  onSelect,
}: {
  node: TreeNode;
  depth: number;
  expanded: Record<string, boolean>;
  toggle: (id: string) => void;
  defaultExpand: number;
  labelField: string;
  iconField?: string;
  nodeIntent?: string;
  onSelect?: Action;
}) {
  const isOpen = expanded[node.id] ?? depth < defaultExpand;
  const hasChildren = node.children.length > 0;

  return (
    <li>
      <div
        role="treeitem"
        aria-expanded={hasChildren ? isOpen : undefined}
        tabIndex={0}
        onClick={() => onSelect && dispatchAction(onSelect, { value: node.data })}
        className={cn(
          "group flex items-center gap-1 rounded-md px-2 py-1 hover:bg-accent",
          onSelect && "cursor-pointer",
        )}
        style={{ paddingLeft: 8 + depth * 14 }}
      >
        {hasChildren ? (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              toggle(node.id);
            }}
            className="inline-flex h-5 w-5 items-center justify-center rounded hover:bg-foreground/10"
            aria-label={isOpen ? "Collapse" : "Expand"}
          >
            <ChevronRight className={cn("h-3.5 w-3.5 transition-transform", isOpen && "rotate-90")} />
          </button>
        ) : (
          <span className="inline-block h-5 w-5" />
        )}
        {iconField && node.data[iconField] ? (
          <Icon
            node={{} as never}
            slots={{}}
            props={{ name: String(node.data[iconField]), size: 14 }}
          />
        ) : null}
        {nodeIntent ? (
          <GraphRenderer node={{ intent: nodeIntent, props: { row: node.data } } as GraphNode} />
        ) : (
          <span className="truncate">{String(node.data[labelField] ?? node.id)}</span>
        )}
      </div>
      {hasChildren && isOpen ? (
        <ul className="space-y-0.5">
          {node.children.map((child) => (
            <TreeNodeRow
              key={child.id}
              node={child}
              depth={depth + 1}
              expanded={expanded}
              toggle={toggle}
              defaultExpand={defaultExpand}
              labelField={labelField}
              iconField={iconField}
              nodeIntent={nodeIntent}
              onSelect={onSelect}
            />
          ))}
        </ul>
      ) : null}
    </li>
  );
}

function normalize(data: unknown, childrenField: string): TreeNode[] {
  const raw = extractRoots(data);
  const walk = (item: Record<string, unknown>, path: string): TreeNode => {
    const id = String(item.id ?? path);
    const kids = Array.isArray(item[childrenField])
      ? (item[childrenField] as Record<string, unknown>[]).map((c, i) => walk(c, `${id}.${i}`))
      : [];
    return { id, data: item, children: kids };
  };
  return raw.map((item, i) => walk(item, `r${i}`));
}

function extractRoots(data: unknown): Record<string, unknown>[] {
  if (Array.isArray(data)) return data as Record<string, unknown>[];
  if (data && typeof data === "object") {
    const obj = data as Record<string, unknown>;
    for (const k of ["nodes", "items", "tree", "data", "roots"]) {
      if (Array.isArray(obj[k])) return obj[k] as Record<string, unknown>[];
    }
  }
  return [];
}
