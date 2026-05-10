import * as React from "react";
import { AlertCircle, LayoutDashboard } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ContractClient, ContractClientError } from "../contract/client";
import { usePrincipalStore } from "./principal";
import { loginOp } from "../runtime/config";
import { cn } from "@/lib/utils";

const DEFAULT_AUTH_CONTRIBUTOR = "auth";

interface LoginScreenProps {
  /** Override the contributor that owns the login command. Defaults to "auth". */
  contributor?: string;
  /** Override the command op. Defaults to runtime config's loginOp ("auth.login"). */
  op?: string;
  /** Override the brand label shown above the form. */
  brand?: string;
  /** Optional secondary description rendered below the title. */
  description?: string;
}

/**
 * LoginScreen is the built-in fallback rendered by AuthGate when no contract
 * /login route is registered. Visual style follows the latest shadcn
 * "login-03" layout — centered card on a subtle muted background, branded
 * lockup above, polished form controls. Authsome-style auth extensions
 * normally replace this entirely by publishing their own /login graph
 * route; this component exists so the shell ships with a working sign-in
 * UX out of the box.
 */
export function LoginScreen({
  contributor = DEFAULT_AUTH_CONTRIBUTOR,
  op,
  brand = "Forge Dashboard",
  description = "Sign in to continue.",
}: LoginScreenProps) {
  const reloadPrincipal = usePrincipalStore((s) => s.load);
  const [email, setEmail] = React.useState("");
  const [password, setPassword] = React.useState("");
  const [submitting, setSubmitting] = React.useState(false);
  const [errorMsg, setErrorMsg] = React.useState<string | null>(null);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
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
    <div className="flex min-h-svh w-full items-center justify-center bg-muted/40 p-6 md:p-10">
      <div className="flex w-full max-w-sm flex-col gap-6">
        <BrandLockup brand={brand} />
        <div className="flex flex-col gap-6 rounded-xl border border-border/60 bg-background p-6 shadow-sm sm:p-8">
          <header className="flex flex-col gap-1.5">
            <h1 className="text-xl font-semibold tracking-tight">Welcome back</h1>
            <p className="text-sm text-muted-foreground">{description}</p>
          </header>
          <form onSubmit={submit} className="flex flex-col gap-5">
            <div className="flex flex-col gap-2">
              <Label htmlFor="login-email">Email</Label>
              <Input
                id="login-email"
                type="email"
                autoComplete="email"
                placeholder="you@example.com"
                required
                value={email}
                onChange={(e) => setEmail(e.target.value)}
              />
            </div>
            <div className="flex flex-col gap-2">
              <div className="flex items-center justify-between">
                <Label htmlFor="login-password">Password</Label>
                <a
                  href="#"
                  className="text-xs font-medium text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
                  tabIndex={-1}
                >
                  Forgot password?
                </a>
              </div>
              <Input
                id="login-password"
                type="password"
                autoComplete="current-password"
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </div>
            {errorMsg ? (
              <div
                role="alert"
                className={cn(
                  "flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive",
                )}
              >
                <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />
                <span className="leading-tight">{errorMsg}</span>
              </div>
            ) : null}
            <Button type="submit" className="w-full" disabled={submitting}>
              {submitting ? "Signing in…" : "Sign in"}
            </Button>
          </form>
        </div>
        <p className="text-center text-xs text-muted-foreground">
          Protected by your organization&apos;s sign-in policy.
        </p>
      </div>
    </div>
  );
}

function BrandLockup({ brand }: { brand: string }) {
  return (
    <div className="flex flex-col items-center gap-2 text-center">
      <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-foreground text-background">
        <LayoutDashboard className="h-5 w-5" aria-hidden />
      </div>
      <span className="text-sm font-medium tracking-tight text-foreground">{brand}</span>
    </div>
  );
}
