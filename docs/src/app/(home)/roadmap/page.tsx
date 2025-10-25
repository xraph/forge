import { Calendar, CheckCircle2, Clock, Rocket, Wrench } from "lucide-react";
import { SectionHeader } from "@/components/ui/section-header";
import { roadmapItems } from "@/constants/changelog";

const getStatusIcon = (status: string) => {
  switch (status) {
    case "completed":
      return <CheckCircle2 className="w-5 h-5 text-green-600" />;
    case "in-progress":
      return <Clock className="w-5 h-5 text-blue-600" />;
    case "planned":
      return <Calendar className="w-5 h-5 text-orange-600" />;
    default:
      return <Rocket className="w-5 h-5 text-purple-600" />;
  }
};

const getStatusLabel = (status: string) => {
  switch (status) {
    case "completed":
      return "Completed";
    case "in-progress":
      return "In Progress";
    case "planned":
      return "Planned";
    default:
      return "Future";
  }
};

const getStatusColor = (status: string) => {
  switch (status) {
    case "completed":
      return "bg-green-500/10 text-green-600 border-green-500/20";
    case "in-progress":
      return "bg-blue-500/10 text-blue-600 border-blue-500/20";
    case "planned":
      return "bg-orange-500/10 text-orange-600 border-orange-500/20";
    default:
      return "bg-purple-500/10 text-purple-600 border-purple-500/20";
  }
};

export default function RoadmapPage() {
  return (
    <main className="pt-20 pb-20">
      <div className="container mx-auto px-4 sm:px-6 lg:px-8">
        <div className="container mx-auto px-4 sm:px-6 lg:px-8">
          <SectionHeader
            label="ROADMAP"
            title="Future of Forge"
            description="Our vision and upcoming features"
          />

          <div className="max-w-4xl mx-auto">
            {roadmapItems.map((item, index) => (
              <article
                key={index}
                className="relative pl-8 pb-16 border-l-2 border-border last:border-l-0 last:pb-0"
              >
                {/* Status marker */}
                <div className="absolute left-0 -translate-x-1/2 w-3 h-3 rounded-full bg-primary ring-4 ring-background" />

                <div className="absolute left-8 -ml-8 -translate-x-full pr-8 whitespace-nowrap">
                  <span
                    className={`inline-flex items-center gap-1.5 px-2 py-1 rounded-full text-xs font-medium border ${getStatusColor(
                      item.status
                    )}`}
                  >
                    {getStatusIcon(item.status)}
                    {getStatusLabel(item.status)}
                  </span>
                </div>

                <div className="space-y-4">
                  <div>
                    <h2 className="text-2xl font-bold mb-2">{item.title}</h2>
                    <p className="text-base text-muted-foreground leading-relaxed">
                      {item.description}
                    </p>
                  </div>

                  {item.quarter && (
                    <div className="flex items-center gap-2 text-sm text-muted-foreground">
                      <Calendar className="w-4 h-4" />
                      <span>{item.quarter}</span>
                    </div>
                  )}
                </div>
              </article>
            ))}
          </div>
        </div>
      </div>
    </main>
  );
}
