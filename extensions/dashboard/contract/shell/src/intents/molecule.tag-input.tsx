import * as React from "react";
import { X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { useFormStateOptional } from "./_form-state";
import { dispatchAction, type Action } from "../runtime/actions";
import type { IntentComponentProps } from "../runtime/registry";

interface TagInputProps {
  name: string;
  value?: string[];
  default?: string[];
  placeholder?: string;
  maxTags?: number;
  validate?: string;
  disabled?: boolean;
  onChange?: Action;
  error?: string;
  className?: string;
}

export function TagInput({ props }: IntentComponentProps<unknown, TagInputProps>) {
  const form = useFormStateOptional();
  const initial = (form?.values[props.name] as string[] | undefined) ?? props.value ?? props.default ?? [];
  const [tags, setTags] = React.useState<string[]>(initial);
  const [draft, setDraft] = React.useState("");
  const pattern = props.validate ? new RegExp(props.validate) : null;

  const commit = (next: string[]) => {
    setTags(next);
    form?.setValue(props.name, next);
    if (props.onChange) dispatchAction(props.onChange, { value: next });
  };

  const add = (raw: string) => {
    const value = raw.trim();
    if (!value) return;
    if (props.maxTags && tags.length >= props.maxTags) return;
    if (tags.includes(value)) return;
    if (pattern && !pattern.test(value)) return;
    commit([...tags, value]);
    setDraft("");
  };

  const remove = (tag: string) => commit(tags.filter((t) => t !== tag));

  const onKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" || e.key === ",") {
      e.preventDefault();
      add(draft);
    } else if (e.key === "Backspace" && draft === "" && tags.length > 0) {
      remove(tags[tags.length - 1]!);
    }
  };

  return (
    <div className="space-y-1">
      <div
        className={cn(
          "flex min-h-10 flex-wrap items-center gap-1.5 rounded-md border border-input bg-background px-2 py-1.5 ring-offset-background focus-within:ring-2 focus-within:ring-ring",
          props.error && "border-destructive",
          props.disabled && "opacity-50",
          props.className,
        )}
      >
        {tags.map((tag) => (
          <Badge key={tag} variant="secondary" className="gap-1">
            {tag}
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="-mr-1 h-4 w-4 hover:bg-transparent"
              onClick={() => remove(tag)}
              disabled={props.disabled}
              aria-label={`Remove ${tag}`}
            >
              <X className="h-3 w-3" />
            </Button>
          </Badge>
        ))}
        <Input
          name={props.name}
          value={draft}
          placeholder={tags.length === 0 ? props.placeholder ?? "Add tag…" : ""}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={onKeyDown}
          onBlur={() => add(draft)}
          disabled={props.disabled}
          className="m-0 h-7 min-w-[120px] flex-1 border-0 bg-transparent p-0 focus-visible:ring-0 focus-visible:ring-offset-0"
        />
      </div>
      {props.error ? <p className="text-xs text-destructive">{props.error}</p> : null}
    </div>
  );
}
