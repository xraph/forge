import * as React from "react";
import { ContractClient, ContractClientError } from "../contract/client";
import { usePrincipalStore } from "./principal";
import { loginOp } from "../runtime/config";
import { LoginCard } from "./login-card";

const DEFAULT_AUTH_CONTRIBUTOR = "auth";

interface LoginScreenProps {
  /** Override the contributor that owns the login command. Defaults to "auth". */
  contributor?: string;
  /** Override the command op. Defaults to runtime config's loginOp ("auth.login"). */
  op?: string;
  /** Override the brand label shown above the form. */
  brand?: string;
}

/**
 * LoginScreen is the built-in fallback rendered when no contract /login
 * route is registered (e.g. no auth extension wired or the extension
 * doesn't ship a route). Visual style matches shadcn's login-04 block via
 * the shared LoginCard so authsome-rendered and fallback flows look
 * identical to the user.
 */
export function LoginScreen({
  contributor = DEFAULT_AUTH_CONTRIBUTOR,
  op,
  brand = "Forge Dashboard",
}: LoginScreenProps) {
  const reloadPrincipal = usePrincipalStore((s) => s.load);
  const [errorMsg, setErrorMsg] = React.useState<string | null>(null);
  const [submitting, setSubmitting] = React.useState(false);

  const handleSubmit = async ({ email, password }: { email: string; password: string }) => {
    setErrorMsg(null);
    setSubmitting(true);
    try {
      const client = new ContractClient();
      await client.command(contributor, op ?? loginOp, { email, password });
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
      brand={brand}
      passwordEnabled
      socialProviders={[]}
      errorMsg={errorMsg}
      submitting={submitting}
      onSubmit={handleSubmit}
    />
  );
}
