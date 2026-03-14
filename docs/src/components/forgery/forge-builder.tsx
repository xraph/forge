"use client";

import { useState, useMemo, useCallback, type ReactNode } from "react";
import {
  Check,
  Copy,
  ChevronDown,
  ChevronUp,
  Download,
  FileCode,
  FileText,
  Settings,
  GitBranch,
} from "lucide-react";

// ─── Minimal ZIP Creator ────────────────────────────────────────────────────

function createZip(files: { name: string; content: string }[]): Blob {
  const encoder = new TextEncoder();
  const entries: { name: Uint8Array; data: Uint8Array; offset: number }[] = [];
  const parts: Uint8Array[] = [];
  let offset = 0;

  for (const file of files) {
    const name = encoder.encode(file.name);
    const data = encoder.encode(file.content);

    // Local file header (30 bytes + name + data)
    const header = new ArrayBuffer(30);
    const hv = new DataView(header);
    hv.setUint32(0, 0x04034b50, true); // signature
    hv.setUint16(4, 20, true); // version needed
    hv.setUint16(6, 0, true); // flags
    hv.setUint16(8, 0, true); // compression (store)
    hv.setUint16(10, 0, true); // mod time
    hv.setUint16(12, 0, true); // mod date
    hv.setUint32(14, crc32(data), true); // crc-32
    hv.setUint32(18, data.length, true); // compressed size
    hv.setUint32(22, data.length, true); // uncompressed size
    hv.setUint16(26, name.length, true); // name length
    hv.setUint16(28, 0, true); // extra length

    const headerBytes = new Uint8Array(header);
    entries.push({ name, data, offset });
    parts.push(headerBytes, name, data);
    offset += 30 + name.length + data.length;
  }

  // Central directory
  const cdStart = offset;
  for (const entry of entries) {
    const cd = new ArrayBuffer(46);
    const cv = new DataView(cd);
    cv.setUint32(0, 0x02014b50, true); // signature
    cv.setUint16(4, 20, true); // version made by
    cv.setUint16(6, 20, true); // version needed
    cv.setUint16(8, 0, true); // flags
    cv.setUint16(10, 0, true); // compression
    cv.setUint16(12, 0, true); // mod time
    cv.setUint16(14, 0, true); // mod date
    cv.setUint32(16, crc32(entry.data), true);
    cv.setUint32(20, entry.data.length, true);
    cv.setUint32(24, entry.data.length, true);
    cv.setUint16(28, entry.name.length, true);
    cv.setUint16(30, 0, true); // extra
    cv.setUint16(32, 0, true); // comment
    cv.setUint16(34, 0, true); // disk
    cv.setUint16(36, 0, true); // internal attrs
    cv.setUint32(38, 0, true); // external attrs
    cv.setUint32(42, entry.offset, true);

    parts.push(new Uint8Array(cd), entry.name);
    offset += 46 + entry.name.length;
  }

  // End of central directory
  const eocd = new ArrayBuffer(22);
  const ev = new DataView(eocd);
  ev.setUint32(0, 0x06054b50, true);
  ev.setUint16(4, 0, true);
  ev.setUint16(6, 0, true);
  ev.setUint16(8, entries.length, true);
  ev.setUint16(10, entries.length, true);
  ev.setUint32(12, offset - cdStart, true);
  ev.setUint32(16, cdStart, true);
  ev.setUint16(20, 0, true);
  parts.push(new Uint8Array(eocd));

  return new Blob(parts as BlobPart[], { type: "application/zip" });
}

function crc32(data: Uint8Array): number {
  let crc = 0xffffffff;
  for (let i = 0; i < data.length; i++) {
    crc ^= data[i];
    for (let j = 0; j < 8; j++) {
      crc = crc & 1 ? (crc >>> 1) ^ 0xedb88320 : crc >>> 1;
    }
  }
  return (crc ^ 0xffffffff) >>> 0;
}

// ─── Syntax Highlighting ────────────────────────────────────────────────────

const TC: Record<string, string> = {
  kw: "text-purple-400",
  str: "text-emerald-400",
  cmt: "text-zinc-500 italic",
  num: "text-amber-400",
  fn: "text-blue-400",
  typ: "text-cyan-400",
  blt: "text-orange-400",
  pn: "text-zinc-400",
  pl: "text-zinc-200",
  yk: "text-blue-400",
  env: "text-orange-400",
};

function highlightGo(code: string): ReactNode[] {
  return code.split("\n").map((line, i, arr) => (
    <span key={i}>
      {goLine(line)}
      {i < arr.length - 1 ? "\n" : ""}
    </span>
  ));
}

