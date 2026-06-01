import * as React from "react";
import { cn } from "@/lib/utils";
import { useFormStateOptional } from "./_form-state";
import { dispatchAction, type Action } from "../runtime/actions";
import type { IntentComponentProps } from "../runtime/registry";

interface CodeEditorProps {
  name?: string;
  language?: string;
  value?: string;
  default?: string;
  readOnly?: boolean;
  theme?: "auto" | "light" | "dark";
  lineNumbers?: boolean;
  height?: string;
  onChange?: Action;
  className?: string;
}

// Monaco is heavy (~700KB gz). React.lazy makes Vite emit a separate chunk
// loaded only when the editor first mounts; the Suspense fallback below
// shows a textarea-style placeholder so non-code pages don't pay the cost.
const MonacoEditor = React.lazy(() =>
  import("@monaco-editor/react").then((m) => ({ default: m.default })),
);

export function CodeEditor({ props }: IntentComponentProps<unknown, CodeEditorProps>) {
  const form = useFormStateOptional();
  const initial =
    (props.name && (form?.values[props.name] as string | undefined)) ??
    props.value ??
    props.default ??
    "";
  const [local, setLocal] = React.useState(initial);

  const commit = (next: string) => {
    setLocal(next);
    if (props.name) form?.setValue(props.name, next);
    if (props.onChange) dispatchAction(props.onChange, { value: next });
  };

  const theme = resolveTheme(props.theme);
  const height = props.height ?? "320px";

  return (
    <div
      className={cn("overflow-hidden rounded-md border bg-card", props.className)}
      style={{ height }}
    >
      <React.Suspense
        fallback={
          <TextareaFallback
            value={local}
            onChange={commit}
            language={props.language}
            readOnly={props.readOnly}
            lineNumbers={props.lineNumbers}
            name={props.name}
          />
        }
      >
        <MonacoEditor
          height="100%"
          language={props.language}
          value={local}
          theme={theme}
          options={{
            readOnly: props.readOnly,
            lineNumbers: props.lineNumbers === false ? "off" : "on",
            minimap: { enabled: false },
            scrollBeyondLastLine: false,
            fontSize: 13,
            tabSize: 2,
            wordWrap: "on",
            automaticLayout: true,
            renderLineHighlight: "line",
          }}
          onChange={(v) => commit(v ?? "")}
        />
      </React.Suspense>
    </div>
  );
}

function resolveTheme(t: CodeEditorProps["theme"]): "vs" | "vs-dark" {
  if (t === "light") return "vs";
  if (t === "dark") return "vs-dark";
  if (typeof document !== "undefined") {
    return document.documentElement.classList.contains("dark") ? "vs-dark" : "vs";
  }
  return "vs";
}

/**
 * TextareaFallback is what users see while Monaco's chunk loads (and on
 * test platforms / SSR where Monaco can't initialize). Visually identical
 * to the pre-upgrade renderer so the transition is invisible.
 */
function TextareaFallback({
  value,
  onChange,
  language,
  readOnly,
  lineNumbers,
  name,
}: {
  value: string;
  onChange: (next: string) => void;
  language?: string;
  readOnly?: boolean;
  lineNumbers?: boolean;
  name?: string;
}) {
  const lines = value.split("\n").length;
  return (
    <div className="flex h-full font-mono text-sm">
      {lineNumbers !== false ? (
        <pre
          aria-hidden
          className="select-none border-r bg-muted/40 px-2 py-2 text-right text-xs text-muted-foreground"
          style={{ minWidth: "2.5rem" }}
        >
          {Array.from({ length: Math.max(1, lines) })
            .map((_, i) => i + 1)
            .join("\n")}
        </pre>
      ) : null}
      <textarea
        name={name}
        value={value}
        readOnly={readOnly}
        spellCheck={false}
        onChange={(e) => onChange(e.target.value)}
        className="h-full flex-1 resize-none bg-transparent px-3 py-2 outline-none placeholder:text-muted-foreground"
        placeholder={`// ${language ?? "code"}`}
      />
    </div>
  );
}
