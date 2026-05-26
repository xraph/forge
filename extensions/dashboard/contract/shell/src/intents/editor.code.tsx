import * as React from "react";
import { useContractQuery, useContractCommand } from "../contract/hooks";
import { useContributor, useRouteParams } from "../runtime/context";
import { resolvePayload } from "../runtime/bindings";
import { LoadingNode, ErrorNode } from "../runtime/fallbacks";
import { CodeEditor } from "./organism.code-editor";
import { FormStateContext } from "./form.edit";
import { Button } from "@/components/ui/button";
import type { IntentComponentProps } from "../runtime/registry";

interface EditorCodeProps {
  /** Language for the underlying organism.code-editor (json, yaml, etc.). */
  language?: string;
  /** Editor height (CSS value). */
  height?: string;
  /** When read-only, the Save button is hidden. */
  readOnly?: boolean;
  /** Field on the loaded query payload to read the editor's initial value from. */
  valueField?: string;
  /** Label for the save button. Defaults to "Save". */
  saveLabel?: string;
  /**
   * Field name the editor writes into the form state. Defaults to "value".
   * The save command receives this under the same key, merged with manifest
   * payload and route params.
   */
  fieldName?: string;
}

/**
 * editor.code is a query-bound full-page code editor. It loads a value
 * from node.data (typically settings.detail), renders an editable code
 * surface via organism.code-editor, and submits changes via node.op
 * when the save button is pressed.
 *
 * editor.code creates its own FormStateContext so organism.code-editor's
 * existing form-state hook (`useFormStateOptional`) writes back to a
 * local store on every edit. On save, the latest form-state values are
 * merged with the manifest's payload bindings (route params, parent
 * fields, etc.) and dispatched to node.op.
 */
export function EditorCode({
  node,
  props,
}: IntentComponentProps<unknown, EditorCodeProps>) {
  const contributor = useContributor();
  const route = useRouteParams();

  const fieldName = props.fieldName ?? "value";
  const dataIntent = node.data?.intent ?? "";

  const dataParams = React.useMemo(
    () => resolvePayload(node.data?.params as Record<string, unknown> | undefined, { route }),
    [node.data?.params, route],
  );

  const query = useContractQuery<Record<string, unknown>>(
    contributor,
    dataIntent,
    undefined,
    dataParams,
  );

  const command = useContractCommand(contributor, node.op ?? "");

  const [values, setValues] = React.useState<Record<string, unknown>>({});
  const hydrated = React.useRef(false);

  React.useEffect(() => {
    if (!hydrated.current && query.data) {
      setValues({ [fieldName]: pluckValue(query.data, props.valueField) });
      hydrated.current = true;
    }
  }, [query.data, props.valueField, fieldName]);

  const setValue = React.useCallback((name: string, value: unknown) => {
    setValues((v) => ({ ...v, [name]: value }));
  }, []);

  if (dataIntent && query.isLoading) return <LoadingNode />;
  if (dataIntent && query.error) return <ErrorNode message={(query.error as Error).message} />;

  const handleSave = () => {
    if (!node.op) return;
    const payload = resolvePayload(node.payload, { route });
    command.mutate({ ...payload, [fieldName]: values[fieldName] ?? "" });
  };

  return (
    <FormStateContext.Provider
      value={{ values, setValue, submitting: command.isPending }}
    >
      <div className="space-y-3">
        <CodeEditor
          node={{} as never}
          slots={{}}
          props={{
            name: fieldName,
            language: props.language ?? "json",
            height: props.height ?? "480px",
            readOnly: props.readOnly,
            value: (values[fieldName] as string | undefined) ?? "",
          }}
        />
        {!props.readOnly && node.op ? (
          <div className="flex justify-end">
            <Button onClick={handleSave} disabled={command.isPending}>
              {props.saveLabel ?? "Save"}
            </Button>
          </div>
        ) : null}
      </div>
    </FormStateContext.Provider>
  );
}

function pluckValue(data: unknown, field: string | undefined): string {
  if (!data) return "";
  const obj = data as Record<string, unknown>;
  const raw = field ? obj[field] : (obj.value ?? obj);
  if (raw == null) return "";
  if (typeof raw === "string") return raw;
  try {
    return JSON.stringify(raw, null, 2);
  } catch {
    return String(raw);
  }
}
