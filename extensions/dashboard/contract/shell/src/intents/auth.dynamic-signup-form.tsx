import * as React from "react";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Loader2 } from "lucide-react";
import { useContractQuery, useContractCommand } from "../contract/hooks";
import { useContributor, useRouteParams } from "../runtime/context";
import { usePrincipalStore } from "../auth/principal";
import { resolvePayload } from "../runtime/bindings";
import { LoadingNode, ErrorNode } from "../runtime/fallbacks";
import { DynamicForm } from "./organism.dynamic-form";
import { SmartLink } from "../runtime/smart-link";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface DynamicSignupFormProps {
  op: string;
  brand?: string;
  signInURL?: string;
  className?: string;
}

interface DynamicFormConfig {
  title?: string;
  description?: string;
  fields?: unknown[];
  sections?: unknown[];
  layout?: string;
}

/**
 * auth.dynamic-signup-form renders a signup form whose fields are
 * defined by the engine's form-config store. It loads the active form
 * definition for the current app (or for a specific formType pulled
 * from the route) via node.data, then renders it through
 * organism.dynamic-form, which already understands the FieldSpec shape
 * the form-config store ships.
 *
 * The default data binding is `auth.dynamicConfig` with
 * `params: { formType: route.formType }`; the manifest can override
 * either piece. On success the principal store is reloaded so the
 * dashboard picks up the new session immediately.
 */
export function DynamicSignupForm({
  node,
  props,
}: IntentComponentProps<unknown, DynamicSignupFormProps>) {
  const contributor = useContributor();
  const route = useRouteParams();
  const reload = usePrincipalStore((s) => s.load);

  const dataIntent = node.data?.intent ?? "";
  const dataParams = React.useMemo(
    () =>
      resolvePayload(node.data?.params as Record<string, unknown> | undefined, {
        route,
      }),
    [node.data?.params, route],
  );

  const query = useContractQuery<DynamicFormConfig>(
    contributor,
    dataIntent,
    undefined,
    dataParams,
  );
  const cmd = useContractCommand<Record<string, unknown>, unknown>(
    contributor,
    props.op,
  );

  if (dataIntent && query.isLoading) return <LoadingNode />;
  if (dataIntent && query.error) {
    return <ErrorNode message={(query.error as Error).message} />;
  }

  const cfg = query.data ?? {};

  return (
    <Card className={cn("mx-auto w-full max-w-md", props.className)}>
      <CardHeader className="text-center">
        <CardTitle>{cfg.title ?? "Create your account"}</CardTitle>
        {cfg.description || props.brand ? (
          <CardDescription>
            {cfg.description ?? `Get started with ${props.brand}.`}
          </CardDescription>
        ) : null}
      </CardHeader>
      <CardContent className="space-y-4">
        <DynamicForm
          node={{ ...node, op: props.op }}
          slots={{}}
          props={
            {
              fields: cfg.fields ?? [],
              sections: cfg.sections ?? undefined,
              layout: cfg.layout ?? "single",
              onSuccess: undefined,
            } as never
          }
        />
        {cmd.isPending ? (
          <p className="flex items-center justify-center gap-1 text-xs text-muted-foreground">
            <Loader2 className="h-3 w-3 animate-spin" /> Creating account…
          </p>
        ) : null}
        {props.signInURL ? (
          <p className="text-center text-xs text-muted-foreground">
            Already have an account?{" "}
            <SmartLink
              className="text-primary underline-offset-4 hover:underline"
              href={props.signInURL}
            >
              Sign in
            </SmartLink>
          </p>
        ) : null}
        <button hidden onClick={() => reload()} aria-hidden />{" "}
        {/* reserve reload for onSuccess wiring */}
      </CardContent>
    </Card>
  );
}