function goLine(line: string): ReactNode[] {
  const nodes: ReactNode[] = [];
  const trimmed = line.trimStart();

  if (trimmed.startsWith("//")) {
    const ci = line.indexOf("//");
    if (ci > 0) nodes.push(line.slice(0, ci));
    nodes.push(<span key="c" className={TC.cmt}>{line.slice(ci)}</span>);
    return nodes;
  }

  type M = { s: number; e: number; t: string; x: string };
  const ms: M[] = [];

  const scan = (re: RegExp, t: string, g = 0) => {
    re.lastIndex = 0;
    let m: RegExpExecArray | null;
    while ((m = re.exec(line)) !== null) {
      const x = g > 0 ? m[g] : m[0];
      const s = g > 0 ? m.index + m[0].indexOf(x) : m.index;
      ms.push({ s, e: s + x.length, t, x });
    }
  };

  scan(/"(?:[^"\\]|\\.)*"/g, "str");
  scan(/\b(package|import|func|return|if|else|for|range|var|const|type|struct|interface|map|defer|go|nil|true|false|err)\b/g, "kw");
  scan(/\b(string|int|int64|bool|error|any|byte)\b/g, "typ");
  scan(/\b(make|len|append|log\.Fatal)\b/g, "blt");
  scan(/\b\d+\b/g, "num");
  scan(/\.([A-Z]\w*)\s*\(/g, "fn", 1);

  ms.sort((a, b) => a.s - b.s || b.e - a.e);
  const filtered: M[] = [];
  let last = 0;
  for (const m of ms) {
    if (m.s >= last) {
      filtered.push(m);
      last = m.e;
    }
  }

  let pos = 0;
  for (const m of filtered) {
    if (m.s > pos) nodes.push(line.slice(pos, m.s));
    nodes.push(<span key={`${m.s}${m.t}`} className={TC[m.t]}>{m.x}</span>);
    pos = m.e;
  }
  if (pos < line.length) nodes.push(line.slice(pos));
  return nodes;
}

function highlightYaml(code: string): ReactNode[] {
  return code.split("\n").map((line, i, arr) => (
    <span key={i}>
      {yamlLine(line)}
      {i < arr.length - 1 ? "\n" : ""}
    </span>
  ));
}

function yamlLine(line: string): ReactNode[] {
  const nodes: ReactNode[] = [];
  const trimmed = line.trimStart();

  if (trimmed.startsWith("#")) {
    const ci = line.indexOf("#");
    if (ci > 0) nodes.push(line.slice(0, ci));
    nodes.push(<span key="c" className={TC.cmt}>{line.slice(ci)}</span>);
    return nodes;
  }

  if (trimmed.startsWith("- ")) {
    const indent = line.slice(0, line.length - trimmed.length);
    nodes.push(indent);
    nodes.push(<span key="d" className={TC.pn}>-</span>);
    nodes.push(...yamlVal(trimmed.slice(1), "v"));
    return nodes;
  }

  const ci = trimmed.indexOf(":");
  if (ci > 0) {
    const indent = line.slice(0, line.length - trimmed.length);
    nodes.push(indent);
    nodes.push(<span key="k" className={TC.yk}>{trimmed.slice(0, ci)}</span>);
    nodes.push(<span key=":" className={TC.pn}>:</span>);
    nodes.push(...yamlVal(trimmed.slice(ci + 1), "v"));
    return nodes;
  }

  nodes.push(line);
  return nodes;
}

function yamlVal(v: string, p: string): ReactNode[] {
  const nodes: ReactNode[] = [];
  if (!v.trim()) { nodes.push(v); return nodes; }

  const parts = v.split(/(\$\{[^}]+\})/g);
  for (let j = 0; j < parts.length; j++) {
    const part = parts[j];
    if (part.startsWith("${")) {
      nodes.push(<span key={`${p}e${j}`} className={TC.env}>{part}</span>);
    } else if (/^\s*".*"$/.test(part.trim())) {
      const lead = part.slice(0, part.length - part.trimStart().length);
      nodes.push(lead);
      nodes.push(<span key={`${p}s${j}`} className={TC.str}>{part.trim()}</span>);
    } else if (/^\s*\d+(\.\d+)?$/.test(part.trim()) && part.trim()) {
      const lead = part.slice(0, part.length - part.trimStart().length);
      nodes.push(lead);
      nodes.push(<span key={`${p}n${j}`} className={TC.num}>{part.trim()}</span>);
    } else if (/^\s*(true|false)$/.test(part.trim())) {
      const lead = part.slice(0, part.length - part.trimStart().length);
      nodes.push(lead);
      nodes.push(<span key={`${p}b${j}`} className={TC.kw}>{part.trim()}</span>);
    } else if (part.trim().startsWith('"')) {
      const lead = part.slice(0, part.length - part.trimStart().length);
      nodes.push(lead);
      nodes.push(<span key={`${p}q${j}`} className={TC.str}>{part.trim()}</span>);
    } else {
      nodes.push(<span key={`${p}p${j}`} className={TC.pl}>{part}</span>);
    }
  }
  return nodes;
}

function highlightGitignore(code: string): ReactNode[] {
  return code.split("\n").map((line, i, arr) => {
    const trimmed = line.trimStart();
    const isComment = trimmed.startsWith("#");
    return (
      <span key={i}>
        {isComment ? (
          <span className={TC.cmt}>{line}</span>
        ) : (
          <span className={TC.pl}>{line}</span>
        )}
        {i < arr.length - 1 ? "\n" : ""}
      </span>
    );
  });
}

