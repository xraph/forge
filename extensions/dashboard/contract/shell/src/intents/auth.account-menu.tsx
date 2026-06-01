import { CreditCard, LogOut, Settings, User as UserIcon } from "lucide-react";
import { Link } from "react-router-dom";
import { Card, CardContent } from "@/components/ui/card";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { useContractCommand } from "../contract/hooks";
import { useContributor } from "../runtime/context";
import { usePrincipalStore } from "../auth/principal";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface AccountMenuProps {
  signOutOp?: string;
  profileURL?: string;
  settingsURL?: string;
  billingURL?: string;
  showOrgSwitcher?: boolean;
  className?: string;
}

export function AccountMenu({ props }: IntentComponentProps<unknown, AccountMenuProps>) {
  const principal = usePrincipalStore((s) => s.principal);
  const reload = usePrincipalStore((s) => s.load);
  const contributor = useContributor();
  const signOut = useContractCommand(contributor, props.signOutOp ?? "auth.logout");

  if (!principal) return null;
  const user = principal as { email?: string; name?: string; avatar?: string };
  const initials = (user.name ?? user.email ?? "?").slice(0, 2).toUpperCase();

  const handleSignOut = async () => {
    if (!props.signOutOp) return;
    try {
      await signOut.mutateAsync({});
      await reload();
    } catch (err) {
      console.error("sign out error", err);
    }
  };

  return (
    <Card className={cn("w-full max-w-md", props.className)}>
      <CardContent className="p-6">
        <div className="flex items-center gap-3">
          <Avatar className="h-12 w-12">
            {user.avatar ? <AvatarImage src={user.avatar} alt={user.name ?? ""} /> : null}
            <AvatarFallback>{initials}</AvatarFallback>
          </Avatar>
          <div className="min-w-0 flex-1">
            <p className="truncate text-base font-semibold">{user.name ?? user.email}</p>
            {user.email && user.name ? (
              <p className="truncate text-sm text-muted-foreground">{user.email}</p>
            ) : null}
          </div>
        </div>
        <Separator className="my-4" />
        <div className="space-y-1">
          {props.profileURL ? (
            <Button asChild variant="ghost" className="w-full justify-start">
              <Link to={props.profileURL}>
                <UserIcon className="mr-2 h-4 w-4" /> Profile
              </Link>
            </Button>
          ) : null}
          {props.settingsURL ? (
            <Button asChild variant="ghost" className="w-full justify-start">
              <Link to={props.settingsURL}>
                <Settings className="mr-2 h-4 w-4" /> Account settings
              </Link>
            </Button>
          ) : null}
          {props.billingURL ? (
            <Button asChild variant="ghost" className="w-full justify-start">
              <Link to={props.billingURL}>
                <CreditCard className="mr-2 h-4 w-4" /> Billing
              </Link>
            </Button>
          ) : null}
          {props.signOutOp ? (
            <>
              <Separator className="my-2" />
              <Button
                variant="ghost"
                className="w-full justify-start text-destructive hover:bg-destructive/10 hover:text-destructive"
                onClick={handleSignOut}
              >
                <LogOut className="mr-2 h-4 w-4" /> Sign out
              </Button>
            </>
          ) : null}
        </div>
      </CardContent>
    </Card>
  );
}
