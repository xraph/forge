"use client";

import Link from "next/link";
import { motion, AnimatePresence } from "framer-motion";
import { useEffect, useState } from "react";
import {
  ArrowRight,
  Info,
  Network,
  Server,
  Router,
  Globe,
  Shield,
  RefreshCw,
  Activity,
  Zap,
  Lock,
} from "lucide-react";

const features = [
  {
    icon: Network,
    title: "Zero-Config Discovery",
    desc: "Services auto-register via mDNS, Consul, Kubernetes, etcd, and more.",
  },
  {
    icon: RefreshCw,
    title: "FARP Protocol",
    desc: "Automatic route configuration from OpenAPI/gRPC schemas.",
  },
  {
    icon: Shield,
    title: "Production Ready",
    desc: "Rate limiting, circuit breakers, and auth built-in.",
  },
];

/* ── Discovery backend definitions ── */
const discoveryBackends = [
  { id: "mdns", label: "mDNS", color: "text-blue-400", bg: "bg-blue-500/10", border: "border-blue-500/30" },
  { id: "consul", label: "Consul", color: "text-pink-400", bg: "bg-pink-500/10", border: "border-pink-500/30" },
  { id: "k8s", label: "Kubernetes", color: "text-cyan-400", bg: "bg-cyan-500/10", border: "border-cyan-500/30" },
];

/* ── Service nodes ── */
const services = [
  { name: "User API", protocol: "REST", color: "text-blue-400", borderColor: "border-blue-500/30", bgColor: "bg-blue-500/10" },
  { name: "Auth gRPC", protocol: "gRPC", color: "text-emerald-400", borderColor: "border-emerald-500/30", bgColor: "bg-emerald-500/10" },
  { name: "Events WS", protocol: "WebSocket", color: "text-amber-400", borderColor: "border-amber-500/30", bgColor: "bg-amber-500/10" },
];

/* ── Animated flow particle ── */
function FlowParticle({
  delay,
  duration,
  direction,
  color,
}: {
  delay: number;
  duration: number;
  direction: "ltr" | "rtl";
  color: string;
}) {
  return (
    <motion.div
      className={`absolute top-1/2 -translate-y-1/2 size-1.5 rounded-full ${color}`}
      style={{ boxShadow: `0 0 6px 2px currentColor` }}
      animate={{
        left: direction === "ltr" ? ["-4%", "104%"] : ["104%", "-4%"],
        opacity: [0, 1, 1, 0],
      }}
      transition={{
        duration,
        delay,
        repeat: Infinity,
        ease: "linear",
      }}
    />
  );
}

/* ── Animated connection line with particles ── */
function ConnectionLine({
  direction = "ltr",
  color = "text-purple-500",
  particleCount = 2,
  speed = 2.5,
}: {
  direction?: "ltr" | "rtl";
  color?: string;
  particleCount?: number;
  speed?: number;
}) {
  return (
    <div className="relative h-[2px] w-full bg-zinc-800/60 overflow-hidden">
      {Array.from({ length: particleCount }).map((_, i) => (
        <FlowParticle
          key={i}
          delay={(speed / particleCount) * i}
          duration={speed}
          direction={direction}
          color={color}
        />
      ))}
    </div>
  );
}

/* ── Gateway capability badges ── */
const capabilities = [
  { icon: Activity, label: "Health Checks" },
  { icon: Zap, label: "Load Balancing" },
  { icon: Shield, label: "Circuit Breakers" },
  { icon: Lock, label: "Auth & Rate Limit" },
];

