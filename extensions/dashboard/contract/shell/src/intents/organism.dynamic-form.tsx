import * as React from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Button } from "@/components/ui/button";
import { Loader2 } from "lucide-react";
import { AtomInput } from "./atom.input";
import { AtomTextarea } from "./atom.textarea";
import { AtomCheckbox } from "./atom.checkbox";
import { AtomSwitch } from "./atom.switch";
import { AtomRadio } from "./atom.radio";
import { AtomSelect } from "./atom.select";
import { AtomSlider } from "./atom.slider";
import { Combobox } from "./molecule.combobox";
import { TagInput } from "./molecule.tag-input";
import { DatePicker } from "./molecule.date-picker";
import { FileUpload } from "./molecule.file-upload";
import { GraphRenderer } from "../runtime/renderer";
import { useContractCommand, useContractQuery } from "../contract/hooks";
import { useContributor, useParent, useRouteParams } from "../runtime/context";
import { usePrincipalStore } from "../auth/principal";
import { LoadingNode, ErrorNode } from "../runtime/fallbacks";
import { FormStateContext } from "./form.edit";
import { dispatchAction, type Action } from "../runtime/actions";
import { evaluatePredicate, type Predicate } from "../runtime/predicate";
import { buildZodSchema, type FieldSpecLite } from "@/lib/zod-from-fieldspec";
import { cn } from "@/lib/utils";
import type { DataBinding } from "@/contract/types";
import type { IntentComponentProps } from "../runtime/registry";

interface FieldRule {
  kind: string;
  value?: unknown;
  message?: string;
  intent?: string;
}

interface OptionSource {
  static?: { label: string; value: string; description?: string; icon?: string; disabled?: boolean }[];
  data?: DataBinding;
  map?: {
    labelKey?: string;
    valueKey?: string;
    descriptionKey?: string;
    iconKey?: string;
    disabledKey?: string;
  };
}

interface FieldLayout {
  span?: number;
  fullWidth?: boolean;
  indent?: number;
}

interface FieldSpec {
  name: string;
  type: string;
  label?: string;
  description?: string;
  placeholder?: string;
  default?: unknown;
  required?: boolean;
  disabled?: Predicate;
  visible?: Predicate;
  validate?: FieldRule[];
  options?: OptionSource;
  dependencies?: string[];
  layout?: FieldLayout;
  helpText?: string;
  mask?: string;
  format?: string;
  custom?: string;
  min?: number;
  max?: number;
  step?: number;
  minLength?: number;
  maxLength?: number;
  rows?: number;
  accept?: string;
  multiple?: boolean;
}

interface FormSection {
  id: string;
  title?: string;
  description?: string;
  fields: string[];
  collapsible?: boolean;
  defaultOpen?: boolean;
}

interface SubmitConfig {
  label?: string;
  icon?: string;
  variant?: "default" | "destructive" | "outline" | "secondary" | "ghost" | "link";
  fullWidth?: boolean;
}

interface CancelConfig {
  label?: string;
  action?: Action;
}

interface DynamicFormProps {
  fields: FieldSpec[];
  sections?: FormSection[];
  layout?: "single" | "two-column" | "grid";
  gridCols?: number;
  defaultValues?: Record<string, unknown>;
  initialData?: DataBinding;
  validation?: "onSubmit" | "onChange" | "onBlur";
  submit?: SubmitConfig;
  cancel?: CancelConfig;
  reset?: { label?: string };
  onSuccess?: Action;
  onError?: Action;
  className?: string;
}

