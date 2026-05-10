import * as React from "react";
import { Button } from "@/components/ui/button";
import { useContractCommand, useContractQuery } from "../contract/hooks";
import { useContributor, useParent } from "../runtime/context";
import { usePrincipalStore } from "../auth/principal";
import { resolvePayload, resolveValue } from "../runtime/bindings";
import { LoadingNode, ErrorNode } from "../runtime/fallbacks";
import { SlotRenderer } from "../runtime/slots";
import type { IntentComponentProps } from "../runtime/registry";

interface FormEditProps {
  submitLabel?: string;
  cancelLabel?: string;
}

interface FormStateValue {
  /** Current value of each field by name. */
  values: Record<string, unknown>;
  /** Setter for a single field. */
  setValue: (name: string, value: unknown) => void;
  /** Whether the form is currently submitting (mutation in flight). */
  submitting: boolean;
}

export const FormStateContext = React.createContext<FormStateValue | null>(null);

/** useFormState is consumed by form.field to register itself with the parent form.edit. */
export function useFormState(): FormStateValue {
  const ctx = React.useContext(FormStateContext);
  if (!ctx) throw new Error("useFormState called outside form.edit");
  return ctx;
}

/**
 * form.edit wraps a fields slot and submits the gathered values via the
 * intent referenced by node.op. When node.data is present, the form preloads
 * values from that query intent (typically a resource.detail-style fetch).
 */
export function FormEdit({
  node,
  props,
  slots,
}: IntentComponentProps<unknown, FormEditProps>) {
  const fallbackContributor = useContributor();
  const parent = useParent();
  const principal = usePrincipalStore((s) => s.principal);

  const dataIntent = node.data?.intent;
  const dataParams = React.useMemo(() => {
    return resolvePayload(node.data?.params as Record<string, unknown> | undefined, {
      parent,
      session: { user: principal },
    });
  }, [node.data?.params, parent, principal]);

  const initialQuery = useContractQuery<Record<string, unknown>>(
    fallbackContributor,
    dataIntent ?? "",
    undefined,
    dataParams,
  );

  const [values, setValues] = React.useState<Record<string, unknown>>({});

  // Hydrate from the loaded data the first time it arrives.
  React.useEffect(() => {
    if (!dataIntent) return;
    if (initialQuery.data && Object.keys(values).length === 0) {
      setValues(initialQuery.data);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialQuery.data, dataIntent]);

  const command = useContractCommand(fallbackContributor, node.op ?? "");

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!node.op) return;
    // Merge resolved manifest payload (e.g., id from parent) with field values.
    const manifestPayload = resolvePayload(node.payload, {
      parent,
      session: { user: principal },
    });
    command.mutate({ ...manifestPayload, ...values });
  };

  const setValue = React.useCallback((name: string, value: unknown) => {
    setValues((v) => ({ ...v, [name]: value }));
  }, []);

  if (dataIntent && initialQuery.isLoading) return <LoadingNode />;
  if (dataIntent && initialQuery.error) {
    return <ErrorNode message={(initialQuery.error as Error).message} />;
  }

  return (
    <form onSubmit={submit} className="space-y-4">
      <FormStateContext.Provider value={{ values, setValue, submitting: command.isPending }}>
        <SlotRenderer slot="fields" slots={slots} />
        <div className="flex justify-end gap-2">
          <Button type="submit" disabled={command.isPending || !node.op}>
            {props.submitLabel ?? "Save"}
          </Button>
        </div>
      </FormStateContext.Provider>
    </form>
  );
}

// Keep resolveValue referenced so unused-import lint doesn't strip it; the
// helper may be useful for richer field bindings in slice (f).
void resolveValue;
