import Link from 'next/link';
import {
  Blocks,
  Box,
  ChevronRight,
  Code2,
  Eye,
  FileCode2,
  Hammer,
  Network,
  Terminal,
} from 'lucide-react';

const features = [
  {
    icon: Blocks,
    title: 'Modular Extensions',
    description:
      '28+ production-ready extensions for databases, caching, messaging, gRPC, GraphQL, and more. Plug in what you need.',
  },
  {
    icon: Box,
    title: 'Type-Safe DI',
    description:
      'Vessel container with Go generics, constructor injection, scoped services, and lazy dependencies.',
  },
  {
    icon: Network,
    title: 'Multi-Protocol',
    description:
      'HTTP, gRPC, GraphQL, WebSocket, SSE, and WebTransport out of the box with unified routing.',
  },
  {
    icon: FileCode2,
    title: 'Auto-Generated Schemas',
    description:
      'OpenAPI 3.1 and AsyncAPI 3.0 generated automatically from your handler signatures and struct tags.',
  },
  {
    icon: Eye,
    title: 'Enterprise Observability',
    description:
      'Built-in metrics, health checks, distributed tracing with OpenTelemetry, and structured logging.',
  },
  {
    icon: Terminal,
    title: 'Powerful CLI',
    description:
      'Dev server with hot reload, multi-platform builds, database migrations, code generation, and deployment.',
  },
];

const extensionCategories = [
  {
    name: 'Data & Storage',
    items: ['Database', 'Cache', 'Storage', 'Search'],
  },
  {
    name: 'Transport',
    items: ['gRPC', 'GraphQL', 'oRPC', 'MCP'],
  },
  {
    name: 'Real-Time',
    items: ['Streaming', 'WebRTC', 'HLS'],
  },
  {
    name: 'Events',
    items: ['Events', 'Queue', 'Kafka', 'MQTT'],
  },
  {
    name: 'Infrastructure',
    items: ['Gateway', 'Discovery', 'Consensus', 'Cron', 'Dashboard'],
  },
  {
    name: 'Security',
    items: ['Auth', 'Security', 'Features'],
  },
];

