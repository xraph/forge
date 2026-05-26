import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { cn } from "@/lib/utils";
import { containerClass, type ContainerSize } from "@/lib/tokens";
import { SlotRenderer } from "../runtime/slots";
import type { IntentComponentProps } from "../runtime/registry";

interface PageCrumb {
  label: string;
  href?: string;
  icon?: string;
}

interface PageProps {
  description?: string;
  breadcrumbs?: PageCrumb[];
  containerWidth?: ContainerSize;
  noChrome?: boolean;
  className?: string;
}

export function Page({ node, props, slots }: IntentComponentProps<unknown, PageProps>) {
  if (props.noChrome) {
    return (
      <div className={cn("w-full", props.className)}>
        <SlotRenderer slot="content" slots={slots} />
      </div>
    );
  }

  const crumbs = props.breadcrumbs ?? [];
  const actions = slots["actions"] ?? [];

  return (
    <div className={cn("mx-auto w-full space-y-6", containerClass(props.containerWidth), props.className)}>
      {crumbs.length > 0 ? (
        <Breadcrumb>
          <BreadcrumbList>
            {crumbs.map((c, i) => {
              const isLast = i === crumbs.length - 1;
              return (
                <span key={`${c.label}-${i}`} className="contents">
                  <BreadcrumbItem>
                    {isLast || !c.href ? (
                      <BreadcrumbPage>{c.label}</BreadcrumbPage>
                    ) : (
                      <BreadcrumbLink href={c.href}>{c.label}</BreadcrumbLink>
                    )}
                  </BreadcrumbItem>
                  {!isLast ? <BreadcrumbSeparator /> : null}
                </span>
              );
            })}
          </BreadcrumbList>
        </Breadcrumb>
      ) : null}

      {(node.title || props.description || actions.length > 0) && (
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-1">
            {node.title ? (
              <h1 className="text-2xl font-semibold tracking-tight">{node.title}</h1>
            ) : null}
            {props.description ? (
              <p className="text-sm text-muted-foreground">{props.description}</p>
            ) : null}
          </div>
          {actions.length > 0 ? (
            <div className="flex items-center gap-2">
              <SlotRenderer slot="actions" slots={slots} />
            </div>
          ) : null}
        </div>
      )}

      <div>
        <SlotRenderer slot="content" slots={slots} />
      </div>
    </div>
  );
}
