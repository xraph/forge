import { useState } from "react";
import { Check, Clock, Sparkles } from "lucide-react";
import { Button } from "@/components/ui/button";
import { changelog, roadmap } from "@/constants/changelog";

export const ChangelogRoadmap = () => {
  const [activeTab, setActiveTab] = useState<"changelog" | "roadmap">("changelog");

  const getStatusIcon = (status: string) => {
    switch (status) {
      case "latest": return <Sparkles className="w-4 h-4 text-yellow-500" />;
      case "in-progress": return <Clock className="w-4 h-4 text-blue-500" />;
      case "stable": return <Check className="w-4 h-4 text-green-500" />;
      default: return <Clock className="w-4 h-4 text-gray-500" />;
    }
  };

  return (
    <section className="relative py-32 bg-secondary/30">
      <div className="container mx-auto px-4 sm:px-6 lg:px-8">
        <div className="text-center max-w-3xl mx-auto mb-20">
          <h2 className="text-5xl sm:text-6xl font-black mb-6">
            What's new & next.
          </h2>
          <p className="text-xl text-muted-foreground">
            Track our progress and see what's coming to Forge.
          </p>
        </div>

        <div className="max-w-4xl mx-auto">
          <div className="flex gap-4 mb-12 justify-center">
            <Button
              variant={activeTab === "changelog" ? "default" : "outline"}
              onClick={() => setActiveTab("changelog")}
              size="lg"
            >
              Changelog
            </Button>
            <Button
              variant={activeTab === "roadmap" ? "default" : "outline"}
              onClick={() => setActiveTab("roadmap")}
              size="lg"
            >
              Roadmap
            </Button>
          </div>

          {activeTab === "changelog" ? (
            <div className="space-y-8">
              {changelog.map((release, index) => (
                <div key={index} className="p-8 rounded-2xl border border-border bg-card">
                  <div className="flex items-center gap-3 mb-4">
                    {getStatusIcon(release.status)}
                    <span className="font-mono text-sm font-semibold">{release.version}</span>
                    <span className="text-sm text-muted-foreground">{release.date}</span>
                    {release.status === "latest" && (
                      <span className="px-2 py-1 bg-yellow-500/10 text-yellow-600 text-xs font-semibold rounded-full border border-yellow-500/20">
                        Latest
                      </span>
                    )}
                  </div>
                  <h3 className="text-2xl font-bold mb-4">{release.title}</h3>
                  <ul className="space-y-2">
                    {release.items.map((item, i) => (
                      <li key={i} className="flex gap-3">
                        <span className="text-primary mt-1">•</span>
                        <span className="text-muted-foreground">{item}</span>
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </div>
          ) : (
            <div className="space-y-8">
              {roadmap.map((quarter, index) => (
                <div key={index} className="p-8 rounded-2xl border border-border bg-card">
                  <div className="flex items-center gap-3 mb-6">
                    {getStatusIcon(quarter.status)}
                    <span className="text-xl font-black">{quarter.quarter}</span>
                    <span className="px-2 py-1 bg-secondary text-xs font-semibold rounded-full capitalize">
                      {quarter.status.replace("-", " ")}
                    </span>
                  </div>
                  <div className="space-y-4">
                    {quarter.items.map((item, i) => (
                      <div key={i} className="p-4 rounded-xl bg-secondary/50">
                        <h4 className="font-bold mb-2">{item.title}</h4>
                        <p className="text-sm text-muted-foreground">{item.description}</p>
                      </div>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </section>
  );
};
