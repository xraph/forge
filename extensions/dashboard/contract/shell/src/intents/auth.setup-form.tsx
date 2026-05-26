import * as React from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Loader2, ShieldCheck } from "lucide-react";
import { useContractCommand } from "../contract/hooks";
import { useContributor } from "../runtime/context";
import { usePrincipalStore } from "../auth/principal";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface SetupFormProps {
  op: string;
  brand?: string;
  className?: string;
}

interface SetupPayload {
  email: string;
  password: string;
  name?: string;
  organizationName?: string;
}

/**
 * auth.setup-form is the one-time bootstrap page rendered when no admin
 * user exists yet on the deployment. It collects an initial admin
 * email + password (and optional name + org name) and posts to props.op
 * (typically `auth.setup`). On success the principal store is reloaded
 * to pick up the new session.
 */
export function SetupForm({ props }: IntentComponentProps<unknown, SetupFormProps>) {
  const contributor = useContributor();
  const reload = usePrincipalStore((s) => s.load);
  const cmd = useContractCommand<SetupPayload, unknown>(contributor, props.op);

  const [name, setName] = React.useState("");
  const [email, setEmail] = React.useState("");
  const [password, setPassword] = React.useState("");
  const [organizationName, setOrganizationName] = React.useState("");
  const [error, setError] = React.useState<string | null>(null);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    try {
      await cmd.mutateAsync({
        email,
        password,
        name: name || undefined,
        organizationName: organizationName || undefined,
      });
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <Card className={cn("mx-auto w-full max-w-md", props.className)}>
      <CardHeader className="text-center">
        <ShieldCheck className="mx-auto h-10 w-10 text-primary" />
        <CardTitle>Welcome{props.brand ? ` to ${props.brand}` : ""}</CardTitle>
        <CardDescription>
          Create the first admin account. You can invite teammates and
          configure settings once you sign in.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={submit} className="space-y-4" noValidate>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="setup-name">Your name</Label>
              <Input
                id="setup-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                autoComplete="name"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="setup-org">Organization (optional)</Label>
              <Input
                id="setup-org"
                value={organizationName}
                onChange={(e) => setOrganizationName(e.target.value)}
                autoComplete="organization"
              />
            </div>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="setup-email">Admin email</Label>
            <Input
              id="setup-email"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
              autoComplete="email"
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="setup-password">Password</Label>
            <Input
              id="setup-password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              minLength={12}
              autoComplete="new-password"
            />
            <p className="text-xs text-muted-foreground">At least 12 characters.</p>
          </div>
          {error ? <p className="text-xs text-destructive">{error}</p> : null}
          <Button type="submit" className="w-full" disabled={cmd.isPending}>
            {cmd.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
            Create admin account
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}
