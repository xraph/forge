'use client';

import { motion } from 'framer-motion';
import {
  Blocks,
  Box,
  Eye,
  FileCode2,
  Network,
  Terminal,
  ArrowRight,
  Zap,
  DatabaseIcon,
  MessageSquare,
  Radio,
  Shield,
  Activity,
  HeartPulse,
  GitBranch,
  Layers,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import type { ReactNode } from 'react';
import { cn } from '@/lib/cn';
import { Card, CardContent, CardHeader } from '@/components/ui/card';
import { SectionHeader } from './section-header';

/* ─── Corner decorators (from features-ui pattern) ─── */
const CardDecorator = () => (
  <>
    <span className="border-primary absolute -left-px -top-px block size-2 border-l-2 border-t-2" />
    <span className="border-primary absolute -right-px -top-px block size-2 border-r-2 border-t-2" />
    <span className="border-primary absolute -bottom-px -left-px block size-2 border-b-2 border-l-2" />
    <span className="border-primary absolute -bottom-px -right-px block size-2 border-b-2 border-r-2" />
  </>
);

const FeatureCard = ({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) => (
  <Card
    className={cn(
      'group relative rounded-none shadow-zinc-950/5 border-fd-border bg-fd-card overflow-hidden',
      className,
    )}
  >
    <CardDecorator />
    {children}
  </Card>
);

interface CardHeadingProps {
  icon: LucideIcon;
  title: string;
  description: string;
}

const CardHeading = ({ icon: Icon, title, description }: CardHeadingProps) => (
  <div className="p-6">
    <span className="text-fd-muted-foreground flex items-center gap-2 text-sm">
      <Icon className="size-4" />
      {title}
    </span>
    <p className="mt-6 text-2xl font-semibold leading-snug">{description}</p>
  </div>
);

/* ─── Mini code snippet (themed) ─── */
function MiniCode({
  lines,
  className,
}: {
  lines: { text: string; indent?: number; color?: string }[];
  className?: string;
}) {
  return (
    <pre
      className={cn(
        'overflow-x-auto text-[12px] leading-relaxed font-mono text-zinc-400 select-none',
        className,
      )}
    >
      {lines.map((l, i) => (
        <div key={i} style={{ paddingLeft: (l.indent ?? 0) * 16 }}>
          <span className={l.color ?? 'text-zinc-400'}>{l.text}</span>
        </div>
      ))}
    </pre>
  );
}

/* ─── Protocol pill ─── */
function ProtocolPill({
  label,
  active,
}: {
  label: string;
  active?: boolean;
}) {
  return (
    <span
      className={cn(
        'inline-flex items-center border px-2.5 py-1 text-xs font-mono tracking-wide transition-colors',
        active
          ? 'border-blue-500/40 bg-blue-500/10 text-blue-400'
          : 'border-fd-border bg-fd-background text-fd-muted-foreground',
      )}
    >
      {label}
    </span>
  );
}

/* ─── Extension chip grid ─── */
const extensionChips = [
  { label: 'PostgreSQL', icon: DatabaseIcon },
  { label: 'Redis', icon: Zap },
  { label: 'gRPC', icon: Radio },
  { label: 'GraphQL', icon: Network },
  { label: 'Kafka', icon: MessageSquare },
  { label: 'Auth', icon: Shield },
];

/* ─── Observability metric ─── */
function MetricCard({
  label,
  value,
  sub,
}: {
  label: string;
  value: string;
  sub: string;
}) {
  return (
    <div className="border border-fd-border bg-fd-background/50 p-3">
      <div className="text-[10px] uppercase tracking-wider text-fd-muted-foreground">
        {label}
      </div>
      <div className="mt-1 text-lg font-bold tabular-nums">{value}</div>
      <div className="text-xs text-fd-muted-foreground">{sub}</div>
    </div>
  );
}

/* ─── Animation variants ─── */
const containerVariants = {
  hidden: {},
  visible: {
    transition: { staggerChildren: 0.08 },
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

/* ═══════════════════════════════════════════
   Main component
   ═══════════════════════════════════════════ */

export function FeatureBento() {
  return (
    <section className="container max-w-(--fd-layout-width) mx-auto px-6 py-16 md:py-24">
      <SectionHeader
        title="Everything you need to build at scale"
        description="A complete toolkit for production Go services, from routing to observability."
        className="text-left"
        leftAlign={true}
      />
      <motion.div
        variants={containerVariants}
        initial="hidden"
        whileInView="visible"
        viewport={{ once: true, margin: '-80px' }}
        className="grid gap-4 lg:grid-cols-2"
      >
        {/* ── Card 1: Modular Extensions ── */}
        <motion.div variants={itemVariants}>
          <FeatureCard>
            <CardHeader className="pb-3">
              <CardHeading
                icon={Blocks}
                title="Modular Extensions"
                description="28+ production-ready extensions. Plug in only what you need."
              />
            </CardHeader>

            <div className="relative border-t border-dashed border-fd-border">
              <div className="absolute inset-0 [background:radial-gradient(125%_125%_at_50%_0%,transparent_40%,hsl(var(--muted)),transparent_125%)]" />
              <div className="relative grid grid-cols-3 gap-2 p-6">
                {extensionChips.map((ext) => (
                  <div
                    key={ext.label}
                    className="flex items-center gap-2 border border-fd-border bg-fd-background/60 px-3 py-2.5 text-xs font-medium transition-colors hover:border-fd-border/50 hover:bg-fd-accent/30"
                  >
                    <ext.icon className="size-3.5 text-fd-muted-foreground flex-shrink-0" />
                    <span>{ext.label}</span>
                  </div>
                ))}
              </div>
              <div className="px-6 pb-6">
                <MiniCode
                  lines={[
                    { text: '// Register any extension with one line', color: 'text-zinc-600' },
                    { text: 'app.Use(postgres.Extension())', color: 'text-zinc-300' },
                    { text: 'app.Use(redis.Extension())', color: 'text-zinc-300' },
                    { text: 'app.Use(grpc.Extension())', color: 'text-zinc-300' },
                  ]}
                />
              </div>
            </div>
          </FeatureCard>
        </motion.div>

        {/* ── Card 2: Multi-Protocol ── */}
        <motion.div variants={itemVariants}>
          <FeatureCard>
            <CardHeader className="pb-3">
              <CardHeading
                icon={Network}
                title="Multi-Protocol"
                description="Unified routing across every protocol, from REST to WebTransport."
              />
            </CardHeader>

            <CardContent>
              <div className="relative">
                <div className="absolute -inset-6 [background:radial-gradient(50%_50%_at_75%_50%,transparent,hsl(var(--background))_100%)]" />
                <div className="relative space-y-4">
                  <div className="flex flex-wrap gap-2">
                    {['HTTP', 'gRPC', 'GraphQL', 'WebSocket', 'SSE', 'WebTransport'].map(
                      (p, i) => (
                        <ProtocolPill key={p} label={p} active={i < 3} />
                      ),
                    )}
                  </div>
                  <div className="border border-fd-border bg-zinc-950/60 p-4">
                    <MiniCode
                      lines={[
                        { text: '// One router, every protocol', color: 'text-zinc-600' },
                        { text: 'app.Router().GET("/api/users", listUsers)', color: 'text-zinc-300' },
                        { text: 'app.Router().POST("/api/users", createUser)', color: 'text-zinc-300' },
                        { text: '', color: 'text-zinc-600' },
                        { text: '// gRPC auto-registered from proto', color: 'text-zinc-600' },
                        { text: 'app.Use(grpc.Extension())', color: 'text-zinc-300' },
                        { text: '', color: 'text-zinc-600' },
                        { text: '// GraphQL from schema', color: 'text-zinc-600' },
                        { text: 'app.Use(graphql.Extension())', color: 'text-zinc-300' },
                      ]}
                    />
                  </div>
                </div>
              </div>
            </CardContent>
          </FeatureCard>
        </motion.div>

        {/* ── Card 3: Type-Safe DI ── */}
        <motion.div variants={itemVariants}>
          <FeatureCard>
            <CardHeader className="pb-3">
              <CardHeading
                icon={Box}
                title="Type-Safe DI"
                description="Go generics-powered container with compile-time safety."
              />
            </CardHeader>

            <div className="relative border-t border-dashed border-fd-border">
              <div className="absolute inset-0 [background:radial-gradient(125%_125%_at_50%_100%,transparent_40%,hsl(var(--muted)),transparent_125%)]" />
              <div className="relative p-6 space-y-4">
                <div className="grid grid-cols-3 gap-3">
                  {[
                    { icon: GitBranch, label: 'Constructor Injection', desc: 'Auto-resolve deps' },
                    { icon: Layers, label: 'Scoped Lifetimes', desc: 'Singleton, transient' },
                    { icon: Shield, label: 'Circular Detection', desc: 'Compile-time checks' },
                  ].map((item) => (
                    <div key={item.label} className="text-center space-y-1.5">
                      <div className="mx-auto flex items-center justify-center size-8 border border-fd-border bg-fd-background">
                        <item.icon className="size-3.5 text-fd-muted-foreground" />
                      </div>
                      <div className="text-xs font-medium leading-tight">{item.label}</div>
                      <div className="text-[10px] text-fd-muted-foreground">{item.desc}</div>
                    </div>
                  ))}
                </div>
                <div className="border border-fd-border bg-zinc-950/60 p-4">
                  <MiniCode
                    lines={[
                      { text: '// Type-safe with Go generics', color: 'text-zinc-600' },
                      { text: 'vessel.Register[UserService](c, NewUserService)', color: 'text-zinc-300' },
                      { text: 'vessel.Register[OrderService](c, NewOrderService)', color: 'text-zinc-300' },
                      { text: '', color: 'text-zinc-600' },
                      { text: '// Auto-resolve entire dependency graph', color: 'text-zinc-600' },
                      { text: 'svc := vessel.Resolve[UserService](c)', color: 'text-zinc-300' },
                    ]}
                  />
                </div>
              </div>
            </div>
          </FeatureCard>
        </motion.div>

        {/* ── Card 4: Auto-Generated Schemas ── */}
        <motion.div variants={itemVariants}>
          <FeatureCard>
            <CardHeader className="pb-3">
              <CardHeading
                icon={FileCode2}
                title="Auto-Generated Schemas"
                description="OpenAPI 3.1 & AsyncAPI 3.0 from your handler signatures."
              />
            </CardHeader>

            <CardContent>
              <div className="relative">
                <div className="absolute -inset-6 [background:radial-gradient(50%_50%_at_25%_50%,transparent,hsl(var(--background))_100%)]" />
                <div className="relative space-y-3">
                  <div className="flex gap-2">
                    <ProtocolPill label="OpenAPI 3.1" active />
                    <ProtocolPill label="AsyncAPI 3.0" active />
                    <ProtocolPill label="JSON Schema" />
                  </div>
                  <div className="border border-fd-border bg-zinc-950/60 p-4">
                    <MiniCode
                      lines={[
                        { text: '// Schemas generated at build time', color: 'text-zinc-600' },
                        { text: 'type CreateUserInput struct {', color: 'text-zinc-300' },
                        { text: '  Name  string `json:"name"  validate:"required"`', color: 'text-zinc-300', indent: 1 },
                        { text: '  Email string `json:"email" validate:"email"`', color: 'text-zinc-300', indent: 1 },
                        { text: '}', color: 'text-zinc-300' },
                        { text: '', color: 'text-zinc-600' },
                        { text: '// → OpenAPI spec auto-generated ✓', color: 'text-emerald-500/70' },
                      ]}
                    />
                  </div>
                  <p className="text-xs text-fd-muted-foreground leading-relaxed">
                    No manual spec writing. Forge inspects your Go types and handler
                    signatures to produce standards-compliant schemas with validation
                    rules, examples, and documentation included.
                  </p>
                </div>
              </div>
            </CardContent>
          </FeatureCard>
        </motion.div>

        {/* ── Card 5: Full-width — Observability ── */}
        <motion.div variants={itemVariants} className="lg:col-span-2">
          <FeatureCard className="p-0">
            <div className="grid lg:grid-cols-[1fr_1fr] gap-0">
              <div>
                <CardHeader className="pb-3">
                  <CardHeading
                    icon={Eye}
                    title="Enterprise Observability"
                    description="Metrics, tracing, health checks — all built in."
                  />
                </CardHeader>
                <div className="px-6 pb-6 space-y-3">
                  <div className="grid grid-cols-2 gap-2">
                    {[
                      { icon: Activity, label: 'OpenTelemetry', desc: 'Distributed traces' },
                      { icon: HeartPulse, label: 'Health Checks', desc: 'Liveness & readiness' },
                      { icon: Eye, label: 'Prometheus', desc: 'Metrics export' },
                      { icon: Terminal, label: 'Structured Logs', desc: 'JSON / logfmt' },
                    ].map((item) => (
                      <div
                        key={item.label}
                        className="flex items-start gap-2.5 p-2.5 transition-colors hover:bg-fd-accent/30"
                      >
                        <item.icon className="size-3.5 text-fd-muted-foreground mt-0.5 flex-shrink-0" />
                        <div>
                          <div className="text-sm font-medium leading-tight">{item.label}</div>
                          <div className="text-xs text-fd-muted-foreground mt-0.5">{item.desc}</div>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              </div>

              <div className="border-t lg:border-t-0 lg:border-l border-fd-border bg-zinc-950/40 p-6 flex flex-col justify-center">
                <div className="grid grid-cols-3 gap-3">
                  <MetricCard label="Uptime" value="99.97%" sub="last 30 days" />
                  <MetricCard label="P95 Latency" value="7.8ms" sub="GET /api/*" />
                  <MetricCard label="Throughput" value="12.4k" sub="req/sec" />
                </div>
                <div className="mt-4 border border-fd-border bg-zinc-950/60 p-4">
                  <MiniCode
                    lines={[
                      { text: '// Zero-config observability', color: 'text-zinc-600' },
                      { text: 'app.Use(otel.Extension())', color: 'text-zinc-300' },
                      { text: 'app.Use(prometheus.Extension())', color: 'text-zinc-300' },
                      { text: 'app.Use(healthcheck.Extension())', color: 'text-zinc-300' },
                    ]}
                  />
                </div>
              </div>
            </div>
          </FeatureCard>
        </motion.div>
      </motion.div>
    </section>
  );
}
