import { GraphRenderer } from "../runtime/renderer";
import { cn } from "@/lib/utils";
import type { GraphNode } from "@/contract/types";
import type { IntentComponentProps } from "../runtime/registry";

interface SessionListProps {
  revokeOp?: string;
  revokeAllOp?: string;
  showCurrent?: boolean;
  currentSessionField?: string;
  className?: string;
}

/**
 * auth.session-list composes a DataGrid showing active sessions with a
 * per-row revoke action and an optional bulk revoke. node.data binds the
 * session list query — the renderer wires it through to organism.data-grid.
 */
export function SessionList({ node, props }: IntentComponentProps<unknown, SessionListProps>) {
  const graph: GraphNode = {
    intent: "organism.data-grid",
    data: node.data,
    props: {
      rowId: "id",
      columns: [
        { id: "device", field: "device", header: "Device", type: "text" },
        { id: "location", field: "location", header: "Location", type: "text" },
        { id: "ip", field: "ipAddress", header: "IP address", type: "text", truncate: true },
        { id: "lastUsed", field: "lastUsedAt", header: "Last used", type: "datetime", sortable: true },
        { id: "current", field: "isCurrent", header: "Status", type: "badge" },
      ],
      density: "compact",
      pagination: { pageSize: 25 },
      toolbar: { search: true, refresh: true, title: "Active sessions" },
      rowActions: props.revokeOp
        ? [
            {
              id: "revoke",
              label: "Revoke session",
              icon: "log-out",
              variant: "destructive",
              visible: { not: ["form.isCurrent"] },
              action: { kind: "runCommand", intent: props.revokeOp },
            },
          ]
        : undefined,
      bulkActions: props.revokeOp
        ? [
            {
              id: "bulk-revoke",
              label: "Revoke selected",
              icon: "log-out",
              variant: "destructive",
              action: { kind: "runCommand", intent: props.revokeOp },
            },
          ]
        : undefined,
    },
  };

  return (
    <div className={cn("space-y-3", props.className)}>
      <GraphRenderer node={graph} />
      {props.revokeAllOp ? (
        <GraphRenderer
          node={{
            intent: "atom.button",
            op: props.revokeAllOp,
            props: {
              label: "Sign out everywhere else",
              variant: "outline",
              confirm: "This will end all other sessions. Continue?",
              leadingIcon: "log-out",
            },
          }}
        />
      ) : null}
    </div>
  );
}
