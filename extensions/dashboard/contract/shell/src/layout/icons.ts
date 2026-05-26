import {
  Activity,
  ChartBar,
  Heart,
  History,
  Home,
  Package,
  Plug,
  ScanSearch,
  ScrollText,
  Server,
  Shield,
  Users,
  type LucideIcon,
} from "lucide-react";

// ICONS maps the manifest's icon string (lucide kebab-name OR a few
// authsome-flavoured aliases) to the actual component. Add entries here
// as new icons start appearing in contract manifests — unknown names
// fall back to the generic Plug icon (matches the legacy templ default).
//
// The "forge" alias is intentionally NOT in this map: the AppSwitcher
// renders the inline ForgeLogo SVG for it instead so the brand mark
// stays distinct from a stock lucide glyph.
const ICONS: Record<string, LucideIcon> = {
  home: Home,
  "heart-pulse": Heart,
  "chart-bar": ChartBar,
  "scan-search": ScanSearch,
  package: Package,
  server: Server,
  activity: Activity,
  history: History,
  shield: Shield,
  users: Users,
  scroll: ScrollText,
};

export function iconFor(name?: string): LucideIcon {
  if (!name) return Plug;
  return ICONS[name] ?? Plug;
}
