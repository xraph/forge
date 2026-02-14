"use client";

import Link from "next/link";
import {
  motion,
  useInView,
  useMotionValue,
  useTransform,
  animate,
} from "framer-motion";
import { useEffect, useRef } from "react";
import {
  ArrowRight,
  Database,
  Globe,
  Lock,
  Radio,
  Server,
  Brain,
  Zap,
  MessageSquare,
  Search,
  Tv,
  Gamepad2,
  LayoutDashboard,
  Calendar,
  Shield,
  Workflow,
} from "lucide-react";
import { Card, CardContent, CardHeader } from "@/components/ui/card";

/* ─── Animated number counter ─── */
function AnimatedNumber({ value, suffix }: { value: number; suffix: string }) {
  const ref = useRef<HTMLSpanElement>(null);
  const motionVal = useMotionValue(0);
  const rounded = useTransform(motionVal, (v) => Math.round(v));
  const inView = useInView(ref, { once: true, margin: "-40px" });

  useEffect(() => {
    if (inView) {
      animate(motionVal, value, { duration: 1.5, ease: "easeOut" });
    }
  }, [inView, motionVal, value]);

  return (
    <span ref={ref} className="text-3xl font-bold tabular-nums sm:text-4xl">
      <motion.span>{rounded}</motion.span>
      {suffix}
    </span>
  );
}

/* ─── Extension categories (factual from codebase) ─── */
const categories = [
  {
    icon: Database,
    title: "Data & Storage",
    extensions: [
      "PostgreSQL",
      "MySQL",
      "MongoDB",
      "Redis",
      "SQLite",
      "ClickHouse",
      "BadgerDB",
    ],
  },
  {
    icon: Radio,
    title: "Transport & Protocol",
    extensions: [
      "gRPC",
      "GraphQL",
      "WebSocket",
      "NATS",
      "Kafka",
      "MQTT",
      "oRPC",
    ],
  },
  {
    icon: Lock,
    title: "Security & Middleware",
    extensions: [
      "JWT Auth",
      "OAuth2",
      "CORS",
      "Rate Limiting",
      "RBAC",
      "Compression",
      "Recovery",
    ],
  },
  {
    icon: Server,
    title: "Infrastructure",
    extensions: [
      "Prometheus",
      "OpenTelemetry",
      "Consul",
      "mDNS",
      "Docker",
      "Kubernetes",
      "Gateway",
    ],
  },
  {
    icon: Brain,
    title: "AI & Intelligence",
    extensions: [
      "LLM Providers",
      "ReAct Agents",
      "RAG Pipelines",
      "MCP Server",
      "Embeddings",
      "Guardrails",
    ],
  },
  {
    icon: Zap,
    title: "Runtime & Utilities",
    extensions: [
      "Cron Jobs",
      "Event Bus",
      "Feature Flags",
      "Search",
      "Streaming",
      "Dashboard",
      "Queue",
    ],
  },
];

/* ─── Stats (factual) ─── */
const stats = [
  { value: 24, suffix: "+", label: "Extensions" },
  { value: 7, suffix: "", label: "Protocols" },
  { value: 100, suffix: "%", label: "Type-Safe" },
  { value: 7, suffix: "", label: "Middleware" },
];

/* ─── Extension icon mapping for the visual grid ─── */
const extensionIcons = [
  { icon: Database, label: "Database" },
  { icon: Radio, label: "gRPC" },
  { icon: Globe, label: "GraphQL" },
  { icon: Brain, label: "AI SDK" },
  { icon: MessageSquare, label: "Kafka" },
  { icon: Shield, label: "Auth" },
  { icon: Search, label: "Search" },
  { icon: Tv, label: "Streaming" },
  { icon: Gamepad2, label: "WebRTC" },
  { icon: LayoutDashboard, label: "Dashboard" },
  { icon: Calendar, label: "Cron" },
  { icon: Workflow, label: "Events" },
];

