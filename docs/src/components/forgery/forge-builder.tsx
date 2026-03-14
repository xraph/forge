"use client";

import { useState, useMemo, useCallback } from "react";
import { Check, Copy, ChevronDown, ChevronUp } from "lucide-react";

// ─── Extension Definitions ─────────────────────────────────────────────────

interface ExtensionDef {
  id: string;
  name: string;
  description: string;
  category: "core" | "data" | "messaging" | "transport" | "realtime" | "platform" | "security" | "ai" | "forgery";
  importPath: string;
  setupCode: string;
  configCode?: string;
  /** Additional imports needed */
  extraImports?: string[];
}

const EXTENSIONS: ExtensionDef[] = [
  // ── Core (Built-in) ──
  {
    id: "auth",
    name: "Auth",
    description: "Pluggable authentication with JWT, API keys, OAuth2, OIDC",
    category: "core",
    importPath: "github.com/xraph/forge/extensions/auth",
    setupCode: `    app.RegisterExtension(auth.NewExtension(
        auth.WithEnabled(true),
    ))`,
  },
  {
    id: "database",
    name: "Database",
    description: "Database connections with Bun ORM and migrations",
    category: "data",
    importPath: "github.com/xraph/forge/extensions/database",
    setupCode: `    app.RegisterExtension(database.NewExtension(
        database.WithDriver("postgres"),
        database.WithDSN(os.Getenv("DATABASE_URL")),
    ))`,
    extraImports: ["os"],
  },
  {
    id: "cache",
    name: "Cache",
    description: "Multi-backend caching with Redis, Memcached, and in-memory",
    category: "data",
    importPath: "github.com/xraph/forge/extensions/cache",
    setupCode: `    app.RegisterExtension(cache.NewExtension(
        cache.WithProvider("redis"),
        cache.WithRedisURL(os.Getenv("REDIS_URL")),
    ))`,
    extraImports: ["os"],
  },
  {
    id: "search",
    name: "Search",
    description: "Full-text search with Elasticsearch, Meilisearch, Typesense",
    category: "data",
    importPath: "github.com/xraph/forge/extensions/search",
    setupCode: `    app.RegisterExtension(search.NewExtension(
        search.WithProvider("meilisearch"),
    ))`,
  },
  {
    id: "storage",
    name: "Storage",
    description: "Object storage with S3, GCS, and local filesystem",
    category: "data",
    importPath: "github.com/xraph/forge/extensions/storage",
    setupCode: `    app.RegisterExtension(storage.NewExtension(
        storage.WithProvider("s3"),
    ))`,
  },
  {
    id: "events",
    name: "Events",
    description: "Event-driven messaging with pub/sub",
    category: "messaging",
    importPath: "github.com/xraph/forge/extensions/events",
    setupCode: `    app.RegisterExtension(events.NewExtension())`,
  },
  {
    id: "queue",
    name: "Queue",
    description: "Background job queue with workers",
    category: "messaging",
    importPath: "github.com/xraph/forge/extensions/queue",
    setupCode: `    app.RegisterExtension(queue.NewExtension(
        queue.WithConcurrency(10),
    ))`,
  },
  {
    id: "kafka",
    name: "Kafka",
    description: "Apache Kafka producer/consumer integration",
    category: "messaging",
    importPath: "github.com/xraph/forge/extensions/kafka",
    setupCode: `    app.RegisterExtension(kafka.NewExtension(
        kafka.WithBrokers(os.Getenv("KAFKA_BROKERS")),
    ))`,
    extraImports: ["os"],
  },
  {
    id: "mqtt",
    name: "MQTT",
    description: "MQTT messaging for IoT and real-time",
    category: "messaging",
    importPath: "github.com/xraph/forge/extensions/mqtt",
    setupCode: `    app.RegisterExtension(mqtt.NewExtension(
        mqtt.WithBrokerURL(os.Getenv("MQTT_URL")),
    ))`,
    extraImports: ["os"],
  },
  {
    id: "grpc",
    name: "gRPC",
    description: "gRPC server and client support",
    category: "transport",
    importPath: "github.com/xraph/forge/extensions/grpc",
    setupCode: `    app.RegisterExtension(grpc.NewExtension(
        grpc.WithPort(50051),
    ))`,
  },
  {
    id: "graphql",
    name: "GraphQL",
    description: "GraphQL server with schema-first or code-first",
    category: "transport",
    importPath: "github.com/xraph/forge/extensions/graphql",
    setupCode: `    app.RegisterExtension(graphql.NewExtension())`,
  },
  {
    id: "orpc",
    name: "oRPC",
    description: "OpenAPI-native RPC framework",
    category: "transport",
    importPath: "github.com/xraph/forge/extensions/orpc",
    setupCode: `    app.RegisterExtension(orpc.NewExtension())`,
  },
  {
    id: "mcp",
    name: "MCP",
    description: "Model Context Protocol server",
    category: "transport",
    importPath: "github.com/xraph/forge/extensions/mcp",
    setupCode: `    app.RegisterExtension(mcp.NewExtension())`,
  },
  {
    id: "streaming",
    name: "Streaming",
    description: "SSE and WebSocket streaming",
    category: "realtime",
    importPath: "github.com/xraph/forge/extensions/streaming",
    setupCode: `    app.RegisterExtension(streaming.NewExtension())`,
  },
  {
    id: "webrtc",
    name: "WebRTC",
    description: "WebRTC signaling and media",
    category: "realtime",
    importPath: "github.com/xraph/forge/extensions/webrtc",
    setupCode: `    app.RegisterExtension(webrtc.NewExtension())`,
  },
  {
    id: "hls",
    name: "HLS",
    description: "HTTP Live Streaming for video",
    category: "realtime",
    importPath: "github.com/xraph/forge/extensions/hls",
    setupCode: `    app.RegisterExtension(hls.NewExtension())`,
  },
  {
    id: "gateway",
    name: "Gateway",
    description: "API gateway with routing and load balancing",
    category: "platform",
    importPath: "github.com/xraph/forge/extensions/gateway",
    setupCode: `    app.RegisterExtension(gateway.NewExtension())`,
  },
  {
    id: "discovery",
    name: "Discovery",
    description: "Service discovery with Consul, etcd, or mDNS",
    category: "platform",
    importPath: "github.com/xraph/forge/extensions/discovery",
    setupCode: `    app.RegisterExtension(discovery.NewExtension(
        discovery.WithProvider("consul"),
    ))`,
  },
  {
    id: "cron",
    name: "Cron",
    description: "Scheduled job execution",
    category: "platform",
    importPath: "github.com/xraph/forge/extensions/cron",
    setupCode: `    app.RegisterExtension(cron.NewExtension())`,
  },
  {
    id: "dashboard",
    name: "Dashboard",
    description: "Admin dashboard with micro-frontend contributors",
    category: "platform",
    importPath: "github.com/xraph/forge/extensions/dashboard",
    setupCode: `    app.RegisterExtension(dashboard.NewExtension())`,
  },
  {
    id: "consensus",
    name: "Consensus",
    description: "Distributed consensus with Raft",
    category: "platform",
    importPath: "github.com/xraph/forge/extensions/consensus",
    setupCode: `    app.RegisterExtension(consensus.NewExtension())`,
  },
  {
    id: "security",
    name: "Security",
    description: "CORS, CSRF, rate limiting, and security headers",
    category: "security",
    importPath: "github.com/xraph/forge/extensions/security",
    setupCode: `    app.RegisterExtension(security.NewExtension(
        security.WithCORS(true),
        security.WithCSRF(true),
        security.WithRateLimiting(true),
    ))`,
  },
  {
    id: "features",
    name: "Feature Flags",
    description: "Feature flag management with multiple providers",
    category: "security",
    importPath: "github.com/xraph/forge/extensions/features",
    setupCode: `    app.RegisterExtension(features.NewExtension(
        features.WithProvider("local"),
    ))`,
  },
  {
    id: "ai",
    name: "AI",
    description: "AI SDK integration for inference and agents",
    category: "ai",
    importPath: "github.com/xraph/forge/extensions/ai",
    setupCode: `    app.RegisterExtension(ai.NewExtension())`,
  },
  // ── Forgery Ecosystem ──
  {
    id: "authsome",
    name: "Authsome",
    description: "OAuth2, WebAuthn, MFA, sessions, and API keys",
    category: "forgery",
    importPath: "github.com/xraph/authsome",
    setupCode: `    app.RegisterExtension(authsome.NewForgeExtension(
        authsome.WithOAuth2(oauthConfig),
        authsome.WithSessionStore(sessionStore),
    ))`,
  },
  {
    id: "keysmith",
    name: "Keysmith",
    description: "API key lifecycle — issuance, rotation, and revocation",
    category: "forgery",
    importPath: "github.com/xraph/keysmith",
    setupCode: `    app.RegisterExtension(keysmith.NewForgeExtension(
        keysmith.WithStore(store),
        keysmith.WithPrefix("sk_live_"),
    ))`,
  },
  {
    id: "warden",
    name: "Warden",
    description: "RBAC, ABAC, and Zanzibar-style ReBAC permissions",
    category: "forgery",
    importPath: "github.com/xraph/warden",
    setupCode: `    app.RegisterExtension(warden.NewForgeExtension(
        warden.WithModel("rbac"),
        warden.WithStore(store),
    ))`,
  },
  {
    id: "chronicle",
    name: "Chronicle",
    description: "Immutable audit trail with hash chain",
    category: "forgery",
    importPath: "github.com/xraph/chronicle",
    setupCode: `    app.RegisterExtension(chronicle.NewForgeExtension(
        chronicle.WithAutoAudit(true),
    ))`,
  },
  {
    id: "relay-forgery",
    name: "Relay",
    description: "Webhook delivery with HMAC signing and retries",
    category: "forgery",
    importPath: "github.com/xraph/relay",
    setupCode: `    app.RegisterExtension(relay.NewForgeExtension(
        relay.WithStore(store),
        relay.WithMaxRetries(5),
    ))`,
  },
  {
    id: "herald",
    name: "Herald",
    description: "Multi-channel notifications — email, SMS, push",
    category: "forgery",
    importPath: "github.com/xraph/herald",
    setupCode: `    app.RegisterExtension(herald.NewForgeExtension(
        herald.WithEmailProvider("smtp", smtpConfig),
    ))`,
  },
  {
    id: "dispatch-forgery",
    name: "Dispatch",
    description: "Durable execution for background jobs and workflows",
    category: "forgery",
    importPath: "github.com/xraph/dispatch",
    setupCode: `    app.RegisterExtension(dispatch.NewForgeExtension(
        dispatch.WithStore(store),
        dispatch.WithWorkerCount(10),
    ))`,
  },
  {
    id: "grove",
    name: "Grove",
    description: "Polyglot Go ORM for PostgreSQL, SQLite, and more",
    category: "forgery",
    importPath: "github.com/xraph/grove",
    setupCode: `    app.RegisterExtension(grove.NewForgeExtension(
        grove.WithDSN(os.Getenv("DATABASE_URL")),
    ))`,
    extraImports: ["os"],
  },
  {
    id: "trove",
    name: "Trove",
    description: "Multi-tenant data isolation with row-level security",
    category: "forgery",
    importPath: "github.com/xraph/trove",
    setupCode: `    app.RegisterExtension(trove.NewForgeExtension(
        trove.WithStrategy("row-level"),
    ))`,
  },
  {
    id: "ledger",
    name: "Ledger",
    description: "Usage-based billing with metering and invoicing",
    category: "forgery",
    importPath: "github.com/xraph/ledger",
    setupCode: `    app.RegisterExtension(ledger.NewForgeExtension(
        ledger.WithStore(store),
    ))`,
  },
  {
    id: "ctrlplane",
    name: "Ctrl Plane",
    description: "Infrastructure control plane for multi-tenant instances",
    category: "forgery",
    importPath: "github.com/xraph/ctrlplane",
    setupCode: `    app.RegisterExtension(ctrlplane.NewForgeExtension(
        ctrlplane.WithStore(store),
    ))`,
  },
  {
    id: "cortex",
    name: "Cortex",
    description: "AI agent orchestration with skills and personas",
    category: "forgery",
    importPath: "github.com/xraph/cortex",
    setupCode: `    app.RegisterExtension(cortex.NewForgeExtension(
        cortex.WithDefaultModel("gpt-4"),
    ))`,
  },
  {
    id: "nexus",
    name: "Nexus",
    description: "AI gateway for routing and caching LLM traffic",
    category: "forgery",
    importPath: "github.com/xraph/nexus",
    setupCode: `    app.RegisterExtension(nexus.NewForgeExtension(
        nexus.WithRateLimiting(true),
    ))`,
  },
  {
    id: "sentinel",
    name: "Sentinel",
    description: "AI evaluation framework with 22 scorers",
    category: "forgery",
    importPath: "github.com/xraph/sentinel",
    setupCode: `    app.RegisterExtension(sentinel.NewForgeExtension(
        sentinel.WithDefaultScorers("relevance", "coherence"),
    ))`,
  },
  {
    id: "shield-forgery",
    name: "Shield",
    description: "Multi-layer AI safety and content filtering",
    category: "forgery",
    importPath: "github.com/xraph/shield",
    setupCode: `    app.RegisterExtension(shield.NewForgeExtension(
        shield.WithAllLayers(),
    ))`,
  },
  {
    id: "weave",
    name: "Weave",
    description: "RAG pipeline — chunk, embed, store, retrieve",
    category: "forgery",
    importPath: "github.com/xraph/weave",
    setupCode: `    app.RegisterExtension(weave.NewForgeExtension(
        weave.WithDefaultEmbedder("openai"),
    ))`,
  },
  {
    id: "vault-forgery",
    name: "Vault",
    description: "Secrets management with encryption at rest",
    category: "forgery",
    importPath: "github.com/xraph/vault",
    setupCode: `    app.RegisterExtension(vault.NewForgeExtension(
        vault.WithHotReload(true),
    ))`,
  },
];