export default function HomePage() {
  return (
    <main className="flex flex-col items-center">
      {/* Hero */}
      <section className="relative flex flex-col items-center justify-center text-center px-6 pt-20 pb-16 overflow-hidden w-full">
        <div className="absolute inset-0 -z-10 bg-[radial-gradient(ellipse_80%_60%_at_50%_-20%,rgba(245,158,11,0.15),transparent)]" />
        <div className="inline-flex items-center gap-2 rounded-full border border-amber-500/30 bg-amber-500/10 px-4 py-1.5 text-sm text-amber-600 dark:text-amber-400 mb-8">
          <Hammer className="size-3.5" />
          the production-grade Go framework you need.
        </div>
        <h1 className="max-w-3xl text-4xl font-bold tracking-tight sm:text-5xl lg:text-6xl">
          Build production-grade Go services,{' '}
          <span className="text-transparent bg-clip-text bg-gradient-to-r from-amber-500 to-orange-600">
            your way.
          </span>
        </h1>
        <p className="mt-6 max-w-2xl text-lg text-fd-muted-foreground">
          A modular, extensible framework for building backend services in Go.
          Type-safe dependency injection, auto-generated API schemas, and 28+
          production-ready extensions.
        </p>
        <div className="mt-8 flex flex-wrap items-center justify-center gap-4">
          <Link
            href="/docs/forge"
            className="inline-flex items-center gap-2 rounded-lg bg-gradient-to-r from-amber-500 to-orange-600 px-6 py-3 text-sm font-medium text-white shadow-lg shadow-amber-500/25 transition-all hover:shadow-amber-500/40 hover:brightness-110"
          >
            Get Started
            <ChevronRight className="size-4" />
          </Link>
          <a
            href="https://github.com/xraph/forge"
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center gap-2 rounded-lg border border-fd-border bg-fd-background px-6 py-3 text-sm font-medium transition-colors hover:bg-fd-accent"
          >
            <svg className="size-4" fill="currentColor" viewBox="0 0 24 24">
              <path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12" />
            </svg>
            View on GitHub
          </a>
        </div>
      </section>

      {/* Code Preview */}
      <section className="w-full max-w-4xl px-6 pb-16">
        <div className="overflow-hidden rounded-xl border border-fd-border bg-fd-card">
          <div className="flex items-center gap-2 border-b border-fd-border px-4 py-3">
            <Code2 className="size-4 text-fd-muted-foreground" />
            <span className="text-sm text-fd-muted-foreground">main.go</span>
          </div>
          <pre className="overflow-x-auto p-4 text-sm leading-relaxed">
            <code>{`package main

import "github.com/xraph/forge"

func main() {
    app := forge.New(
        forge.WithAppName("my-service"),
        forge.WithAppVersion("1.0.0"),
    )

    app.Router().GET("/hello", func(ctx forge.Context) error {
        return ctx.JSON(200, forge.Map{"message": "Hello, World!"})
    })

    app.Run()
}`}</code>
          </pre>
        </div>
      </section>

      {/* Features */}
      <section className="w-full max-w-6xl px-6 pb-20">
        <div className="text-center mb-12">
          <h2 className="text-3xl font-bold tracking-tight">
            Everything you need to build at scale
          </h2>
          <p className="mt-3 text-fd-muted-foreground">
            A complete toolkit for production Go services, from routing to
            observability.
          </p>
        </div>
        <div className="grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
          {features.map((feature) => (
            <div
              key={feature.title}
              className="group rounded-xl border border-fd-border bg-fd-card p-6 transition-colors hover:border-amber-500/50 hover:bg-fd-accent/50"
            >
              <feature.icon className="size-8 text-amber-500 mb-4" />
              <h3 className="font-semibold text-lg mb-2">{feature.title}</h3>
              <p className="text-sm text-fd-muted-foreground leading-relaxed">
                {feature.description}
              </p>
            </div>
          ))}
        </div>
      </section>

      {/* Extensions Showcase */}
      <section className="w-full max-w-6xl px-6 pb-20">
        <div className="text-center mb-12">
          <h2 className="text-3xl font-bold tracking-tight">
            28+ Production-Ready Extensions
          </h2>
          <p className="mt-3 text-fd-muted-foreground">
            Plug in exactly what you need. Every extension follows the same
            lifecycle, config, and health check patterns.
          </p>
        </div>
        <div className="grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
          {extensionCategories.map((category) => (
            <div
              key={category.name}
              className="rounded-xl border border-fd-border bg-fd-card p-6"
            >
              <h3 className="font-semibold mb-3 text-amber-600 dark:text-amber-400">
                {category.name}
              </h3>
              <div className="flex flex-wrap gap-2">
                {category.items.map((item) => (
                  <span
                    key={item}
                    className="inline-flex items-center rounded-md border border-fd-border bg-fd-background px-2.5 py-1 text-xs font-medium"
                  >
                    {item}
                  </span>
                ))}
              </div>
            </div>
          ))}
        </div>
      </section>

      {/* CTA */}
      <section className="w-full max-w-4xl px-6 pb-20">
        <div className="relative overflow-hidden rounded-2xl border border-amber-500/20 bg-gradient-to-br from-amber-500/10 via-orange-500/5 to-transparent p-12 text-center">
          <h2 className="text-3xl font-bold tracking-tight mb-4">
            Ready to build?
          </h2>
          <p className="text-fd-muted-foreground mb-8 max-w-lg mx-auto">
            Get started with Forge in minutes. Follow the quick start guide to
            create your first production-ready service.
          </p>
          <Link
            href="/docs/forge/quick-start"
            className="inline-flex items-center gap-2 rounded-lg bg-gradient-to-r from-amber-500 to-orange-600 px-8 py-3 text-sm font-medium text-white shadow-lg shadow-amber-500/25 transition-all hover:shadow-amber-500/40 hover:brightness-110"
          >
            Quick Start Guide
            <ChevronRight className="size-4" />
          </Link>
        </div>
      </section>
    </main>
  );
}
