import * as React from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { Label } from "@/components/ui/label";
import { useContractCommand, useContractQuery } from "../contract/hooks";
import { useContributor } from "../runtime/context";
import { LoadingNode, ErrorNode } from "../runtime/fallbacks";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface FeatureToggle {
  key: string;
  label: string;
  description?: string;
  enabled: boolean;
  available: boolean;
}

interface FeatureTogglesResponse {
  toggles: FeatureToggle[];
}

export interface FeatureTogglesProps {
  /** Intent name fetching the toggle list. Defaults to "auth.featureToggles". */
  queryIntent?: string;
  /** Command name dispatched per flipped toggle. Defaults to "auth.toggleFeature". */
  toggleOp?: string;
  /** Optional title shown above the card. */
  title?: string;
  /** Optional description. */
  description?: string;
  /** When true, drop the surrounding Card chrome (for embedded use). */
  bare?: boolean;
}

/**
 * feature.toggles renders the per-app feature switches the templui
 * dashboard's "Sign-in Methods" panel surfaced. It binds to
 * `auth.featureToggles` and dispatches `auth.toggleFeature` per
 * flipped switch. Unavailable features (the underlying plugin isn't
 * installed) render disabled with a "Not installed" badge so admins
 * understand why the toggle wouldn't take effect.
 */
export function FeatureToggles({
  props,
}: IntentComponentProps<unknown, FeatureTogglesProps>) {
  const contributor = useContributor();
  const queryIntent = props.queryIntent ?? "auth.featureToggles";
  const toggleOp = props.toggleOp ?? "auth.toggleFeature";

  const query = useContractQuery<FeatureTogglesResponse>(contributor, queryIntent);
  const cmd = useContractCommand<{ key: string; enabled: boolean }, unknown>(
    contributor,
    toggleOp,
  );

  // Mirror server state in a local map so optimistic updates show
  // immediately; query refetch on success replaces it with the truth.
  const [local, setLocal] = React.useState<Record<string, boolean>>({});
  React.useEffect(() => {
    if (query.data?.toggles) {
      const init: Record<string, boolean> = {};
      for (const t of query.data.toggles) init[t.key] = t.enabled;
      setLocal(init);
    }
  }, [query.data?.toggles]);

  if (query.isLoading) return <LoadingNode />;
  if (query.error) return <ErrorNode message={(query.error as Error).message} />;
  if (!query.data) return null;

  const flip = async (key: string, enabled: boolean) => {
    setLocal((m) => ({ ...m, [key]: enabled }));
    try {
      await cmd.mutateAsync({ key, enabled });
      query.refetch();
    } catch {
      // Roll back to server state on failure.
      query.refetch();
    }
  };

  const body = (
    <div className="space-y-4">
      {query.data.toggles.map((t) => {
        const checked = local[t.key] ?? t.enabled;
        return (
          <div
            key={t.key}
            className={cn(
              "flex items-start justify-between gap-4 rounded-md border p-3",
              !t.available && "opacity-60",
            )}
          >
            <div className="space-y-1">
              <div className="flex items-center gap-2">
                <Label htmlFor={`toggle-${t.key}`} className="font-medium">
                  {t.label}
                </Label>
                {!t.available ? (
                  <Badge variant="outline" className="text-xs">
                    Not installed
                  </Badge>
                ) : null}
              </div>
              {t.description ? (
                <p className="text-xs text-muted-foreground">{t.description}</p>
              ) : null}
            </div>
            <Switch
              id={`toggle-${t.key}`}
              checked={checked}
              disabled={!t.available || cmd.isPending}
              onCheckedChange={(v) => flip(t.key, Boolean(v))}
            />
          </div>
        );
      })}
    </div>
  );

  if (props.bare) {
    return body;
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{props.title ?? "Sign-in methods"}</CardTitle>
        <CardDescription>
          {props.description ??
            "Toggle which authentication features are enabled for this app. Disabled features won't be offered to end users."}
        </CardDescription>
      </CardHeader>
      <CardContent>{body}</CardContent>
    </Card>
  );
}
