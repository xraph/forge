import * as React from "react";
import { useSearchParams } from "react-router-dom";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Loader2, CheckCircle2 } from "lucide-react";
import { useContractCommand } from "../contract/hooks";
import { useContributor } from "../runtime/context";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface ResetPasswordFormProps {
  op: string;
  brand?: string;
  signInURL?: string;
  /**
   * Query parameter name carrying the reset token from the email link.
   * Defaults to "token" — matches the convention authsome's reset
   * emails use.
   */
  tokenParam?: string;
  className?: string;
}

/**
 * auth.reset-password-form is the page the reset-link email points at.
 * Token comes from the URL query string (?token=...); the user types
 * a new password and submits to the configured command (`auth.resetPassword`).
 * Success swaps the form for a confirmation card with a link back to
 * sign-in.
 */
export function ResetPasswordForm({
  props,
}: IntentComponentProps<unknown, ResetPasswordFormProps>) {
  const contributor = useContributor();
  const [searchParams] = useSearchParams();
  const tokenParam = props.tokenParam ?? "token";
  const token = searchParams.get(tokenParam) ?? "";

  const cmd = useContractCommand<{ token: string; password: string }, unknown>(
    contributor,
    props.op,
  );
  const [password, setPassword] = React.useState("");
  const [confirm, setConfirm] = React.useState("");
  const [submitted, setSubmitted] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (password.length < 8) {
      setError("Password must be at least 8 characters.");
      return;
    }
    if (password !== confirm) {
      setError("Passwords do not match.");
      return;
    }
    if (!token) {
      setError("Reset link is missing its token. Request a fresh email and try again.");
      return;
    }
    try {
      await cmd.mutateAsync({ token, password });
      setSubmitted(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  if (submitted) {
    return (
      <Card className={cn("mx-auto w-full max-w-sm", props.className)}>
        <CardHeader className="text-center">
          <CheckCircle2 className="mx-auto h-10 w-10 text-primary" />
          <CardTitle>Password updated</CardTitle>
          <CardDescription>You can now sign in with your new password.</CardDescription>
        </CardHeader>
        {props.signInURL ? (
          <CardContent>
            <Button asChild className="w-full">
              <a href={props.signInURL}>Continue to sign in</a>
            </Button>
          </CardContent>
        ) : null}
      </Card>
    );
  }

  return (
    <Card className={cn("mx-auto w-full max-w-sm", props.className)}>
      <CardHeader className="text-center">
        <CardTitle>Set a new password</CardTitle>
        <CardDescription>
          {props.brand
            ? `Choose a new password for your ${props.brand} account.`
            : "Choose a new password for your account."}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={submit} className="space-y-4" noValidate>
          <div className="space-y-1.5">
            <Label htmlFor="reset-password">New password</Label>
            <Input
              id="reset-password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              minLength={8}
              autoComplete="new-password"
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="reset-confirm">Confirm password</Label>
            <Input
              id="reset-confirm"
              type="password"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              required
              minLength={8}
              autoComplete="new-password"
            />
          </div>
          {error ? <p className="text-xs text-destructive">{error}</p> : null}
          <Button type="submit" className="w-full" disabled={cmd.isPending}>
            {cmd.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
            Update password
          </Button>
          {props.signInURL ? (
            <p className="text-center text-xs text-muted-foreground">
              Remembered it?{" "}
              <a className="text-primary underline-offset-4 hover:underline" href={props.signInURL}>
                Sign in
              </a>
            </p>
          ) : null}
        </form>
      </CardContent>
    </Card>
  );
}
