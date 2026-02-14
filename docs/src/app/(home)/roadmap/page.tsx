import {
  CheckCircle2,
  CircleDashed,
  Construction,
  Rocket,
  Shield,
  Zap,
  Package,
  Globe,
  ArrowRight,
} from "lucide-react";

const phases = [
  {
    title: "Core Framework",
    version: "v0.5 – v0.6",
    status: "completed" as const,
    description:
      "Foundation of the Forge framework with modular architecture and production-ready core.",
    milestones: [
      "Type-safe dependency injection container",
      "Lifecycle management system",
      "Functional options configuration",
      "HTTP router with OpenAPI auto-generation",
      "CLI framework and dashboard extension",
      "Hot reload dev tooling",
      "28+ production-ready extensions",
      "Comprehensive CI/CD with Release Please",
    ],
    icon: Package,
    accent: "emerald",
  },
  {
    title: "API Stability & Validation",
    version: "v0.7",
    status: "completed" as const,
    description:
      "Hardened API surface with enhanced JSON handling, validation improvements, and bug fixes.",
    milestones: [
      "Enhanced JSON response handling with struct tags",
      "Sensitive field cleaning in responses",
      "Validation fixes for zero-value primitives",
      "Boolean query parameter validation fix",
      "Improved error handling across endpoints",
    ],
    icon: Shield,
    accent: "emerald",
  },
  {
    title: "Storage, Extensions & DI",
    version: "v0.8 – v0.9",
    status: "completed" as const,
    description:
      "Pluggable storage backends, AI streaming, Vessel DI migration, gateway extension, and CLI code generation.",
    milestones: [
      "AI extension with streaming & multi-provider support",
      "Database migration tooling & lazy discovery",
      "Vessel dependency injection migration",
      "Gateway extension with auth, caching & circuit breaker",
      "CLI code generation command",
      "Config env-var expansion & .forge.yaml support",
      "Release workflow automation for all modules",
    ],
    icon: Zap,
    accent: "emerald",
  },
  {
    title: "Pre-Release Hardening",
    version: "v0.10",
    status: "in-progress" as const,
    description:
      "Final stabilization and polish before the v1.0 release. Performance audits, documentation gaps, and API refinements.",
    milestones: [
      "Performance benchmarking & optimization",
      "API surface audit and deprecation cleanup",
      "Extended test coverage across all modules",
      "Store interface standardization",
      "Error handling consistency review",
    ],
    icon: Construction,
    accent: "amber",
  },
  {
    title: "v1.0 Stable Release",
    version: "v1.0",
    status: "planned" as const,
    description:
      "The first stable release with a frozen API surface, comprehensive documentation, and production deployment guides.",
    milestones: [
      "Public API freeze and stability guarantees",
      "Semantic versioning commitment",
      "Migration guide from v0.x",
      "Production deployment documentation",
      "Complete API reference documentation",
      "Performance and security audit",
      "Release candidate testing period",
    ],
    icon: Rocket,
    accent: "blue",
  },
  {
    title: "Ecosystem & Beyond",
    version: "Post v1.0",
    status: "future" as const,
    description:
      "Expanding the Forge ecosystem with community tooling, cloud-native integrations, and advanced extensions.",
    milestones: [
      "Extension marketplace",
      "Cloud-native deployment templates (K8s, Docker)",
      "Advanced telemetry and observability",
      "Community extension SDK",
      "gRPC and GraphQL protocol support",
      "Distributed tracing integration",
    ],
    icon: Globe,
    accent: "violet",
  },
];

function getStatusConfig(status: string) {
  switch (status) {
    case "completed":
      return {
        label: "Completed",
        icon: CheckCircle2,
        className:
          "border-emerald-500/40 text-emerald-600 dark:text-emerald-400 bg-emerald-500/10",
        lineColor: "bg-emerald-500",
        iconColor: "text-emerald-500",
      };
    case "in-progress":
      return {
        label: "In Progress",
        icon: Construction,
        className:
          "border-amber-500/40 text-amber-600 dark:text-amber-400 bg-amber-500/10",
        lineColor: "bg-amber-500",
        iconColor: "text-amber-500",
      };
    case "planned":
      return {
        label: "Planned",
        icon: CircleDashed,
        className:
          "border-blue-500/40 text-blue-600 dark:text-blue-400 bg-blue-500/10",
        lineColor: "bg-blue-500/40",
        iconColor: "text-blue-500",
      };
    default:
      return {
        label: "Future",
        icon: CircleDashed,
        className:
          "border-violet-500/40 text-violet-600 dark:text-violet-400 bg-violet-500/10",
        lineColor: "bg-violet-500/30",
        iconColor: "text-violet-400",
      };
  }
}

// Progress calculation
const completedCount = phases.filter((p) => p.status === "completed").length;
const totalCount = phases.length;
const progressPercent = Math.round((completedCount / totalCount) * 100);

