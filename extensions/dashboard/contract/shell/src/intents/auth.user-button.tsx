import { LogOut, Settings, User as UserIcon } from "lucide-react";
import { Link } from "react-router-dom";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Icon } from "./atom.icon";
import { useContractCommand } from "../contract/hooks";
import { useContributor } from "../runtime/context";
import { usePrincipalStore } from "../auth/principal";
import { dispatchAction, type Action } from "../runtime/actions";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface MenuItem {
  label?: string;
  icon?: string;
  shortcut?: string[];
  variant?: "default" | "destructive" | "secondary";
  disabled?: boolean;
  separator?: boolean;
  action?: Action;
}

interface UserButtonProps {
  showName?: boolean;
  showEmail?: boolean;
  menuItems?: MenuItem[];
  signOutOp?: string;
  profileURL?: string;
  settingsURL?: string;
  className?: string;
}

export function UserButton({ props }: IntentComponentProps<unknown, UserButtonProps>) {
  const principal = usePrincipalStore((s) => s.principal);
  const reload = usePrincipalStore((s) => s.load);
  const contributor = useContributor();
  const signOut = useContractCommand(contributor, props.signOutOp ?? "auth.logout");

  if (!principal) return null;
  const display = principal as { email?: string; name?: string; avatar?: string };
  const initials = (display.name ?? display.email ?? "?").slice(0, 2).toUpperCase();

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
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" className={cn("flex h-auto items-center gap-2 px-2 py-1.5", props.className)}>
          <Avatar className="h-7 w-7">
            {display.avatar ? <AvatarImage src={display.avatar} alt={display.name ?? ""} /> : null}
            <AvatarFallback>{initials}</AvatarFallback>
          </Avatar>
          {props.showName !== false ? (
            <div className="flex flex-col items-start text-left">
              <span className="text-sm font-medium leading-tight">{display.name ?? display.email}</span>
              {props.showEmail !== false && display.name && display.email ? (
                <span className="text-xs text-muted-foreground leading-tight">{display.email}</span>
              ) : null}
            </div>
          ) : null}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuLabel>
          <div className="flex flex-col">
            <span className="font-medium">{display.name ?? "Account"}</span>
            {display.email ? <span className="text-xs text-muted-foreground">{display.email}</span> : null}
          </div>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        {props.profileURL ? (
          <DropdownMenuItem render={<Link to={props.profileURL} />}>
            <UserIcon className="mr-2 h-4 w-4" /> Profile
          </DropdownMenuItem>
        ) : null}
        {props.settingsURL ? (
          <DropdownMenuItem render={<Link to={props.settingsURL} />}>
            <Settings className="mr-2 h-4 w-4" /> Settings
          </DropdownMenuItem>
        ) : null}
        {(props.menuItems ?? []).map((item, i) => {
          if (item.separator) return <DropdownMenuSeparator key={`sep-${i}`} />;
          return (
            <DropdownMenuItem
              key={i}
              disabled={item.disabled}
              className={item.variant === "destructive" ? "text-destructive focus:text-destructive" : ""}
              onClick={() => item.action && dispatchAction(item.action, {})}
            >
              {item.icon ? <Icon node={{} as never} slots={{}} props={{ name: item.icon, size: 14 }} /> : null}
              <span className={cn(item.icon && "ml-2")}>{item.label}</span>
            </DropdownMenuItem>
          );
        })}
        {props.signOutOp ? (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={handleSignOut} className="text-destructive focus:text-destructive">
              <LogOut className="mr-2 h-4 w-4" /> Sign out
            </DropdownMenuItem>
          </>
        ) : null}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