function highlightGoMod(code: string): ReactNode[] {
  return code.split("\n").map((line, i, arr) => {
    const trimmed = line.trimStart();
    const nodes: ReactNode[] = [];

    if (trimmed.startsWith("//")) {
      nodes.push(<span className={TC.cmt}>{line}</span>);
    } else if (/^(module|go|require|replace)\b/.test(trimmed)) {
      const keyword = trimmed.match(/^(module|go|require|replace)/)?.[0] || "";
      const rest = line.slice(line.indexOf(keyword) + keyword.length);
      const lead = line.slice(0, line.indexOf(keyword));
      nodes.push(lead);
      nodes.push(<span className={TC.kw}>{keyword}</span>);
      // Highlight strings in the rest
      const strParts = rest.split(/(".*?")/g);
      for (const sp of strParts) {
        if (sp.startsWith('"')) {
          nodes.push(<span className={TC.str}>{sp}</span>);
        } else {
          nodes.push(<span className={TC.pl}>{sp}</span>);
        }
      }
    } else if (trimmed.startsWith(")") || trimmed.startsWith("(")) {
      nodes.push(<span className={TC.pn}>{line}</span>);
    } else {
      // Dependency lines - highlight version
      const vMatch = line.match(/(v\d+\.\d+\.\d+\S*)/);
      if (vMatch) {
        const vi = line.indexOf(vMatch[1]);
        nodes.push(<span className={TC.pl}>{line.slice(0, vi)}</span>);
        nodes.push(<span className={TC.num}>{vMatch[1]}</span>);
        nodes.push(<span className={TC.pl}>{line.slice(vi + vMatch[1].length)}</span>);
      } else {
        nodes.push(<span className={TC.pl}>{line}</span>);
      }
    }

    return (
      <span key={i}>
        {nodes}
        {i < arr.length - 1 ? "\n" : ""}
      </span>
    );
  });
}

// ─── Extension Definitions ─────────────────────────────────────────────────

interface ExtensionDef {
  id: string;
  name: string;
  description: string;
  category: "core" | "data" | "messaging" | "transport" | "realtime" | "platform" | "security" | "ai" | "forgery";
  importPath: string;
  setupCode: string;
  yamlConfig: string;
  configKey: string;
}

