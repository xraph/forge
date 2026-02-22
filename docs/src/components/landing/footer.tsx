"use client";

import Link from "next/link";
import { ThemedLogo } from "@/components/ui/themed-logo";

const footerLinks = {
  Framework: [
    { label: "Getting Started", href: "/docs/forge" },
    { label: "Configuration", href: "/docs/forge/configuration" },
    { label: "Examples", href: "/docs/forge/examples" },
    { label: "Architecture", href: "/docs/forge/architecture" },
    { label: "Changelog", href: "/changelog" },
  ],
  Extensions: [
    { label: "Extension Catalog", href: "/docs/extensions" },
    { label: "Database", href: "/docs/extensions/database" },
    { label: "Gateway", href: "/docs/extensions/gateway" },
    { label: "AI SDK", href: "/docs/ai-sdk" },
    { label: "gRPC", href: "/docs/extensions/grpc" },
  ],
  Ecosystem: [
    { label: "Vessel DI", href: "/docs/vessel" },
    { label: "CLI Framework", href: "/docs/cli" },
    { label: "FARP Protocol", href: "/docs/farp" },
    { label: "Service Discovery", href: "/docs/farp/discovery" },
    { label: "Roadmap", href: "/roadmap" },
    { label: "Forgery", href: "/forgery" },
  ],
  Community: [
    { label: "GitHub", href: "https://github.com/xraph/forge", external: true },
    { label: "Blog", href: "/blog" },
    { label: "Contributing", href: "https://github.com/xraph/forge/blob/main/CONTRIBUTING.md", external: true },
    { label: "Issues", href: "https://github.com/xraph/forge/issues", external: true },
    { label: "Discussions", href: "https://github.com/xraph/forge/discussions", external: true },
  ],
};

