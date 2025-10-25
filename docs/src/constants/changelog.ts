export const changelog = [
  {
    version: "v2.0.0",
    date: "2025-01-15",
    status: "latest" as const,
    title: "Major Release - Forge 2.0",
    items: [
      "Complete rewrite with improved performance",
      "New DI container with better type safety",
      "Enhanced observability features",
      "Breaking: Updated routing API"
    ]
  },
  {
    version: "v1.5.0",
    date: "2024-11-20",
    status: "stable" as const,
    title: "Feature Release",
    items: [
      "Added WebSocket support",
      "Improved OpenAPI generation",
      "New middleware system",
      "Performance optimizations"
    ]
  },
  {
    version: "v1.4.0",
    date: "2024-09-10",
    status: "stable" as const,
    title: "Security & Performance",
    items: [
      "Enhanced security features",
      "Rate limiting improvements",
      "Better error handling",
      "Documentation updates"
    ]
  }
];

export const changelogEntries = [
  {
    date: "Oct 24, 2025",
    title: "Forge 2.0 - Complete Rewrite",
    description: "We've completely rebuilt Forge from the ground up with a focus on performance, type safety, and developer experience. This major release includes breaking changes but provides significant improvements across the board.",
    improvements: [
      "Complete rewrite with 3x performance improvements in routing and middleware",
      "New dependency injection container with compile-time type safety",
      "Enhanced observability with structured logging, metrics, and distributed tracing",
      "Improved extension system with better lifecycle management",
      "Better documentation and examples for common use cases"
    ],
    bugFixes: [
      "Fixed race condition in middleware chain execution",
      "Resolved memory leak in connection pooling",
      "Fixed OpenAPI schema generation for nested types"
    ]
  },
  {
    date: "Sep 10, 2025",
    title: "Security Enhancements & Rate Limiting",
    description: "Major security improvements including advanced rate limiting, enhanced authentication, and better error handling throughout the framework.",
    improvements: [
      "Advanced rate limiting with Redis backend support and distributed rate limiting",
      "Enhanced security middleware with CSRF protection and secure headers",
      "Improved error handling with structured error responses",
      "Better input validation with automatic sanitization",
      "Updated dependencies to address security vulnerabilities"
    ],
    bugFixes: [
      "Fixed authentication edge cases in multi-tenant scenarios",
      "Resolved rate limiter reset issues",
      "Fixed error stack trace exposure in production mode"
    ]
  },
  {
    date: "June 20, 2025",
    title: "WebSocket Support & Middleware Improvements",
    description: "Added first-class WebSocket support and a completely redesigned middleware system for better composability and performance.",
    improvements: [
      "Native WebSocket support with connection pooling and automatic reconnection",
      "New middleware system with improved error handling and async support",
      "Enhanced OpenAPI generation with better type inference",
      "Performance optimizations in routing and request handling",
      "Added middleware for automatic request/response compression"
    ],
    bugFixes: [
      "Fixed middleware ordering issues in certain edge cases",
      "Resolved OpenAPI generation bugs for complex schemas",
      "Fixed memory leak in long-lived WebSocket connections"
    ]
  },
];

export const roadmap = [
  {
    quarter: "Q1 2025",
    status: "in-progress" as const,
    items: [
      { title: "GraphQL Support", description: "Native GraphQL server integration" },
      { title: "gRPC Support", description: "First-class gRPC service support" },
      { title: "Plugin Marketplace", description: "Community plugin ecosystem" }
    ]
  },
  {
    quarter: "Q2 2025",
    status: "planned" as const,
    items: [
      { title: "Admin Dashboard", description: "Built-in monitoring dashboard" },
      { title: "AI-Powered Debugging", description: "Smart error analysis and suggestions" },
      { title: "Multi-region Deploy", description: "One-click multi-region deployment" }
    ]
  },
  {
    quarter: "Q3 2025",
    status: "planned" as const,
    items: [
      { title: "Event Streaming", description: "Native event streaming support" },
      { title: "Service Mesh", description: "Built-in service mesh capabilities" },
      { title: "CLI Enhancements", description: "Improved developer experience" }
    ]
  }
];

export const roadmapItems = [
  {
    title: "GraphQL Support",
    description: "First-class GraphQL server integration with automatic schema generation, resolver mapping, and subscriptions support. Build modern GraphQL APIs with the same developer experience as REST endpoints.",
    status: "in-progress" as const,
    quarter: "Q1 2025"
  },
  {
    title: "gRPC Support",
    description: "Native gRPC service support with automatic proto generation, streaming support, and service discovery. Build high-performance microservices with type-safe communication.",
    status: "in-progress" as const,
    quarter: "Q1 2025"
  },
  {
    title: "Plugin Marketplace",
    description: "Community-driven plugin ecosystem where developers can publish and discover Forge extensions. Share authentication providers, middleware, and integrations with the community.",
    status: "in-progress" as const,
    quarter: "Q1 2025"
  },
  {
    title: "Admin Dashboard",
    description: "Built-in web-based monitoring dashboard with real-time metrics, logs, traces, and health checks. Monitor your services without external tools.",
    status: "planned" as const,
    quarter: "Q2 2025"
  },
  {
    title: "AI-Powered Debugging",
    description: "Smart error analysis and debugging suggestions powered by AI. Get contextual help and solutions for common problems directly in your development workflow.",
    status: "planned" as const,
    quarter: "Q2 2025"
  },
  {
    title: "Multi-region Deployment",
    description: "One-click multi-region deployment with automatic traffic routing, data replication, and failover. Deploy globally with minimal configuration.",
    status: "planned" as const,
    quarter: "Q2 2025"
  },
  {
    title: "Event Streaming",
    description: "Native event streaming support with Kafka, NATS, and RabbitMQ backends. Build event-driven architectures with strong consistency guarantees.",
    status: "planned" as const,
    quarter: "Q3 2025"
  },
  {
    title: "Service Mesh Integration",
    description: "Built-in service mesh capabilities for advanced traffic management, security, and observability in distributed systems.",
    status: "planned" as const,
    quarter: "Q3 2025"
  },
  {
    title: "CLI Enhancements",
    description: "Improved developer experience with interactive scaffolding, better code generation, and integrated testing tools.",
    status: "planned" as const,
    quarter: "Q3 2025"
  }
];
