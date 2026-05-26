import * as React from "react";
import { Upload, FileIcon, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { useFormStateOptional } from "./_form-state";
import { dispatchAction, type Action } from "../runtime/actions";
import type { IntentComponentProps } from "../runtime/registry";

interface FileUploadProps {
  name: string;
  accept?: string;
  multiple?: boolean;
  maxSize?: number; // bytes
  maxFiles?: number;
  disabled?: boolean;
  onChange?: Action;
  hint?: string;
  error?: string;
  className?: string;
}

interface UploadedFile {
  name: string;
  size: number;
  type: string;
}

export function FileUpload({ props }: IntentComponentProps<unknown, FileUploadProps>) {
  const form = useFormStateOptional();
  const initial = (form?.values[props.name] as UploadedFile[] | undefined) ?? [];
  const [files, setFiles] = React.useState<UploadedFile[]>(initial);
  const [dragging, setDragging] = React.useState(false);
  const [localError, setLocalError] = React.useState<string | null>(null);
  const inputRef = React.useRef<HTMLInputElement>(null);

  const commit = (next: UploadedFile[]) => {
    setFiles(next);
    form?.setValue(props.name, next);
    if (props.onChange) dispatchAction(props.onChange, { value: next });
  };

  const handle = (incoming: File[]) => {
    setLocalError(null);
    if (props.maxFiles && files.length + incoming.length > props.maxFiles) {
      setLocalError(`Maximum ${props.maxFiles} files`);
      return;
    }
    for (const f of incoming) {
      if (props.maxSize && f.size > props.maxSize) {
        setLocalError(`File too large (${prettyBytes(props.maxSize)} max)`);
        return;
      }
    }
    const mapped = incoming.map((f) => ({ name: f.name, size: f.size, type: f.type }));
    commit(props.multiple ? [...files, ...mapped] : mapped);
  };

  const onDrop = (e: React.DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    setDragging(false);
    if (props.disabled) return;
    handle(Array.from(e.dataTransfer.files));
  };

  const onSelect = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (!e.target.files) return;
    handle(Array.from(e.target.files));
    e.target.value = "";
  };

  const remove = (name: string) => commit(files.filter((f) => f.name !== name));

  const error = props.error ?? localError;

  return (
    <div className={cn("space-y-2", props.className)}>
      <div
        onDragEnter={(e) => {
          e.preventDefault();
          if (!props.disabled) setDragging(true);
        }}
        onDragOver={(e) => e.preventDefault()}
        onDragLeave={() => setDragging(false)}
        onDrop={onDrop}
        onClick={() => !props.disabled && inputRef.current?.click()}
        className={cn(
          "flex cursor-pointer flex-col items-center justify-center rounded-lg border border-dashed border-input bg-background p-6 text-center transition-colors",
          dragging && "border-primary bg-accent",
          props.disabled && "cursor-not-allowed opacity-50",
          error && "border-destructive",
        )}
      >
        <Upload className="h-6 w-6 text-muted-foreground" />
        <p className="mt-2 text-sm">
          <span className="font-medium">Click to upload</span> or drag and drop
        </p>
        {props.hint ? <p className="mt-1 text-xs text-muted-foreground">{props.hint}</p> : null}
        <input
          ref={inputRef}
          type="file"
          name={props.name}
          accept={props.accept}
          multiple={props.multiple}
          disabled={props.disabled}
          onChange={onSelect}
          className="hidden"
        />
      </div>
      {files.length > 0 ? (
        <ul className="space-y-1">
          {files.map((f) => (
            <li
              key={f.name}
              className="flex items-center justify-between gap-2 rounded-md border bg-background px-3 py-2 text-sm"
            >
              <span className="flex min-w-0 items-center gap-2">
                <FileIcon className="h-4 w-4 shrink-0 text-muted-foreground" />
                <span className="truncate">{f.name}</span>
                <span className="shrink-0 text-xs text-muted-foreground">
                  {prettyBytes(f.size)}
                </span>
              </span>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="h-6 w-6"
                onClick={() => remove(f.name)}
                aria-label={`Remove ${f.name}`}
              >
                <X className="h-3.5 w-3.5" />
              </Button>
            </li>
          ))}
        </ul>
      ) : null}
      {error ? <p className="text-xs text-destructive">{error}</p> : null}
    </div>
  );
}

function prettyBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
  return `${(n / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}