export function Footer() {
  return (
    <footer className="w-full border-t border-fd-border bg-fd-card/50">
      <div className="container max-w-(--fd-layout-width) mx-auto px-4 sm:px-6">
        {/* Main footer grid */}
        <div className="grid grid-cols-2 gap-8 py-12 sm:py-16 md:grid-cols-5 lg:gap-12">
          {/* Brand column */}
          <div className="col-span-2 md:col-span-1">
            <Link href="/" className="inline-flex items-center gap-2 mb-4">
              <ThemedLogo />
              <span className="font-bold text-lg">Forge</span>
            </Link>
            <p className="text-sm text-fd-muted-foreground leading-relaxed max-w-xs">
              The production-grade Go framework for building scalable backend
              services with modular extensions.
            </p>
            {/* Social links */}
            <div className="flex items-center gap-3 mt-6">
              <a
                href="https://github.com/xraph/forge"
                target="_blank"
                rel="noreferrer"
                className="text-fd-muted-foreground hover:text-fd-foreground transition-colors"
                aria-label="GitHub"
              >
                <svg className="size-5" fill="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                  <path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.286-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12" />
                </svg>
              </a>
              <a
                href="https://x.com/xraph"
                target="_blank"
                rel="noreferrer"
                className="text-fd-muted-foreground hover:text-fd-foreground transition-colors"
                aria-label="X (Twitter)"
              >
                <svg className="size-5" fill="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                  <path d="M18.244 2.25h3.308l-7.227 8.26 8.502 11.24H16.17l-5.214-6.817L4.99 21.75H1.68l7.73-8.835L1.254 2.25H8.08l4.713 6.231zm-1.161 17.52h1.833L7.084 4.126H5.117z" />
                </svg>
              </a>
              <a
                href="https://discord.gg/xraph"
                target="_blank"
                rel="noreferrer"
                className="text-fd-muted-foreground hover:text-fd-foreground transition-colors"
                aria-label="Discord"
              >
                <svg className="size-5" fill="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                  <path d="M20.317 4.37a19.791 19.791 0 0 0-4.885-1.515.074.074 0 0 0-.079.037c-.21.375-.444.864-.608 1.25a18.27 18.27 0 0 0-5.487 0 12.64 12.64 0 0 0-.617-1.25.077.077 0 0 0-.079-.037A19.736 19.736 0 0 0 3.677 4.37a.07.07 0 0 0-.032.027C.533 9.046-.32 13.58.099 18.057a.082.082 0 0 0 .031.057 19.9 19.9 0 0 0 5.993 3.03.078.078 0 0 0 .084-.028 14.09 14.09 0 0 0 1.226-1.994.076.076 0 0 0-.041-.106 13.107 13.107 0 0 1-1.872-.892.077.077 0 0 1-.008-.128 10.2 10.2 0 0 0 .372-.292.074.074 0 0 1 .077-.01c3.928 1.793 8.18 1.793 12.062 0a.074.074 0 0 1 .078.01c.12.098.246.198.373.292a.077.077 0 0 1-.006.127 12.299 12.299 0 0 1-1.873.892.077.077 0 0 0-.041.107c.36.698.772 1.362 1.225 1.993a.076.076 0 0 0 .084.028 19.839 19.839 0 0 0 6.002-3.03.077.077 0 0 0 .032-.054c.5-5.177-.838-9.674-3.549-13.66a.061.061 0 0 0-.031-.03zM8.02 15.33c-1.183 0-2.157-1.085-2.157-2.419 0-1.333.956-2.419 2.157-2.419 1.21 0 2.176 1.096 2.157 2.42 0 1.333-.956 2.418-2.157 2.418zm7.975 0c-1.183 0-2.157-1.085-2.157-2.419 0-1.333.955-2.419 2.157-2.419 1.21 0 2.176 1.096 2.157 2.42 0 1.333-.946 2.418-2.157 2.418z" />
                </svg>
              </a>
            </div>
          </div>

          {/* Link columns */}
          {Object.entries(footerLinks).map(([category, links]) => (
            <div key={category}>
              <h3 className="text-sm font-semibold text-fd-foreground mb-4">
                {category}
              </h3>
              <ul className="space-y-2.5">
                {links.map((link) => (
                  <li key={link.label}>
                    {"external" in link && link.external ? (
                      <a
                        href={link.href}
                        target="_blank"
                        rel="noreferrer"
                        className="text-sm text-fd-muted-foreground hover:text-fd-foreground transition-colors"
                      >
                        {link.label}
                      </a>
                    ) : (
                      <Link
                        href={link.href}
                        className="text-sm text-fd-muted-foreground hover:text-fd-foreground transition-colors"
                      >
                        {link.label}
                      </Link>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>

        {/* Bottom bar */}
        <div className="border-t border-fd-border py-6 flex flex-col sm:flex-row items-center justify-between gap-4">
          <p className="text-xs text-fd-muted-foreground">
            &copy; {new Date().getFullYear()} Xraph. All rights reserved.
          </p>
          <div className="flex items-center gap-1 text-xs text-fd-muted-foreground">
            <span>Built with</span>
            <span className="inline-block text-amber-500 mx-0.5">
              <svg className="size-3.5 inline-block" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
                <path d="M11.645 20.91l-.007-.003-.022-.012a15.247 15.247 0 01-.383-.218 25.18 25.18 0 01-4.244-3.17C4.688 15.36 2.25 12.174 2.25 8.25 2.25 5.322 4.714 3 7.688 3A5.5 5.5 0 0112 5.052 5.5 5.5 0 0116.313 3c2.973 0 5.437 2.322 5.437 5.25 0 3.925-2.438 7.111-4.739 9.256a25.175 25.175 0 01-4.244 3.17 15.247 15.247 0 01-.383.219l-.022.012-.007.004-.003.001a.752.752 0 01-.704 0l-.003-.001z" />
              </svg>
            </span>
            <span>and Go</span>
          </div>
        </div>
      </div>
    </footer>
  );
}
