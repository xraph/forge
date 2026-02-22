import { Puzzle, Star } from "lucide-react";
import {
  FORGERY_EXTENSIONS,
  CATEGORY_ORDER,
  CATEGORY_STYLE,
} from "@/lib/forgery-extensions";
import { fetchAllRepoData } from "@/lib/forgery-github";
import { ForgeryGrid } from "@/components/forgery/forgery-grid";

export const revalidate = 3600;

export const metadata = {
  title: "Forgery — Ecosystem Extensions",
  description:
    "Open-source Go libraries built alongside Forge. From authentication to AI agents, billing to webhooks.",
};

export default async function ForgeryPage() {
  const githubData = await fetchAllRepoData(
    FORGERY_EXTENSIONS.map((e) => ({
      slug: e.slug,
      fallbackStars: e.fallbackStars,
      fallbackVersion: e.fallbackVersion,
      description: e.description,
    })),
  );

  const enrichedExtensions = FORGERY_EXTENSIONS.map((ext) => {
    const live = githubData.get(ext.slug);
    return {
      slug: ext.slug,
      name: ext.name,
      description: live?.description ?? ext.description,
      category: ext.category,
      iconBg: ext.iconBg,
      iconColor: ext.iconColor,
      gradient: ext.gradient,
      stars: live?.stars ?? ext.fallbackStars,
      latestVersion: live?.latestVersion ?? ext.fallbackVersion,
      githubUrl: `https://github.com/xraph/${ext.slug}`,
      modulePath: ext.modulePath,
    };
  });

  const totalStars = enrichedExtensions.reduce((sum, e) => sum + e.stars, 0);

  return (
    <main className="container max-w-6xl mx-auto px-6 py-16 md:py-24">
      {/* Header */}
      <div className="mb-16">
        <div className="inline-flex items-center gap-2 border border-fd-border bg-fd-card/80 backdrop-blur-sm px-3 py-1.5 text-xs font-medium text-fd-muted-foreground mb-4">
          <Puzzle className="size-3" />
          {FORGERY_EXTENSIONS.length} ecosystem libraries
        </div>
        <h1 className="text-4xl font-bold mb-4 tracking-tight">Forgery</h1>
        <p className="text-lg text-fd-muted-foreground max-w-2xl">
          Standalone Go libraries built alongside Forge, each solving a specific
          infrastructure problem. Use them with Forge or completely standalone
          &mdash; no framework lock-in.
        </p>

        {totalStars > 0 && (
          <div className="mt-6 inline-flex items-center gap-3">
            <div className="flex items-center gap-1.5 border border-fd-border bg-fd-card/60 px-3 py-1.5 text-sm">
              <Star className="size-3.5 text-amber-500" />
              <span className="font-mono font-semibold">
                {totalStars.toLocaleString()}
              </span>
              <span className="text-fd-muted-foreground text-xs">
                total stars
              </span>
            </div>
          </div>
        )}
      </div>

      {/* Category filter + animated grid */}
      <ForgeryGrid
        extensions={enrichedExtensions}
        categoryOrder={CATEGORY_ORDER}
        categoryStyle={CATEGORY_STYLE}
      />
    </main>
  );
}
