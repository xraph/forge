"use client";

import Link from "next/link";
import { motion } from "framer-motion";
import { ChevronRight, Copy } from "lucide-react";
import { useState } from "react";

export function CTA() {
  const [copied, setCopied] = useState(false);
  const cmd = "go get github.com/xraph/forge";

  const handleCopy = async () => {
    await navigator.clipboard.writeText(cmd);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <section className="w-full border-t border-fd-border">
      <div className="container max-w-(--fd-layout-width) mx-auto px-4 sm:px-6 py-12 sm:py-16 md:py-24">
        <motion.div
          initial={{ opacity: 0, y: 32 }}
          whileInView={{ opacity: 1, y: 0 }}
          viewport={{ once: true, margin: "-80px" }}
          transition={{ duration: 0.6, ease: "easeOut" }}
          className="relative overflow-hidden border border-fd-border bg-fd-card"
        >
          <div className="absolute inset-0 bg-fd-card z-0" />
          <div className="metal-shader absolute inset-0 opacity-50 z-0 pointer-events-none" />
          <div className="noise-overlay absolute inset-0 pointer-events-none opacity-50" />

          <div className="relative z-10 flex flex-col items-center text-center py-10 sm:py-16 md:py-20 px-4 sm:px-6">
            <h2 className="text-2xl font-bold tracking-tight sm:text-3xl md:text-4xl mb-4">
              Start forging today
            </h2>
            <p className="text-fd-muted-foreground text-lg max-w-lg mb-8">
              From prototype to production in minutes. Build scalable Go backend
              services with the framework that grows with you.
            </p>

            <div className="inline-flex items-center gap-2 sm:gap-3 border border-fd-border bg-fd-background px-3 sm:px-5 py-3 font-mono text-xs sm:text-sm mb-8 max-w-full overflow-x-auto">
              <span className="text-fd-muted-foreground select-none">$</span>
              <span className="text-fd-foreground">{cmd}</span>
              <button
                type="button"
                onClick={handleCopy}
                className="text-fd-muted-foreground hover:text-fd-foreground transition-colors ml-2"
                aria-label="Copy install command"
              >
                <Copy
                  className={`size-3.5 ${copied ? "text-emerald-500" : ""}`}
                />
              </button>
            </div>

            <div className="flex flex-wrap items-center justify-center gap-3">
              <Link
                href="/docs/forge"
                className="group inline-flex items-center gap-2 bg-gradient-to-r from-amber-500 to-orange-600 px-6 py-3 text-sm font-semibold text-white shadow-lg shadow-amber-500/20 transition-all hover:shadow-amber-500/30 hover:brightness-110"
              >
                Get Started
                <ChevronRight className="size-4 transition-transform group-hover:translate-x-0.5" />
              </Link>
              <Link
                href="/docs/forge/examples"
                className="inline-flex items-center gap-2 border border-fd-border bg-fd-background px-6 py-3 text-sm font-semibold transition-colors hover:bg-fd-accent"
              >
                View Examples
              </Link>
            </div>
          </div>
        </motion.div>
      </div>
    </section>
  );
}