// ─── Category metadata ──────────────────────────────────────────────────────

const CATEGORIES: {
  id: ExtensionDef["category"];
  label: string;
  color: string;
}[] = [
  { id: "data", label: "Data & Storage", color: "text-emerald-500 border-emerald-500/40 bg-emerald-500/10" },
  { id: "messaging", label: "Messaging", color: "text-sky-500 border-sky-500/40 bg-sky-500/10" },
  { id: "transport", label: "Transport & RPC", color: "text-blue-500 border-blue-500/40 bg-blue-500/10" },
  { id: "realtime", label: "Realtime & Media", color: "text-purple-500 border-purple-500/40 bg-purple-500/10" },
  { id: "platform", label: "Platform & Infra", color: "text-teal-500 border-teal-500/40 bg-teal-500/10" },
  { id: "security", label: "Security & Access", color: "text-amber-500 border-amber-500/40 bg-amber-500/10" },
  { id: "ai", label: "AI", color: "text-violet-500 border-violet-500/40 bg-violet-500/10" },
  { id: "forgery", label: "Forgery Ecosystem", color: "text-orange-500 border-orange-500/40 bg-orange-500/10" },
];

// ─── Code generation ────────────────────────────────────────────────────────

function generateCode(
  appName: string,
  selected: Set<string>,
): string {
  const selectedExts = EXTENSIONS.filter((e) => selected.has(e.id));
  if (selectedExts.length === 0) {
    return `package main

import (
    "context"
    "fmt"

    "github.com/xraph/forge"
)

func main() {
    app := forge.NewApp(forge.AppConfig{
        Name:    "${appName}",
        Version: "1.0.0",
    })

    ctx := context.Background()
    app.Start(ctx)
    defer app.Stop(ctx)

    // Add routes
    app.GET("/", func(ctx forge.Context) error {
        return ctx.JSON(200, map[string]string{"status": "ok"})
    })

    fmt.Println("listening on :8080")
    app.Listen(":8080")
}
`;
  }

  // Collect all imports
  const stdImports = new Set<string>(["context", "fmt"]);
  const forgeImports = new Set<string>(["github.com/xraph/forge"]);

  for (const ext of selectedExts) {
    forgeImports.add(ext.importPath);
    if (ext.extraImports) {
      for (const imp of ext.extraImports) {
        stdImports.add(imp);
      }
    }
  }

  // Build import block
  const stdBlock = Array.from(stdImports)
    .sort()
    .map((i) => `    "${i}"`)
    .join("\n");

  const extBlock = Array.from(forgeImports)
    .sort()
    .map((i) => {
      // Use alias for packages with path conflicts
      const parts = i.split("/");
      const pkgName = parts[parts.length - 1];
      // Check if this is a forgery package that might conflict with a built-in
      const hasDuplicate = Array.from(forgeImports).filter((other) => {
        const otherParts = other.split("/");
        return otherParts[otherParts.length - 1] === pkgName && other !== i;
      }).length > 0;

      if (hasDuplicate && !i.includes("forge/extensions")) {
        return `    forgery${pkgName} "${i}"`;
      }
      return `    "${i}"`;
    })
    .join("\n");

  // Build setup code
  const setupLines = selectedExts.map((ext) => ext.setupCode).join("\n\n");

  return `package main

import (
${stdBlock}

${extBlock}
)

func main() {
    app := forge.NewApp(forge.AppConfig{
        Name:    "${appName}",
        Version: "1.0.0",
    })

    // Register extensions
${setupLines}

    ctx := context.Background()
    app.Start(ctx)
    defer app.Stop(ctx)

    // Add routes
    app.GET("/", func(ctx forge.Context) error {
        return ctx.JSON(200, map[string]string{"status": "ok"})
    })

    fmt.Println("listening on :8080")
    app.Listen(":8080")
}
`;
}

