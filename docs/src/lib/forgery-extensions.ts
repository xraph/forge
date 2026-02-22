import type { LucideIcon } from "lucide-react";
import {
  Brain,
  Database,
  Shield,
  Zap,
  Key,
  CreditCard,
  Network,
  Send,
  FlaskConical,
  ShieldCheck,
  Lock,
  WandSparkles,
  GitBranch,
  Siren,
} from "lucide-react";

export type ExtensionCategory =
  | "AI & ML"
  | "Data & Storage"
  | "Messaging"
  | "Platform"
  | "Security";

export interface ForgeryExtension {
  /** GitHub repo name under github.com/xraph/ */
  slug: string;
  /** Display name */
  name: string;
  /** One-line description */
  description: string;
  category: ExtensionCategory;
  /** Tailwind color classes for icon bg/text */
  iconBg: string;
  iconColor: string;
  /** Tailwind gradient for the card accent strip */
  gradient: string;
  /** Go module path */
  modulePath: string;
  /** Static fallback star count */
  fallbackStars: number;
  /** Static fallback version tag */
  fallbackVersion: string;
}

// ─── Extension Registry ───────────────────────────────────────────────────────
// To add a new extension, add an entry here and a corresponding icon in FORGERY_ICON_MAP below.

export const FORGERY_EXTENSIONS: ForgeryExtension[] = [
  // ── AI & ML ──
  {
    slug: "cortex",
    name: "Cortex",
    description:
      "Human-emulating AI agent orchestration with skills, traits, and personas.",
    category: "AI & ML",
    iconBg: "bg-violet-500/10",
    iconColor: "text-violet-500",
    gradient: "from-violet-500/20 to-purple-500/10",
    modulePath: "github.com/xraph/cortex",
    fallbackStars: 0,
    fallbackVersion: "v0.1.0",
  },
  {
    slug: "nexus",
    name: "Nexus",
    description:
      "AI gateway for routing, caching, and guarding LLM traffic at scale.",
    category: "AI & ML",
    iconBg: "bg-indigo-500/10",
    iconColor: "text-indigo-500",
    gradient: "from-indigo-500/20 to-blue-500/10",
    modulePath: "github.com/xraph/nexus",
    fallbackStars: 0,
    fallbackVersion: "v0.1.0",
  },
  {
    slug: "sentinel",
    name: "Sentinel",
    description:
      "AI evaluation and testing framework with 22 scorers and red-team generators.",
    category: "AI & ML",
    iconBg: "bg-cyan-500/10",
    iconColor: "text-cyan-500",
    gradient: "from-cyan-500/20 to-teal-500/10",
    modulePath: "github.com/xraph/sentinel",
    fallbackStars: 0,
    fallbackVersion: "v0.1.0",
  },
  {
    slug: "shield",
    name: "Shield",
    description:
      "Human-centric AI safety across 6 cognitive layers — from threat detection to compliance.",
    category: "AI & ML",
    iconBg: "bg-rose-500/10",
    iconColor: "text-rose-500",
    gradient: "from-rose-500/20 to-pink-500/10",
    modulePath: "github.com/xraph/shield",
    fallbackStars: 0,
    fallbackVersion: "v0.1.0",
  },
  {
    slug: "weave",
    name: "Weave",
    description:
      "RAG pipeline engine — load, chunk, embed, store, retrieve, and assemble context.",
    category: "AI & ML",
    iconBg: "bg-purple-500/10",
    iconColor: "text-purple-500",
    gradient: "from-purple-500/20 to-violet-500/10",
    modulePath: "github.com/xraph/weave",
    fallbackStars: 0,
    fallbackVersion: "v0.1.0",
  },

  // ── Data & Storage ──
  {
    slug: "grove",
    name: "Grove",
    description:
      "Polyglot Go ORM with native query generation for PostgreSQL, SQLite, and more.",
    category: "Data & Storage",
    iconBg: "bg-emerald-500/10",
    iconColor: "text-emerald-500",
    gradient: "from-emerald-500/20 to-green-500/10",
    modulePath: "github.com/xraph/grove",
    fallbackStars: 0,
    fallbackVersion: "v0.1.0",
  },
  {
    slug: "chronicle",
    name: "Chronicle",
    description:
      "Immutable audit trail with SHA-256 hash chain and GDPR crypto-erasure.",
    category: "Data & Storage",
    iconBg: "bg-amber-500/10",
    iconColor: "text-amber-500",
    gradient: "from-amber-500/20 to-orange-500/10",
    modulePath: "github.com/xraph/chronicle",
    fallbackStars: 0,
    fallbackVersion: "v0.1.0",
  },

  // ── Messaging ──
  {
    slug: "relay",
    name: "Relay",
    description:
      "Webhook delivery engine with HMAC signatures, retries, and dead letter replay.",
    category: "Messaging",
    iconBg: "bg-sky-500/10",
    iconColor: "text-sky-500",
    gradient: "from-sky-500/20 to-blue-500/10",
    modulePath: "github.com/xraph/relay",
    fallbackStars: 0,
    fallbackVersion: "v0.1.0",
  },
  {
    slug: "dispatch",
    name: "Dispatch",
    description:
      "Durable execution engine for background jobs, workflows, and distributed cron.",
    category: "Messaging",
    iconBg: "bg-yellow-500/10",
    iconColor: "text-yellow-500",
    gradient: "from-yellow-500/20 to-amber-500/10",
    modulePath: "github.com/xraph/dispatch",
    fallbackStars: 0,
    fallbackVersion: "v0.1.0",
  },

  // ── Platform ──
  {
    slug: "ledger",
    name: "Ledger",
    description:
      "Usage-based billing engine with metering, subscriptions, and invoice generation.",
    category: "Platform",
    iconBg: "bg-teal-500/10",
    iconColor: "text-teal-500",
    gradient: "from-teal-500/20 to-emerald-500/10",
    modulePath: "github.com/xraph/ledger",
    fallbackStars: 0,
    fallbackVersion: "v0.1.0",
  },

  // ── Security ──
  {
    slug: "authsome",
    name: "Authsome",
    description:
      "Composable auth engine — OAuth2, WebAuthn, MFA, sessions, and API keys.",
    category: "Security",
    iconBg: "bg-amber-500/10",
    iconColor: "text-amber-500",
    gradient: "from-amber-500/20 to-orange-500/10",
    modulePath: "github.com/xraph/authsome",
    fallbackStars: 0,
    fallbackVersion: "v0.1.0",
  },
  {
    slug: "keysmith",
    name: "Keysmith",
    description:
      "API key lifecycle management — issuance, rotation, scoping, and revocation.",
    category: "Security",
    iconBg: "bg-orange-500/10",
    iconColor: "text-orange-500",
    gradient: "from-orange-500/20 to-red-500/10",
    modulePath: "github.com/xraph/keysmith",
    fallbackStars: 0,
    fallbackVersion: "v0.1.0",
  },
  {
    slug: "vault",
    name: "Vault",
    description:
      "Secrets management, feature flags, and hot-reloadable runtime configuration.",
    category: "Security",
    iconBg: "bg-red-500/10",
    iconColor: "text-red-500",
    gradient: "from-red-500/20 to-rose-500/10",
    modulePath: "github.com/xraph/vault",
    fallbackStars: 0,
    fallbackVersion: "v0.1.0",
  },
  {
    slug: "warden",
    name: "Warden",
    description:
      "Unified permissions engine supporting RBAC, ABAC, and Zanzibar-style ReBAC.",
    category: "Security",
    iconBg: "bg-lime-500/10",
    iconColor: "text-lime-600",
    gradient: "from-lime-500/20 to-green-500/10",
    modulePath: "github.com/xraph/warden",
    fallbackStars: 0,
    fallbackVersion: "v0.1.0",
  },
];

