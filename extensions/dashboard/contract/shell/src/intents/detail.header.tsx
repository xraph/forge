import { Badge } from "@/components/ui/badge";
import { useParent } from "../runtime/context";
import { Icon } from "./atom.icon";
import { cn } from "@/lib/utils";
import type { IntentComponentProps } from "../runtime/registry";

interface DetailHeaderProps {
  /** Field name on the parent record to render as the title. */
  titleField?: string;
  /** Field name to render as the subtitle (e.g. email under name). */
  subtitleField?: string;
  /** Field name holding an image URL for the leading avatar. */
  imageField?: string;
  /**
   * Field name holding an array of badge specs. Each entry is either a
   * string (rendered as the label with `default` variant) or an object
   * `{ label, variant }` where variant is `default|secondary|destructive|outline`.
   */
  badgesField?: string;
  /** Override the leading icon when no imageField is set or the value is empty. */
  fallbackIcon?: string;
  className?: string;
}

type BadgeSpec = string | { label: string; variant?: BadgeVariant };
type BadgeVariant = "default" | "secondary" | "destructive" | "outline";

/**
 * detail.header is a record-bound page header for resource.detail. It pulls
 * its content from the parent context (the loaded record) — no own data
 * binding. Renders an avatar/icon, title, subtitle, and a row of status
 * badges. Used as the `header` slot fill of resource.detail to give a page
 * the avatar+title+status block the templui user_detail.templ ships today.
 */
export function DetailHeader({ props }: IntentComponentProps<unknown, DetailHeaderProps>) {
  const record = useParent() as Record<string, unknown> | null;
  if (!record) return null;

  const title = stringField(record, props.titleField);
  const subtitle = stringField(record, props.subtitleField);
  const image = stringField(record, props.imageField);
  const badges = badgesFromField(record[props.badgesField ?? ""]);

  return (
    <div className={cn("flex items-center justify-between gap-4", props.className)}>
      <div className="flex items-center gap-4">
        {image ? (
          <img
            src={image}
            alt={title ?? ""}
            className="h-16 w-16 rounded-full object-cover"
          />
        ) : (
          <div className="flex h-16 w-16 items-center justify-center rounded-full bg-muted">
            <Icon
              node={{} as never}
              slots={{}}
              props={{ name: props.fallbackIcon ?? "user", size: 24 }}
            />
          </div>
        )}
        <div>
          {title ? <h1 className="text-2xl font-semibold tracking-tight">{title}</h1> : null}
          {subtitle ? <p className="text-sm text-muted-foreground">{subtitle}</p> : null}
        </div>
      </div>
      {badges.length > 0 ? (
        <div className="flex items-center gap-2">
          {badges.map((b, i) => (
            <Badge key={i} variant={b.variant ?? "default"}>
              {b.label}
            </Badge>
          ))}
        </div>
      ) : null}
    </div>
  );
}

function stringField(record: Record<string, unknown>, name: string | undefined): string | null {
  if (!name) return null;
  const v = record[name];
  return typeof v === "string" && v.length > 0 ? v : null;
}

function badgesFromField(raw: unknown): { label: string; variant?: BadgeVariant }[] {
  if (!Array.isArray(raw)) return [];
  const out: { label: string; variant?: BadgeVariant }[] = [];
  for (const item of raw as BadgeSpec[]) {
    if (typeof item === "string") {
      out.push({ label: item });
    } else if (item && typeof item === "object" && typeof item.label === "string") {
      out.push({ label: item.label, variant: item.variant });
    }
  }
  return out;
}
