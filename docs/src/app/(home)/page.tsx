import Link from "next/link";
import {
  ArrowRight,
  Shield,
  Zap,
  Users,
  Code,
  Globe,
  Lock,
  Database,
  MessageSquare,
  Cpu,
  Layers,
  Terminal,
  Gauge,
  Puzzle,
  Rocket,
  GitBranch,
  Server,
  Brain,
  Activity,
  Settings,
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
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@/components/ui/tabs";
import { cn } from "@/lib/utils";
import { ReactNode } from "react";
import { LineShadowText } from "@/components/ui/line-shadow-text";

/**
 * Hero Section Component
 * Modern hero with gradient background and call-to-action
 */
function HeroSection() {
  return (
    <section className="relative overflow-hidden bg-gradient-to-br from-background via-background to-muted/20 py-24 sm:py-32">
      <div className="absolute inset-0 bg-grid-white/[0.02] bg-[size:60px_60px]" />
      <div className="relative mx-auto max-w-7xl px-6 lg:px-8">
        <div className="mx-auto max-w-2xl text-center">
          <Badge variant="outline" className="mb-4">
            <Rocket className="mr-1 h-3 w-3" />
            Enterprise-Grade Go Framework
          </Badge>
          <h1 className="text-4xl font-bold tracking-tight text-foreground sm:text-6xl">
            <LineShadowText className="italic" shadowColor='black'>
              Forge
            </LineShadowText>{" "}
            your backend
          </h1>
          <p className="mt-6 text-lg leading-8 text-muted-foreground">
            A modern, extensible Go web framework with enterprise-grade features, 
            dependency injection, and 20+ powerful extensions. Built for scale, 
            designed for developers.
          </p>
          <div className="mt-10 flex items-center justify-center gap-x-6">
            <Link
              href="/docs/forge/installation"
              className="rounded-md bg-brand px-3.5 py-2.5 text-sm font-semibold text-brand-foreground shadow-sm hover:bg-brand/90 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand"
            >
              Get Started
              <ArrowRight className="ml-2 h-4 w-4 inline" />
            </Link>
            <Link
              href="/docs/forge/quick-start"
              className="text-sm font-semibold leading-6 text-foreground hover:text-brand"
            >
              Quick Start <span aria-hidden="true">→</span>
            </Link>
          </div>
        </div>
      </div>
    </section>
  );
}

/**
 * Code Examples Section with Tabbed Interface
 * Showcases practical code examples from the Forge framework using tabs
 */
function CodeExamplesSection() {
  const codeExamples = [
    {
      id: "minimal",
      title: "Minimal App",
      description: "Get started with a basic Forge application",
      language: "Go",
      code: `package main

import (
  "github.com/xraph/forge"
)

func main() {
  app := forge.New()
  
  app.GET("/", func(c forge.Context) error {
    return c.JSON(200, map[string]string{
      "message": "Hello, Forge!",
    })
  })
  
  app.Run()
}`,
      icon: Rocket,
    },
    {
      id: "di",
      title: "Dependency Injection",
      description: "Clean DI with service registration and resolution",
      language: "Go",
      code: `package main

import (
  "github.com/xraph/forge"
)

type UserService struct {
  db Database
}

func main() {
  app := forge.New()
  
  // Register services
  app.RegisterSingleton[Database]("db", NewDatabase)
  app.RegisterSingleton[UserService]("userService", 
    func(c forge.Container) (*UserService, error) {
      db, _ := c.Resolve("db")
      return &UserService{db: db.(Database)}, nil
    })
  
  app.Run()
}`,
      icon: Puzzle,
    },
    {
      id: "ai",
      title: "AI Extension",
      description: "Advanced AI capabilities with LLM integration and agents",
      language: "Go",
      code: `package main

import (
  "github.com/xraph/forge"
  "github.com/xraph/forge/extensions/ai"
)

func main() {
  app := forge.New()
  
  // Configure AI extension
  aiExt := ai.New(ai.Config{
    LLM: ai.LLMConfig{
      DefaultProvider: "openai",
      Providers: map[string]ai.ProviderConfig{
        "openai": {
          APIKey: "sk-...",
          Model:  "gpt-4",
        },
      },
    },
    EnableAgents: true,
    EnableInference: true,
  })
  
  app.AddExtension(aiExt)
  app.Run()
}`,
      icon: Brain,
    },
    {
      id: "database",
      title: "Database Integration",
      description: "Type-safe database operations with connection pooling",
      language: "Go",
      code: `package main

import (
  "database/sql"
  "github.com/xraph/forge"
  "github.com/xraph/forge/extensions/database"
)

type User struct {
  ID    int    \`json:"id" db:"id"\`
  Name  string \`json:"name" db:"name"\`
  Email string \`json:"email" db:"email"\`
}

func GetUser(db *sql.DB, id int) (*User, error) {
  var user User
  err := db.QueryRow(
    "SELECT id, name, email FROM users WHERE id = ?", id).Scan(
    &user.ID, &user.Name, &user.Email)
  
  if err != nil {
    return nil, err
  }
  return &user, nil
}`,
      icon: Database,
    }
  ];

  return (
    <section className="py-24 sm:py-32 bg-gray-50 dark:bg-gray-900/50">
      <div className="mx-auto max-w-7xl px-6 lg:px-8">
        {/* Left-aligned titles */}
        <div className="max-w-4xl">
          <h2 className="text-3xl font-bold tracking-tight text-foreground sm:text-4xl">
            Real Code, Real Power
          </h2>
          <p className="mt-4 text-lg text-muted-foreground">
            See how Forge simplifies complex backend development with clean, 
            production-ready code examples.
          </p>
        </div>

        {/* Left-right layout: Tabs on left, Code on right */}
        <div className="mt-16">
          <Tabs defaultValue="minimal" className="w-full">
            <div className="grid grid-cols-1 lg:grid-cols-12 gap-8">
              {/* Left side - Tabs Navigation */}
              <div className="lg:col-span-4">
                <TabsList className="grid w-full grid-rows-4 h-auto bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 p-2 rounded-xl shadow-sm">
                  {codeExamples.map((example) => (
                    <TabsTrigger 
                      key={example.id} 
                      value={example.id} 
                      className="flex items-start gap-3 p-4 h-auto text-left justify-start data-[state=active]:bg-blue-50 dark:data-[state=active]:bg-blue-950/50 data-[state=active]:border-blue-200 dark:data-[state=active]:border-blue-800 rounded-lg transition-all duration-200 hover:bg-gray-50 dark:hover:bg-gray-700/50"
                    >
                      <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-gradient-to-br from-blue-500/10 to-purple-500/10 dark:from-blue-500/20 dark:to-purple-500/20 flex-shrink-0">
                        <example.icon className="h-5 w-5 text-blue-600 dark:text-blue-400" />
                      </div>
                      <div className="flex-1 min-w-0">
                        <div className="font-medium text-sm text-foreground">
                          {example.title}
                        </div>
                        <div className="text-xs text-muted-foreground mt-1 line-clamp-2">
                          {example.description}
                        </div>
                      </div>
                    </TabsTrigger>
                  ))}
                </TabsList>
              </div>

              {/* Right side - Code Content */}
              <div className="lg:col-span-8">
                {codeExamples.map((example) => (
                  <TabsContent key={example.id} value={example.id} className="mt-0">
                    <Card className="border-0 shadow-lg bg-white dark:bg-gray-800/50 backdrop-blur-sm">
                      <CardHeader className="pb-4">
                        <div className="flex items-center gap-3">
                          <div className="flex h-12 w-12 items-center justify-center rounded-xl bg-gradient-to-br from-blue-500/10 to-purple-500/10 dark:from-blue-500/20 dark:to-purple-500/20">
                            <example.icon className="h-6 w-6 text-blue-600 dark:text-blue-400" />
                          </div>
                          <div>
                            <CardTitle className="text-xl font-semibold">{example.title}</CardTitle>
                            <CardDescription className="text-base mt-1">
                              {example.description}
                            </CardDescription>
                          </div>
                        </div>
                      </CardHeader>
                      <CardContent className="pt-0">
                        <div className="relative">
                          <div className="absolute right-4 top-4 z-10 rounded-md bg-gray-100 px-3 py-1.5 text-xs font-medium text-gray-700 dark:bg-gray-700 dark:text-gray-300 shadow-sm">
                            {example.language}
                          </div>
                          <div className="rounded-xl overflow-hidden border border-gray-200 dark:border-gray-700">
                            <pre className="overflow-x-auto bg-gray-900 dark:bg-gray-950 p-6 text-sm text-gray-100 leading-relaxed">
                              <code>{example.code}</code>
                            </pre>
                          </div>
                        </div>
                      </CardContent>
                    </Card>
                  </TabsContent>
                ))}
              </div>
            </div>
          </Tabs>
        </div>

        {/* Call to action */}
        <div className="mt-12">
          <Link
            href="/docs/forge/quick-start"
            className="inline-flex items-center rounded-lg bg-gradient-to-r from-blue-600 to-purple-600 px-6 py-3 text-sm font-medium text-white shadow-lg hover:from-blue-700 hover:to-purple-700 transition-all duration-200 hover:shadow-xl hover:scale-105"
          >
            Explore More Examples
            <ArrowRight className="ml-2 h-4 w-4" />
          </Link>
        </div>
      </div>
    </section>
  );
}

/**
 * Feature Cards Section with Aceternity UI Bento Grid
 * Showcases key capabilities of Forge with glassmorphism effects and hover animations
 */
function FeaturesSection() {
  const features = [
    {
      icon: Layers,
      title: "Clean Architecture",
      description:
        "Built on clean architecture principles with dependency injection, service layer, and clear separation of concerns.",
      className: "md:col-span-2",
      header: (
        <div className="flex h-full min-h-[6rem] w-full flex-1 rounded-xl bg-gradient-to-br from-blue-500/10 via-purple-500/10 to-pink-500/10 dark:from-blue-500/20 dark:via-purple-500/20 dark:to-pink-500/20" />
      ),
    },
    {
      icon: Puzzle,
      title: "20+ Extensions",
      description:
        "Powerful extension system with AI, databases, messaging, GraphQL, gRPC, and more built-in.",
      className: "md:col-span-1",
      header: (
        <div className="flex h-full min-h-[6rem] w-full flex-1 rounded-xl bg-gradient-to-br from-green-500/10 via-emerald-500/10 to-teal-500/10 dark:from-green-500/20 dark:via-emerald-500/20 dark:to-teal-500/20" />
      ),
    },
    {
      icon: Activity,
      title: "Built-in Observability",
      description:
        "Comprehensive metrics, logging, health checks, and distributed tracing out of the box.",
      className: "md:col-span-1",
      header: (
        <div className="flex h-full min-h-[6rem] w-full flex-1 rounded-xl bg-gradient-to-br from-orange-500/10 via-red-500/10 to-pink-500/10 dark:from-orange-500/20 dark:via-red-500/20 dark:to-pink-500/20" />
      ),
    },
    {
      icon: Zap,
      title: "High Performance",
      description:
        "Optimized for high concurrency with connection pooling, caching, and efficient middleware.",
      className: "md:col-span-2",
      header: (
        <div className="flex h-full min-h-[6rem] w-full flex-1 rounded-xl bg-gradient-to-br from-cyan-500/10 via-blue-500/10 to-indigo-500/10 dark:from-cyan-500/20 dark:via-blue-500/20 dark:to-indigo-500/20" />
      ),
    },
    {
      icon: Shield,
      title: "Enterprise Security",
      description:
        "Authentication, authorization, rate limiting, and security middleware with multiple provider support.",
      className: "md:col-span-1",
      header: (
        <div className="flex h-full min-h-[6rem] w-full flex-1 rounded-xl bg-gradient-to-br from-violet-500/10 via-purple-500/10 to-fuchsia-500/10 dark:from-violet-500/20 dark:via-purple-500/20 dark:to-fuchsia-500/20" />
      ),
    },
    {
      icon: Terminal,
      title: "Developer Experience",
      description:
        "Rich CLI tools, code generation, hot reload, and comprehensive documentation for rapid development.",
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
            Everything you need to build modern backends
          </h2>
          <p className="mt-4 text-lg text-muted-foreground">
            From simple APIs to complex microservices, Forge provides all the 
            tools and extensions you need for production-ready applications.
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
 * Extensions Showcase Section
 * Highlights the powerful extension ecosystem
 */
function ExtensionsSection() {
  const extensions = [
    {
      icon: Brain,
      title: "AI & LLM",
      description: "Agents, inference, and LLM integration",
      href: "/docs/extensions/ai",
    },
    {
      icon: Database,
      title: "Database",
      description: "SQL & NoSQL with connection pooling",
      href: "/docs/extensions/database",
    },
    {
      icon: MessageSquare,
      title: "GraphQL",
      description: "GraphQL server with federation support",
      href: "/docs/extensions/graphql",
    },
    {
      icon: Server,
      title: "gRPC",
      description: "gRPC server with interceptors",
      href: "/docs/extensions/grpc",
    },
    {
      icon: Globe,
      title: "Kafka",
      description: "Apache Kafka integration",
      href: "/docs/extensions/kafka",
    },
    {
      icon: Lock,
      title: "Auth",
      description: "Authentication & authorization",
      href: "/docs/extensions/auth",
    },
    {
      icon: Gauge,
      title: "Dashboard",
      description: "Real-time monitoring dashboard",
      href: "/docs/extensions/dashboard",
    },
    {
      icon: Settings,
      title: "MCP",
      description: "Model Context Protocol server",
      href: "/docs/extensions/mcp",
    },
  ];

  return (
    <section className="py-24 sm:py-32 bg-muted/30">
      <div className="mx-auto max-w-7xl px-6 lg:px-8">
        <div className="mx-auto max-w-2xl text-center">
          <h2 className="text-3xl font-bold tracking-tight text-foreground sm:text-4xl">
            Powerful extension ecosystem
          </h2>
          <p className="mt-4 text-lg text-muted-foreground">
            20+ first-party extensions for AI, databases, messaging, and more. 
            All production-ready and seamlessly integrated.
          </p>
        </div>
        <div className="mx-auto mt-16 grid max-w-2xl grid-cols-1 gap-6 sm:mt-20 lg:mx-0 lg:max-w-none lg:grid-cols-4">
          {extensions.map((extension, index) => (
            <Link key={index} href={extension.href} className="group">
              <Card className="h-full transition-all duration-200 hover:shadow-lg hover:border-brand/50 group-hover:scale-[1.02]">
                <CardHeader className="pb-3">
                  <div className="flex items-center gap-3">
                    <div className="p-2 rounded-lg bg-brand/10 group-hover:bg-brand/20 transition-colors">
                      <extension.icon className="h-5 w-5 text-brand" />
                    </div>
                    <CardTitle className="text-lg group-hover:text-brand transition-colors">
                      {extension.title}
                    </CardTitle>
                  </div>
                </CardHeader>
                <CardContent>
                  <CardDescription className="text-sm">
                    {extension.description}
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
 * Quick Start Section
 * Shows installation and basic usage
 */
function QuickStartSection() {
  return (
    <section className="py-24 sm:py-32">
      <div className="mx-auto max-w-7xl px-6 lg:px-8">
        <div className="mx-auto max-w-2xl text-center">
          <h2 className="text-3xl font-bold tracking-tight text-foreground sm:text-4xl">
            Get started in minutes
          </h2>
          <p className="mt-4 text-lg text-muted-foreground">
            Create a new Forge application and start building with enterprise-grade 
            features out of the box.
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
                  Install Forge CLI
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div className="rounded-lg bg-muted p-4">
                  <code className="text-sm">
                    go install github.com/xraph/forge/cmd/forge@latest
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
                  Create New Project
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div className="rounded-lg bg-muted p-4">
                  <code className="text-sm">
                    forge new my-app
                    <br />
                    cd my-app && go run main.go
                  </code>
                </div>
              </CardContent>
            </Card>
          </div>
          <div className="mt-8 text-center">
            <Link
              href="/docs/forge/quick-start"
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
        "Installation, project setup, and your first Forge application.",
      href: "/docs/forge/installation",
      icon: "🚀",
    },
    {
      title: "Core Concepts",
      description:
        "Understanding dependency injection, services, and clean architecture.",
      href: "/docs/forge/concepts",
      icon: "🧠",
    },
    {
      title: "Extensions",
      description:
        "Explore 20+ extensions for databases, AI, messaging, and more.",
      href: "/docs/extensions",
      icon: "🔌",
    },
    {
      title: "API Reference",
      description: "Complete API documentation for all framework components.",
      href: "/docs/forge/api",
      icon: "📚",
    },
    {
      title: "Guides",
      description:
        "Step-by-step tutorials for building production applications.",
      href: "/docs/forge/guides",
      icon: "📖",
    },
    {
      title: "Examples",
      description: "Real-world examples and sample applications.",
      href: "/docs/forge/examples",
      icon: "💡",
    },
  ];

  return (
    <section className="py-24 sm:py-32 bg-muted/30">
      <div className="mx-auto max-w-7xl px-6 lg:px-8">
        <div className="mx-auto max-w-2xl text-center">
          <h2 className="text-3xl font-bold tracking-tight text-foreground sm:text-4xl">
            Explore the documentation
          </h2>
          <p className="mt-4 text-lg text-muted-foreground">
            Everything you need to build scalable, maintainable backend 
            applications with Forge.
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
    <section className="py-24 sm:py-32">
      <div className="mx-auto max-w-7xl px-6 lg:px-8">
        <div className="mx-auto max-w-2xl text-center">
          <h2 className="text-3xl font-bold tracking-tight text-foreground sm:text-4xl">
            Join the community
          </h2>
          <p className="mt-4 text-lg text-muted-foreground">
            Forge is open source and built by developers, for developers.
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
                href="https://github.com/xraph/forge"
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
                href="/docs/forge/community"
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
 * Container Section
 * Provides consistent layout wrapper
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

      <CodeExamplesSection />
      <Separator />

      <ContainerSection>
        <div className="h-full border-l border-r border-border">
          <FeaturesSection />
          <Separator />
          <ExtensionsSection />
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
