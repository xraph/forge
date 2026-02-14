import Link from "next/link";
import { blog } from "@/lib/source";
import { BookOpen, ArrowRight, Hammer } from "lucide-react";

/* ─── Corner decorators matching feature-bento pattern ─── */
function CardDecorator() {
  return (
    <>
      <span className="border-primary absolute -left-px -top-px block size-2 border-l-2 border-t-2" />
      <span className="border-primary absolute -right-px -top-px block size-2 border-r-2 border-t-2" />
      <span className="border-primary absolute -bottom-px -left-px block size-2 border-b-2 border-l-2" />
      <span className="border-primary absolute -bottom-px -right-px block size-2 border-b-2 border-r-2" />
    </>
  );
}

function formatDate(dateStr: string | Date) {
  return new Date(dateStr).toLocaleDateString("en-US", {
    year: "numeric",
    month: "long",
    day: "numeric",
  });
}

function getCategoryStyle(category: string) {
  switch (category) {
    case "Announcement":
      return "border-amber-500/40 text-amber-600 dark:text-amber-400 bg-amber-500/10";
    case "Tutorial":
      return "border-purple-500/40 text-purple-600 dark:text-purple-400 bg-purple-500/10";
    case "Guide":
      return "border-emerald-500/40 text-emerald-600 dark:text-emerald-400 bg-emerald-500/10";
    default:
      return "border-fd-border text-fd-muted-foreground bg-fd-card";
  }
}