function DiscoveryDiagram() {
  const [activeBackend, setActiveBackend] = useState(0);

  useEffect(() => {
    const timer = setInterval(() => {
      setActiveBackend((prev) => (prev + 1) % discoveryBackends.length);
    }, 3000);
    return () => clearInterval(timer);
  }, []);

  const currentBackend = discoveryBackends[activeBackend];

  return (
    <div className="relative w-full bg-zinc-950 border border-fd-border overflow-hidden">
      {/* Background Grid */}
      <div className="absolute inset-0 bg-[linear-gradient(to_right,#80808008_1px,transparent_1px),linear-gradient(to_bottom,#80808008_1px,transparent_1px)] bg-[size:20px_20px]" />

      {/* Radial glow behind gateway */}
      <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-64 h-64 bg-purple-500/8 rounded-full blur-3xl" />

      <div className="relative z-10 p-5 md:p-6">
        {/* ── Discovery Backend Selector ── */}
        <div className="flex items-center justify-center gap-2 mb-5">
          <span className="text-[10px] uppercase tracking-wider text-zinc-500 mr-2">Discovery via</span>
          {discoveryBackends.map((b, i) => (
            <button
              key={b.id}
              type="button"
              onClick={() => setActiveBackend(i)}
              className={`px-2.5 py-1 text-[11px] font-medium border transition-all duration-300 ${
                i === activeBackend
                  ? `${b.color} ${b.bg} ${b.border}`
                  : "text-zinc-500 border-zinc-800 bg-zinc-900/50 hover:border-zinc-700"
              }`}
            >
              {b.label}
            </button>
          ))}
        </div>

        {/* ── Main Flow Diagram ── */}
        <div className="grid grid-cols-[1fr_auto_1.2fr_auto_1fr] items-center gap-0 min-h-[220px]">

          {/* ── Left: Services ── */}
          <div className="flex flex-col gap-2.5">
            {services.map((svc, i) => (
              <motion.div
                key={svc.name}
                initial={{ opacity: 0, x: -12 }}
                animate={{ opacity: 1, x: 0 }}
                transition={{ duration: 0.4, delay: i * 0.12 }}
                className={`relative border ${svc.borderColor} ${svc.bgColor} p-2.5 group/svc`}
              >
                <div className="flex items-center gap-2">
                  <Server className={`size-3.5 ${svc.color} shrink-0`} />
                  <div className="min-w-0">
                    <div className="text-[11px] font-semibold text-zinc-200 truncate">{svc.name}</div>
                    <div className="text-[9px] text-zinc-500 font-mono">{svc.protocol}</div>
                  </div>
                </div>
                {/* Health dot */}
                <div className="absolute -top-1 -right-1 size-2 rounded-full bg-emerald-500 animate-pulse" />
              </motion.div>
            ))}
            {/* FARP label */}
            <div className="text-center">
              <span className="text-[9px] uppercase tracking-widest text-zinc-600 font-medium">
                FARP Schema
              </span>
            </div>
          </div>

          {/* ── Left Connection Lines (Services → Gateway) ── */}
          <div className="flex flex-col justify-center gap-2.5 px-2 md:px-3 w-12 md:w-20">
            {services.map((svc, i) => (
              <div key={svc.name} className="h-[36px] flex items-center">
                <ConnectionLine
                  direction="ltr"
                  color={svc.color}
                  particleCount={2}
                  speed={2.2 + i * 0.3}
                />
              </div>
            ))}
          </div>

          {/* ── Center: Gateway ── */}
          <div className="flex flex-col items-center">
            <motion.div
              animate={{
                boxShadow: [
                  "0 0 20px -5px rgba(168,85,247,0.2)",
                  "0 0 40px -5px rgba(168,85,247,0.35)",
                  "0 0 20px -5px rgba(168,85,247,0.2)",
                ],
              }}
              transition={{ duration: 3, repeat: Infinity, ease: "easeInOut" }}
              className="relative border-2 border-purple-500/40 bg-purple-500/10 p-5 md:p-6"
            >
              <Router className="size-8 md:size-10 text-purple-400" />
              {/* Status indicator */}
              <div className="absolute -top-2 -right-2 size-4 bg-emerald-500 rounded-full border-2 border-zinc-950 flex items-center justify-center">
                <div className="size-1.5 bg-white rounded-full animate-pulse" />
              </div>
            </motion.div>
            <div className="mt-2.5 text-center">
              <div className="text-sm font-bold text-zinc-200">Forge Gateway</div>
              <AnimatePresence mode="wait">
                <motion.div
                  key={currentBackend?.id}
                  initial={{ opacity: 0, y: 4 }}
                  animate={{ opacity: 1, y: 0 }}
                  exit={{ opacity: 0, y: -4 }}
                  transition={{ duration: 0.25 }}
                  className={`text-[10px] font-medium mt-1 ${currentBackend?.color}`}
                >
                  via {currentBackend?.label}
                </motion.div>
              </AnimatePresence>
            </div>

            {/* Capability badges */}
            <div className="grid grid-cols-2 gap-1.5 mt-3 w-full max-w-[200px]">
              {capabilities.map((cap, i) => (
                <motion.div
                  key={cap.label}
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 1 }}
                  transition={{ delay: 0.6 + i * 0.1 }}
                  className="flex items-center gap-1 border border-zinc-800 bg-zinc-900/80 px-1.5 py-1"
                >
                  <cap.icon className="size-2.5 text-zinc-500 shrink-0" />
                  <span className="text-[8px] text-zinc-400 truncate">{cap.label}</span>
                </motion.div>
              ))}
            </div>
          </div>

          {/* ── Right Connection Lines (Gateway → Clients) ── */}
          <div className="flex flex-col justify-center px-2 md:px-3 w-12 md:w-20">
            <div className="h-[36px] flex items-center">
              <ConnectionLine
                direction="rtl"
                color="text-zinc-400"
                particleCount={3}
                speed={1.8}
              />
            </div>
          </div>

          {/* ── Right: Client ── */}
          <div className="flex flex-col items-center">
            <motion.div
              animate={{ y: [0, -4, 0] }}
              transition={{ duration: 3, repeat: Infinity, ease: "easeInOut" }}
              className="border border-zinc-700 bg-zinc-900/80 p-4"
            >
              <Globe className="size-7 text-zinc-300" />
            </motion.div>
            <div className="mt-2.5 text-center">
              <div className="text-[11px] font-semibold text-zinc-300">Clients</div>
              <div className="text-[9px] text-zinc-500 mt-0.5">HTTP / gRPC / WS</div>
            </div>

            {/* Protocol badges */}
            <div className="mt-2 flex flex-col gap-1">
              {["REST", "gRPC", "WebSocket", "SSE"].map((p, i) => (
                <motion.span
                  key={p}
                  initial={{ opacity: 0, x: 8 }}
                  animate={{ opacity: 1, x: 0 }}
                  transition={{ delay: 0.8 + i * 0.08 }}
                  className="text-[8px] font-mono text-zinc-500 border border-zinc-800 bg-zinc-900/50 px-1.5 py-0.5 text-center"
                >
                  {p}
                </motion.span>
              ))}
            </div>
          </div>
        </div>

        {/* ── Bottom: Flow Legend ── */}
        <div className="flex items-center justify-center gap-4 mt-5 pt-4 border-t border-zinc-800/60">
          <div className="flex items-center gap-1.5">
            <div className="size-1.5 rounded-full bg-blue-400" />
            <span className="text-[9px] text-zinc-500">Schema Registration</span>
          </div>
          <div className="flex items-center gap-1.5">
            <div className="size-1.5 rounded-full bg-purple-400" />
            <span className="text-[9px] text-zinc-500">Route Discovery</span>
          </div>
          <div className="flex items-center gap-1.5">
            <div className="size-1.5 rounded-full bg-zinc-400" />
            <span className="text-[9px] text-zinc-500">Client Traffic</span>
          </div>
        </div>
      </div>
    </div>
  );
}

