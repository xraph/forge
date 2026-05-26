import * as React from "react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { SignInForm } from "./auth.signin-form";
import { SignUpForm } from "./auth.signup-form";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface AuthTabsProps {
  defaultTab?: "signin" | "signup";
  signInOp?: string;
  signUpOp?: string;
  oauthProviders?: string[];
  brand?: string;
  className?: string;
}

export function AuthTabs({ node, props, slots }: IntentComponentProps<unknown, AuthTabsProps>) {
  const [tab, setTab] = React.useState(props.defaultTab ?? "signin");

  return (
    <Tabs value={tab} onValueChange={(v) => setTab(v as "signin" | "signup")} className={cn("w-full max-w-sm", props.className)}>
      <TabsList className="grid w-full grid-cols-2">
        <TabsTrigger value="signin">Sign in</TabsTrigger>
        <TabsTrigger value="signup">Sign up</TabsTrigger>
      </TabsList>
      <TabsContent value="signin" className="mt-4">
        <SignInForm
          node={node}
          slots={slots}
          props={{ op: props.signInOp }}
        />
      </TabsContent>
      <TabsContent value="signup" className="mt-4">
        <SignUpForm
          node={node}
          slots={slots}
          props={{
            op: props.signUpOp ?? "auth.signup",
            brand: props.brand,
            nameField: true,
          }}
        />
      </TabsContent>
    </Tabs>
  );
}