const EXTENSIONS: ExtensionDef[] = [
  { id: "auth", name: "Auth", description: "Pluggable authentication with JWT, API keys, OAuth2, OIDC", category: "core", importPath: "github.com/xraph/forge/extensions/auth", configKey: "auth", setupCode: `    app.RegisterExtension(auth.NewExtension())`, yamlConfig: `  auth:\n    enabled: true\n    default_provider: "jwt"` },
  { id: "database", name: "Database", description: "Database connections with Bun ORM and migrations", category: "data", importPath: "github.com/xraph/forge/extensions/database", configKey: "database", setupCode: `    app.RegisterExtension(database.NewExtension())`, yamlConfig: `  database:\n    databases:\n      - name: "default"\n        type: "postgres"\n        dsn: "\${DATABASE_URL}"\n        max_open_conns: 25` },
  { id: "cache", name: "Cache", description: "Multi-backend caching with Redis, Memcached, and in-memory", category: "data", importPath: "github.com/xraph/forge/extensions/cache", configKey: "cache", setupCode: `    app.RegisterExtension(cache.NewExtension())`, yamlConfig: `  cache:\n    driver: "redis"\n    url: "\${REDIS_URL}"\n    default_ttl: 5m\n    prefix: "app:"` },
  { id: "search", name: "Search", description: "Full-text search with Elasticsearch, Meilisearch, Typesense", category: "data", importPath: "github.com/xraph/forge/extensions/search", configKey: "search", setupCode: `    app.RegisterExtension(search.NewExtension())`, yamlConfig: `  search:\n    provider: "meilisearch"\n    url: "\${MEILISEARCH_URL}"` },
  { id: "storage", name: "Storage", description: "Object storage with S3, GCS, and local filesystem", category: "data", importPath: "github.com/xraph/forge/extensions/storage", configKey: "storage", setupCode: `    app.RegisterExtension(storage.NewExtension())`, yamlConfig: `  storage:\n    provider: "s3"\n    bucket: "\${S3_BUCKET}"` },
  { id: "events", name: "Events", description: "Event-driven messaging with pub/sub", category: "messaging", importPath: "github.com/xraph/forge/extensions/events", configKey: "events", setupCode: `    app.RegisterExtension(events.NewExtension())`, yamlConfig: `  events:\n    driver: "memory"` },
  { id: "queue", name: "Queue", description: "Background job queue with workers", category: "messaging", importPath: "github.com/xraph/forge/extensions/queue", configKey: "queue", setupCode: `    app.RegisterExtension(queue.NewExtension())`, yamlConfig: `  queue:\n    driver: "redis"\n    url: "\${REDIS_URL}"\n    concurrency: 10` },
  { id: "kafka", name: "Kafka", description: "Apache Kafka producer/consumer integration", category: "messaging", importPath: "github.com/xraph/forge/extensions/kafka", configKey: "kafka", setupCode: `    app.RegisterExtension(kafka.NewExtension())`, yamlConfig: `  kafka:\n    brokers: "\${KAFKA_BROKERS}"` },
  { id: "mqtt", name: "MQTT", description: "MQTT messaging for IoT and real-time", category: "messaging", importPath: "github.com/xraph/forge/extensions/mqtt", configKey: "mqtt", setupCode: `    app.RegisterExtension(mqtt.NewExtension())`, yamlConfig: `  mqtt:\n    broker_url: "\${MQTT_URL}"` },
  { id: "grpc", name: "gRPC", description: "gRPC server and client support", category: "transport", importPath: "github.com/xraph/forge/extensions/grpc", configKey: "grpc", setupCode: `    app.RegisterExtension(grpc.NewExtension())`, yamlConfig: `  grpc:\n    port: 50051\n    reflection: true` },
  { id: "graphql", name: "GraphQL", description: "GraphQL server with schema-first or code-first", category: "transport", importPath: "github.com/xraph/forge/extensions/graphql", configKey: "graphql", setupCode: `    app.RegisterExtension(graphql.NewExtension())`, yamlConfig: `  graphql:\n    path: "/graphql"\n    playground: true` },
  { id: "orpc", name: "oRPC", description: "OpenAPI-native RPC framework", category: "transport", importPath: "github.com/xraph/forge/extensions/orpc", configKey: "orpc", setupCode: `    app.RegisterExtension(orpc.NewExtension())`, yamlConfig: `  orpc:\n    enabled: true` },
  { id: "mcp", name: "MCP", description: "Model Context Protocol server", category: "transport", importPath: "github.com/xraph/forge/extensions/mcp", configKey: "mcp", setupCode: `    app.RegisterExtension(mcp.NewExtension())`, yamlConfig: `  mcp:\n    enabled: true` },
  { id: "streaming", name: "Streaming", description: "SSE and WebSocket streaming", category: "realtime", importPath: "github.com/xraph/forge/extensions/streaming", configKey: "streaming", setupCode: `    app.RegisterExtension(streaming.NewExtension())`, yamlConfig: `  streaming:\n    websocket: true\n    sse: true` },
  { id: "webrtc", name: "WebRTC", description: "WebRTC signaling and media", category: "realtime", importPath: "github.com/xraph/forge/extensions/webrtc", configKey: "webrtc", setupCode: `    app.RegisterExtension(webrtc.NewExtension())`, yamlConfig: `  webrtc:\n    enabled: true` },
  { id: "hls", name: "HLS", description: "HTTP Live Streaming for video", category: "realtime", importPath: "github.com/xraph/forge/extensions/hls", configKey: "hls", setupCode: `    app.RegisterExtension(hls.NewExtension())`, yamlConfig: `  hls:\n    enabled: true` },
  { id: "gateway", name: "Gateway", description: "API gateway with routing and load balancing", category: "platform", importPath: "github.com/xraph/forge/extensions/gateway", configKey: "gateway", setupCode: `    app.RegisterExtension(gateway.NewExtension())`, yamlConfig: `  gateway:\n    enabled: true` },
  { id: "discovery", name: "Discovery", description: "Service discovery with Consul, etcd, or mDNS", category: "platform", importPath: "github.com/xraph/forge/extensions/discovery", configKey: "discovery", setupCode: `    app.RegisterExtension(discovery.NewExtension())`, yamlConfig: `  discovery:\n    provider: "consul"\n    address: "\${CONSUL_ADDR}"` },
  { id: "cron", name: "Cron", description: "Scheduled job execution", category: "platform", importPath: "github.com/xraph/forge/extensions/cron", configKey: "cron", setupCode: `    app.RegisterExtension(cron.NewExtension())`, yamlConfig: `  cron:\n    enabled: true` },
  { id: "dashboard", name: "Dashboard", description: "Admin dashboard with micro-frontend contributors", category: "platform", importPath: "github.com/xraph/forge/extensions/dashboard", configKey: "dashboard", setupCode: `    app.RegisterExtension(dashboard.NewExtension())`, yamlConfig: `  dashboard:\n    base_path: "/dashboard"\n    title: "Dashboard"\n    enable_realtime: true\n    theme: "auto"` },
  { id: "consensus", name: "Consensus", description: "Distributed consensus with Raft", category: "platform", importPath: "github.com/xraph/forge/extensions/consensus", configKey: "consensus", setupCode: `    app.RegisterExtension(consensus.NewExtension())`, yamlConfig: `  consensus:\n    enabled: true` },
  { id: "security", name: "Security", description: "CORS, CSRF, rate limiting, and security headers", category: "security", importPath: "github.com/xraph/forge/extensions/security", configKey: "security", setupCode: `    app.RegisterExtension(security.NewExtension())`, yamlConfig: `  security:\n    cors: true\n    csrf: true\n    rate_limiting: true` },
  { id: "features", name: "Feature Flags", description: "Feature flag management with multiple providers", category: "security", importPath: "github.com/xraph/forge/extensions/features", configKey: "features", setupCode: `    app.RegisterExtension(features.NewExtension())`, yamlConfig: `  features:\n    provider: "local"` },
  { id: "ai", name: "AI", description: "AI SDK integration for inference and agents", category: "ai", importPath: "github.com/xraph/forge/extensions/ai", configKey: "ai", setupCode: `    app.RegisterExtension(ai.NewExtension())`, yamlConfig: `  ai:\n    enabled: true` },
  // ── Forgery Ecosystem ──
  { id: "authsome", name: "Authsome", description: "OAuth2, WebAuthn, MFA, sessions, and API keys", category: "forgery", importPath: "github.com/xraph/authsome", configKey: "authsome", setupCode: `    app.RegisterExtension(authsome.NewForgeExtension())`, yamlConfig: `  authsome:\n    session_store: "database"\n    mfa_enabled: true` },
  { id: "keysmith", name: "Keysmith", description: "API key lifecycle — issuance, rotation, and revocation", category: "forgery", importPath: "github.com/xraph/keysmith", configKey: "keysmith", setupCode: `    app.RegisterExtension(keysmith.NewForgeExtension())`, yamlConfig: `  keysmith:\n    prefix: "sk_live_"\n    hash_algorithm: "sha256"` },
  { id: "warden", name: "Warden", description: "RBAC, ABAC, and Zanzibar-style ReBAC permissions", category: "forgery", importPath: "github.com/xraph/warden", configKey: "warden", setupCode: `    app.RegisterExtension(warden.NewForgeExtension())`, yamlConfig: `  warden:\n    model: "rbac"` },
  { id: "chronicle", name: "Chronicle", description: "Immutable audit trail with hash chain", category: "forgery", importPath: "github.com/xraph/chronicle", configKey: "chronicle", setupCode: `    app.RegisterExtension(chronicle.NewForgeExtension())`, yamlConfig: `  chronicle:\n    auto_audit: true\n    retention_days: 365` },
  { id: "relay-forgery", name: "Relay", description: "Webhook delivery with HMAC signing and retries", category: "forgery", importPath: "github.com/xraph/relay", configKey: "relay", setupCode: `    app.RegisterExtension(relay.NewForgeExtension())`, yamlConfig: `  relay:\n    max_retries: 5\n    signing_secret: "\${WEBHOOK_SECRET}"` },
  { id: "herald", name: "Herald", description: "Multi-channel notifications — email, SMS, push", category: "forgery", importPath: "github.com/xraph/herald", configKey: "herald", setupCode: `    app.RegisterExtension(herald.NewForgeExtension())`, yamlConfig: `  herald:\n    email:\n      provider: "smtp"\n      host: "\${SMTP_HOST}"` },
  { id: "dispatch-forgery", name: "Dispatch", description: "Durable execution for background jobs and workflows", category: "forgery", importPath: "github.com/xraph/dispatch", configKey: "dispatch", setupCode: `    app.RegisterExtension(dispatch.NewForgeExtension())`, yamlConfig: `  dispatch:\n    worker_count: 10\n    queues:\n      - "default"\n      - "notifications"` },
  { id: "grove", name: "Grove", description: "Polyglot Go ORM for PostgreSQL, SQLite, and more", category: "forgery", importPath: "github.com/xraph/grove", configKey: "grove", setupCode: `    app.RegisterExtension(grove.NewForgeExtension())`, yamlConfig: `  grove:\n    dsn: "\${DATABASE_URL}"\n    auto_migrate: true` },
  { id: "trove", name: "Trove", description: "Multi-tenant data isolation with row-level security", category: "forgery", importPath: "github.com/xraph/trove", configKey: "trove", setupCode: `    app.RegisterExtension(trove.NewForgeExtension())`, yamlConfig: `  trove:\n    strategy: "row-level"\n    tenant_column: "tenant_id"` },
  { id: "ledger", name: "Ledger", description: "Usage-based billing with metering and invoicing", category: "forgery", importPath: "github.com/xraph/ledger", configKey: "ledger", setupCode: `    app.RegisterExtension(ledger.NewForgeExtension())`, yamlConfig: `  ledger:\n    auto_invoicing: true\n    stripe_key: "\${STRIPE_SECRET_KEY}"` },
  { id: "ctrlplane", name: "Ctrl Plane", description: "Infrastructure control plane for multi-tenant instances", category: "forgery", importPath: "github.com/xraph/ctrlplane", configKey: "ctrlplane", setupCode: `    app.RegisterExtension(ctrlplane.NewForgeExtension())`, yamlConfig: `  ctrlplane:\n    health_check_interval: 30s` },
  { id: "cortex", name: "Cortex", description: "AI agent orchestration with skills and personas", category: "forgery", importPath: "github.com/xraph/cortex", configKey: "cortex", setupCode: `    app.RegisterExtension(cortex.NewForgeExtension())`, yamlConfig: `  cortex:\n    default_model: "gpt-4"\n    max_concurrent_agents: 10` },
  { id: "nexus", name: "Nexus", description: "AI gateway for routing and caching LLM traffic", category: "forgery", importPath: "github.com/xraph/nexus", configKey: "nexus", setupCode: `    app.RegisterExtension(nexus.NewForgeExtension())`, yamlConfig: `  nexus:\n    rate_limiting: true` },
  { id: "sentinel", name: "Sentinel", description: "AI evaluation framework with 22 scorers", category: "forgery", importPath: "github.com/xraph/sentinel", configKey: "sentinel", setupCode: `    app.RegisterExtension(sentinel.NewForgeExtension())`, yamlConfig: `  sentinel:\n    default_scorers:\n      - "relevance"\n      - "coherence"` },
  { id: "shield-forgery", name: "Shield", description: "Multi-layer AI safety and content filtering", category: "forgery", importPath: "github.com/xraph/shield", configKey: "shield", setupCode: `    app.RegisterExtension(shield.NewForgeExtension())`, yamlConfig: `  shield:\n    all_layers: true\n    pii_redaction: true` },
  { id: "weave", name: "Weave", description: "RAG pipeline — chunk, embed, store, retrieve", category: "forgery", importPath: "github.com/xraph/weave", configKey: "weave", setupCode: `    app.RegisterExtension(weave.NewForgeExtension())`, yamlConfig: `  weave:\n    default_embedder: "openai"\n    chunk_size: 512` },
  { id: "vault-forgery", name: "Vault", description: "Secrets management with encryption at rest", category: "forgery", importPath: "github.com/xraph/vault", configKey: "vault", setupCode: `    app.RegisterExtension(vault.NewForgeExtension())`, yamlConfig: `  vault:\n    backend: "encrypted"\n    hot_reload: true` },
];

