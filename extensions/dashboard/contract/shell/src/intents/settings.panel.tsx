import * as React from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Switch } from "@/components/ui/switch";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Loader2 } from "lucide-react";
import { useContractCommand, useContractQuery } from "../contract/hooks";
import { useContributor } from "../runtime/context";
import { LoadingNode, ErrorNode } from "../runtime/fallbacks";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

// SettingsScope mirrors the authsome settings.Scope wire values.
export type SettingsScope = "global" | "app" | "org" | "user";

// SettingField is the wire-side shape for one resolved setting. Keep
// these field names in sync with handlers_settings.go's projection.
interface SettingField {
  key: string;
  displayName: string;
  description?: string;
  type: string; // "string" | "int" | "float" | "bool" | "object" | "array"
  inputType?: string; // "text" | "number" | "switch" | "select" | "textarea" | ...
  default?: unknown;
  effectiveValue?: unknown;
  isOverridden?: boolean;
  isEnforced?: boolean;
  canOverride?: boolean;
  readOnly?: boolean;
  sensitive?: boolean;
  placeholder?: string;
  helpText?: string;
  options?: { label: string; value: string }[];
  validation?: { min?: number; max?: number; required?: boolean; pattern?: string };
  order?: number;
  section?: string;
}

interface SettingCategory {
  name: string;
  description?: string;
  settings: SettingField[];
}

interface SettingsNamespacePayload {
  namespace: string;
  displayName?: string;
  categories: SettingCategory[];
}

export interface SettingsPanelProps {
  /** Namespace to render. Required. */
  namespace: string;
  /** Scope to read/write (global/app/org/user). Defaults to the parent scope. */
  scope?: SettingsScope;
  /** Scope target IDs. */
  appId?: string;
  orgId?: string;
  userId?: string;
  /** Intent name for the namespace query (default: "settings.namespace"). */
  queryIntent?: string;
  /** Intent name for the update command (default: "settings.update"). */
  updateOp?: string;
  /** Render in bare mode (no top-level Card wrapper) — useful when nested. */
  bare?: boolean;
}

/**
 * settings.panel renders one plugin namespace's settings as category
 * cards. Each Category becomes a Card; fields inside the card are
 * rendered per the field's `inputType` (switch / number / select /
 * textarea / text / etc.) with override / enforced badges and
 * help-text affordances. Save is per-card — the button collects only
 * the fields that changed within that card and dispatches one
 * `settings.update` command per changed field.
 *
 * The renderer reuses existing forgeui primitives (Card, Tabs, Input,
 * Select, Switch, Textarea, Badge) — no new lower-level components
 * needed. The shape of the SettingField wire model is defined in
 * authsome's handlers_settings.go projection; field-name drift between
 * the two surfaces is a wire break.
 */
export function SettingsPanel({
  props,
}: IntentComponentProps<unknown, SettingsPanelProps>) {
  const contributor = useContributor();
  const queryIntent = props.queryIntent ?? "settings.namespace";
  const updateOp = props.updateOp ?? "settings.update";
  const scope = props.scope ?? "global";

  const params = React.useMemo(
    () => ({
      namespace: props.namespace,
      scope,
      appId: props.appId ?? "",
      orgId: props.orgId ?? "",
      userId: props.userId ?? "",
    }),
    [props.namespace, scope, props.appId, props.orgId, props.userId],
  );

  const query = useContractQuery<SettingsNamespacePayload>(
    contributor,
    queryIntent,
    undefined,
    params,
  );

  if (query.isLoading) return <LoadingNode />;
  if (query.error) return <ErrorNode message={(query.error as Error).message} />;
  if (!query.data) return null;

  const data = query.data;
  const categories = data.categories ?? [];

  if (categories.length === 0) {
    return (
      <div className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
        No settings declared for {data.displayName ?? data.namespace}.
      </div>
    );
  }

  return (
    <div className={cn("space-y-6", props.bare && "space-y-4")}>
      {categories.map((cat) => (
        <CategoryCard
          key={cat.name}
          category={cat}
          scope={scope}
          appId={props.appId}
          orgId={props.orgId}
          userId={props.userId}
          updateOp={updateOp}
          bare={props.bare}
          contributor={contributor}
          onSaved={() => query.refetch()}
        />
      ))}
    </div>
  );
}

interface CategoryCardProps {
  category: SettingCategory;
  scope: SettingsScope;
  appId?: string;
  orgId?: string;
  userId?: string;
  updateOp: string;
  bare?: boolean;
  contributor: string;
  onSaved: () => void;
}

