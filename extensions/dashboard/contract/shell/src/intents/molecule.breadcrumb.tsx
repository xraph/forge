import * as React from "react";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { Icon } from "./atom.icon";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface BreadcrumbItemSpec {
  label: string;
  href?: string;
  icon?: string;
}

interface BreadcrumbProps {
  items: BreadcrumbItemSpec[];
  separator?: string;
  className?: string;
}

export function MoleculeBreadcrumb({
  props,
}: IntentComponentProps<unknown, BreadcrumbProps>) {
  const items = props.items ?? [];
  return (
    <Breadcrumb className={cn(props.className)}>
      <BreadcrumbList>
        {items.map((c, i) => {
          const isLast = i === items.length - 1;
          return (
            <React.Fragment key={`${c.label}-${i}`}>
              <BreadcrumbItem>
                {c.icon ? (
                  <span className="mr-1">
                    <Icon node={{} as never} slots={{}} props={{ name: c.icon, size: 14 }} />
                  </span>
                ) : null}
                {isLast || !c.href ? (
                  <BreadcrumbPage>{c.label}</BreadcrumbPage>
                ) : (
                  <BreadcrumbLink href={c.href}>{c.label}</BreadcrumbLink>
                )}
              </BreadcrumbItem>
              {!isLast ? <BreadcrumbSeparator>{props.separator}</BreadcrumbSeparator> : null}
            </React.Fragment>
          );
        })}
      </BreadcrumbList>
    </Breadcrumb>
  );
}
