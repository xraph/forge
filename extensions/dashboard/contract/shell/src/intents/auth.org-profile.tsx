import { GraphRenderer } from "../runtime/renderer";
import { cn } from "@/lib/utils";
import type { DataBinding, GraphNode } from "@/contract/types";
import type { IntentComponentProps } from "../runtime/registry";

interface Role {
  label: string;
  value: string;
  description?: string;
}

interface OrgProfileProps {
  orgSource?: DataBinding;
  membersSource?: DataBinding;
  updateOp?: string;
  inviteOp?: string;
  removeMemberOp?: string;
  updateMemberRoleOp?: string;
  deleteOrgOp?: string;
  availableRoles?: Role[];
  className?: string;
}

/**
 * auth.org-profile composes layout.tabs + organism.dynamic-form (general
 * settings) + organism.data-grid (members) + danger-zone delete. It's a
 * thin wrapper that builds a graph at render time so individual deployments
 * can rely on a single intent instead of stitching the pieces by hand.
 */
export function OrgProfile({
  node,
  props,
}: IntentComponentProps<unknown, OrgProfileProps>) {
  const roles = props.availableRoles ?? [
    { label: "Admin", value: "admin" },
    { label: "Member", value: "member" },
  ];

  const graph: GraphNode = {
    intent: "layout.tabs",
    props: {
      defaultValue: "general",
      items: [
        { value: "general", label: "General" },
        { value: "members", label: "Members" },
        { value: "danger", label: "Danger zone" },
      ],
    },
    slots: {
      general: [
        {
          intent: "organism.dynamic-form",
          op: props.updateOp,
          data: props.orgSource,
          props: {
            fields: [
              { name: "name", type: "text", label: "Organization name", required: true },
              { name: "slug", type: "text", label: "URL slug", helpText: "Used in URLs and APIs" },
              { name: "description", type: "textarea", label: "Description", rows: 3 },
              { name: "website", type: "text", label: "Website" },
            ],
            initialData: props.orgSource,
            submit: { label: "Save changes" },
          },
        },
      ],
      members: [
        {
          intent: "organism.data-grid",
          data: props.membersSource,
          props: {
            rowId: "id",
            columns: [
              { id: "user", field: "user.name", header: "Member", type: "text" },
              { id: "email", field: "user.email", header: "Email", type: "text" },
              { id: "role", field: "role", header: "Role", type: "badge" },
              { id: "joinedAt", field: "joinedAt", header: "Joined", type: "date", sortable: true },
            ],
            selection: { mode: "multi" },
            pagination: { pageSize: 25, pageSizeOptions: [10, 25, 50] },
            toolbar: { search: true, columnPicker: true },
            rowActions: props.removeMemberOp || props.updateMemberRoleOp
              ? [
                  ...(props.updateMemberRoleOp
                    ? roles.map((r) => ({
                        id: `set-${r.value}`,
                        label: `Set role: ${r.label}`,
                        action: { kind: "runCommand", intent: props.updateMemberRoleOp, params: { role: { value: r.value } } },
                      }))
                    : []),
                  ...(props.removeMemberOp
                    ? [
                        {
                          id: "remove",
                          label: "Remove from org",
                          icon: "user-x",
                          variant: "destructive",
                          action: { kind: "runCommand", intent: props.removeMemberOp },
                        },
                      ]
                    : []),
                ]
              : undefined,
            bulkActions: props.removeMemberOp
              ? [
                  {
                    id: "bulk-remove",
                    label: "Remove selected",
                    icon: "user-x",
                    variant: "destructive",
                    action: { kind: "runCommand", intent: props.removeMemberOp },
                  },
                ]
              : undefined,
          },
        },
        ...(props.inviteOp
          ? [
              {
                intent: "molecule.alert",
                props: {
                  variant: "muted",
                  title: "Invite members",
                  description: "Send invites via email or generate a shareable link.",
                  icon: "user-plus",
                },
              } as GraphNode,
            ]
          : []),
      ],
      danger: props.deleteOrgOp
        ? [
            {
              intent: "molecule.alert",
              props: {
                variant: "destructive",
                title: "Delete organization",
                description: "This action is permanent. All data will be deleted.",
                icon: "alert-triangle",
              },
            },
            {
              intent: "atom.button",
              op: props.deleteOrgOp,
              props: {
                label: "Delete organization",
                variant: "destructive",
                confirm: "Are you absolutely sure? This cannot be undone.",
              },
            },
          ]
        : [],
    },
  };

  return (
    <div className={cn("space-y-6", props.className)}>
      {node.title ? <h2 className="text-2xl font-semibold">{node.title}</h2> : null}
      <GraphRenderer node={graph} />
    </div>
  );
}
