"use client";

import { motion } from "framer-motion";
import { ArrowUpRight, Layers, Shield } from "lucide-react";
import { SectionHeader } from "./section-header";

const products = [
  {
    name: "Ctrl Plane",
    description:
      "Service management and orchestration platform. Infrastructure control plane built with Forge.",
    url: "https://ctrl.xraph.com/",
    icon: Layers,
    gradient: "from-cyan-500/20 to-blue-500/10",
    gradientPosition: "-top-24 -left-24",
    iconColor: "text-cyan-500",
    iconBg: "bg-cyan-500/10",
  },
  {
    name: "Authsome",
    description:
      "Authentication and authorization service. OAuth2, SAML, OIDC, and API key management — built with Forge.",
    url: "https://authsome.xraph.com/",
    icon: Shield,
    gradient: "from-amber-500/20 to-orange-500/10",
    gradientPosition: "-bottom-24 -right-24",
    iconColor: "text-amber-500",
    iconBg: "bg-amber-500/10",
  },
];

const containerVariants = {
  hidden: {},
  visible: { transition: { staggerChildren: 0.12 } },
};

const itemVariants = {
  hidden: { opacity: 0, y: 20 },
  visible: {
    opacity: 1,
    y: 0,
    transition: { duration: 0.5, ease: "easeOut" as const },
  },
};

export function Ecosystem() {
  return (
    <section className="container max-w-(--fd-layout-width) mx-auto px-6 py-16 md:py-24">
      <SectionHeader
        title="Built with Forge"
        description="Production services powered by the Forge framework."
        className="mb-8"
      />
      <motion.div
        variants={containerVariants}
        initial="hidden"
        whileInView="visible"
        viewport={{ once: true, margin: "-80px" }}
        className="grid grid-cols-1 sm:grid-cols-2 gap-4"
      >
        {products.map((product) => (
          <motion.a
            key={product.name}
            variants={itemVariants}
            href={product.url}
            target="_blank"
            rel="noreferrer"
            className="group relative overflow-hidden border border-fd-border bg-fd-card transition-all hover:border-fd-border/80 hover:shadow-lg hover:shadow-black/5"
          >
            <div className="absolute inset-0 bg-fd-card z-0" />
            <div className="metal-shader absolute inset-0 opacity-50 z-0 pointer-events-none" />
            <div className="noise-overlay absolute inset-0 pointer-events-none opacity-50" />

            {/* Content */}
            <div className="relative z-10 p-6 lg:p-8">
              <div className="flex items-start justify-between mb-4">
                <div
                  className={`flex items-center justify-center size-10 ${product.iconBg} ${product.iconColor}`}
                >
                  <product.icon className="size-5" />
                </div>
                <ArrowUpRight className="size-4 text-fd-muted-foreground group-hover:text-fd-foreground transition-colors" />
              </div>
              <h3 className="font-semibold text-lg mb-2">{product.name}</h3>
              <p className="text-sm text-fd-muted-foreground leading-relaxed">
                {product.description}
              </p>
            </div>
          </motion.a>
        ))}
      </motion.div>
    </section>
  );
}
