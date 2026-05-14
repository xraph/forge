import { MoreHorizontal } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useContractCommand } from "../contract/hooks";
import { useContributor, useParent, useRouteParams } from "../runtime/context";
import { usePrincipalStore } from "../auth/principal";
import { resolvePayload } from "../runtime/bindings";
import type { GraphNode } from "@/contract/types";
import type { IntentComponentProps } from "../runtime/registry";

interface ActionMenuProps {
  label?: string;
}

/**
 * action.menu groups action.button-shaped children into a dropdown. Children
 * with intent="action.divider" render as separators; everything else renders
 * as a clickable item that issues the command.
 */
export function ActionMenu({ node, props, slots }: IntentComponentProps<unknown, ActionMenuProps>) {
  const items = slots["items"] ?? [];
  const label = props.label ?? node.title ?? "Actions";

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" aria-label={label} onClick={(e) => e.stopPropagation()}>
          <MoreHorizontal className="h-4 w-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" onClick={(e) => e.stopPropagation()}>
        {items.map((child, i) => (
          <MenuChild key={`${child.intent}:${i}`} node={child} />
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function MenuChild({ node }: { node: GraphNode }) {
  const fallbackContributor = useContributor();
  const parent = useParent();
  const route = useRouteParams();
  const principal = usePrincipalStore((s) => s.principal);

  if (node.intent === "action.divider") {
    return <DropdownMenuSeparator />;
  }

  const targetContributor =
    (typeof node.props?.contributor === "string" ? node.props.contributor : undefined) ??
    fallbackContributor;
  const op = node.op ?? "";
  const label =
    (typeof node.props?.label === "string" ? node.props.label : undefined) ??
    node.title ??
    op ??
    "Action";

  const command = useContractCommand(targetContributor ?? "core-contract", op);

  return (
    <DropdownMenuItem
      onClick={() => {
        if (!op) return;
        const payload = resolvePayload(node.payload, {
          parent,
          session: { user: principal },
          route,
        });
        command.mutate(payload);
      }}
      disabled={!op || command.isPending}
    >
      {label}
    </DropdownMenuItem>
  );
}