const CATEGORIES: { id: ExtensionDef["category"]; label: string; color: string }[] = [
  { id: "data", label: "Data & Storage", color: "text-emerald-500 border-emerald-500/40 bg-emerald-500/10" },
  { id: "messaging", label: "Messaging", color: "text-sky-500 border-sky-500/40 bg-sky-500/10" },
  { id: "transport", label: "Transport & RPC", color: "text-blue-500 border-blue-500/40 bg-blue-500/10" },
  { id: "realtime", label: "Realtime & Media", color: "text-purple-500 border-purple-500/40 bg-purple-500/10" },
  { id: "platform", label: "Platform & Infra", color: "text-teal-500 border-teal-500/40 bg-teal-500/10" },
  { id: "security", label: "Security & Access", color: "text-amber-500 border-amber-500/40 bg-amber-500/10" },
  { id: "ai", label: "AI", color: "text-violet-500 border-violet-500/40 bg-violet-500/10" },
  { id: "forgery", label: "Forgery Ecosystem", color: "text-orange-500 border-orange-500/40 bg-orange-500/10" },
];

// ─── File tabs ──────────────────────────────────────────────────────────────

interface ProjectFile {
  name: string;
  path: string;
  icon: typeof FileCode;
  highlight: (code: string) => ReactNode[];
}

