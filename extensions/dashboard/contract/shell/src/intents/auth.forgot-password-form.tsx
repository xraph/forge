import * as React from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Loader2, MailCheck } from "lucide-react";
import { useContractCommand } from "../contract/hooks";
import { useContributor } from "../runtime/context";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface ForgotPasswordFormProps {
  op: string;
  signInURL?: string;
  brand?: string;
  className?: string;
}

/**
 * auth.forgot-password-form is the "forgot password" page rendered by the
 * dashboard's AuthGate when the user clicks the recovery link on the login
 * page. It collects an email, posts to props.op (typically
 * `auth.forgotPassword`), and shows a success state regardless of whether
 * the email exists (to avoid account enumeration).
 */
export function ForgotPasswordForm({
  props,
}: IntentComponentProps<unknown, ForgotPasswordFormProps>) {
  const contributor = useContributor();
  const cmd = useContractCommand<{ email: string }, unknown>(contributor, props.op);
  const [email, setEmail] = React.useState("");
  const [submitted, setSubmitted] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    try {
      await cmd.mutateAsync({ email });
      setSubmitted(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  if (submitted) {
    return (
      <Card className={cn("mx-auto w-full max-w-sm", props.className)}>
        <CardHeader className="text-center">
          <MailCheck className="mx-auto h-10 w-10 text-primary" />
          <CardTitle>Check your email</CardTitle>
          <CardDescription>
            If an account exists for {email}, you'll receive password reset instructions shortly.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {props.signInURL ? (
            <Button asChild variant="outline" className="w-full">
              <a href={props.signInURL}>Back to sign in</a>
            </Button>
          ) : null}
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className={cn("mx-auto w-full max-w-sm", props.className)}>
      <CardHeader className="text-center">
        <CardTitle>Reset your password</CardTitle>
        <CardDescription>
          {props.brand
            ? `Enter the email associated with your ${props.brand} account.`
            : "Enter your email and we'll send you a reset link."}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={submit} className="space-y-4" noValidate>
          <div className="space-y-1.5">
            <Label htmlFor="forgot-email">Email</Label>
            <Input
              id="forgot-email"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
              autoComplete="email"
            />
          </div>
          {error ? <p className="text-xs text-destructive">{error}</p> : null}
          <Button type="submit" className="w-full" disabled={cmd.isPending}>
            {cmd.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
            Send reset link
          </Button>
          {props.signInURL ? (
            <p className="text-center text-xs text-muted-foreground">
              Remember your password?{" "}
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
