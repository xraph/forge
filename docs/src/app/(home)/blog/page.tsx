import Link from "next/link";

const posts = [
  {
    slug: "introducing-forge",
    title: "Introducing Forge: The Production-Grade Go Framework",
    excerpt:
      "Today we are announcing Forge, a new framework designed to help you build scalable backend services in Go with ease.",
    date: "2025-01-01",
    author: "Rex Raphael",
    readTime: "5 min read",
  },
  {
    slug: "dependency-injection-in-go",
    title: "Mastering Dependency Injection in Go",
    excerpt:
      "Learn how to leverage Forge's type-safe dependency injection container to write clean, testable code.",
    date: "2025-01-10",
    author: "Forge Team",
    readTime: "8 min read",
  },
  {
    slug: "building-extensions",
    title: "Building Custom Extensions for Forge",
    excerpt:
      "A comprehensive guide to extending Forge's capabilities with your own custom modules.",
    date: "2025-01-20",
    author: "Rex Raphael",
    readTime: "6 min read",
  },
];

export default function BlogPage() {
  return (
    <main className="container max-w-6xl mx-auto px-6 py-16 md:py-24">
      <div className="mb-12 text-center max-w-2xl mx-auto">
        <h1 className="text-4xl font-bold mb-4">Blog</h1>
        <p className="text-lg text-fd-muted-foreground">
          Insights, tutorials, and announcements from the Forge team.
        </p>
      </div>

      <div className="grid md:grid-cols-2 lg:grid-cols-3 gap-8">
        {posts.map((post) => (
          <Link
            key={post.slug}
            href={`/blog/${post.slug}`} // Note: Actual blog post pages not implemented in this task
            className="group block"
          >
            <article className="h-full flex flex-col rounded-2xl border border-fd-border bg-fd-card overflow-hidden transition-all hover:border-fd-border/80 hover:shadow-lg">
              <div className="h-48 bg-fd-accent/50 group-hover:bg-fd-accent transition-colors" />{" "}
              {/* Placeholder image */}
              <div className="p-6 flex-1 flex flex-col">
                <div className="flex items-center gap-2 text-xs text-fd-muted-foreground mb-3">
                  <time dateTime={post.date}>{post.date}</time>
                  <span>•</span>
                  <span>{post.readTime}</span>
                </div>
                <h2 className="text-xl font-semibold mb-3 group-hover:text-amber-600 dark:group-hover:text-amber-400 transition-colors">
                  {post.title}
                </h2>
                <p className="text-sm text-fd-muted-foreground leading-relaxed mb-4 flex-1">
                  {post.excerpt}
                </p>
                <div className="text-sm font-medium">{post.author}</div>
              </div>
            </article>
          </Link>
        ))}
      </div>
    </main>
  );
}
