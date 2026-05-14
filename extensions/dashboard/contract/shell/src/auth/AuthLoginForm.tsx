import * as React from "react";
import { useContractCommand, useContractQuery } from "../contract/hooks";
import { useContributor } from "../runtime/context";
import { usePrincipalStore } from "./principal";
import { LoginCard, type SocialProvider } from "./login-card";
import { LoadingNode, ErrorNode } from "../runtime/fallbacks";
import { ContractClient, ContractClientError } from "../contract/client";
import { loginOp } from "../runtime/config";
import type { IntentComponentProps } from "../runtime/registry";

// AuthLoginForm is the React component registered for the `auth.login.form`
// vocabulary intent. It pulls the per-deployment login configuration
// (brand, password toggle, signup, social providers) from a contract
// query named `auth.config` on the same contributor that owns the route
// and renders shadcn login-04 against it. Authsome's manifest binds the
// query at /login; deployments without authsome use the LoginScreen
// fallback instead which renders the same visual with hardcoded defaults.

export interface AuthConfigResponse {
  brand?: string;
  brandLogoURL?: string;
  passwordEnabled?: boolean;
  signupURL?: string;
  signupLabel?: string;
  termsURL?: string;
  privacyURL?: string;
  socialProviders?: SocialProviderConfig[];
}

export interface SocialProviderConfig {
  id: string;
  label: string;
  /** Absolute URL the shell POSTs to begin the OAuth flow. The endpoint
   *  responds with `{auth_url}` which the shell then navigates to. */
  authStartURL: string;
}

interface AuthLoginFormProps {
  /** Override the command op the password form submits. Defaults to runtime
   *  config's loginOp ("auth.login"). */
  op?: string;
  /** Override the contributor data is fetched from. Defaults to the active
   *  ContributorContext (set by AuthGate when rendering the contract /login). */
  configContributor?: string;
  /** Override the configuration source. Useful for tests. */
  config?: AuthConfigResponse;
}

const CONFIG_INTENT = "auth.config";

export function AuthLoginForm({ node, props }: IntentComponentProps<unknown, AuthLoginFormProps>) {
  const ctxContributor = useContributor();
  const contributor = props.configContributor ?? ctxContributor;
  const reloadPrincipal = usePrincipalStore((s) => s.load);

  const cfgQuery = useContractQuery<AuthConfigResponse>(contributor, CONFIG_INTENT, undefined, undefined);
  const passwordCmd = useContractCommand<{ email: string; password: string }, unknown>(
    contributor,
    props.op ?? loginOp,
  );

  const [errorMsg, setErrorMsg] = React.useState<string | null>(null);
  const [submitting, setSubmitting] = React.useState(false);

  // Allow tests / advanced consumers to inject a static config and skip the
  // round-trip. Production always goes through the query.
  const cfg = props.config ?? cfgQuery.data ?? {};

  if (!props.config && cfgQuery.isLoading) {
    return <LoadingNode />;
  }
  if (!props.config && cfgQuery.error) {
    return <ErrorNode message={(cfgQuery.error as Error).message} />;
  }

  const passwordEnabled = cfg.passwordEnabled !== false;
  const social: SocialProvider[] = (cfg.socialProviders ?? []).map((p) => ({
    id: p.id,
    label: p.label,
    onClick: () => {
      void beginOAuth(p.authStartURL);
    },
  }));

  const handleSubmit = async ({ email, password }: { email: string; password: string }) => {
    setErrorMsg(null);
    setSubmitting(true);
    try {
      await passwordCmd.mutateAsync({ email, password });
      await reloadPrincipal();
    } catch (err) {
      if (err instanceof ContractClientError) {
        setErrorMsg(err.message || err.code);
      } else {
        setErrorMsg(String(err));
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <LoginCard
      brand={cfg.brand ?? node.title ?? "Forge Dashboard"}
      brandLogoURL={cfg.brandLogoURL}
      signupHref={cfg.signupURL ?? null}
      signupLabel={cfg.signupLabel ?? "Sign up"}
      termsURL={cfg.termsURL ?? null}
      privacyURL={cfg.privacyURL ?? null}
      passwordEnabled={passwordEnabled}
      socialProviders={social}
      errorMsg={errorMsg}
      submitting={submitting}
      onSubmit={handleSubmit}
    />
  );
}

// beginOAuth POSTs to the provider's auth-start URL and navigates the
// browser to the returned auth_url. The endpoint conventionally accepts
// `{redirect_url}` indicating where to come back to after the upstream
// callback completes — we point it at the dashboard root so the shell
// reloads with the freshly-set session cookie.
async function beginOAuth(authStartURL: string): Promise<void> {
  // The auth-start URL belongs to the auth extension (e.g. authsome's
  // /v1/social/<provider> route), not the contract envelope endpoint.
  // We use a plain fetch so we don't drag CSRF/idempotency through it —
  // the upstream OAuth flow handles its own state token via cookie.
  try {
    const res = await fetch(authStartURL, {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        redirect_url: typeof window !== "undefined" ? window.location.origin : "",
        frontend_url: typeof window !== "undefined" ? window.location.origin : "",
      }),
    });
    if (!res.ok) {
      throw new Error(`oauth start failed (${res.status})`);
    }
    const body = (await res.json()) as { auth_url?: string };
    if (!body.auth_url) {
      throw new Error("oauth start: missing auth_url");
    }
    window.location.assign(body.auth_url);
  } catch (err) {
    // Surface as a console error; the LoginCard doesn't display per-button
    // errors — a failed OAuth start is rare and operational, not a UX flow.
    // eslint-disable-next-line no-console
    console.error("oauth start error", err);
  }
  // Keep the ContractClient referenced so future callers using it within
  // a custom auth flow stay typed.
  void ContractClient;
}
