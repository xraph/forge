import { getChangelog } from "@/lib/changelog";
import {
  GitBranch,
  ExternalLink,
  AlertTriangle,
  Sparkles,
  Wrench,
  BookOpen,
  Tag,
} from "lucide-react";

function getTypeConfig(type: string) {
  switch (type) {
    case "breaking":
      return {
        label: "Breaking",
        className:
          "border-red-500/40 text-red-600 dark:text-red-400 bg-red-500/10",
        dotColor: "bg-red-500",
        gradientFrom: "from-red-500/20",
      };
    case "major":
      return {
        label: "Major",
        className:
          "border-amber-500/40 text-amber-600 dark:text-amber-400 bg-amber-500/10",
        dotColor: "bg-amber-500",
        gradientFrom: "from-amber-500/20",
      };
    case "minor":
      return {
        label: "Minor",
        className:
          "border-blue-500/40 text-blue-600 dark:text-blue-400 bg-blue-500/10",
        dotColor: "bg-blue-500",
        gradientFrom: "from-blue-500/20",
      };
    default:
      return {
        label: "Patch",
        className:
          "border-emerald-500/40 text-emerald-600 dark:text-emerald-400 bg-emerald-500/10",
        dotColor: "bg-emerald-500",
        gradientFrom: "from-emerald-500/20",
      };
  }
}

function getSectionIcon(heading: string) {
  const lower = heading.toLowerCase();
  if (lower.includes("breaking")) return AlertTriangle;
  if (lower.includes("feature")) return Sparkles;
  if (lower.includes("bug") || lower.includes("fix")) return Wrench;
  if (lower.includes("doc")) return BookOpen;
  if (lower.includes("maintenance")) return Tag;
  return GitBranch;
}

function formatDate(dateStr: string) {
  return new Date(dateStr).toLocaleDateString("en-US", {
    year: "numeric",
    month: "long",
    day: "numeric",
  });
}

export default function ChangelogPage() {
  const entries = getChangelog();

  return (
    <main className="container max-w-4xl mx-auto px-6 py-16 md:py-24">
      {/* Header */}
      <div className="mb-16">
        <div className="inline-flex items-center gap-2 border border-fd-border bg-fd-card/80 backdrop-blur-sm px-3 py-1.5 text-xs font-medium text-fd-muted-foreground mb-4">
          <GitBranch className="size-3" />
          {entries.length} releases
        </div>
        <h1 className="text-4xl font-bold mb-4 tracking-tight">Changelog</h1>
        <p className="text-lg text-fd-muted-foreground max-w-xl">
          All notable changes to Forge. Each release is automatically tracked
          and documented.
        </p>
      </div>

      {/* Timeline */}
      <div className="relative">
        {/* Timeline line */}
        <div className="absolute left-[7px] top-2 bottom-2 w-px bg-gradient-to-b from-fd-border via-fd-border to-transparent" />

        <div className="space-y-12">
          {entries.map((entry) => {
            const config = getTypeConfig(entry.type);

            return (
              <article key={entry.version} className="relative pl-10">
                {/* Timeline dot */}
                <div
                  className={`absolute left-0 top-1.5 size-[15px] rounded-full ${config.dotColor} ring-4 ring-fd-background shadow-sm`}
                />

                {/* Version header */}
                <div className="flex flex-col sm:flex-row sm:items-center gap-2 sm:gap-3 mb-4">
                  <h2 className="font-mono font-bold text-xl tracking-tight">
                    v{entry.version}
                  </h2>
                  <span
                    className={`text-[10px] font-semibold uppercase tracking-wider px-2 py-0.5 border w-fit ${config.className}`}
                  >
                    {config.label}
                  </span>
                  <span className="text-sm text-fd-muted-foreground">
                    {formatDate(entry.date)}
                  </span>
                  <a
                    href={entry.url}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1 text-xs text-fd-muted-foreground hover:text-fd-foreground transition-colors"
                  >
                    <ExternalLink className="size-3" />
                    Compare
                  </a>
                </div>

                {/* Sections */}
                <div className="space-y-5">
                  {entry.sections.map((section) => {
                    const Icon = getSectionIcon(section.heading);

                    return (
                      <div key={section.heading}>
                        <div className="flex items-center gap-2 mb-2.5">
                          <Icon className="size-3.5 text-fd-muted-foreground" />
                          <h3 className="text-sm font-semibold uppercase tracking-wider text-fd-muted-foreground">
                            {section.heading}
                          </h3>
                        </div>
                        <ul className="space-y-2">
                          {section.items.map((item, idx) => (
                            <li
                              key={idx}
                              className="relative pl-4 text-sm text-fd-foreground/85 leading-relaxed before:absolute before:left-0 before:top-[9px] before:size-1 before:rounded-full before:bg-fd-muted-foreground/40"
                            >
                              {item}
                            </li>
                          ))}
                        </ul>
                      </div>
                    );
                  })}
                </div>

                {/* Subtle bottom separator */}
                <div className="mt-8 h-px bg-gradient-to-r from-fd-border/60 via-fd-border/20 to-transparent" />
              </article>
            );
          })}
        </div>
      </div>
    </main>
  );
}
