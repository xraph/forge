"use client";

import Link from "next/link";
import { motion } from "framer-motion";
import {
  ArrowRight,
  Box,
  GitBranch,
  Layers,
  RefreshCw,
  ScanSearch,
  Settings,
} from "lucide-react";
import { CodeBlock } from "./code-block";

const highlights = [
  { icon: Box, label: "Go Generics", desc: "Fully type-safe container API" },
  {
    icon: GitBranch,
    label: "Constructor Injection",
    desc: "Auto-resolve dependencies",
  },
  {
    icon: Layers,
    label: "Scoped Services",
    desc: "Singleton, transient, scoped lifetimes",
  },
  {
    icon: ScanSearch,
    label: "Circular Detection",
    desc: "Compile-time safety checks",
  },
  {
    icon: RefreshCw,
    label: "Lifecycle Hooks",
    desc: "Init, start, stop callbacks",
  },
  {
    icon: Settings,
    label: "Standalone or Forge",
    desc: "Use independently or integrated",
  },
];

const vesselCode = `// Register services with type-safe generics
vessel.Register[UserService](container, NewUserService)
vessel.Register[OrderService](container, NewOrderService)
vessel.RegisterScoped[RequestCtx](container, NewRequestCtx)

// Resolve with automatic dependency injection
userSvc := vessel.Resolve[UserService](container)`;

export function VesselSection() {
  return (
    <section className="container max-w-(--fd-layout-width) mx-auto px-4 sm:px-6">
      <motion.div
        initial={{ opacity: 0, y: 32 }}
        whileInView={{ opacity: 1, y: 0 }}
        viewport={{ once: true, margin: "-80px" }}
        transition={{ duration: 0.6, ease: "easeOut" }}
        className="group relative overflow-hidden border border-fd-border bg-fd-card"
      >
        <div className="absolute inset-0 bg-fd-card z-0" />
        <div className="metal-shader absolute inset-0 opacity-40 z-0 pointer-events-none" />
        <div className="noise-overlay absolute inset-0 pointer-events-none opacity-50" />

        <div className="relative z-10 grid lg:grid-cols-2 gap-0">
          {/* Code side (left on desktop) */}
          <div className="border-b lg:border-b-0 lg:border-r border-fd-border bg-zinc-950 p-4 sm:p-6 lg:p-8 flex items-center order-2 lg:order-1">
            <div className="relative w-full transform transition-transform group-hover:scale-[1.01] duration-500">
              <div className="absolute -inset-1 bg-gradient-to-r from-sky-500/10 to-cyan-500/10 blur opacity-0 group-hover:opacity-40 transition duration-500" />
              <CodeBlock code={vesselCode} filename="container.go" />
            </div>
          </div>

          {/* Text side */}
          <div className="p-5 sm:p-8 lg:p-10 flex flex-col justify-center order-1 lg:order-2">
            <div className="inline-flex items-center gap-2 border border-sky-500/30 bg-sky-500/10 px-3 py-1 text-xs font-medium text-sky-600 dark:text-sky-400 mb-6 w-fit">
              <Box className="size-3" />
              Vessel
            </div>
            <h2 className="text-xl sm:text-2xl lg:text-3xl font-bold tracking-tight mb-3 sm:mb-4">
              Type-Safe Dependency Injection
            </h2>
            <p className="text-sm sm:text-base text-fd-muted-foreground leading-relaxed mb-4 sm:mb-6">
              A DI container built on Go generics. Register services once,
              resolve them anywhere with compile-time type safety, automatic
              dependency resolution, and lifecycle management.
            </p>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-2.5 mb-6">
              {highlights.map((h) => (
                <div
                  key={h.label}
                  className="flex items-start gap-2 p-2 transition-colors hover:bg-fd-accent/50"
                >
                  <h.icon className="size-3.5 text-sky-500 mt-0.5 flex-shrink-0" />
                  <div>
                    <div className="text-sm font-medium leading-tight">
                      {h.label}
                    </div>
                    <div className="text-xs text-fd-muted-foreground mt-0.5">
                      {h.desc}
                    </div>
                  </div>
                </div>
              ))}
            </div>

            <Link
              href="/docs/vessel"
              className="inline-flex items-center gap-2 text-sm font-semibold text-sky-600 dark:text-sky-400 hover:text-sky-500 transition-colors w-fit"
            >
              Explore Vessel
              <ArrowRight className="size-4" />
            </Link>
          </div>
        </div>
      </motion.div>
    </section>
  );
}
