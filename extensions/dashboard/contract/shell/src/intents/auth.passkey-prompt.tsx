import * as React from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Fingerprint, Loader2 } from "lucide-react";
import { useContractCommand } from "../contract/hooks";
import { useContributor } from "../runtime/context";
import { dispatchAction, type Action } from "../runtime/actions";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface PasskeyPromptProps {
  registerOp: string;
  onComplete?: Action;
  onSkip?: Action;
  skipLabel?: string;
  dismissible?: boolean;
  className?: string;
}

export function PasskeyPrompt({ props }: IntentComponentProps<unknown, PasskeyPromptProps>) {
  const contributor = useContributor();
  const cmd = useContractCommand<unknown, unknown>(contributor, props.registerOp);
  const [dismissed, setDismissed] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  if (dismissed) return null;

  const register = async () => {
    setError(null);
    if (!("PublicKeyCredential" in window)) {
      setError("Passkeys are not supported in this browser.");
      return;
    }
    try {
      await cmd.mutateAsync({});
      if (props.onComplete) dispatchAction(props.onComplete, {});
      setDismissed(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const skip = () => {
    if (props.onSkip) dispatchAction(props.onSkip, {});
    setDismissed(true);
  };

  return (
    <Card className={cn("mx-auto w-full max-w-md", props.className)}>
      <CardHeader>
        <div className="mx-auto rounded-full bg-primary/10 p-3 text-primary">
          <Fingerprint className="h-6 w-6" />
        </div>
        <CardTitle className="text-center">Add a passkey</CardTitle>
        <CardDescription className="text-center">
          Passkeys let you sign in with Face ID, Touch ID, or a security key. They're phishing-resistant and faster than passwords.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {error ? <p className="text-center text-xs text-destructive">{error}</p> : null}
        <Button onClick={register} className="w-full" disabled={cmd.isPending}>
          {cmd.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
          Add passkey
        </Button>
        {props.dismissible !== false ? (
          <Button variant="ghost" onClick={skip} className="w-full">
            {props.skipLabel ?? "Skip for now"}
          </Button>
        ) : null}
      </CardContent>
    </Card>
  );
}