const FILE_TABS: ProjectFile[] = [
  { name: "main.go", path: "cmd/app/main.go", icon: FileCode, highlight: highlightGo },
  { name: "config.yaml", path: "config.yaml", icon: Settings, highlight: highlightYaml },
  { name: ".forge.yaml", path: ".forge.yaml", icon: Settings, highlight: highlightYaml },
  { name: "go.mod", path: "go.mod", icon: FileText, highlight: highlightGoMod },
  { name: ".gitignore", path: ".gitignore", icon: GitBranch, highlight: highlightGitignore },
];

// ─── File generation ────────────────────────────────────────────────────────

function genMainGo(appName: string, selected: Set<string>): string {
  const exts = EXTENSIONS.filter((e) => selected.has(e.id));

  const forgeImports = new Set(["github.com/xraph/forge"]);
  for (const ext of exts) forgeImports.add(ext.importPath);

  const extBlock = Array.from(forgeImports)
    .sort()
    .map((i) => {
      const parts = i.split("/");
      const pkgName = parts[parts.length - 1];
      const hasDup = Array.from(forgeImports).filter((o) => {
        const op = o.split("/");
        return op[op.length - 1] === pkgName && o !== i;
      }).length > 0;
      if (hasDup && !i.includes("forge/extensions")) {
        return `    forgery${pkgName} "${i}"`;
      }
      return `    "${i}"`;
    })
    .join("\n");

  const setupLines = exts.length > 0 ? `\n    // Register extensions (configured via config.yaml)\n${exts.map((e) => e.setupCode).join("\n")}\n` : "";

  return `package main

import (
    "log"

${extBlock}
)

func main() {
    app := forge.New(
        forge.WithAppName("${appName}"),
        forge.WithAppVersion("1.0.0"),
    )
${setupLines}
    // Register routes
    _ = app.Router().GET("/health", func(c forge.Context) error {
        return c.JSON(200, map[string]string{
            "service": "${appName}",
            "status":  "ok",
        })
    })

    if err := app.Run(); err != nil {
        log.Fatal(err)
    }
}
`;
}