export function ExtensionsStatsSection() {
  return (
    <section className="container max-w-(--fd-layout-width) mx-auto px-4 sm:px-6 py-12 sm:py-16 md:py-24">
      <div className="grid gap-2 grid-cols-1 sm:grid-cols-5">
        {/* ── Top-left: Stats + headline (3 cols) ── */}
        <Card className="group overflow-hidden shadow-black/5 sm:col-span-3 sm:rounded-none sm:rounded-tl-xl border-fd-border bg-fd-card">
          <CardHeader>
            <div className="sm:p-4 md:p-6">
              <p className="font-medium">24+ Production Extensions</p>
              <p className="text-fd-muted-foreground mt-3 max-w-sm text-sm">
                From databases to AI, transport layers to security. Every
                extension integrates in one line with full type safety and
                lifecycle management.
              </p>
            </div>
          </CardHeader>

          <div className="relative h-fit pl-4 sm:pl-6 md:pl-12">
            <div className="absolute -inset-6 [background:radial-gradient(75%_95%_at_50%_0%,transparent,hsl(var(--background))_100%)]" />

            <div className="bg-fd-background overflow-hidden rounded-tl-lg border-l border-t border-fd-border pl-4 pt-4 dark:bg-zinc-950">
              {/* Animated stats grid */}
              <div className="grid grid-cols-2 gap-px bg-fd-border sm:grid-cols-4">
                {stats.map((stat) => (
                  <motion.div
                    key={stat.label}
                    initial={{ opacity: 0, y: 16 }}
                    whileInView={{ opacity: 1, y: 0 }}
                    viewport={{ once: true, margin: "-40px" }}
                    transition={{ duration: 0.5, ease: "easeOut" }}
                    className="flex flex-col items-center justify-center bg-fd-card py-8"
                  >
                    <AnimatedNumber value={stat.value} suffix={stat.suffix} />
                    <span className="text-sm text-fd-muted-foreground mt-1">
                      {stat.label}
                    </span>
                  </motion.div>
                ))}
              </div>
            </div>
          </div>
        </Card>

        {/* ── Top-right: Quick install (2 cols) ── */}
        <Card className="group overflow-hidden shadow-zinc-950/5 sm:col-span-2 sm:rounded-none sm:rounded-tr-xl border-fd-border bg-fd-card">
          <p className="mx-auto my-4 sm:my-6 max-w-md text-balance px-4 sm:px-6 text-center text-base font-semibold sm:text-lg md:text-2xl md:p-6">
            One line to add. Zero config needed.
          </p>

          <CardContent className="mt-auto h-fit">
            <div className="relative mb-6 sm:mb-0">
              <div className="absolute -inset-6 [background:radial-gradient(50%_75%_at_75%_50%,transparent,hsl(var(--background))_100%)]" />
              <div className="relative overflow-hidden rounded-r-lg border border-fd-border bg-zinc-950 p-3 sm:p-4">
                <pre className="text-[10px] sm:text-[12px] leading-relaxed font-mono text-zinc-400 overflow-x-auto">
                  <div>
                    <span className="text-zinc-600">
                      // Add any extension in one line
                    </span>
                  </div>
                  <div>
                    <span className="text-zinc-300">app</span>.
                    <span className="text-sky-400">Use</span>(
                    <span className="text-sky-400">PostgreSQL</span>.
                    <span className="text-sky-400">Extension</span>())
                  </div>
                  <div>
                    <span className="text-zinc-300">app</span>.
                    <span className="text-sky-400">Use</span>(
                    <span className="text-sky-400">Redis</span>.
                    <span className="text-sky-400">Extension</span>())
                  </div>
                  <div>
                    <span className="text-zinc-300">app</span>.
                    <span className="text-sky-400">Use</span>(
                    <span className="text-sky-400">GRPC</span>.
                    <span className="text-sky-400">Extension</span>())
                  </div>
                  <div>
                    <span className="text-zinc-300">app</span>.
                    <span className="text-sky-400">Use</span>(
                    <span className="text-sky-400">AI</span>.
                    <span className="text-sky-400">Extension</span>())
                  </div>
                  <div>
                    <span className="text-zinc-300">app</span>.
                    <span className="text-sky-400">Use</span>(
                    <span className="text-sky-400">Gateway</span>.
                    <span className="text-sky-400">Extension</span>())
                  </div>
                </pre>
              </div>
            </div>
          </CardContent>
        </Card>

        {/* ── Bottom-left: Extension icon grid (2 cols) ── */}
        <Card className="group p-4 sm:p-6 shadow-black/5 sm:col-span-2 sm:rounded-none sm:rounded-bl-xl border-fd-border bg-fd-card md:p-8">
          <p className="mx-auto mb-6 sm:mb-8 max-w-md text-balance text-center text-base font-semibold sm:text-lg md:text-xl">
            Pluggable architecture for every use case
          </p>

          <div className="grid grid-cols-3 gap-3 sm:grid-cols-4">
            {extensionIcons.map((ext) => (
              <div
                key={ext.label}
                className="group/icon flex flex-col items-center gap-1.5"
              >
                <div className="bg-fd-muted/50 dark:bg-fd-muted/30 flex aspect-square w-full items-center justify-center border border-fd-border p-3 transition-colors hover:bg-fd-accent/40">
                  <ext.icon className="size-5 text-fd-muted-foreground group-hover/icon:text-fd-foreground transition-colors" />
                </div>
                <span className="text-[10px] text-fd-muted-foreground">
                  {ext.label}
                </span>
              </div>
            ))}
          </div>
        </Card>

        {/* ── Bottom-right: Categories list (3 cols) ── */}
        <Card className="group relative shadow-black/5 sm:col-span-3 sm:rounded-none sm:rounded-br-xl border-fd-border bg-fd-card">
          <CardHeader className="p-4 sm:p-6 md:p-8">
            <p className="font-medium">Extension Categories</p>
            <p className="text-fd-muted-foreground mt-2 max-w-sm text-sm">
              Organized into 6 categories covering data, networking, security,
              infrastructure, AI, and runtime utilities.
            </p>
          </CardHeader>
          <CardContent className="relative h-fit px-4 pb-4 sm:px-6 sm:pb-6 md:px-8 md:pb-8">
            <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
              {categories.map((cat) => (
                <div
                  key={cat.title}
                  className="border border-fd-border p-4 transition-colors hover:bg-fd-accent/20"
                >
                  <div className="flex items-center gap-2 mb-3">
                    <cat.icon className="size-4 text-fd-muted-foreground" />
                    <span className="text-sm font-medium">{cat.title}</span>
                  </div>
                  <div className="flex flex-wrap gap-1.5">
                    {cat.extensions.map((ext) => (
                      <span
                        key={ext}
                        className="inline-flex items-center border border-fd-border bg-fd-background px-2 py-0.5 text-[11px] font-mono text-fd-muted-foreground"
                      >
                        {ext}
                      </span>
                    ))}
                  </div>
                </div>
              ))}
            </div>

            <motion.div
              initial={{ opacity: 0 }}
              whileInView={{ opacity: 1 }}
              viewport={{ once: true }}
              transition={{ duration: 0.5, delay: 0.3 }}
              className="mt-6"
            >
              <Link
                href="/docs/forge/extensions"
                className="inline-flex items-center gap-2 text-sm font-semibold text-fd-foreground hover:text-fd-foreground/80 transition-colors"
              >
                View All Extensions
                <ArrowRight className="size-4" />
              </Link>
            </motion.div>
          </CardContent>
        </Card>
      </div>
    </section>
  );
}
