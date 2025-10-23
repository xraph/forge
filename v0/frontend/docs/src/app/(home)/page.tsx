import Link from "next/link";
import {
  ArrowRight,
  Shield,
  Zap,
  Users,
  Code,
  Globe,
  Lock,
} from "lucide-react";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import { cn } from "@/lib/utils";
import { ReactNode } from "react";
import { LineShadowText } from "@/components/ui/line-shadow-text";
// import { useTheme } from "next-themes";

/**
 * Hero Section Component
 * Modern hero with gradient background and call-to-action
 */
function HeroSection() {
  // const theme = useTheme()
  // const shadowColor = theme.resolvedTheme === "dark" ? "white" : "black"
  return (
    <section className="relative overflow-hidden bg-gradient-to-br from-background via-background to-muted/20 py-24 sm:py-32">
      <div className="absolute inset-0 bg-grid-white/[0.02] bg-[size:60px_60px]" />
      <div className="relative mx-auto max-w-7xl px-6 lg:px-8">
        <div className="mx-auto max-w-2xl text-center">
          <Badge variant="outline" className="mb-4">
            <Zap className="mr-1 h-3 w-3" />
            Enterprise-Grade Authentication
          </Badge>
          <h1 className="text-4xl font-bold tracking-tight text-foreground sm:text-6xl">
            Auth
            <LineShadowText className="italic" shadowColor='black'>
            some
          </LineShadowText>
          </h1>
          <p className="mt-6 text-lg leading-8 text-muted-foreground">
            A comprehensive, pluggable authentication framework for Go
            applications. Built for enterprise with multi-tenancy, RBAC, and 12+
            authentication methods.
          </p>
          <div className="mt-10 flex items-center justify-center gap-x-6">
            <Link
              href="/portal"
              className="rounded-md bg-brand px-3.5 py-2.5 text-sm font-semibold text-brand-foreground shadow-sm hover:bg-brand/90 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand"
            >
              Get Started
              <ArrowRight className="ml-2 h-4 w-4 inline" />
            </Link>
            <Link
              href="/docs/go/examples"
              className="text-sm font-semibold leading-6 text-foreground hover:text-brand"
            >
              View Examples <span aria-hidden="true">→</span>
            </Link>
          </div>
        </div>
      </div>
    </section>
  );
}

/**
 * Feature Cards Section with Aceternity UI Bento Grid
 * Showcases key capabilities of AuthSome with glassmorphism effects and hover animations
 */