function CategoryCard({
  category,
  scope,
  appId,
  orgId,
  userId,
  updateOp,
  bare,
  contributor,
  onSaved,
}: CategoryCardProps) {
  const update = useContractCommand<Record<string, unknown>, unknown>(contributor, updateOp);
  const [values, setValues] = React.useState<Record<string, unknown>>({});
  const [dirty, setDirty] = React.useState<Set<string>>(new Set());
  const [error, setError] = React.useState<string | null>(null);
  const [success, setSuccess] = React.useState(false);

  const setValue = (key: string, value: unknown) => {
    setValues((v) => ({ ...v, [key]: value }));
    setDirty((d) => new Set(d).add(key));
    setSuccess(false);
  };

  const getValue = (s: SettingField): unknown => {
    if (dirty.has(s.key)) return values[s.key];
    return s.effectiveValue ?? s.default;
  };

  const handleSave = async () => {
    setError(null);
    setSuccess(false);
    const toSave = Array.from(dirty);
    if (toSave.length === 0) return;
    try {
      for (const key of toSave) {
        await update.mutateAsync({
          key,
          value: values[key],
          scope,
          appId: appId ?? "",
          orgId: orgId ?? "",
          userId: userId ?? "",
        });
      }
      setDirty(new Set());
      setValues({});
      setSuccess(true);
      onSaved();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const sortedFields = [...category.settings].sort(
    (a, b) => (a.order ?? 999) - (b.order ?? 999),
  );

  const body = (
    <div className="space-y-5">
      {sortedFields.map((s) => (
        <FieldRow
          key={s.key}
          field={s}
          value={getValue(s)}
          onChange={(v) => setValue(s.key, v)}
        />
      ))}
      <div className="flex items-center justify-end gap-3 pt-2">
        {error ? <p className="text-xs text-destructive">{error}</p> : null}
        {success ? <p className="text-xs text-muted-foreground">Saved.</p> : null}
        <Button onClick={handleSave} disabled={dirty.size === 0 || update.isPending}>
          {update.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
          {dirty.size === 0 ? "No changes" : `Save ${dirty.size} change${dirty.size > 1 ? "s" : ""}`}
        </Button>
      </div>
    </div>
  );

  if (bare) {
    return (
      <div className="space-y-3">
        <h3 className="text-base font-semibold">{category.name}</h3>
        {category.description ? (
          <p className="text-sm text-muted-foreground">{category.description}</p>
        ) : null}
        {body}
      </div>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{category.name}</CardTitle>
        {category.description ? <CardDescription>{category.description}</CardDescription> : null}
      </CardHeader>
      <CardContent>{body}</CardContent>
    </Card>
  );
}

interface FieldRowProps {
  field: SettingField;
  value: unknown;
  onChange: (v: unknown) => void;
}

function FieldRow({ field, value, onChange }: FieldRowProps) {
  const inputType = field.inputType ?? inferInputType(field.type);
  const id = `setting-${field.key.replace(/[^\w]/g, "-")}`;
  const disabled = field.readOnly || field.isEnforced || !(field.canOverride ?? true);

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <Label htmlFor={id} className="text-sm font-medium">
          {field.displayName || field.key}
          {field.validation?.required ? (
            <span className="ml-0.5 text-destructive">*</span>
          ) : null}
        </Label>
        <div className="flex items-center gap-1">
          {field.isOverridden ? <Badge variant="secondary">Customized</Badge> : null}
          {field.isEnforced ? <Badge variant="outline">Enforced</Badge> : null}
        </div>
      </div>
      {field.description ? (
        <p className="text-xs text-muted-foreground">{field.description}</p>
      ) : null}
      <FieldInput
        id={id}
        field={field}
        inputType={inputType}
        value={value}
        disabled={disabled}
        onChange={onChange}
      />
      {field.helpText ? (
        <p className="text-xs text-muted-foreground">{field.helpText}</p>
      ) : null}
    </div>
  );
}

interface FieldInputProps {
  id: string;
  field: SettingField;
  inputType: string;
  value: unknown;
  disabled: boolean;
  onChange: (v: unknown) => void;
}

function FieldInput({ id, field, inputType, value, disabled, onChange }: FieldInputProps) {
  switch (inputType) {
    case "switch":
    case "checkbox":
      return (
        <div className="flex items-center gap-2">
          <Switch
            id={id}
            checked={Boolean(value)}
            disabled={disabled}
            onCheckedChange={(v) => onChange(Boolean(v))}
          />
          {field.placeholder ? (
            <span className="text-sm text-muted-foreground">{field.placeholder}</span>
          ) : null}
        </div>
      );
    case "number":
      return (
        <Input
          id={id}
          type="number"
          value={typeof value === "number" ? String(value) : ""}
          placeholder={field.placeholder}
          disabled={disabled}
          min={field.validation?.min}
          max={field.validation?.max}
          onChange={(e) => {
            const raw = e.target.value;
            onChange(raw === "" ? null : Number(raw));
          }}
        />
      );
    case "select":
    case "radio": {
      const opts = field.options ?? [];
      const current = typeof value === "string" ? value : "";
      return (
        <Select value={current || undefined} onValueChange={(v) => onChange(v)} disabled={disabled}>
          <SelectTrigger id={id}>
            <SelectValue>{current ? undefined : field.placeholder ?? "Select…"}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            {opts.map((o) => (
              <SelectItem key={o.value} value={o.value}>
                {o.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      );
    }
    case "textarea":
      return (
        <Textarea
          id={id}
          value={typeof value === "string" ? value : ""}
          placeholder={field.placeholder}
          disabled={disabled}
          onChange={(e) => onChange(e.target.value)}
          rows={4}
        />
      );
    case "email":
    case "url":
    case "tel":
    case "date":
    case "password":
    case "text":
    default:
      return (
        <Input
          id={id}
          type={inputType === "text" ? undefined : inputType}
          value={typeof value === "string" ? value : value == null ? "" : String(value)}
          placeholder={field.placeholder}
          disabled={disabled}
          onChange={(e) => onChange(e.target.value)}
        />
      );
  }
}

function inferInputType(t: string): string {
  switch (t) {
    case "bool":
    case "boolean":
      return "switch";
    case "int":
    case "float":
    case "number":
      return "number";
    case "array":
      return "textarea";
    case "object":
      return "textarea";
    case "string":
    default:
      return "text";
  }
}
