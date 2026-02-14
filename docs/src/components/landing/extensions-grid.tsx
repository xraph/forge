'use client';

import Link from 'next/link';
import { motion } from 'framer-motion';
import { ArrowRight, Brain, Database, Globe, Lock, Radio, Server, Send } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { SectionHeader } from './section-header';

interface Category {
  icon: LucideIcon;
  title: string;
  extensions: string[];
  gradient: string;
  gradientPosition: string;
  iconColor: string;
  iconBg: string;
}

const categories: Category[] = [
  {
    icon: Database,
    title: 'Data & Storage',
    extensions: ['PostgreSQL', 'MySQL', 'MongoDB', 'Redis', 'SQLite', 'ClickHouse'],
    gradient: 'from-blue-500/20 to-indigo-500/10',
    gradientPosition: '-top-20 -right-20',
    iconColor: 'text-blue-500',
    iconBg: 'bg-blue-500/10',
  },
  {
    icon: Radio,
    title: 'Transport & Protocol',
    extensions: ['gRPC', 'GraphQL', 'WebSocket', 'NATS', 'Kafka', 'RabbitMQ'],
    gradient: 'from-violet-500/20 to-purple-500/10',
    gradientPosition: '-bottom-20 -left-20',
    iconColor: 'text-violet-500',
    iconBg: 'bg-violet-500/10',
  },
  {
    icon: Lock,
    title: 'Security & Auth',
    extensions: ['JWT', 'OAuth2', 'CORS', 'Rate Limiting', 'API Keys', 'RBAC'],
    gradient: 'from-amber-500/20 to-orange-500/10',
    gradientPosition: '-top-20 -left-20',
    iconColor: 'text-amber-500',
    iconBg: 'bg-amber-500/10',
  },
  {
    icon: Server,
    title: 'Infrastructure',
    extensions: ['Docker', 'Kubernetes', 'Prometheus', 'OpenTelemetry', 'Consul', 'Vault'],
    gradient: 'from-emerald-500/20 to-green-500/10',
    gradientPosition: '-bottom-20 -right-20',
    iconColor: 'text-emerald-500',
    iconBg: 'bg-emerald-500/10',
  },
];

const containerVariants = {
  hidden: {},
  visible: {
    transition: { staggerChildren: 0.1 },
  },
};

const itemVariants = {
  hidden: { opacity: 0, y: 20 },
  visible: {
    opacity: 1,
    y: 0,
    transition: { duration: 0.5, ease: 'easeOut' as const },
  },
};

export function ExtensionsGrid() {
  return (
    <section className="container max-w-(--fd-layout-width) mx-auto px-6 py-16 md:py-24">
      <SectionHeader
        title="28+ Production-Ready Extensions"
        description="From databases to message queues, authentication to observability. Everything plugs in cleanly."
        className="mb-8"
      />
      <motion.div
        variants={containerVariants}
        initial="hidden"
        whileInView="visible"
        viewport={{ once: true, margin: '-80px' }}
        className="grid grid-cols-1 sm:grid-cols-2 gap-4"
      >
        {categories.map((cat) => (
          <motion.div
            key={cat.title}
            variants={itemVariants}
            className="group relative overflow-hidden border border-fd-border bg-fd-card transition-all hover:border-fd-border/80 hover:shadow-lg hover:shadow-black/5"
          >
            <div className="absolute inset-0 bg-fd-card z-0" />
            <div className="metal-shader absolute inset-0 opacity-50 z-0 pointer-events-none" />
            <div className="noise-overlay absolute inset-0 pointer-events-none opacity-50" />

            {/* Content */}
            <div className="relative z-10 p-6 lg:p-8">
              <div className={`flex items-center justify-center size-10 ${cat.iconBg} ${cat.iconColor} mb-4`}>
                <cat.icon className="size-5" />
              </div>
              <h3 className="font-semibold text-lg mb-3">{cat.title}</h3>
              <div className="flex flex-wrap gap-2">
                {cat.extensions.map((ext) => (
                  <span
                    key={ext}
                    className="inline-flex items-center border border-fd-border bg-fd-background px-2.5 py-1 text-xs font-mono transition-colors hover:bg-fd-accent"
                  >
                    {ext}
                  </span>
                ))}
              </div>
            </div>
          </motion.div>
        ))}
      </motion.div>

      <motion.div
        initial={{ opacity: 0 }}
        whileInView={{ opacity: 1 }}
        viewport={{ once: true }}
        transition={{ duration: 0.5, delay: 0.3 }}
        className="text-center mt-8"
      >
        <Link
          href="/docs/forge/extensions"
          className="inline-flex items-center gap-2 text-sm font-semibold text-fd-foreground hover:text-fd-foreground/80 transition-colors"
        >
          View All Extensions
          <ArrowRight className="size-4" />
        </Link>
      </motion.div>
    </section>
  );
}
