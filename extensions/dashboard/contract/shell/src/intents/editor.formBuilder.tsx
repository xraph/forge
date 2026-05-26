import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { EditorCode } from "./editor.code";
import { Info } from "lucide-react";
import type { IntentComponentProps } from "../runtime/registry";

interface EditorFormBuilderProps {
  /** Label for the save button. */
  saveLabel?: string;
  /**
   * Field name on the loaded definition payload that holds the form
   * schema. Defaults to "definition".
   */
  valueField?: string;
  /** Suppress the placeholder banner. */
  hideBanner?: boolean;
}

/**
 * editor.formBuilder is the signup-form editor. v1 ships as a thin
 * delegate over editor.code — the manifest provides a JSON definition
 * via node.data and the editor lets admins edit it directly. A future
 * iteration will swap the JSON surface for a true drag-and-drop builder
 * over organism.dynamic-form in design mode; the manifest surface stays
 * the same so that swap is invisible to authors.
 */
export function EditorFormBuilder({
  node,
  props,
}: IntentComponentProps<unknown, EditorFormBuilderProps>) {
  return (
    <div className="space-y-3">
      {props.hideBanner ? null : (
        <Alert>
          <Info className="h-4 w-4" />
          <AlertTitle>Form definition (JSON)</AlertTitle>
          <AlertDescription>
            Edit the JSON below to add, reorder, or remove signup fields.
            The visual drag-and-drop builder is on the roadmap.
          </AlertDescription>
        </Alert>
      )}
      <EditorCode
        node={node}
        slots={{}}
        props={{
          language: "json",
          height: "560px",
          valueField: props.valueField ?? "definition",
          saveLabel: props.saveLabel,
          fieldName: props.valueField ?? "definition",
        }}
      />
    </div>
  );
}
