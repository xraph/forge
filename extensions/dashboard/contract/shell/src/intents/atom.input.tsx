import * as React from "react";
import { Input } from "@/components/ui/input";
import { Icon } from "./atom.icon";
import { cn } from "@/lib/utils";
import { useFormStateOptional } from "./_form-state";
import { dispatchAction, type Action } from "../runtime/actions";
import { useContributor, useParent, useRouteParams } from "../runtime/context";
import { usePrincipalStore } from "../auth/principal";
import type { Size } from "@/lib/tokens";
import type { IntentComponentProps } from "../runtime/registry";

interface InputAtomProps {
  name: string;
  type?: "text" | "email" | "password" | "number" | "search" | "tel" | "url" | "date" | "time";
  placeholder?: string;
  value?: string;
  default?: string;
  disabled?: boolean;
  readOnly?: boolean;
  required?: boolean;
  autofocus?: boolean;
  autocomplete?: string;
  prefix?: string;
  suffix?: string;
  prefixIcon?: string;
  suffixIcon?: string;
  minLength?: number;
  maxLength?: number;
  pattern?: string;
  min?: number;
  max?: number;
  step?: number;
  onChange?: Action;
  onSubmit?: Action;
  size?: Size;
  error?: string;
  className?: string;
}

export function AtomInput({ props }: IntentComponentProps<unknown, InputAtomProps>) {
  const form = useFormStateOptional();
  const contributor = useContributor();
  const parent = useParent();
  const route = useRouteParams();
  const principal = usePrincipalStore((s) => s.principal);

  const initial =
    (form?.values[props.name] as string | undefined) ?? props.value ?? props.default ?? "";
  const [local, setLocal] = React.useState(initial);

  React.useEffect(() => {
    if (form && props.name in form.values) setLocal(form.values[props.name] as string);
  }, [form, props.name]);

  const onChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setLocal(e.target.value);
    form?.setValue(props.name, e.target.value);
    if (props.onChange) {
      dispatchAction(props.onChange, {
        parent,
        session: { user: principal },
        route,
        contributor,
        value: e.target.value,
      });
    }
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" && props.onSubmit) {
      dispatchAction(props.onSubmit, {
        parent,
        session: { user: principal },
        route,
        contributor,
        value: local,
      });
    }
  };

  const hasAffix = props.prefix || props.suffix || props.prefixIcon || props.suffixIcon;
  const input = (
    <Input
      name={props.name}
      type={props.type ?? "text"}
      placeholder={props.placeholder}
      value={local}
      disabled={props.disabled}
      readOnly={props.readOnly}
      required={props.required}
      autoFocus={props.autofocus}
      autoComplete={props.autocomplete}
      minLength={props.minLength}
      maxLength={props.maxLength}
      pattern={props.pattern}
      min={props.min}
      max={props.max}
      step={props.step}
      onChange={onChange}
      onKeyDown={onKeyDown}
      className={cn(
        hasAffix && "rounded-none border-0 focus-visible:ring-0 focus-visible:ring-offset-0",
        props.error && !hasAffix && "border-destructive",
        props.className,
      )}
      aria-invalid={!!props.error}
    />
  );

  if (!hasAffix) return wrapWithError(input, props.error);

  return wrapWithError(
    <div
      className={cn(
        "flex items-center rounded-md border border-input bg-background ring-offset-background focus-within:ring-2 focus-within:ring-ring",
        props.error && "border-destructive",
      )}
    >
      {props.prefixIcon ? (
        <span className="pl-3 text-muted-foreground">
          <Icon node={{} as never} slots={{}} props={{ name: props.prefixIcon, size: 16 }} />
        </span>
      ) : null}
      {props.prefix ? <span className="px-3 text-sm text-muted-foreground">{props.prefix}</span> : null}
      {input}
      {props.suffix ? <span className="px-3 text-sm text-muted-foreground">{props.suffix}</span> : null}
      {props.suffixIcon ? (
        <span className="pr-3 text-muted-foreground">
          <Icon node={{} as never} slots={{}} props={{ name: props.suffixIcon, size: 16 }} />
        </span>
      ) : null}
    </div>,
    props.error,
  );
}

function wrapWithError(el: React.ReactNode, error?: string) {
  if (!error) return <>{el}</>;
  return (
    <div className="space-y-1">
      {el}
      <p className="text-xs text-destructive">{error}</p>
    </div>
  );
}