// ─── Component ──────────────────────────────────────────────────────────────

export function ForgeBuilder() {
  const [appName, setAppName] = useState("my-app");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [copied, setCopied] = useState(false);
  const [expandedCategories, setExpandedCategories] = useState<Set<string>>(
    new Set(CATEGORIES.map((c) => c.id)),
  );

  const toggle = useCallback((id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  }, []);

  const toggleCategory = useCallback((catId: string) => {
    setExpandedCategories((prev) => {
      const next = new Set(prev);
      if (next.has(catId)) {
        next.delete(catId);
      } else {
        next.add(catId);
      }
      return next;
    });
  }, []);

  const selectAll = useCallback((catId: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      const catExts = EXTENSIONS.filter((e) => e.category === catId);
      const allSelected = catExts.every((e) => next.has(e.id));
      if (allSelected) {
        for (const e of catExts) next.delete(e.id);
      } else {
        for (const e of catExts) next.add(e.id);
      }
      return next;
    });
  }, []);

  const code = useMemo(
    () => generateCode(appName, selected),
    [appName, selected],
  );

  const handleCopy = useCallback(() => {
    navigator.clipboard.writeText(code);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }, [code]);

  return (
    <div className="grid lg:grid-cols-[380px_1fr] gap-6">
      {/* Left: Extension picker */}
      <div className="space-y-4">
        {/* App name input */}
        <div>
          <label
            htmlFor="app-name"
            className="block text-xs font-medium text-fd-muted-foreground mb-1.5"
          >
            App Name
          </label>
          <input
            id="app-name"
            type="text"
            value={appName}
            onChange={(e) => setAppName(e.target.value.replace(/[^a-zA-Z0-9-_]/g, ""))}
            className="w-full px-3 py-2 text-sm font-mono bg-fd-card border border-fd-border focus:border-fd-primary focus:outline-none"
            placeholder="my-app"
          />
        </div>

        <div className="text-xs text-fd-muted-foreground">
          {selected.size} extension{selected.size !== 1 ? "s" : ""} selected
        </div>

        {/* Extension categories */}
        <div className="space-y-2 max-h-[600px] overflow-y-auto pr-1">
          {CATEGORIES.map((cat) => {
            const catExts = EXTENSIONS.filter((e) => e.category === cat.id);
            const selectedCount = catExts.filter((e) =>
              selected.has(e.id),
            ).length;
            const isExpanded = expandedCategories.has(cat.id);

            return (
              <div
                key={cat.id}
                className="border border-fd-border bg-fd-card"
              >
                {/* Category header */}
                <div className="flex items-center justify-between px-3 py-2">
                  <button
                    type="button"
                    onClick={() => toggleCategory(cat.id)}
                    className="flex items-center gap-2 text-sm font-medium flex-1 text-left"
                  >
                    {isExpanded ? (
                      <ChevronUp className="size-3.5 text-fd-muted-foreground" />
                    ) : (
                      <ChevronDown className="size-3.5 text-fd-muted-foreground" />
                    )}
                    <span className={`text-[10px] font-semibold uppercase tracking-wider px-2 py-0.5 border ${cat.color}`}>
                      {cat.label}
                    </span>
                  </button>
                  <button
                    type="button"
                    onClick={() => selectAll(cat.id)}
                    className="text-[10px] text-fd-muted-foreground hover:text-fd-foreground transition-colors"
                  >
                    {selectedCount === catExts.length ? "None" : "All"}
                  </button>
                </div>

                {/* Extension list */}
                {isExpanded && (
                  <div className="border-t border-fd-border">
                    {catExts.map((ext) => (
                      <label
                        key={ext.id}
                        className="flex items-start gap-3 px-3 py-2 cursor-pointer hover:bg-fd-accent/50 transition-colors"
                      >
                        <input
                          type="checkbox"
                          checked={selected.has(ext.id)}
                          onChange={() => toggle(ext.id)}
                          className="mt-0.5 accent-fd-primary"
                        />
                        <div className="min-w-0">
                          <div className="text-sm font-medium">
                            {ext.name}
                          </div>
                          <div className="text-xs text-fd-muted-foreground truncate">
                            {ext.description}
                          </div>
                        </div>
                      </label>
                    ))}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </div>

      {/* Right: Generated code */}
      <div className="relative">
        <div className="sticky top-4">
          <div className="flex items-center justify-between mb-2">
            <span className="text-xs font-mono text-fd-muted-foreground">
              main.go
            </span>
            <button
              type="button"
              onClick={handleCopy}
              className="flex items-center gap-1.5 text-xs text-fd-muted-foreground hover:text-fd-foreground border border-fd-border px-2.5 py-1 transition-colors"
            >
              {copied ? (
                <>
                  <Check className="size-3" />
                  Copied
                </>
              ) : (
                <>
                  <Copy className="size-3" />
                  Copy
                </>
              )}
            </button>
          </div>
          <div className="border border-fd-border bg-fd-card overflow-hidden">
            <pre className="p-4 text-sm font-mono overflow-x-auto max-h-[700px] overflow-y-auto leading-relaxed">
              <code>{code}</code>
            </pre>
          </div>
          <div className="mt-3 text-xs text-fd-muted-foreground">
            Save as <code className="px-1 py-0.5 bg-fd-muted">main.go</code> then
            run <code className="px-1 py-0.5 bg-fd-muted">go mod init {appName} && go mod tidy && go run .</code>
          </div>
        </div>
      </div>
    </div>
  );
}