export default function RoadmapPage() {
  return (
    <main className="container max-w-5xl mx-auto px-6 py-16 md:py-24">
      {/* Header */}
      <div className="mb-16">
        <div className="inline-flex items-center gap-2 border border-fd-border bg-fd-card/80 backdrop-blur-sm px-3 py-1.5 text-xs font-medium text-fd-muted-foreground mb-4">
          <Construction className="size-3" />
          Currently v0 · Working toward v1.0
        </div>
        <h1 className="text-4xl font-bold mb-4 tracking-tight">Roadmap</h1>
        <p className="text-lg text-fd-muted-foreground max-w-2xl">
          Forge is currently in{" "}
          <strong className="text-fd-foreground">v0</strong> — rapidly evolving
          with a clear path to v1.0 stable release. Here's where we are and
          where we're headed.
        </p>

        {/* Progress bar */}
        <div className="mt-8 max-w-md">
          <div className="flex items-center justify-between text-xs text-fd-muted-foreground mb-2">
            <span>Progress to v1.0</span>
            <span className="font-mono">{progressPercent}%</span>
          </div>
          <div className="h-2 bg-fd-border/40 rounded-full overflow-hidden">
            <div
              className="h-full bg-gradient-to-r from-emerald-500 via-amber-500 to-blue-500 rounded-full transition-all duration-700"
              style={{ width: `${progressPercent}%` }}
            />
          </div>
          <div className="flex items-center justify-between mt-2">
            <span className="text-[10px] text-fd-muted-foreground font-medium">
              {completedCount} of {totalCount} phases complete
            </span>
            <span className="inline-flex items-center gap-1 text-[10px] text-amber-600 dark:text-amber-400 font-medium">
              <Construction className="size-2.5" />
              Active development
            </span>
          </div>
        </div>
      </div>

      {/* Phased Timeline */}
      <div className="relative">
        {/* Timeline line */}
        <div className="absolute left-[19px] top-4 bottom-4 w-px bg-gradient-to-b from-emerald-500 via-amber-500/50 to-violet-500/30" />

        <div className="space-y-10">
          {phases.map((phase, index) => {
            const config = getStatusConfig(phase.status);
            const StatusIcon = config.icon;
            const PhaseIcon = phase.icon;

            return (
              <div key={phase.title} className="relative pl-14">
                {/* Timeline node */}
                <div
                  className={`absolute left-2 top-1 flex items-center justify-center size-[24px] rounded-full border-2 ${
                    phase.status === "completed"
                      ? "border-emerald-500 bg-emerald-500/10"
                      : phase.status === "in-progress"
                        ? "border-amber-500 bg-amber-500/10"
                        : "border-fd-border bg-fd-background"
                  }`}
                >
                  <StatusIcon className={`size-3 ${config.iconColor}`} />
                </div>

                {/* Phase card */}
                <div
                  className={`border border-fd-border bg-fd-card/60 backdrop-blur-sm p-6 transition-all hover:border-fd-border/80 hover:bg-fd-card/80 ${
                    phase.status === "in-progress"
                      ? "ring-1 ring-amber-500/20"
                      : ""
                  }`}
                >
                  {/* Card header */}
                  <div className="flex flex-col sm:flex-row sm:items-center gap-2 sm:gap-3 mb-3">
                    <div className="flex items-center gap-2">
                      <PhaseIcon className="size-4 text-fd-muted-foreground" />
                      <h2 className="text-lg font-bold tracking-tight">
                        {phase.title}
                      </h2>
                    </div>
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-xs text-fd-muted-foreground bg-fd-background px-2 py-0.5 border border-fd-border">
                        {phase.version}
                      </span>
                      <span
                        className={`text-[10px] font-semibold uppercase tracking-wider px-2 py-0.5 border ${config.className}`}
                      >
                        {config.label}
                      </span>
                    </div>
                  </div>

                  <p className="text-sm text-fd-muted-foreground leading-relaxed mb-4">
                    {phase.description}
                  </p>

                  {/* Milestones */}
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-2">
                    {phase.milestones.map((milestone, idx) => (
                      <div key={idx} className="flex items-start gap-2 text-sm">
                        {phase.status === "completed" ? (
                          <CheckCircle2 className="size-3.5 mt-0.5 text-emerald-500 shrink-0" />
                        ) : (
                          <ArrowRight className="size-3.5 mt-0.5 text-fd-muted-foreground/50 shrink-0" />
                        )}
                        <span
                          className={
                            phase.status === "completed"
                              ? "text-fd-foreground/70"
                              : "text-fd-foreground/85"
                          }
                        >
                          {milestone}
                        </span>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      </div>

      {/* Footer note */}
      <div className="mt-16 border border-fd-border bg-fd-card/40 backdrop-blur-sm p-6 text-center">
        <p className="text-sm text-fd-muted-foreground">
          This roadmap is a living document and may evolve as priorities shift.
          <br />
          Want to influence the direction?{" "}
          <a
            href="https://github.com/xraph/forge/issues"
            target="_blank"
            rel="noreferrer"
            className="text-fd-foreground font-medium underline underline-offset-4 decoration-fd-border hover:decoration-fd-foreground transition-colors"
          >
            Open an issue on GitHub
          </a>{" "}
          or{" "}
          <a
            href="https://github.com/xraph/forge/discussions"
            target="_blank"
            rel="noreferrer"
            className="text-fd-foreground font-medium underline underline-offset-4 decoration-fd-border hover:decoration-fd-foreground transition-colors"
          >
            start a discussion
          </a>
          .
        </p>
      </div>
    </main>
  );
}
