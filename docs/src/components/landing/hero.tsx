"use client";

import { motion } from "framer-motion";
import {
  Activity,
  Boxes,
  ChevronRight,
  Copy,
  Hammer,
  Route,
  ShieldCheck,
} from "lucide-react";
import dynamic from "next/dynamic";
import { useEffect, useState } from "react";
import { AnimatedTagline } from "./animated-tagline";
import Link from "next/link";
import { DottedSurface } from "../ui/dotted-surface";

const ForgeBlob = dynamic(
  () => import("../ui/features-ui").then((mod) => mod.Scene),
  { ssr: false },
);

const showcaseStages = [
  {
    label: "Router",
    value: "GET /v1/forge",
    icon: Route,
    accent: "from-cyan-300/0 via-cyan-300 to-blue-400/0",
  },
  {
    label: "DI Graph",
    value: "Providers synced",
    icon: Boxes,
    accent: "from-violet-300/0 via-violet-300 to-indigo-400/0",
  },
  {
    label: "Schemas",
    value: "OpenAPI generated",
    icon: ShieldCheck,
    accent: "from-emerald-300/0 via-emerald-300 to-teal-400/0",
  },
  {
    label: "Runtime",
    value: "P95: 7.8ms",
    icon: Activity,
    accent: "from-amber-300/0 via-amber-300 to-orange-400/0",
  },
];

function InstallCommand() {
  const [copied, setCopied] = useState(false);
  const cmd = "go get github.com/xraph/forge";

  const handleCopy = async () => {
    await navigator.clipboard.writeText(cmd);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="inline-flex items-center gap-3 border border-fd-border bg-fd-card/80 backdrop-blur-sm px-4 py-2.5 font-mono text-sm">
      <span className="select-none text-fd-muted-foreground">$</span>
      <span className="text-fd-foreground">{cmd}</span>
      <button
        type="button"
        onClick={handleCopy}
        className="ml-1 text-fd-muted-foreground transition-colors hover:text-fd-foreground"
        aria-label="Copy install command"
      >
        <Copy className={`size-3.5 ${copied ? "text-emerald-500" : ""}`} />
      </button>
    </div>
  );
}

function ForgeShowcaseOverlay() {
  const [activeStage, setActiveStage] = useState(0);

  useEffect(() => {
    const timer = window.setInterval(() => {
      setActiveStage((current) => (current + 1) % showcaseStages.length);
    }, 1650);

    return () => window.clearInterval(timer);
  }, []);

  return (
    <>
      <div className="absolute left-4 top-4 z-20 flex flex-wrap gap-2">
        {["Go 1.22+", "28+ Extensions", "OpenAPI + AsyncAPI"].map(
          (label, index) => (
            <motion.span
              key={label}
              animate={{ y: [0, -4, 0], opacity: [0.72, 1, 0.72] }}
              transition={{
                duration: 3.6,
                repeat: Number.POSITIVE_INFINITY,
                delay: index * 0.45,
                ease: "easeInOut",
              }}
              className="border border-slate-500/45 bg-slate-900/75 px-2.5 py-1 text-[11px] font-medium tracking-wide text-slate-200 backdrop-blur-sm"
            >
              {label}
            </motion.span>
          ),
        )}
      </div>

      <div className="absolute inset-x-4 bottom-4 z-20">
        <div className="border border-slate-600/55 bg-slate-950/76 p-4 backdrop-blur-md">
          <div className="flex items-center justify-between text-[10px] font-semibold tracking-[0.18em] text-slate-300/85 uppercase">
            <span>Forge Runtime Loop</span>
            <span className="text-amber-300">LIVE</span>
          </div>

          <div className="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-4">
            {showcaseStages.map((stage, index) => {
              const Icon = stage.icon;
              const isActive = index === activeStage;

              return (
                <motion.div
                  key={stage.label}
                  animate={{
                    y: isActive ? -2 : 0,
                    borderColor: isActive
                      ? "rgba(251, 191, 36, 0.72)"
                      : "rgba(100, 116, 139, 0.4)",
                    backgroundColor: isActive
                      ? "rgba(15, 23, 42, 0.94)"
                      : "rgba(15, 23, 42, 0.62)",
                  }}
                  transition={{ duration: 0.32, ease: "easeOut" }}
                  className="relative overflow-hidden border px-2.5 py-2"
                >
                  <div className="flex items-center gap-1.5 text-[10px] text-slate-300">
                    <Icon className="size-3" />
                    <span className="font-medium tracking-[0.12em] uppercase">
                      {stage.label}
                    </span>
                  </div>
                  <span className="mt-1 block text-[11px] font-medium text-slate-100">
                    {stage.value}
                  </span>
                  <span
                    className={`pointer-events-none absolute inset-x-0 bottom-0 h-px bg-gradient-to-r ${stage.accent} ${isActive ? "opacity-100" : "opacity-0"} transition-opacity duration-300`}
                  />
                </motion.div>
              );
            })}
          </div>

          <div className="mt-3 h-1 overflow-hidden rounded-full bg-slate-800/70">
            <motion.div
              className="h-full w-1/4 bg-gradient-to-r from-amber-300 via-orange-400 to-yellow-200"
              animate={{ x: ["-10%", "330%"] }}
              transition={{
                duration: 4.1,
                repeat: Number.POSITIVE_INFINITY,
                ease: "linear",
              }}
            />
          </div>
        </div>
      </div>
    </>
  );
}