function genConfigYaml(appName: string, selected: Set<string>): string {
  const exts = EXTENSIONS.filter((e) => selected.has(e.id));

  let y = `# Application runtime configuration
# Docs: https://forge.dev/docs/forge/configuration

app:
  name: "${appName}"
  version: "1.0.0"
  environment: "development"

server:
  host: "0.0.0.0"
  port: 8080

logging:
  level: "info"
  format: "json"
`;

  if (exts.length > 0) {
    y += `\nextensions:\n`;
    for (const ext of exts) {
      y += `${ext.yamlConfig}\n\n`;
    }
  }

  return y.trimEnd() + "\n";
}

function genForgeYaml(appName: string, moduleName: string): string {
  return `# Forge framework configuration
# Docs: https://forge.dev/docs/cli

project:
  name: "${appName}"
  version: "1.0.0"
  layout: "single-module"
  module: "${moduleName}"

dev:
  auto_discover: true
  watch:
    enabled: true
    paths:
      - "./**/*.go"
    exclude:
      - "**/*_test.go"
      - "**/vendor/**"
  hot_reload:
    enabled: true
    delay: 300ms

database:
  driver: "postgres"
  migrations_path: ./database/migrations
  seeds_path: ./database/seeds

build:
  output_dir: ./bin
  auto_discover: true
`;
}

function genGoMod(moduleName: string, selected: Set<string>): string {
  const exts = EXTENSIONS.filter((e) => selected.has(e.id));
  const deps = new Set(["github.com/xraph/forge v1.3.1"]);
  for (const ext of exts) {
    if (ext.importPath.includes("forge/extensions/")) {
      deps.add(`${ext.importPath} v1.3.1`);
    } else if (!ext.importPath.includes("xraph/forge")) {
      deps.add(`${ext.importPath} v1.3.0`);
    }
  }

  const depLines = Array.from(deps).sort().map((d) => `    ${d}`).join("\n");

  return `module ${moduleName}

go 1.24

require (
${depLines}
)
`;
}

function genGitignore(): string {
  return `# Binaries
bin/
*.exe
*.dll
*.so
*.dylib

# Test
*.test
*.out

# Dependencies
vendor/

# Environment
.env
.env.local
.env.*.local

# IDE
.vscode/
.idea/
*.swp
*~

# OS
.DS_Store
Thumbs.db

# Forge
.forge.local.yaml

# Local configs
config.local.yaml
config.local.yml
`;
}

// ─── Component ──────────────────────────────────────────────────────────────

