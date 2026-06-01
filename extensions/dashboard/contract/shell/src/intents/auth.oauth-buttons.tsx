import * as React from "react";
import { Button } from "@/components/ui/button";
import { Icon } from "./atom.icon";
import { useContractQuery } from "../contract/hooks";
import { useContributor } from "../runtime/context";
import { cn } from "@/lib/utils";
import type { ColorVariant } from "@/lib/tokens";
import type { DataBinding } from "@/contract/types";
import type { IntentComponentProps } from "../runtime/registry";

interface OAuthProvider {
  id: string;
  label: string;
  icon?: string;
  authStartURL: string;
}

interface OAuthButtonsProps {
  providers?: OAuthProvider[];
  configSource?: DataBinding;
  variant?: ColorVariant;
  fullWidth?: boolean;
  layout?: "grid" | "stack";
  redirectURL?: string;
  className?: string;
}

function resolveVariant(v?: ColorVariant): "default" | "destructive" | "outline" | "secondary" | "ghost" | "link" {
  if (!v) return "outline";
  if (v === "muted" || v === "accent") return "secondary";
  return v as "default" | "destructive" | "outline" | "secondary" | "ghost" | "link";
}

export function OAuthButtons({ props }: IntentComponentProps<unknown, OAuthButtonsProps>) {
  const contributor = useContributor();
  const dynamic = useContractQuery<{ socialProviders?: OAuthProvider[] }>(
    contributor,
    props.configSource?.intent ?? "",
    undefined,
    undefined,
  );

  const providers = props.providers ?? dynamic.data?.socialProviders ?? [];
  const [pending, setPending] = React.useState<string | null>(null);

  const begin = async (p: OAuthProvider) => {
    setPending(p.id);
    try {
      const res = await fetch(p.authStartURL, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          redirect_url: props.redirectURL ?? (typeof window !== "undefined" ? window.location.origin : ""),
        }),
      });
      if (!res.ok) throw new Error(`oauth start failed (${res.status})`);
      const body = (await res.json()) as { auth_url?: string };
      if (!body.auth_url) throw new Error("missing auth_url");
      window.location.assign(body.auth_url);
    } catch (err) {
      console.error("oauth start error", err);
      setPending(null);
    }
  };

  if (providers.length === 0) return null;

  return (
    <div
      className={cn(
        props.layout === "grid" ? "grid grid-cols-2 gap-2" : "flex flex-col gap-2",
        props.className,
      )}
    >
      {providers.map((p) => (
        <Button
          key={p.id}
          variant={resolveVariant(props.variant)}
          className={cn(props.fullWidth !== false && "w-full")}
          disabled={pending !== null}
          onClick={() => begin(p)}
        >
          {p.icon ? (
            <Icon node={{} as never} slots={{}} props={{ name: p.icon, size: 16 }} />
          ) : null}
          {pending === p.id ? "Connecting…" : p.label}
        </Button>
      ))}
    </div>
  );
}
