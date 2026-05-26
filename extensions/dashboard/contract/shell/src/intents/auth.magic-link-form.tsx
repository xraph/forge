import * as React from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Mail, Loader2 } from "lucide-react";
import { useContractCommand } from "../contract/hooks";
import { useContributor } from "../runtime/context";
import { dispatchAction, type Action } from "../runtime/actions";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface MagicLinkFormProps {
  op: string;
  brand?: string;
  successMessage?: string;
  backToSignIn?: string;
  onSuccess?: Action;
  className?: string;
}

export function MagicLinkForm({ props }: IntentComponentProps<unknown, MagicLinkFormProps>) {
  const contributor = useContributor();
  const cmd = useContractCommand<{ email: string }, unknown>(contributor, props.op);
  const [email, setEmail] = React.useState("");
  const [sent, setSent] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    try {
      await cmd.mutateAsync({ email });
      setSent(true);
      if (props.onSuccess) dispatchAction(props.onSuccess, { value: { email } });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <Card className={cn("mx-auto w-full max-w-sm", props.className)}>
      <CardHeader className="text-center">
        <CardTitle>Sign in with email</CardTitle>
        <CardDescription>
          We'll email you a magic link to sign in to {props.brand ?? "your account"}.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {sent ? (
          <div className="space-y-3 text-center">
            <Mail className="mx-auto h-10 w-10 text-primary" />
            <p className="text-sm">
              {props.successMessage ?? `Check your inbox — we sent a sign-in link to ${email}.`}
            </p>
            {props.backToSignIn ? (
              <a className="text-xs text-primary underline-offset-4 hover:underline" href={props.backToSignIn}>
                Use a different method
              </a>
            ) : null}
          </div>
        ) : (
          <form onSubmit={submit} className="space-y-4" noValidate>
            <div className="space-y-1.5">
              <Label htmlFor="magic-email">Email</Label>
              <Input
                id="magic-email"
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
              Send magic link
            </Button>
            {props.backToSignIn ? (
              <p className="text-center text-xs text-muted-foreground">
                <a className="text-primary underline-offset-4 hover:underline" href={props.backToSignIn}>
                  Back to sign in
                </a>
              </p>
            ) : null}
          </form>
        )}
      </CardContent>
    </Card>
  );
}
