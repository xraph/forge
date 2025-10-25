import { Star, GitFork, Eye, Users } from "lucide-react";

const stats = [
  { icon: Star, label: "Stars", value: "50.2k", trend: "+12% this month" },
  { icon: GitFork, label: "Forks", value: "8.3k", trend: "+8% this month" },
  { icon: Eye, label: "Watchers", value: "2.1k", trend: "Active monitoring" },
  { icon: Users, label: "Contributors", value: "523", trend: "Growing community" }
];

export const GitHubStats = ({noTopBorder = false}) => {
  return (
    <section className={`relative py-10 ${noTopBorder ? 'border-b border-border' : 'border-y '} bg-secondary/20`}>
      <div className="container mx-auto px-4 sm:px-6 lg:px-8">
        <div className="grid sm:grid-cols-2 lg:grid-cols-4 gap-8">
          {stats.map((stat, index) => (
            <div key={index} className="text-center">
              <div className="inline-flex p-3 rounded-xl bg-primary/10 border border-primary/20 mb-4">
                <stat.icon className="w-6 h-6 text-primary" />
              </div>
              <div className="text-3xl font-black mb-1">{stat.value}</div>
              <div className="text-sm font-semibold mb-2">{stat.label}</div>
              <div className="text-xs text-muted-foreground">{stat.trend}</div>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
};
