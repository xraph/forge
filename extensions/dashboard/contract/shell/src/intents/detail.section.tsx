import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Icon } from "./atom.icon";
import { SlotRenderer } from "../runtime/slots";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface DetailSectionProps {
  title?: string;
  description?: string;
  icon?: string;
  /** Optional badge displayed next to the title (e.g. row count). */
  badge?: string;
  /** When true, the section renders without the surrounding Card chrome. */
  bare?: boolean;
  className?: string;
}

/**
 * detail.section is a labelled card wrapper for one section of a
 * resource.detail page. It renders its `content` slot inside a Card with
 * an optional title, description, and leading icon. The `bare` prop drops
 * the Card wrapper so a section can host a full-bleed table or other
 * component that already provides its own chrome.
 */
export function DetailSection({
  props,
  slots,
}: IntentComponentProps<unknown, DetailSectionProps>) {
  const hasHeader = Boolean(props.title || props.description || props.icon);

  const body = <SlotRenderer slot="content" slots={slots} />;

  if (props.bare) {
    return (
      <div className={cn("space-y-3", props.className)}>
        {hasHeader ? <SectionHeader {...props} /> : null}
        {body}
      </div>
    );
  }

  return (
    <Card className={props.className}>
      {hasHeader ? (
        <CardHeader>
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              {props.icon ? (
                <Icon
                  node={{} as never}
                  slots={{}}
                  props={{ name: props.icon, size: 18 }}
                />
              ) : null}
              {props.title ? <CardTitle>{props.title}</CardTitle> : null}
              {props.badge ? <Badge variant="secondary">{props.badge}</Badge> : null}
            </div>
          </div>
          {props.description ? <CardDescription>{props.description}</CardDescription> : null}
        </CardHeader>
      ) : null}
      <CardContent>{body}</CardContent>
    </Card>
  );
}

function SectionHeader(props: DetailSectionProps) {
  return (
    <div className="flex items-center gap-2">
      {props.icon ? (
        <Icon node={{} as never} slots={{}} props={{ name: props.icon, size: 18 }} />
      ) : null}
      {props.title ? <h3 className="text-lg font-semibold">{props.title}</h3> : null}
      {props.badge ? <Badge variant="secondary">{props.badge}</Badge> : null}
    </div>
  );
}
