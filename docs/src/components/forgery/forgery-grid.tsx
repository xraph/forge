"use client";

import { useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { ExternalLink, Star, Tag } from "lucide-react";
import type { ExtensionCategory } from "@/lib/forgery-extensions";
import { FORGERY_ICON_MAP } from "@/lib/forgery-extensions";

function CardDecorator() {
  return (
    <>
      <span className="border-primary absolute -left-px -top-px block size-2 border-l-2 border-t-2" />
      <span className="border-primary absolute -right-px -top-px block size-2 border-r-2 border-t-2" />
      <span className="border-primary absolute -bottom-px -left-px block size-2 border-b-2 border-l-2" />
      <span className="border-primary absolute -bottom-px -right-px block size-2 border-b-2 border-r-2" />
    </>
  );
}

interface EnrichedExtension {
  slug: string;
  name: string;
  description: string;
  category: ExtensionCategory;
  iconBg: string;
  iconColor: string;
  gradient: string;
  stars: number;
  latestVersion: string;
  githubUrl: string;
  modulePath: string;
}

interface ForgeryGridProps {
  extensions: EnrichedExtension[];
  categoryOrder: ExtensionCategory[];
  categoryStyle: Record<ExtensionCategory, string>;
}

const containerVariants = {
  hidden: {},
  visible: { transition: { staggerChildren: 0.06 } },
};

const itemVariants = {
  hidden: { opacity: 0, y: 20 },
  visible: {
    opacity: 1,
    y: 0,
    transition: { duration: 0.45, ease: "easeOut" as const },
  },
};

export function ForgeryGrid({
  extensions,
  categoryOrder,
  categoryStyle,
}: ForgeryGridProps) {
  const [activeCategory, setActiveCategory] = useState<
    ExtensionCategory | "All"
  >("All");

  const filtered =
    activeCategory === "All"
      ? extensions
      : extensions.filter((e) => e.category === activeCategory);

  return (
    <div>
      {/* Category filter pills */}
      <div className="flex flex-wrap gap-2 mb-10">
        <button
          type="button"
          onClick={() => setActiveCategory("All")}
          className={`text-[10px] font-semibold uppercase tracking-wider px-3 py-1.5 border transition-colors ${
            activeCategory === "All"
              ? "border-fd-foreground/30 bg-fd-foreground/10 text-fd-foreground"
              : "border-fd-border text-fd-muted-foreground hover:text-fd-foreground hover:border-fd-border/80"
          }`}
        >
          All ({extensions.length})
        </button>
        {categoryOrder.map((cat) => {
          const count = extensions.filter((e) => e.category === cat).length;
          return (
            <button
              key={cat}
              type="button"
              onClick={() => setActiveCategory(cat)}
              className={`text-[10px] font-semibold uppercase tracking-wider px-3 py-1.5 border transition-colors ${
                activeCategory === cat
                  ? categoryStyle[cat]
                  : "border-fd-border text-fd-muted-foreground hover:text-fd-foreground hover:border-fd-border/80"
              }`}
            >
              {cat} ({count})
            </button>
          );
        })}
      </div>

      {/* Extension cards */}
      <AnimatePresence mode="wait">
        <motion.div
          key={activeCategory}
          variants={containerVariants}
          initial="hidden"
          animate="visible"
          className="grid md:grid-cols-2 lg:grid-cols-3 gap-5"
        >
          {filtered.map((ext) => {
            const Icon = FORGERY_ICON_MAP[ext.slug];
            return (
              <motion.a
                key={ext.slug}
                variants={itemVariants}
                href={ext.githubUrl}
                target="_blank"
                rel="noreferrer"
                className="group relative overflow-hidden border border-fd-border bg-fd-card transition-all hover:border-fd-border/80 hover:shadow-lg hover:shadow-black/5"
              >
                <CardDecorator />
                <div className="metal-shader absolute inset-0 opacity-40 pointer-events-none" />
                <div className="noise-overlay absolute inset-0 pointer-events-none opacity-40" />

                {/* Gradient accent strip */}
                <div
                  className={`h-1 w-full bg-linear-to-r ${ext.gradient}`}
                />

                <div className="relative z-10 p-5">
                  {/* Icon + external link */}
                  <div className="flex items-start justify-between mb-3">
                    <div
                      className={`flex items-center justify-center size-9 ${ext.iconBg} ${ext.iconColor}`}
                    >
                      {Icon && <Icon className="size-4" />}
                    </div>
                    <ExternalLink className="size-3.5 text-fd-muted-foreground group-hover:text-fd-foreground transition-colors mt-1" />
                  </div>

                  {/* Name */}
                  <h3 className="font-semibold text-base mb-1.5">
                    {ext.name}
                  </h3>

                  {/* Category badge */}
                  <span
                    className={`text-[9px] font-semibold uppercase tracking-wider px-2 py-0.5 border mb-3 inline-block ${categoryStyle[ext.category]}`}
                  >
                    {ext.category}
                  </span>

                  {/* Description */}
                  <p className="text-sm text-fd-muted-foreground leading-relaxed mb-4">
                    {ext.description}
                  </p>

                  {/* Module path */}
                  <p className="text-[11px] font-mono text-fd-muted-foreground/70 mb-3 truncate">
                    {ext.modulePath}
                  </p>

                  {/* Metadata row */}
                  <div className="flex items-center justify-between pt-3 border-t border-dashed border-fd-border/60">
                    <div className="flex items-center gap-1.5 text-xs text-fd-muted-foreground">
                      <Tag className="size-3" />
                      <span className="font-mono">{ext.latestVersion}</span>
                    </div>
                    {ext.stars > 0 && (
                      <div className="flex items-center gap-1 text-xs text-fd-muted-foreground">
                        <Star className="size-3 text-amber-500" />
                        <span className="font-mono">
                          {ext.stars.toLocaleString()}
                        </span>
                      </div>
                    )}
                  </div>
                </div>
              </motion.a>
            );
          })}
        </motion.div>
      </AnimatePresence>
    </div>
  );
}