export function ForgeBuilder() {
  const [appName, setAppName] = useState("my-app");
  const [moduleName, setModuleName] = useState("github.com/example/my-app");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [activeTab, setActiveTab] = useState(0);
  const [copied, setCopied] = useState(false);
  const [expandedCategories, setExpandedCategories] = useState<Set<string>>(
    new Set(CATEGORIES.map((c) => c.id)),
  );

  const toggle = useCallback((id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      next.has(id) ? next.delete(id) : next.add(id);
      return next;
    });
  }, []);

  const toggleCategory = useCallback((catId: string) => {
    setExpandedCategories((prev) => {
      const next = new Set(prev);
      next.has(catId) ? next.delete(catId) : next.add(catId);
      return next;
    });
  }, []);

  const selectAll = useCallback((catId: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      const catExts = EXTENSIONS.filter((e) => e.category === catId);
      const allSel = catExts.every((e) => next.has(e.id));
      for (const e of catExts) allSel ? next.delete(e.id) : next.add(e.id);
      return next;
    });
  }, []);

  const files = useMemo(() => [
    genMainGo(appName, selected),
    genConfigYaml(appName, selected),
    genForgeYaml(appName, moduleName),
    genGoMod(moduleName, selected),
    genGitignore(),
  ], [appName, moduleName, selected]);

  const tab = FILE_TABS[activeTab];
  const content = files[activeTab];

  const highlighted = useMemo(
    () => tab.highlight(content),
    [tab, content],
  );

  const handleCopy = useCallback(() => {
    navigator.clipboard.writeText(content);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }, [content]);

  const handleDownload = useCallback(() => {
    const zipFiles = FILE_TABS.map((t, i) => ({
      name: `${appName}/${t.path}`,
      content: files[i],
    }));
    const blob = createZip(zipFiles);
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${appName}.zip`;
    a.click();
    URL.revokeObjectURL(url);
  }, [appName, files]);

  return (
    <div className="grid lg:grid-cols-[380px_1fr] gap-6">
      {/* Left: Extension picker */}
      <div className="space-y-4">
        <div className="space-y-3">
          <div>
            <label htmlFor="app-name" className="block text-xs font-medium text-fd-muted-foreground mb-1.5">
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
          <div>
            <label htmlFor="module-name" className="block text-xs font-medium text-fd-muted-foreground mb-1.5">
              Module Path
            </label>
            <input
              id="module-name"
              type="text"
              value={moduleName}
              onChange={(e) => setModuleName(e.target.value.replace(/[^a-zA-Z0-9-_./]/g, ""))}
              className="w-full px-3 py-2 text-sm font-mono bg-fd-card border border-fd-border focus:border-fd-primary focus:outline-none"
              placeholder="github.com/example/my-app"
            />
          </div>
        </div>

        <div className="flex items-center justify-between">
          <div className="text-xs text-fd-muted-foreground">
            {selected.size} extension{selected.size !== 1 ? "s" : ""} selected
          </div>
          <button
            type="button"
            onClick={handleDownload}
            className="flex items-center gap-1.5 text-xs font-medium border border-fd-primary/40 bg-fd-primary/10 text-fd-primary px-2.5 py-1 hover:bg-fd-primary/20 transition-colors"
          >
            <Download className="size-3" />
            Download .zip
          </button>
        </div>

        <div className="space-y-2 max-h-[600px] overflow-y-auto pr-1">
          {CATEGORIES.map((cat) => {
            const catExts = EXTENSIONS.filter((e) => e.category === cat.id);
            const selCount = catExts.filter((e) => selected.has(e.id)).length;
            const isExp = expandedCategories.has(cat.id);
            return (
              <div key={cat.id} className="border border-fd-border bg-fd-card">
                <div className="flex items-center justify-between px-3 py-2">
                  <button type="button" onClick={() => toggleCategory(cat.id)} className="flex items-center gap-2 text-sm font-medium flex-1 text-left">
                    {isExp ? <ChevronUp className="size-3.5 text-fd-muted-foreground" /> : <ChevronDown className="size-3.5 text-fd-muted-foreground" />}
                    <span className={`text-[10px] font-semibold uppercase tracking-wider px-2 py-0.5 border ${cat.color}`}>{cat.label}</span>
                  </button>
                  <button type="button" onClick={() => selectAll(cat.id)} className="text-[10px] text-fd-muted-foreground hover:text-fd-foreground transition-colors">
                    {selCount === catExts.length ? "None" : "All"}
                  </button>
                </div>
                {isExp && (
                  <div className="border-t border-fd-border">
                    {catExts.map((ext) => (
                      <label key={ext.id} className="flex items-start gap-3 px-3 py-2 cursor-pointer hover:bg-fd-accent/50 transition-colors">
                        <input type="checkbox" checked={selected.has(ext.id)} onChange={() => toggle(ext.id)} className="mt-0.5 accent-fd-primary" />
                        <div className="min-w-0">
                          <div className="text-sm font-medium">{ext.name}</div>
                          <div className="text-xs text-fd-muted-foreground truncate">{ext.description}</div>
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

      {/* Right: File viewer */}
      <div className="relative">
        <div className="sticky top-4">
          {/* File tabs + copy */}
          <div className="flex items-center justify-between mb-2">
            <div className="flex items-center border border-fd-border overflow-x-auto">
              {FILE_TABS.map((f, i) => {
                const Icon = f.icon;
                return (
                  <button
                    key={f.name}
                    type="button"
                    onClick={() => setActiveTab(i)}
                    className={`flex items-center gap-1.5 px-3 py-1.5 text-xs font-mono whitespace-nowrap transition-colors border-r border-fd-border last:border-r-0 ${
                      activeTab === i
                        ? "bg-fd-primary/10 text-fd-primary"
                        : "text-fd-muted-foreground hover:text-fd-foreground"
                    }`}
                  >
                    <Icon className="size-3" />
                    {f.name}
                  </button>
                );
              })}
            </div>
            <button
              type="button"
              onClick={handleCopy}
              className="flex items-center gap-1.5 text-xs text-fd-muted-foreground hover:text-fd-foreground border border-fd-border px-2.5 py-1 ml-2 transition-colors shrink-0"
            >
              {copied ? <><Check className="size-3" /> Copied</> : <><Copy className="size-3" /> Copy</>}
            </button>
          </div>

          {/* File path */}
          <div className="text-[11px] font-mono text-fd-muted-foreground/60 mb-1 px-1">
            {appName}/{tab.path}
          </div>

          {/* Code display */}
          <div className="border border-fd-border bg-zinc-950 overflow-hidden rounded-sm">
            <pre className="p-4 text-sm font-mono overflow-x-auto max-h-[650px] overflow-y-auto leading-relaxed text-zinc-200">
              <code>{highlighted}</code>
            </pre>
          </div>

          {/* Instructions */}
          <div className="mt-4 border border-fd-border bg-fd-card p-4 space-y-2">
            <div className="text-xs font-semibold text-fd-foreground">Getting Started</div>
            <div className="text-xs text-fd-muted-foreground space-y-1.5 font-mono">
              <p><span className="text-fd-muted-foreground/60">1.</span> Download and extract the .zip</p>
              <p><span className="text-fd-muted-foreground/60">2.</span> <code className="px-1 py-0.5 bg-fd-muted rounded-sm">cd {appName}</code></p>
              <p><span className="text-fd-muted-foreground/60">3.</span> <code className="px-1 py-0.5 bg-fd-muted rounded-sm">go mod tidy</code></p>
              <p><span className="text-fd-muted-foreground/60">4.</span> <code className="px-1 py-0.5 bg-fd-muted rounded-sm">forge dev</code></p>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
