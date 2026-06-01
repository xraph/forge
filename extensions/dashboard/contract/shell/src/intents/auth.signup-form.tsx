import * as React from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { Loader2 } from "lucide-react";
import { useContractCommand } from "../contract/hooks";
import { useContributor } from "../runtime/context";
import { usePrincipalStore } from "../auth/principal";
import { dispatchAction, type Action } from "../runtime/actions";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface SignUpFormProps {
  op: string;
  showOAuth?: boolean;
  oauthProviders?: string[];
  nameField?: boolean;
  termsURL?: string;
  privacyURL?: string;
  signInURL?: string;
  brand?: string;
  onSuccess?: Action;
  className?: string;
}

export function SignUpForm({ props }: IntentComponentProps<unknown, SignUpFormProps>) {
  const contributor = useContributor();
  const reload = usePrincipalStore((s) => s.load);
  const cmd = useContractCommand<Record<string, unknown>, unknown>(contributor, props.op);
  const [name, setName] = React.useState("");
  const [email, setEmail] = React.useState("");
  const [password, setPassword] = React.useState("");
  const [agree, setAgree] = React.useState(!props.termsURL && !props.privacyURL);
  const [error, setError] = React.useState<string | null>(null);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (!agree) {
      setError("You must accept the terms to continue.");
      return;
    }
    try {
      await cmd.mutateAsync(props.nameField !== false ? { name, email, password } : { email, password });
      await reload();
      if (props.onSuccess) dispatchAction(props.onSuccess, {});
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <Card className={cn("mx-auto w-full max-w-sm", props.className)}>
      <CardHeader className="text-center">
        <CardTitle>Create your account</CardTitle>
        {props.brand ? <CardDescription>Get started with {props.brand}</CardDescription> : null}
      </CardHeader>
      <CardContent>
        <form onSubmit={submit} className="space-y-4" noValidate>
          {props.nameField !== false ? (
            <div className="space-y-1.5">
              <Label htmlFor="signup-name">Name</Label>
              <Input
                id="signup-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
              />
            </div>
          ) : null}
          <div className="space-y-1.5">
            <Label htmlFor="signup-email">Email</Label>
            <Input
              id="signup-email"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
              autoComplete="email"
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="signup-password">Password</Label>
            <Input
              id="signup-password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              minLength={8}
              autoComplete="new-password"
            />
            <p className="text-xs text-muted-foreground">At least 8 characters.</p>
          </div>
          {(props.termsURL || props.privacyURL) ? (
            <label className="flex items-start gap-2 text-xs text-muted-foreground">
              <Checkbox checked={agree} onCheckedChange={(v) => setAgree(!!v)} />
              <span>
                I agree to the{" "}
                {props.termsURL ? (
                  <a className="text-primary underline-offset-4 hover:underline" href={props.termsURL}>
                    terms
                  </a>
                ) : null}
                {props.termsURL && props.privacyURL ? " and " : null}
                {props.privacyURL ? (
                  <a className="text-primary underline-offset-4 hover:underline" href={props.privacyURL}>
                    privacy policy
                  </a>
                ) : null}
                .
              </span>
            </label>
          ) : null}
          {error ? <p className="text-xs text-destructive">{error}</p> : null}
          <Button type="submit" className="w-full" disabled={cmd.isPending}>
            {cmd.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
            Create account
          </Button>
          {props.signInURL ? (
            <p className="text-center text-xs text-muted-foreground">
              Already have an account?{" "}
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