export function GatewaySection() {
  return (
    <section className="border-b border-fd-border w-full ">
      <div className="container max-w-(--fd-layout-width) mx-auto px-6 py-16 md:py-24">
        <div className="grid lg:grid-cols-2 gap-12 lg:gap-20 items-center">
          <motion.div
            initial={{ opacity: 0, x: -20 }}
            whileInView={{ opacity: 1, x: 0 }}
            viewport={{ once: true }}
            transition={{ duration: 0.6 }}
          >
            <div className="inline-flex items-center gap-2 border border-purple-500/30 bg-purple-500/10 px-3 py-1 text-xs font-medium text-purple-600 dark:text-purple-400 mb-6 rounded-none w-fit">
              <Network className="size-3" />
              Gateway & Discovery
            </div>

            <h2 className="text-3xl font-bold tracking-tight mb-6">
              Instant API Gateway.{" "}
              <span className="text-fd-muted-foreground">Just add code.</span>
            </h2>

            <p className="text-lg text-fd-muted-foreground mb-8 leading-relaxed">
              Turn any Forge app into a powerful API Gateway. With the{" "}
              <strong>FARP protocol</strong>, services automatically announce
              their schemas (OpenAPI, gRPC) and the gateway configures routes
              instantly.
            </p>

            <div className="grid gap-6">
              {features.map((feature) => (
                <div key={feature.title} className="flex gap-4">
                  <div className="mt-1 size-10 rounded-lg bg-fd-accent flex items-center justify-center shrink-0">
                    <feature.icon className="size-5 text-fd-foreground" />
                  </div>
                  <div>
                    <h3 className="font-semibold">{feature.title}</h3>
                    <p className="text-sm text-fd-muted-foreground mt-1">
                      {feature.desc}
                    </p>
                  </div>
                </div>
              ))}
            </div>

            {/* Notice */}
            <div className="mt-8 flex items-start gap-2.5 border border-amber-500/20 bg-amber-500/5 dark:bg-amber-500/10 px-4 py-3 text-sm text-fd-muted-foreground">
              <Info className="size-4 text-amber-500 mt-0.5 shrink-0" />
              <p>
                Designed for <strong className="text-fd-foreground">development and small deployments</strong>.
                For production at scale, consider a dedicated API gateway like Kong, Envoy, or AWS API Gateway.
              </p>
            </div>

            {/* Links */}
            <div className="mt-6 flex flex-wrap items-center gap-4">
              <Link
                href="/docs/extensions/gateway"
                className="inline-flex items-center gap-2 text-sm font-semibold text-purple-600 dark:text-purple-400 hover:text-purple-500 transition-colors"
              >
                Gateway Extension
                <ArrowRight className="size-4" />
              </Link>
              <Link
                href="/docs/farp"
                className="inline-flex items-center gap-2 text-sm font-semibold text-purple-600 dark:text-purple-400 hover:text-purple-500 transition-colors"
              >
                FARP Protocol
                <ArrowRight className="size-4" />
              </Link>
              <Link
                href="/docs/farp/discovery"
                className="inline-flex items-center gap-2 text-sm font-semibold text-purple-600 dark:text-purple-400 hover:text-purple-500 transition-colors"
              >
                Service Discovery
                <ArrowRight className="size-4" />
              </Link>
            </div>
          </motion.div>

          <motion.div
            initial={{ opacity: 0, x: 20 }}
            whileInView={{ opacity: 1, x: 0 }}
            viewport={{ once: true }}
            transition={{ duration: 0.6, delay: 0.2 }}
            className="relative group"
          >
            <div className="absolute -inset-1 bg-gradient-to-r from-purple-500/20 to-blue-500/20 rounded-2xl blur-xl opacity-50 group-hover:opacity-75 transition duration-1000" />
            <div className="metal-shader absolute inset-0 rounded-xl opacity-20 z-10 pointer-events-none" />
            <DiscoveryDiagram />
          </motion.div>
        </div>
      </div>
    </section>
  );
}