export function Hero() {
  return (
    <section className="relative w-full overflow-hidden bg-fd-background">
      {/* Dotted surface background */}
      <DottedSurface className="absolute inset-0 z-0 opacity-40" />

      {/* Subtle gradient overlays */}
      <div className="absolute inset-0 z-[1] bg-[radial-gradient(65%_55%_at_15%_22%,rgba(245,158,11,0.08),transparent_68%)]" />
      <div className="absolute inset-0 z-[1] bg-[radial-gradient(70%_60%_at_88%_55%,rgba(30,64,175,0.08),transparent_70%)]" />

      {/* Bottom fade to blend into next section */}
      <div className="absolute bottom-0 left-0 right-0 z-[2] h-32 bg-gradient-to-t from-fd-background to-transparent" />

      <div className="container relative z-10 mx-auto max-w-(--fd-layout-width) px-6 pb-20 pt-24 md:pb-32 md:pt-36">
        <div className="grid items-center gap-12 lg:grid-cols-[minmax(0,1fr)_480px]">
          <div className="max-w-2xl">
            <motion.div
              initial={{ opacity: 0, y: 16 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ duration: 0.5, delay: 0.1 }}
              className="mb-6 inline-flex items-center gap-2 border border-amber-500/30 bg-amber-500/10 px-4 py-1.5 text-sm text-amber-600 dark:text-amber-400 backdrop-blur-sm"
            >
              <Hammer className="size-3.5" />
              the production-grade Go framework you need
            </motion.div>

            <AnimatedTagline />

            <motion.p
              initial={{ opacity: 0, y: 16 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ duration: 0.6, delay: 0.8 }}
              className="mt-6 max-w-lg text-lg leading-relaxed text-fd-muted-foreground"
            >
              A modular, extensible framework for building backend services in
              Go. Type-safe dependency injection, auto-generated API schemas,
              and 28+ production-ready extensions.
            </motion.p>

            <motion.div
              initial={{ opacity: 0, y: 16 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ duration: 0.6, delay: 1 }}
              className="mt-6"
            >
              <InstallCommand />
            </motion.div>

            <motion.div
              initial={{ opacity: 0, y: 16 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ duration: 0.6, delay: 1.2 }}
              className="mt-8 flex flex-wrap items-center gap-3"
            >
              <Link
                href="/docs/forge"
                className="group inline-flex items-center gap-2 bg-fd-foreground px-6 py-3 text-sm font-semibold text-fd-background shadow-lg transition-all hover:brightness-110"
              >
                Get Started
                <ChevronRight className="size-4 transition-transform group-hover:translate-x-0.5" />
              </Link>
              <a
                href="https://github.com/xraph/forge"
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-2 border border-fd-border bg-fd-background/80 backdrop-blur-sm px-6 py-3 text-sm font-semibold transition-colors hover:bg-fd-accent"
              >
                <svg
                  className="size-4"
                  fill="currentColor"
                  viewBox="0 0 24 24"
                  aria-hidden="true"
                  focusable="false"
                >
                  <path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.286-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12" />
                </svg>
                GitHub
              </a>
            </motion.div>
          </div>

          {/* Right side — SDF blob visual */}
          <motion.div
            initial={{ opacity: 0, scale: 0.92 }}
            animate={{ opacity: 1, scale: 1 }}
            transition={{ duration: 1, delay: 0.6, ease: "easeOut" }}
            className="relative hidden lg:block"
          >
            <div className="relative aspect-square w-full">
              {/* Glow ring behind the blob */}
              <div className="absolute inset-0 rounded-full bg-gradient-to-br from-amber-500/10 via-purple-500/5 to-blue-500/10 blur-3xl" />

              {/* SDF shader blob */}
              <div className="relative h-full w-full">
                <ForgeBlob />
              </div>

              {/* Floating stats orbiting the blob */}
              <motion.div
                animate={{ y: [0, -8, 0] }}
                transition={{
                  duration: 4,
                  repeat: Infinity,
                  ease: "easeInOut",
                }}
                className="absolute -left-4 top-8 z-20 border border-fd-border bg-fd-card/90 backdrop-blur-md px-3 py-2 shadow-lg"
              >
                <div className="flex items-center gap-2">
                  <div className="size-2 rounded-full bg-emerald-500 animate-pulse" />
                  <span className="text-[11px] font-medium text-fd-foreground">
                    24 Extensions
                  </span>
                </div>
              </motion.div>

              <motion.div
                animate={{ y: [0, 6, 0] }}
                transition={{
                  duration: 3.5,
                  repeat: Infinity,
                  ease: "easeInOut",
                  delay: 0.8,
                }}
                className="absolute -right-2 top-1/4 z-20 border border-fd-border bg-fd-card/90 backdrop-blur-md px-3 py-2 shadow-lg"
              >
                <div className="flex items-center gap-2">
                  <Route className="size-3 text-cyan-400" />
                  <span className="text-[11px] font-medium text-fd-foreground">
                    7 Protocols
                  </span>
                </div>
              </motion.div>

              <motion.div
                animate={{ y: [0, -6, 0] }}
                transition={{
                  duration: 5,
                  repeat: Infinity,
                  ease: "easeInOut",
                  delay: 1.5,
                }}
                className="absolute -left-2 bottom-1/4 z-20 border border-fd-border bg-fd-card/90 backdrop-blur-md px-3 py-2 shadow-lg"
              >
                <div className="flex items-center gap-2">
                  <ShieldCheck className="size-3 text-amber-400" />
                  <span className="text-[11px] font-medium text-fd-foreground">
                    Type-Safe DI
                  </span>
                </div>
              </motion.div>

              <motion.div
                animate={{ y: [0, 8, 0] }}
                transition={{
                  duration: 4.2,
                  repeat: Infinity,
                  ease: "easeInOut",
                  delay: 2,
                }}
                className="absolute right-4 bottom-12 z-20 border border-fd-border bg-fd-card/90 backdrop-blur-md px-3 py-2 shadow-lg"
              >
                <div className="flex items-center gap-2">
                  <Activity className="size-3 text-violet-400" />
                  <span className="text-[11px] font-medium text-fd-foreground">
                    P95: 7.8ms
                  </span>
                </div>
              </motion.div>
            </div>
          </motion.div>
        </div>
      </div>
    </section>
  );
}