function FeaturesSection() {
  const features = [
    {
      icon: Shield,
      title: "Enterprise Security",
      description:
        "Built-in security features including rate limiting, device tracking, and audit logging with comprehensive threat detection.",
      className: "md:col-span-2",
      header: (
        <div className="flex h-full min-h-[6rem] w-full flex-1 rounded-xl bg-gradient-to-br from-blue-500/10 via-purple-500/10 to-pink-500/10 dark:from-blue-500/20 dark:via-purple-500/20 dark:to-pink-500/20" />
      ),
    },
    {
      icon: Users,
      title: "Multi-Tenancy",
      description:
        "Organization-scoped configurations and user management with seamless tenant isolation.",
      className: "md:col-span-1",
      header: (
        <div className="flex h-full min-h-[6rem] w-full flex-1 rounded-xl bg-gradient-to-br from-green-500/10 via-emerald-500/10 to-teal-500/10 dark:from-green-500/20 dark:via-emerald-500/20 dark:to-teal-500/20" />
      ),
    },
    {
      icon: Code,
      title: "Plugin Architecture",
      description:
        "12+ authentication methods via extensible plugin system. Add custom auth flows easily.",
      className: "md:col-span-1",
      header: (
        <div className="flex h-full min-h-[6rem] w-full flex-1 rounded-xl bg-gradient-to-br from-orange-500/10 via-red-500/10 to-pink-500/10 dark:from-orange-500/20 dark:via-red-500/20 dark:to-pink-500/20" />
      ),
    },
    {
      icon: Globe,
      title: "Framework Agnostic",
      description:
        "Mounts on Forge framework but designed to work with any Go web framework.",
      className: "md:col-span-2",
      header: (
        <div className="flex h-full min-h-[6rem] w-full flex-1 rounded-xl bg-gradient-to-br from-cyan-500/10 via-blue-500/10 to-indigo-500/10 dark:from-cyan-500/20 dark:via-blue-500/20 dark:to-indigo-500/20" />
      ),
    },
    {
      icon: Lock,
      title: "RBAC & Policies",
      description:
        "Role-based access control with policy language for fine-grained permissions.",
      className: "md:col-span-1",
      header: (
        <div className="flex h-full min-h-[6rem] w-full flex-1 rounded-xl bg-gradient-to-br from-violet-500/10 via-purple-500/10 to-fuchsia-500/10 dark:from-violet-500/20 dark:via-purple-500/20 dark:to-fuchsia-500/20" />
      ),
    },
    {
      icon: Zap,
      title: "High Performance",
      description:
        "Session caching with Redis, connection pooling, and optimized database queries for lightning-fast responses.",
      className: "md:col-span-2",
      header: (
        <div className="flex h-full min-h-[6rem] w-full flex-1 rounded-xl bg-gradient-to-br from-yellow-500/10 via-orange-500/10 to-red-500/10 dark:from-yellow-500/20 dark:via-orange-500/20 dark:to-red-500/20" />
      ),
    },
  ];

  return (
    <section className="py-24 sm:py-32 relative">
      {/* Background with subtle pattern */}
      <div className="absolute inset-0 bg-gradient-to-br from-background via-background/95 to-muted/20" />
      <div className="absolute inset-0 bg-[radial-gradient(circle_at_50%_50%,rgba(120,119,198,0.1),transparent_50%)]" />

      <div className="relative mx-auto max-w-7xl px-6 lg:px-8">
        <div className="mx-auto max-w-2xl text-center">
          <h2 className="text-3xl font-bold tracking-tight text-foreground sm:text-4xl bg-gradient-to-r from-foreground to-foreground/70 bg-clip-text">
            Everything you need for authentication
          </h2>
          <p className="mt-4 text-lg text-muted-foreground">
            From simple username/password to enterprise SSO, AuthSome provides
            all the tools you need.
          </p>
        </div>

        {/* Aceternity UI Bento Grid */}
        <div className="mx-auto mt-16 grid max-w-7xl grid-cols-1 gap-4 sm:mt-20 md:auto-rows-[18rem] md:grid-cols-3">
          {features.map((feature, index) => (
            <div
              key={index}
              className={cn(
                "group/bento row-span-1 flex flex-col justify-between space-y-4 rounded-xl border border-transparent bg-white p-4 shadow-input transition duration-200 hover:shadow-xl dark:border-white/[0.2] dark:bg-black dark:shadow-none",
                feature.className
              )}
            >
              {/* Header with gradient background */}
              {feature.header}

              {/* Content with hover animation */}
              <div className="transition duration-200 group-hover/bento:translate-x-2">
                {/* Icon */}
                <div className="mb-2">
                  <feature.icon className="h-4 w-4 text-neutral-500 dark:text-neutral-400" />
                </div>

                {/* Title */}
                <div className="mb-2 mt-2 font-sans font-bold text-neutral-600 dark:text-neutral-200">
                  {feature.title}
                </div>

                {/* Description */}
                <div className="font-sans text-xs font-normal text-neutral-600 dark:text-neutral-300">
                  {feature.description}
                </div>
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

/**
 * Quick Start Section
 * Shows installation and basic usage
 */
function QuickStartSection() {
  return (
    <section className="py-24 sm:py-32 bg-muted/30">
      <div className="mx-auto max-w-7xl px-6 lg:px-8">
        <div className="mx-auto max-w-2xl text-center">
          <h2 className="text-3xl font-bold tracking-tight text-foreground sm:text-4xl">
            Get started in minutes
          </h2>
          <p className="mt-4 text-lg text-muted-foreground">
            Add enterprise-grade authentication to your Go application with just
            a few lines of code.
          </p>
        </div>
        <div className="mx-auto mt-16 max-w-4xl">
          <div className="grid gap-8 lg:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <span className="flex h-6 w-6 items-center justify-center rounded-full bg-brand text-xs font-bold text-brand-foreground">
                    1
                  </span>
                  Install AuthSome
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div className="rounded-lg bg-muted p-4">
                  <code className="text-sm">
                    go get github.com/xraph/authsome
                  </code>
                </div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <span className="flex h-6 w-6 items-center justify-center rounded-full bg-brand text-xs font-bold text-brand-foreground">
                    2
                  </span>
                  Mount to Forge
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div className="rounded-lg bg-muted p-4">
                  <code className="text-sm">
                    auth := authsome.New(config)
                    <br />
                    app.Mount("/auth", auth)
                  </code>
                </div>
              </CardContent>
            </Card>
          </div>
          <div className="mt-8 text-center">
            <Link
              href="/docs/go/getting-started"
              className="inline-flex items-center rounded-md bg-brand px-4 py-2 text-sm font-semibold text-brand-foreground shadow-sm hover:bg-brand/90"
            >
              View Full Tutorial
              <ArrowRight className="ml-2 h-4 w-4" />
            </Link>
          </div>
        </div>
      </div>
    </section>
  );
}

/**
 * Navigation Cards Section
 * Main documentation sections with visual cards
 */
function NavigationSection() {
  const sections = [
    {
      title: "Getting Started",
      description:
        "Installation, configuration, and your first authentication flow.",
      href: "/docs/go/getting-started",
      icon: "🚀",
    },
    {
      title: "Core Concepts",
      description:
        "Understanding users, sessions, organizations, and multi-tenancy.",
      href: "/docs/go/concepts",
      icon: "🧠",
    },
    {
      title: "Plugins",
      description:
        "Explore 12+ authentication methods and how to create custom plugins.",
      href: "/docs/go/plugins",
      icon: "🔌",
    },
    {
      title: "API Reference",
      description: "Complete API documentation for all services and handlers.",
      href: "/docs/go/api",
      icon: "📚",
    },
    {
      title: "Guides",
      description:
        "Step-by-step tutorials for common authentication scenarios.",
      href: "/docs/go/guides",
      icon: "📖",
    },
    {
      title: "Examples",
      description: "Real-world examples and sample applications.",
      href: "/docs/go/examples",
      icon: "💡",
    },
  ];

  return (
    <section className="py-24 sm:py-32">
      <div className="mx-auto max-w-7xl px-6 lg:px-8">
        <div className="mx-auto max-w-2xl text-center">
          <h2 className="text-3xl font-bold tracking-tight text-foreground sm:text-4xl">
            Explore the documentation
          </h2>
          <p className="mt-4 text-lg text-muted-foreground">
            Everything you need to build secure, scalable authentication
            systems.
          </p>
        </div>
        <div className="mx-auto mt-16 grid max-w-2xl grid-cols-1 gap-6 sm:mt-20 lg:mx-0 lg:max-w-none lg:grid-cols-3">
          {sections.map((section, index) => (
            <Link key={index} href={section.href} className="group">
              <Card className="h-full transition-all duration-200 hover:shadow-lg hover:border-brand/50 group-hover:scale-[1.02]">
                <CardHeader>
                  <div className="flex items-center gap-3">
                    <span className="text-2xl">{section.icon}</span>
                    <CardTitle className="text-xl group-hover:text-brand transition-colors">
                      {section.title}
                    </CardTitle>
                  </div>
                </CardHeader>
                <CardContent>
                  <CardDescription className="text-base">
                    {section.description}
                  </CardDescription>
                </CardContent>
              </Card>
            </Link>
          ))}
        </div>
      </div>
    </section>
  );
}

/**
 * Community Section
 * Links to community resources and contribution
 */
function CommunitySection() {
  return (
    <section className="py-24 sm:py-32 bg-muted/30">
      <div className="mx-auto max-w-7xl px-6 lg:px-8">
        <div className="mx-auto max-w-2xl text-center">
          <h2 className="text-3xl font-bold tracking-tight text-foreground sm:text-4xl">
            Join the community
          </h2>
          <p className="mt-4 text-lg text-muted-foreground">
            AuthSome is open source and built by developers, for developers.
          </p>
        </div>
        <div className="mx-auto mt-16 grid max-w-2xl grid-cols-1 gap-8 sm:mt-20 lg:mx-0 lg:max-w-none lg:grid-cols-2">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <span className="text-xl">🐙</span>
                GitHub Repository
              </CardTitle>
            </CardHeader>
            <CardContent>
              <CardDescription className="text-base mb-4">
                Star the project, report issues, and contribute to the codebase.
              </CardDescription>
              <Link
                href="https://github.com/xraph/authsome"
                className="inline-flex items-center text-sm font-semibold text-brand hover:text-brand/80"
              >
                View on GitHub
                <ArrowRight className="ml-1 h-4 w-4" />
              </Link>
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <span className="text-xl">💬</span>
                Community Support
              </CardTitle>
            </CardHeader>
            <CardContent>
              <CardDescription className="text-base mb-4">
                Get help, share ideas, and connect with other developers.
              </CardDescription>
              <Link
                href="/docs/go/guides"
                className="inline-flex items-center text-sm font-semibold text-brand hover:text-brand/80"
              >
                Join Discussions
                <ArrowRight className="ml-1 h-4 w-4" />
              </Link>
            </CardContent>
          </Card>
        </div>
      </div>
    </section>
  );
}

/**
 * Community Section
 * Links to community resources and contribution
 */
function ContainerSection({ children }: { children: ReactNode }) {
  return (
    <section className="container mx-auto max-w-6xl px-6 lg:px-8">
      {children}
    </section>
  );
}

/**
 * Main Home Page Component
 * Combines all sections into a comprehensive landing page
 */
export default function HomePage() {
  return (
    <main className="min-h-screen">
      <ContainerSection>
        <div className="h-full border-l border-r border-border">
          <HeroSection />
        </div>
      </ContainerSection>
      <Separator />

      <ContainerSection>
        <div className="h-full border-l border-r border-border">
          <FeaturesSection />
          <Separator />
          <QuickStartSection />
          <Separator />
          <NavigationSection />
          <Separator />
          <CommunitySection />
        </div>
      </ContainerSection>
    </main>
  );
}
