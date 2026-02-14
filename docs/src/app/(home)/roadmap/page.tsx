import { BentoGrid, BentoGridItem } from "@/components/ui/bento-grid";
import {
  CheckCircle2,
  CircleDashed,
  Clock,
  Construction,
  Rocket,
} from "lucide-react";

const roadmapItems = [
  {
    title: "Q1 2025: Core Stability",
    description:
      "Focus on performance optimization and core framework stability.",
    header: (
      <div className="h-full min-h-[6rem] w-full rounded-xl bg-gradient-to-br from-neutral-200 to-neutral-100 dark:from-neutral-900 dark:to-neutral-800" />
    ),
    icon: <Construction className="h-4 w-4 text-neutral-500" />,
    status: "In Progress",
  },
  {
    title: "Q2 2025: Advanced Extensions",
    description:
      "New extensions for AI integration and real-time data processing.",
    header: (
      <div className="h-full min-h-[6rem] w-full rounded-xl bg-gradient-to-br from-neutral-200 to-neutral-100 dark:from-neutral-900 dark:to-neutral-800" />
    ),
    icon: <Rocket className="h-4 w-4 text-neutral-500" />,
    status: "Planned",
  },
  {
    title: "Q3 2025: Ecosystem Growth",
    description: "Launch of the plugin marketplace and community tools.",
    header: (
      <div className="h-full min-h-[6rem] w-full rounded-xl bg-gradient-to-br from-neutral-200 to-neutral-100 dark:from-neutral-900 dark:to-neutral-800" />
    ),
    icon: <CircleDashed className="h-4 w-4 text-neutral-500" />,
    status: "Planned",
  },
  {
    title: "Completed: v1.0 Launch",
    description: "Initial release of Forge framework with core features.",
    header: (
      <div className="h-full min-h-[6rem] w-full rounded-xl bg-gradient-to-br from-green-200 to-green-100 dark:from-green-900 dark:to-green-800" />
    ),
    icon: <CheckCircle2 className="h-4 w-4 text-green-500" />,
    status: "Completed",
  },
];

export default function RoadmapPage() {
  return (
    <main className="container max-w-7xl mx-auto px-6 py-16 md:py-24">
      <div className="mb-12">
        <h1 className="text-4xl font-bold mb-4">Roadmap</h1>
        <p className="text-lg text-fd-muted-foreground max-w-2xl">
          See what we're working on and where Forge is via our public roadmap.
        </p>
      </div>

      <BentoGrid>
        {roadmapItems.map((item, i) => (
          <BentoGridItem
            key={i}
            title={
              <span className="flex items-center gap-2">
                {item.title}
                <span
                  className={`text-[10px] px-2 py-0.5 rounded-full border ${item.status === "Completed" ? "border-green-500 text-green-600 bg-green-500/10" : item.status === "In Progress" ? "border-amber-500 text-amber-600 bg-amber-500/10" : "border-neutral-500 text-neutral-600 bg-neutral-500/10"}`}
                >
                  {item.status}
                </span>
              </span>
            }
            description={item.description}
            header={item.header}
            icon={item.icon}
            className={i === 0 || i === 3 ? "md:col-span-2" : ""}
          />
        ))}
      </BentoGrid>
    </main>
  );
}