export default function BlogPage() {
  const posts = blog
    .getPages()
    .sort(
      (a, b) =>
        new Date(b.data.date).getTime() - new Date(a.data.date).getTime(),
    );

  return (
    <main className="container max-w-6xl mx-auto px-6 py-16 md:py-24">
      {/* Header — matching changelog/roadmap pattern */}
      <div className="mb-16">
        <div className="inline-flex items-center gap-2 border border-amber-500/30 bg-amber-500/10 px-3 py-1.5 text-xs font-medium text-amber-600 dark:text-amber-400 backdrop-blur-sm mb-4">
          <Hammer className="size-3" />
          {posts.length} articles
        </div>
        <h1 className="text-4xl font-bold mb-4 tracking-tight">Blog</h1>
        <p className="text-lg text-fd-muted-foreground max-w-xl">
          Insights, tutorials, and announcements from the Forge team.
        </p>
      </div>

      {/* Featured post — first post gets special treatment */}
      {posts.length > 0 && (() => {
        const featured = posts[0];
        const gradient = featured.data.coverGradient ?? "from-amber-500/20 via-orange-500/10 to-transparent";
        const tags: string[] = featured.data.tags ?? [];
        const category: string = featured.data.category ?? "General";

        return (
          <Link href={featured.url} className="group block mb-12">
            <article className="relative overflow-hidden border border-fd-border bg-fd-card rounded-none transition-all hover:border-fd-border/80 hover:shadow-lg">
              <CardDecorator />
              <div className="metal-shader absolute inset-0 opacity-30 pointer-events-none" />

              <div className="relative grid lg:grid-cols-[1fr_1fr] gap-0">
                {/* Cover gradient area */}
                <div
                  className={`relative h-48 lg:h-auto min-h-[200px] bg-linear-to-br ${gradient} bg-fd-accent/30`}
                >
                  <div className="absolute inset-0 bg-[radial-gradient(circle_at_30%_40%,rgba(245,158,11,0.12),transparent_60%)]" />
                  <div className="absolute bottom-4 left-4 flex flex-wrap gap-2">
                    {tags.map((tag) => (
                      <span
                        key={tag}
                        className="border border-fd-border/60 bg-fd-background/80 backdrop-blur-sm px-2 py-0.5 text-[10px] font-mono uppercase tracking-wider text-fd-muted-foreground"
                      >
                        {tag}
                      </span>
                    ))}
                  </div>
                </div>

                {/* Content side */}
                <div className="p-8 lg:p-10 flex flex-col justify-center">
                  <div className="flex items-center gap-3 mb-4">
                    <span
                      className={`text-[10px] font-semibold uppercase tracking-wider px-2 py-0.5 border ${getCategoryStyle(category)}`}
                    >
                      {category}
                    </span>
                    <span className="text-xs text-fd-muted-foreground">
                      {formatDate(featured.data.date)}
                    </span>
                  </div>
                  <h2 className="text-2xl lg:text-3xl font-bold tracking-tight mb-3 group-hover:text-amber-600 dark:group-hover:text-amber-400 transition-colors">
                    {featured.data.title}
                  </h2>
                  <p className="text-fd-muted-foreground leading-relaxed mb-6">
                    {featured.data.description}
                  </p>
                  <div className="flex items-center justify-between">
                    <span className="text-sm font-medium">
                      {featured.data.author}
                    </span>
                    <span className="inline-flex items-center gap-1.5 text-sm font-semibold text-amber-600 dark:text-amber-400 group-hover:gap-2.5 transition-all">
                      Read article
                      <ArrowRight className="size-4" />
                    </span>
                  </div>
                </div>
              </div>
            </article>
          </Link>
        );
      })()}

      {/* Remaining posts grid */}
      <div className="grid md:grid-cols-2 lg:grid-cols-3 gap-6">
        {posts.slice(1).map((post) => {
          const gradient = post.data.coverGradient ?? "from-amber-500/20 via-orange-500/10 to-transparent";
          const tags: string[] = post.data.tags ?? [];
          const category: string = post.data.category ?? "General";

          return (
            <Link
              key={post.url}
              href={post.url}
              className="group block"
            >
              <article className="relative h-full flex flex-col rounded-none border border-fd-border bg-fd-card overflow-hidden transition-all hover:border-fd-border/80 hover:shadow-lg">
                <CardDecorator />
                <div className="metal-shader absolute inset-0 opacity-20 pointer-events-none" />

                {/* Cover gradient */}
                <div
                  className={`relative h-36 bg-linear-to-br ${gradient} bg-fd-accent/20`}
                >
                  <div className="absolute bottom-3 left-3 flex flex-wrap gap-1.5">
                    {tags.slice(0, 2).map((tag) => (
                      <span
                        key={tag}
                        className="border border-fd-border/60 bg-fd-background/80 backdrop-blur-sm px-1.5 py-0.5 text-[9px] font-mono uppercase tracking-wider text-fd-muted-foreground"
                      >
                        {tag}
                      </span>
                    ))}
                  </div>
                </div>

                {/* Content */}
                <div className="relative p-6 flex-1 flex flex-col">
                  <div className="flex items-center gap-2 mb-3">
                    <span
                      className={`text-[10px] font-semibold uppercase tracking-wider px-2 py-0.5 border ${getCategoryStyle(category)}`}
                    >
                      {category}
                    </span>
                    <span className="text-xs text-fd-muted-foreground">
                      {formatDate(post.data.date)}
                    </span>
                  </div>

                  <h2 className="text-lg font-semibold mb-2 group-hover:text-amber-600 dark:group-hover:text-amber-400 transition-colors leading-tight">
                    {post.data.title}
                  </h2>
                  <p className="text-sm text-fd-muted-foreground leading-relaxed mb-4 flex-1">
                    {post.data.description}
                  </p>

                  <div className="flex items-center justify-between pt-4 border-t border-dashed border-fd-border">
                    <div className="flex items-center gap-2">
                      <BookOpen className="size-3 text-fd-muted-foreground" />
                      <span className="text-xs text-fd-muted-foreground">
                        {post.data.author}
                      </span>
                    </div>
                    <span className="inline-flex items-center gap-1 text-xs font-medium text-amber-600 dark:text-amber-400">
                      Read
                      <ArrowRight className="size-3" />
                    </span>
                  </div>
                </div>
              </article>
            </Link>
          );
        })}
      </div>
    </main>
  );
}
