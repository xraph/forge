import * as React from "react";
import { Search, X } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { useFormStateOptional } from "./_form-state";
import { dispatchAction, type Action } from "../runtime/actions";
import type { IntentComponentProps } from "../runtime/registry";

interface SearchBarProps {
  placeholder?: string;
  value?: string;
  default?: string;
  name?: string;
  onChange?: Action;
  onSubmit?: Action;
  debounceMs?: number;
  clearable?: boolean;
  className?: string;
}

export function SearchBar({ props }: IntentComponentProps<unknown, SearchBarProps>) {
  const form = useFormStateOptional();
  const initial =
    (props.name && (form?.values[props.name] as string | undefined)) ??
    props.value ??
    props.default ??
    "";
  const [local, setLocal] = React.useState(initial);
  const debounceMs = props.debounceMs ?? 0;
  const timer = React.useRef<ReturnType<typeof setTimeout> | null>(null);

  const fire = React.useCallback(
    (next: string) => {
      if (props.name) form?.setValue(props.name, next);
      if (props.onChange) dispatchAction(props.onChange, { value: next });
    },
    [form, props.name, props.onChange],
  );

  const onChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const next = e.target.value;
    setLocal(next);
    if (debounceMs > 0) {
      if (timer.current) clearTimeout(timer.current);
      timer.current = setTimeout(() => fire(next), debounceMs);
    } else {
      fire(next);
    }
  };

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (props.onSubmit) dispatchAction(props.onSubmit, { value: local });
  };

  const clear = () => {
    setLocal("");
    fire("");
  };

  return (
    <form onSubmit={submit} className={cn("relative w-full", props.className)} role="search">
      <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
      <Input
        type="search"
        name={props.name}
        placeholder={props.placeholder ?? "Search…"}
        value={local}
        onChange={onChange}
        className="pl-9 pr-9"
      />
      {props.clearable !== false && local ? (
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="absolute right-1 top-1/2 h-7 w-7 -translate-y-1/2"
          onClick={clear}
          aria-label="Clear"
        >
          <X className="h-3.5 w-3.5" />
        </Button>
      ) : null}
    </form>
  );
}
