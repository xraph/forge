import * as React from "react";
import { LogIn } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ContractClient, ContractClientError } from "../contract/client";
import { usePrincipalStore } from "./principal";
import { loginOp } from "../runtime/config";

const DEFAULT_AUTH_CONTRIBUTOR = "auth";

interface LoginScreenProps {
  /** Override the contributor that owns the login command. Defaults to "auth". */
  contributor?: string;
  /** Override the command op. Defaults to runtime config's loginOp ("auth.login"). */
  op?: string;
}

/**
 * LoginScreen is the built-in fallback rendered by AuthGate when the
 * /principal endpoint returns 401. It issues a `kind: command` envelope to
 * the configured `loginOp` (default "auth.login") and on success reloads the
 * principal so the gate releases. Auth extensions that prefer a richer flow
 * register a contract /login graph route — AuthGate prefers that path when
 * available.
 */
export function LoginScreen({
  contributor = DEFAULT_AUTH_CONTRIBUTOR,
  op,
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
    <div className="flex min-h-svh items-center justify-center bg-background p-6">
      <Card className="w-full max-w-md">
        <CardHeader className="space-y-1 text-center">
          <div className="mx-auto flex h-10 w-10 items-center justify-center rounded-full bg-primary text-primary-foreground">
            <LogIn className="h-5 w-5" />
          </div>
          <CardTitle>Sign in</CardTitle>
          <CardDescription>
            Use your account credentials to continue to the dashboard.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={submit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="login-email">Email</Label>
              <Input
                id="login-email"
                type="email"
                autoComplete="email"
                required
                value={email}
                onChange={(e) => setEmail(e.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="login-password">Password</Label>
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
              <div className="rounded-md border border-destructive/30 bg-destructive/10 p-2 text-sm text-destructive">
                {errorMsg}
              </div>
            ) : null}
            <Button type="submit" className="w-full" disabled={submitting}>
              {submitting ? "Signing in…" : "Sign in"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
