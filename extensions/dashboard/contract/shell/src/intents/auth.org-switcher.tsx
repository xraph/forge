import * as React from "react";
import { Check, ChevronsUpDown, Plus, Settings } from "lucide-react";
import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Separator } from "@/components/ui/separator";
import { useContractQuery } from "../contract/hooks";
import { useContributor } from "../runtime/context";
import { usePrincipalStore } from "../auth/principal";
import { dispatchAction, type Action } from "../runtime/actions";
import { cn } from "@/lib/utils";
import type { DataBinding } from "@/contract/types";
import type { IntentComponentProps } from "../runtime/registry";

interface Org {
  id: string;
  name: string;
  logo?: string;
}

interface OrgSwitcherProps {
  orgsSource?: DataBinding;
  currentField?: string;
  onSwitch?: Action;
  createOrgURL?: string;
  manageOrgURL?: string;
  showPersonal?: boolean;
  className?: string;
}

export function OrgSwitcher({ props }: IntentComponentProps<unknown, OrgSwitcherProps>) {
  const contributor = useContributor();
  const principal = usePrincipalStore((s) => s.principal);
  const query = useContractQuery<unknown>(contributor, props.orgsSource?.intent ?? "", undefined, undefined);
  const [open, setOpen] = React.useState(false);

  const orgs = extractOrgs(query.data);
  const currentID = readPath(principal, props.currentField ?? "session.user.organizationID");
  const current = orgs.find((o) => o.id === currentID);

  const switchTo = (org: Org) => {
    if (props.onSwitch) dispatchAction(props.onSwitch, { value: org });
    setOpen(false);
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          className={cn("w-full justify-between", props.className)}
        >
          <span className="flex items-center gap-2 truncate">
            {current?.logo ? (
              <Avatar className="h-5 w-5">
                <AvatarImage src={current.logo} alt={current.name} />
                <AvatarFallback>{current.name.slice(0, 1).toUpperCase()}</AvatarFallback>
              </Avatar>
            ) : null}
            <span className="truncate">{current?.name ?? "Select organization"}</span>
          </span>
          <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-72 p-0">
        <div className="p-1">
          {orgs.map((org) => (
            <button
              key={org.id}
              type="button"
              onClick={() => switchTo(org)}
              className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-sm hover:bg-accent hover:text-accent-foreground"
            >
              <Avatar className="h-5 w-5">
                {org.logo ? <AvatarImage src={org.logo} alt={org.name} /> : null}
                <AvatarFallback>{org.name.slice(0, 1).toUpperCase()}</AvatarFallback>
              </Avatar>
              <span className="flex-1 truncate text-left">{org.name}</span>
              {org.id === currentID ? <Check className="h-4 w-4" /> : null}
            </button>
          ))}
          {orgs.length === 0 && !query.isLoading ? (
            <p className="px-2 py-3 text-center text-xs text-muted-foreground">No organizations</p>
          ) : null}
        </div>
        {(props.createOrgURL || props.manageOrgURL) ? (
          <>
            <Separator />
            <div className="p-1">
              {props.createOrgURL ? (
                <Link
                  to={props.createOrgURL}
                  className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-sm hover:bg-accent hover:text-accent-foreground"
                >
                  <Plus className="h-4 w-4" /> Create organization
                </Link>
              ) : null}
              {props.manageOrgURL ? (
                <Link
                  to={props.manageOrgURL}
                  className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-sm hover:bg-accent hover:text-accent-foreground"
                >
                  <Settings className="h-4 w-4" /> Manage organizations
                </Link>
              ) : null}
            </div>
          </>
        ) : null}
      </PopoverContent>
    </Popover>
  );
}

function extractOrgs(data: unknown): Org[] {
  if (Array.isArray(data)) return data as Org[];
  if (data && typeof data === "object") {
    const obj = data as Record<string, unknown>;
    for (const k of ["organizations", "items", "data"]) {
      if (Array.isArray(obj[k])) return obj[k] as Org[];
    }
  }
  return [];
}

function readPath(root: unknown, path: string): unknown {
  if (!root || !path) return undefined;
  const segs = path.split(".").filter((s) => s !== "session");
  let cursor: unknown = root;
  for (const s of segs) {
    if (cursor == null || typeof cursor !== "object") return undefined;
    cursor = (cursor as Record<string, unknown>)[s];
  }
  return cursor;
}
