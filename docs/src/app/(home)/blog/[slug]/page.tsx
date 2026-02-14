import { notFound } from "next/navigation";
import Link from "next/link";
import type { Metadata } from "next";
import {
  ArrowLeft,
  ArrowRight,
  BookOpen,
  Calendar,
  User,
} from "lucide-react";
import { blog } from "@/lib/source";
import defaultMdxComponents from "fumadocs-ui/mdx";
import { InlineTOC } from "fumadocs-ui/components/inline-toc";

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

/* ─── Static generation ─── */
export function generateStaticParams(): { slug: string }[] {
  return blog.getPages().map((page) => ({
    slug: page.slugs[0],
  }));
}

/* ─── SEO Metadata ─── */
export async function generateMetadata(props: {
  params: Promise<{ slug: string }>;
}): Promise<Metadata> {
  const params = await props.params;
  const page = blog.getPage([params.slug]);
  if (!page) return { title: "Post Not Found" };

  return {
    title: `${page.data.title} — Forge Blog`,
    description: page.data.description,
    openGraph: {
      title: page.data.title,
      description: page.data.description,
      type: "article",
      publishedTime:
        typeof page.data.date === "string"
          ? page.data.date
          : page.data.date.toISOString(),
      authors: [page.data.author],
    },
  };
}

/* ─── Main page component ─── */
export default async function BlogPostPage(props: {
  params: Promise<{ slug: string }>;
}) {
  const params = await props.params;
  const page = blog.getPage([params.slug]);
  if (!page) notFound();

  const Mdx = page.data.body;
  const category: string = page.data.category ?? "General";
  const tags: string[] = page.data.tags ?? [];

  const allPosts = blog
    .getPages()
    .sort(
      (a, b) =>
        new Date(b.data.date).getTime() - new Date(a.data.date).getTime(),
    );
  const related = allPosts
    .filter((p) => p.slugs[0] !== params.slug)
    .slice(0, 3);

  return (
    <main className="container max-w-4xl mx-auto px-6 py-16 md:py-24">
      {/* Back navigation */}
      <Link
        href="/blog"
        className="inline-flex items-center gap-2 text-sm text-fd-muted-foreground hover:text-fd-foreground transition-colors mb-10"
      >
        <ArrowLeft className="size-3.5" />
        Back to Blog
      </Link>

      {/* Article header */}
      <header className="mb-12">
        <div className="flex items-center gap-3 mb-5">
          <span
            className={`text-[10px] font-semibold uppercase tracking-wider px-2 py-0.5 border ${getCategoryStyle(category)}`}
          >
            {category}
          </span>
          <div className="flex items-center gap-1.5 text-xs text-fd-muted-foreground">
            <Calendar className="size-3" />
            {formatDate(page.data.date)}
          </div>
        </div>

        <h1 className="text-3xl md:text-4xl font-bold tracking-tight mb-5 leading-tight">
          {page.data.title}
        </h1>

        <p className="text-lg text-fd-muted-foreground leading-relaxed mb-6">
          {page.data.description}
        </p>

        <div className="flex items-center justify-between border-t border-b border-fd-border py-4">
          <div className="flex items-center gap-2">
            <User className="size-3.5 text-fd-muted-foreground" />
            <span className="text-sm font-medium">{page.data.author}</span>
          </div>
          <div className="flex flex-wrap gap-1.5">
            {tags.map((tag) => (
              <span
                key={tag}
                className="border border-fd-border bg-fd-card px-2 py-0.5 text-[10px] font-mono uppercase tracking-wider text-fd-muted-foreground"
              >
                {tag}
              </span>
            ))}
          </div>
        </div>
      </header>

      {/* Table of contents */}
      {page.data.toc && page.data.toc.length > 0 && (
        <div className="mb-10 relative border border-fd-border rounded-none p-5 bg-fd-card">
          <CardDecorator />
          <InlineTOC items={page.data.toc} />
        </div>
      )}

      {/* Article body — rendered from MDX */}
      <article className="prose prose-neutral dark:prose-invert max-w-none prose-headings:tracking-tight prose-headings:font-bold prose-code:before:content-none prose-code:after:content-none prose-pre:rounded-none prose-pre:border prose-pre:border-fd-border">
        <Mdx components={{ ...defaultMdxComponents }} />
      </article>

      {/* Separator */}
      <div className="my-16 h-px bg-linear-to-r from-transparent via-fd-border to-transparent" />

      {/* Related posts */}
      {related.length > 0 && (
        <section>
          <div className="flex items-center gap-2 mb-6">
            <BookOpen className="size-4 text-fd-muted-foreground" />
            <h2 className="text-lg font-bold tracking-tight">
              Related Articles
            </h2>
          </div>

          <div className="grid md:grid-cols-3 gap-4">
            {related.map((relatedPost) => {
              const relCategory: string =
                relatedPost.data.category ?? "General";

              return (
                <Link
                  key={relatedPost.url}
                  href={relatedPost.url}
                  className="group block"
                >
                  <article className="relative h-full border border-fd-border bg-fd-card rounded-none p-5 transition-all hover:border-fd-border/80 hover:shadow-lg overflow-hidden">
                    <CardDecorator />
                    <div className="metal-shader absolute inset-0 opacity-15 pointer-events-none" />

                    <div className="relative">
                      <div className="flex items-center gap-2 mb-3">
                        <span
                          className={`text-[9px] font-semibold uppercase tracking-wider px-1.5 py-0.5 border ${getCategoryStyle(relCategory)}`}
                        >
                          {relCategory}
                        </span>
                        <span className="text-[10px] text-fd-muted-foreground">
                          {formatDate(relatedPost.data.date)}
                        </span>
                      </div>

                      <h3 className="text-sm font-semibold mb-2 group-hover:text-amber-600 dark:group-hover:text-amber-400 transition-colors leading-snug">
                        {relatedPost.data.title}
                      </h3>

                      <span className="inline-flex items-center gap-1 text-xs font-medium text-amber-600 dark:text-amber-400 group-hover:gap-2 transition-all">
                        Read
                        <ArrowRight className="size-3" />
                      </span>
                    </div>
                  </article>
                </Link>
              );
            })}
          </div>
        </section>
      )}
    </main>
  );
}