// ─── Icon Map ─────────────────────────────────────────────────────────────────
// Used by the client component to resolve slug → icon (avoids serialization issues
// when passing LucideIcon references across the server/client component boundary).

export const FORGERY_ICON_MAP: Record<string, LucideIcon> = {
  authsome: Shield,
  chronicle: GitBranch,
  cortex: Brain,
  dispatch: Zap,
  grove: Database,
  keysmith: Key,
  ledger: CreditCard,
  nexus: Network,
  relay: Send,
  sentinel: FlaskConical,
  shield: Siren,
  vault: Lock,
  warden: ShieldCheck,
  weave: WandSparkles,
};

// ─── Category Metadata ────────────────────────────────────────────────────────

export const CATEGORY_ORDER: ExtensionCategory[] = [
  "AI & ML",
  "Data & Storage",
  "Messaging",
  "Platform",
  "Security",
];

export const CATEGORY_STYLE: Record<ExtensionCategory, string> = {
  "AI & ML":
    "border-violet-500/40 text-violet-600 dark:text-violet-400 bg-violet-500/10",
  "Data & Storage":
    "border-emerald-500/40 text-emerald-600 dark:text-emerald-400 bg-emerald-500/10",
  Messaging:
    "border-sky-500/40 text-sky-600 dark:text-sky-400 bg-sky-500/10",
  Platform:
    "border-teal-500/40 text-teal-600 dark:text-teal-400 bg-teal-500/10",
  Security:
    "border-amber-500/40 text-amber-600 dark:text-amber-400 bg-amber-500/10",
};
