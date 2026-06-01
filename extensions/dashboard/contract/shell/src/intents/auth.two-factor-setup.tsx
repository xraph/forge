import * as React from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Check, Loader2, Shield } from "lucide-react";
import { useContractCommand } from "../contract/hooks";
import { useContributor } from "../runtime/context";
import { dispatchAction, type Action } from "../runtime/actions";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface TwoFactorSetupProps {
  generateSecretOp: string;
  verifyOp: string;
  generateBackupOp?: string;
  method?: "totp" | "sms";
  onComplete?: Action;
  className?: string;
}

interface SecretResponse {
  secret?: string;
  qrCodeDataURL?: string;
  otpauthURL?: string;
}

interface BackupResponse {
  codes?: string[];
}

const STEPS = ["generate", "verify", "backup", "done"] as const;
type Step = (typeof STEPS)[number];

export function TwoFactorSetup({ props }: IntentComponentProps<unknown, TwoFactorSetupProps>) {
  const contributor = useContributor();
  const genCmd = useContractCommand<unknown, SecretResponse>(contributor, props.generateSecretOp);
  const verifyCmd = useContractCommand<{ code: string }, unknown>(contributor, props.verifyOp);
  const backupCmd = useContractCommand<unknown, BackupResponse>(contributor, props.generateBackupOp ?? "");
  const [step, setStep] = React.useState<Step>("generate");
  const [secret, setSecret] = React.useState<SecretResponse | null>(null);
  const [backup, setBackup] = React.useState<string[]>([]);
  const [code, setCode] = React.useState("");
  const [error, setError] = React.useState<string | null>(null);

  const generate = async () => {
    setError(null);
    try {
      const res = await genCmd.mutateAsync({});
      setSecret(res);
      setStep("verify");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const verify = async () => {
    setError(null);
    try {
      await verifyCmd.mutateAsync({ code });
      if (props.generateBackupOp) {
        const codes = await backupCmd.mutateAsync({});
        setBackup(codes.codes ?? []);
        setStep("backup");
      } else {
        setStep("done");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const finish = () => {
    if (props.onComplete) dispatchAction(props.onComplete, {});
    setStep("done");
  };

  return (
    <Card className={cn("mx-auto w-full max-w-md", props.className)}>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Shield className="h-5 w-5" /> Two-factor authentication
        </CardTitle>
        <CardDescription>Add a second factor to keep your account safe.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {step === "generate" ? (
          <>
            <p className="text-sm text-muted-foreground">
              We'll generate a secret you can scan into an authenticator app.
            </p>
            <Button onClick={generate} disabled={genCmd.isPending}>
              {genCmd.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
              Generate secret
            </Button>
          </>
        ) : null}

        {step === "verify" && secret ? (
          <>
            {secret.qrCodeDataURL ? (
              <img src={secret.qrCodeDataURL} alt="QR code" className="mx-auto h-44 w-44 rounded-md border" />
            ) : null}
            {secret.secret ? (
              <div className="space-y-1">
                <p className="text-xs text-muted-foreground">Or enter the code manually:</p>
                <code className="block rounded bg-muted px-2 py-1 text-center font-mono text-sm">
                  {secret.secret}
                </code>
              </div>
            ) : null}
            <div className="space-y-1.5">
              <Label htmlFor="2fa-code">Enter the 6-digit code from your authenticator</Label>
              <Input
                id="2fa-code"
                inputMode="numeric"
                pattern="[0-9]*"
                maxLength={6}
                value={code}
                onChange={(e) => setCode(e.target.value)}
                placeholder="123456"
                autoFocus
              />
            </div>
            {error ? <p className="text-xs text-destructive">{error}</p> : null}
            <Button onClick={verify} disabled={verifyCmd.isPending || code.length < 6} className="w-full">
              {verifyCmd.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
              Verify and enable
            </Button>
          </>
        ) : null}

        {step === "backup" ? (
          <>
            <p className="text-sm">
              Save these backup codes somewhere safe — each can be used once if you lose access to your authenticator.
            </p>
            <div className="grid grid-cols-2 gap-2 rounded-md border bg-muted/30 p-3 font-mono text-sm">
              {backup.map((c, i) => (
                <span key={i}>{c}</span>
              ))}
            </div>
            <Button onClick={finish} className="w-full">
              I've saved my codes
            </Button>
          </>
        ) : null}

        {step === "done" ? (
          <div className="flex flex-col items-center gap-2 py-4 text-center">
            <div className="rounded-full bg-primary/15 p-3 text-primary">
              <Check className="h-6 w-6" />
            </div>
            <p className="text-sm font-medium">Two-factor authentication enabled</p>
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}
