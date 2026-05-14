import { Separator } from "@/components/ui/separator";

/**
 * action.divider renders a thin horizontal separator. When used inside an
 * action.menu, the parent renders a DropdownMenuSeparator instead and this
 * component is bypassed; standalone uses get a regular Separator.
 */
export function ActionDivider() {
  return <Separator className="my-1" />;
}
