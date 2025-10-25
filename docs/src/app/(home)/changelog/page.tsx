import { Wrench } from "lucide-react";
import { SectionHeader } from "@/components/ui/section-header";
import { changelogEntries } from "@/constants/changelog";


export default function ChangelogPage() {
  return (
    <main className="pt-20 pb-20">
      <div className="container mx-auto px-4 sm:px-6 lg:px-8">
        <SectionHeader
          label="CHANGELOG"
          title="What's new in Forge"
          description="Latest updates, improvements, and bug fixes"
        />

        <div className="max-w-4xl mx-auto">
          {changelogEntries.map((entry, index) => (
            <article
              key={index}
              className="relative pl-8 pb-16 border-l-2 border-border last:border-l-0 last:pb-0"
            >
              {/* Date marker */}
              <div className="absolute left-0 -translate-x-1/2 w-3 h-3 rounded-full bg-primary ring-4 ring-background" />

              <time className="absolute left-8 -ml-8 -translate-x-full pr-8 text-sm text-muted-foreground whitespace-nowrap">
                {entry.date}
              </time>

              <div className="space-y-6">
                <div>
                  <h2 className="text-3xl font-bold mb-3">{entry.title}</h2>
                  <p className="text-lg text-muted-foreground leading-relaxed">
                    {entry.description}
                  </p>
                </div>

                {/* Improvements */}
                {entry.improvements && entry.improvements.length > 0 && (
                  <div>
                    <div className="flex items-center gap-2 mb-4">
                      <Wrench className="w-5 h-5 text-primary" />
                      <h3 className="text-lg font-semibold">Improvements</h3>
                    </div>
                    <ul className="space-y-2">
                      {entry.improvements.map((improvement, i) => (
                        <li
                          key={i}
                          className="flex gap-3 text-muted-foreground"
                        >
                          <span className="text-primary mt-1.5">•</span>
                          <span>{improvement}</span>
                        </li>
                      ))}
                    </ul>
                  </div>
                )}

                {/* Bug Fixes */}
                {entry.bugFixes && entry.bugFixes.length > 0 && (
                  <div>
                    <h3 className="text-lg font-semibold mb-4">Bug Fixes</h3>
                    <ul className="space-y-2">
                      {entry.bugFixes.map((fix, i) => (
                        <li
                          key={i}
                          className="flex gap-3 text-muted-foreground"
                        >
                          <span className="text-primary mt-1.5">•</span>
                          <span>{fix}</span>
                        </li>
                      ))}
                    </ul>
                  </div>
                )}
              </div>
            </article>
          ))}
        </div>
      </div>
    </main>
  );
}
