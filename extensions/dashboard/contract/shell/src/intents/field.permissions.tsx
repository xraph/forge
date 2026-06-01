import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { useContractQuery } from "../contract/hooks";
import { useContributor } from "../runtime/context";
import { useFormState } from "./form.edit";
import { LoadingNode, ErrorNode } from "../runtime/fallbacks";
import type { IntentComponentProps } from "../runtime/registry";

interface FieldPermissionsProps {
  name: string;
  label?: string;
  description?: string;
  /**
   * Query intent that returns the available permissions tree. Each row
   * is expected to have { id, action, resource, name? }.
   */
  permissionsQuery: string;
}

interface PermissionRow {
  id: string;
  action?: string;
  resource?: string;
  name?: string;
}

/**
 * field.permissions is a form.edit field that renders a checkbox group
 * of available permissions and writes the selected IDs into form state
 * under props.name as an array of strings. v1 ships as a flat checkbox
 * list grouped by resource; a future iteration will swap to
 * organism.tree-view for nested resource/action hierarchies.
 */
export function FieldPermissions({
  props,
}: IntentComponentProps<unknown, FieldPermissionsProps>) {
  const form = useFormState();
  const contributor = useContributor();
  const query = useContractQuery<{ items?: PermissionRow[] } | PermissionRow[]>(
    contributor,
    props.permissionsQuery,
  );

  if (query.isLoading) return <LoadingNode />;
  if (query.error) return <ErrorNode message={(query.error as Error).message} />;

  const rows = extractRows(query.data);
  const selected = Array.isArray(form.values[props.name])
    ? (form.values[props.name] as string[])
    : [];

  const toggle = (id: string, on: boolean) => {
    const next = on
      ? Array.from(new Set([...selected, id]))
      : selected.filter((s) => s !== id);
    form.setValue(props.name, next);
  };

  const grouped = groupByResource(rows);

  return (
    <div className="space-y-3">
      <Label>
        {props.label ?? props.name}
      </Label>
      {props.description ? (
        <p className="text-sm text-muted-foreground">{props.description}</p>
      ) : null}
      <div className="space-y-4 rounded-md border p-3">
        {grouped.map(([resource, perms]) => (
          <div key={resource}>
            <p className="mb-1 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
              {resource}
            </p>
            <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
              {perms.map((p) => {
                const id = `field-perm-${p.id}`;
                return (
                  <label key={p.id} htmlFor={id} className="flex items-center gap-2 text-sm">
                    <Checkbox
                      id={id}
                      checked={selected.includes(p.id)}
                      onCheckedChange={(c) => toggle(p.id, Boolean(c))}
                    />
                    <span>{p.action ?? p.name ?? p.id}</span>
                  </label>
                );
              })}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function extractRows(data: unknown): PermissionRow[] {
  if (!data) return [];
  if (Array.isArray(data)) return data as PermissionRow[];
  const obj = data as Record<string, unknown>;
  if (Array.isArray(obj.items)) return obj.items as PermissionRow[];
  for (const v of Object.values(obj)) {
    if (Array.isArray(v)) return v as PermissionRow[];
  }
  return [];
}

function groupByResource(rows: PermissionRow[]): [string, PermissionRow[]][] {
  const m = new Map<string, PermissionRow[]>();
  for (const r of rows) {
    const key = r.resource ?? "permissions";
    const list = m.get(key) ?? [];
    list.push(r);
    m.set(key, list);
  }
  return Array.from(m.entries()).sort((a, b) => a[0].localeCompare(b[0]));
}