export function DynamicForm({
  node,
  props,
}: IntentComponentProps<unknown, DynamicFormProps>) {
  const contributor = useContributor();
  const parent = useParent();
  const route = useRouteParams();
  const principal = usePrincipalStore((s) => s.principal);

  const dataIntent = props.initialData?.intent;
  const dataParams = React.useMemo(() => {
    const out: Record<string, unknown> = {};
    if (props.initialData?.params) {
      for (const [k, v] of Object.entries(props.initialData.params)) {
        const obj = v as { value?: unknown; from?: string };
        if (obj?.from) {
          // Minimal resolver — see runtime/bindings.ts for the canonical version.
          out[k] = resolveFrom(obj.from, { parent, route });
        } else if (obj?.value !== undefined) {
          out[k] = obj.value;
        } else {
          out[k] = v;
        }
      }
    }
    return out;
  }, [props.initialData?.params, parent, route]);

  const initialQuery = useContractQuery<Record<string, unknown>>(
    contributor,
    dataIntent ?? "",
    undefined,
    dataParams,
  );

  const schema = React.useMemo(() => buildZodSchema(props.fields as FieldSpecLite[]), [props.fields]);

  const defaults = React.useMemo(() => {
    const out: Record<string, unknown> = {};
    for (const f of props.fields) {
      if (f.default !== undefined) out[f.name] = f.default;
      else if (f.type === "checkbox" || f.type === "switch") out[f.name] = false;
      else if (f.type === "multi-select" || f.type === "tag-input") out[f.name] = [];
      else out[f.name] = "";
    }
    return { ...out, ...(props.defaultValues ?? {}) };
  }, [props.fields, props.defaultValues]);

  const form = useForm({
    resolver: zodResolver(schema),
    defaultValues: defaults,
    mode: props.validation === "onChange" ? "onChange" : props.validation === "onBlur" ? "onBlur" : "onSubmit",
  });

  // Hydrate from InitialData once it arrives.
  React.useEffect(() => {
    if (dataIntent && initialQuery.data) {
      form.reset({ ...defaults, ...initialQuery.data }, { keepDefaultValues: false });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialQuery.data, dataIntent]);

  // Subscribe to form values for conditional visibility/disabled predicates.
  const values = form.watch();

  const command = useContractCommand(contributor, node.op ?? "");

  const onSubmit = form.handleSubmit(async (data) => {
    if (!node.op) {
      console.warn("DynamicForm: no node.op set — values gathered but not dispatched.");
      return;
    }
    try {
      await command.mutateAsync(data);
      if (props.onSuccess) {
        dispatchAction(props.onSuccess, { value: data, parent, session: { user: principal }, route });
      }
    } catch (err) {
      if (props.onError) {
        dispatchAction(props.onError, { value: err, parent, session: { user: principal }, route });
      }
    }
  });

  if (dataIntent && initialQuery.isLoading) return <LoadingNode />;
  if (dataIntent && initialQuery.error) {
    return <ErrorNode message={(initialQuery.error as Error).message} />;
  }

  // Bridge to existing FormStateContext so atoms that read form values work
  // without rewriting them — DynamicForm pushes values + setValue through.
  const bridgeValue = {
    values: values as Record<string, unknown>,
    setValue: (name: string, value: unknown) => form.setValue(name, value, { shouldDirty: true }),
    submitting: command.isPending,
  };

  const visibleFields = props.fields.filter((f) =>
    !f.visible
      ? true
      : evaluatePredicate(f.visible, { form: values as Record<string, unknown>, parent, route, session: { user: principal } }),
  );

  const layoutCls =
    props.layout === "two-column"
      ? "grid grid-cols-1 md:grid-cols-2 gap-4"
      : props.layout === "grid"
        ? `grid grid-cols-1 md:grid-cols-${Math.min(12, Math.max(1, props.gridCols ?? 2))} gap-4`
        : "space-y-4";

  return (
    <FormStateContext.Provider value={bridgeValue}>
      <form
        onSubmit={onSubmit}
        className={cn("space-y-6", props.className)}
        noValidate
      >
        {props.sections && props.sections.length > 0 ? (
          props.sections.map((section) => (
            <SectionBlock
              key={section.id}
              section={section}
              fields={visibleFields.filter((f) => section.fields.includes(f.name))}
              layoutCls={layoutCls}
              values={values as Record<string, unknown>}
              errors={form.formState.errors}
              parent={parent}
              route={route}
              session={{ user: principal }}
            />
          ))
        ) : (
          <div className={layoutCls}>
            {visibleFields.map((f) => (
              <FieldBlock
                key={f.name}
                field={f}
                values={values as Record<string, unknown>}
                errors={form.formState.errors}
                parent={parent}
                route={route}
                session={{ user: principal }}
              />
            ))}
          </div>
        )}

        <div className="flex items-center justify-end gap-2">
          {props.reset ? (
            <Button
              type="button"
              variant="ghost"
              onClick={() => form.reset(defaults)}
              disabled={command.isPending}
            >
              {props.reset.label ?? "Reset"}
            </Button>
          ) : null}
          {props.cancel ? (
            <Button
              type="button"
              variant="outline"
              onClick={() => props.cancel?.action && dispatchAction(props.cancel.action, {})}
              disabled={command.isPending}
            >
              {props.cancel.label ?? "Cancel"}
            </Button>
          ) : null}
          <Button
            type="submit"
            variant={props.submit?.variant ?? "default"}
            disabled={command.isPending || !node.op}
            className={cn(props.submit?.fullWidth && "w-full")}
          >
            {command.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
            {props.submit?.label ?? "Save"}
          </Button>
        </div>
      </form>
    </FormStateContext.Provider>
  );
}

function SectionBlock({
  section,
  fields,
  layoutCls,
  values,
  errors,
  parent,
  route,
  session,
}: {
  section: FormSection;
  fields: FieldSpec[];
  layoutCls: string;
  values: Record<string, unknown>;
  errors: Record<string, unknown>;
  parent: unknown;
  route: Record<string, string>;
  session: { user: import("@/contract/types").Principal | null };
}) {
  if (fields.length === 0) return null;
  return (
    <div className="space-y-3">
      {section.title || section.description ? (
        <div className="space-y-1 border-b pb-2">
          {section.title ? <h3 className="text-sm font-semibold">{section.title}</h3> : null}
          {section.description ? (
            <p className="text-xs text-muted-foreground">{section.description}</p>
          ) : null}
        </div>
      ) : null}
      <div className={layoutCls}>
        {fields.map((f) => (
          <FieldBlock
            key={f.name}
            field={f}
            values={values}
            errors={errors}
            parent={parent}
            route={route}
            session={session}
          />
        ))}
      </div>
    </div>
  );
}

function FieldBlock({
  field: f,
  values,
  errors,
  parent,
  route,
  session,
}: {
  field: FieldSpec;
  values: Record<string, unknown>;
  errors: Record<string, unknown>;
  parent: unknown;
  route: Record<string, string>;
  session: { user: import("@/contract/types").Principal | null };
}) {
  const err = errors[f.name] as { message?: string } | undefined;
  const errorMessage = err?.message;
  const disabled = f.disabled
    ? evaluatePredicate(f.disabled, { form: values, parent, route, session })
    : false;

  const span = f.layout?.fullWidth ? "md:col-span-full" : f.layout?.span ? `md:col-span-${Math.min(12, f.layout.span)}` : "";

  if (f.type === "hidden") return null;

  return (
    <div className={cn("space-y-1.5", span)}>
      {f.label && f.type !== "checkbox" && f.type !== "switch" ? (
        <label htmlFor={f.name} className="text-sm font-medium leading-none">
          {f.label}
          {f.required ? <span className="ml-0.5 text-destructive">*</span> : null}
        </label>
      ) : null}
      {f.description && f.type !== "checkbox" && f.type !== "switch" ? (
        <p className="text-xs text-muted-foreground">{f.description}</p>
      ) : null}
      <FieldControl field={f} disabled={disabled} error={errorMessage} />
      {f.helpText && !errorMessage ? (
        <p className="text-xs text-muted-foreground">{f.helpText}</p>
      ) : null}
      {errorMessage ? <p className="text-xs text-destructive">{errorMessage}</p> : null}
    </div>
  );
}

function FieldControl({
  field: f,
  disabled,
  error,
}: {
  field: FieldSpec;
  disabled: boolean;
  error?: string;
}) {
  const common = { node: {} as never, slots: {} };
  const staticOpts = f.options?.static ?? [];
  switch (f.type) {
    case "text":
    case "email":
    case "password":
    case "number":
      return (
        <AtomInput
          {...common}
          props={{
            name: f.name,
            type: f.type as "text",
            placeholder: f.placeholder,
            disabled,
            error,
            minLength: f.minLength,
            maxLength: f.maxLength,
            min: f.min,
            max: f.max,
            step: f.step,
          }}
        />
      );
    case "textarea":
      return (
        <AtomTextarea
          {...common}
          props={{
            name: f.name,
            placeholder: f.placeholder,
            disabled,
            error,
            rows: f.rows ?? 4,
            minLength: f.minLength,
            maxLength: f.maxLength,
          }}
        />
      );
    case "checkbox":
      return (
        <AtomCheckbox
          {...common}
          props={{
            name: f.name,
            label: f.label,
            description: f.description,
            disabled,
            required: f.required,
            error,
          }}
        />
      );
    case "switch":
      return (
        <AtomSwitch
          {...common}
          props={{
            name: f.name,
            label: f.label,
            description: f.description,
            disabled,
          }}
        />
      );
    case "radio":
      return (
        <AtomRadio
          {...common}
          props={{
            name: f.name,
            options: staticOpts,
            disabled,
            required: f.required,
            error,
          }}
        />
      );
    case "select":
      return (
        <AtomSelect
          {...common}
          props={{
            name: f.name,
            placeholder: f.placeholder ?? "Select…",
            options: staticOpts,
            disabled,
            required: f.required,
            error,
          }}
        />
      );
    case "multi-select":
    case "combobox":
      return (
        <Combobox
          {...common}
          props={{
            name: f.name,
            placeholder: f.placeholder,
            options: staticOpts,
            multiple: f.type === "multi-select",
            disabled,
            error,
          }}
        />
      );
    case "tag-input":
      return (
        <TagInput
          {...common}
          props={{
            name: f.name,
            placeholder: f.placeholder,
            disabled,
            error,
          }}
        />
      );
    case "date":
    case "date-range":
      return (
        <DatePicker
          {...common}
          props={{
            name: f.name,
            placeholder: f.placeholder,
            mode: f.type === "date-range" ? "range" : "single",
            disabled,
            error,
          }}
        />
      );
    case "file":
      return (
        <FileUpload
          {...common}
          props={{
            name: f.name,
            accept: f.accept,
            multiple: f.multiple,
            disabled,
            error,
          }}
        />
      );
    case "slider":
      return (
        <AtomSlider
          {...common}
          props={{
            name: f.name,
            min: f.min,
            max: f.max,
            step: f.step,
            disabled,
            showValue: true,
          }}
        />
      );
    case "custom":
      if (!f.custom) return <p className="text-xs text-destructive">Missing `custom` intent for field {f.name}</p>;
      return <GraphRenderer node={{ intent: f.custom, props: { name: f.name } }} />;
    default:
      return (
        <AtomInput
          {...common}
          props={{
            name: f.name,
            type: "text",
            placeholder: f.placeholder,
            disabled,
            error,
          }}
        />
      );
  }
}

function resolveFrom(path: string, ctx: { parent?: unknown; route?: Record<string, string> }): unknown {
  const segs = path.split(".");
  const root = segs.shift();
  let cursor: unknown =
    root === "parent" ? ctx.parent : root === "route" ? ctx.route : undefined;
  for (const s of segs) {
    if (!cursor || typeof cursor !== "object") return undefined;
    cursor = (cursor as Record<string, unknown>)[s];
  }
  return cursor;
}
