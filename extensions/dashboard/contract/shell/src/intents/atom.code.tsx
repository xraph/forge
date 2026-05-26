import * as React from "react";
import { Copy } from "lucide-react";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface CodeProps {
  text: string;
  language?: string;
  inline?: boolean;
  copyable?: boolean;
  className?: string;
}

export function AtomCode({ props }: IntentComponentProps<unknown, CodeProps>) {
  const [copied, setCopied] = React.useState(false);
  const copy = () => {
    void navigator.clipboard.writeText(props.text);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  if (props.inline) {
    return (
      <code className={cn("rounded bg-muted px-1.5 py-0.5 font-mono text-sm", props.className)}>
        {props.text}
      </code>
    );
  }
  return (
    <div className={cn("group relative rounded-md border bg-muted/50", props.className)}>
      {props.language ? (
        <div className="flex items-center justify-between border-b px-3 py-1.5 text-xs text-muted-foreground">
          <span className="font-mono">{props.language}</span>
        </div>
      ) : null}
      {props.copyable ? (
        <button
          type="button"
          onClick={copy}
          className="absolute right-2 top-2 hidden rounded-md border bg-background p-1.5 text-muted-foreground hover:text-foreground group-hover:flex"
          aria-label={copied ? "Copied" : "Copy"}
        >
          <Copy className="h-3.5 w-3.5" />
        </button>
      ) : null}
      <pre className="overflow-x-auto p-4 text-sm">
        <code className="font-mono">{props.text}</code>
      </pre>
    </div>
  );
}
