export const apiEndpoints = [
  {
    method: "INIT" as const,
    path: "forge.NewApp()",
    description: "Initialize a new Forge application",
    parameters: [
      { name: "config", type: "Config", description: "Application configuration" },
      { name: "options", type: "...Option", description: "Optional app configuration" }
    ],
    response: {
      status: 200,
      body: `app := forge.NewApp(
  forge.WithName("my-app"),
  forge.WithVersion("1.0.0"),
  forge.WithLogger(logger),
)`
    }
  },
  {
    method: "REGISTER" as const,
    path: "app.RegisterExtension()",
    description: "Register an extension to the application",
    body: `ext := database.New(
  database.WithDriver("postgres"),
  database.WithDSN(dsn),
)`,
    response: {
      status: 201,
      body: `err := app.RegisterExtension(ext)
if err != nil {
  log.Fatal(err)
}`
    }
  },
  {
    method: "SERVICE" as const,
    path: "app.RegisterService()",
    description: "Register a service with dependency injection",
    body: `app.RegisterService("userService", func(c forge.Container) (interface{}, error) {
  db := c.Get("database").(database.Database)
  return &UserService{db: db}, nil
})`,
    response: {
      status: 200,
      body: `// Service registered and available
service := app.Container().Get("userService")
userSvc := service.(*UserService)`
    }
  },
  {
    method: "ROUTE" as const,
    path: "router.GET()",
    description: "Register HTTP route handlers",
    parameters: [
      { name: "path", type: "string", description: "URL path pattern" },
      { name: "handler", type: "HandlerFunc", description: "HTTP handler function" }
    ],
    body: `router.GET("/users/:id", func(c Context) error {
  id := c.Param("id")
  user := getUserByID(id)
  return c.JSON(200, user)
})`,
    response: {
      status: 200,
      body: `{
  "id": "usr_123",
  "name": "John Doe",
  "email": "john@example.com"
}`
    }
  },
  {
    method: "EXTENSION" as const,
    path: "AI Extension",
    description: "Use AI extension for LLM integration",
    body: `ai := extensions.AI(
  ai.WithProvider("openai"),
  ai.WithModel("gpt-4"),
)

response, err := ai.Chat(ctx, "Hello!")`,
    response: {
      status: 200,
      body: `{
  "content": "Hello! How can I help you?",
  "model": "gpt-4",
  "tokens": 42
}`
    }
  },
  {
    method: "CLI" as const,
    path: "forge generate",
    description: "Generate code using Forge CLI",
    parameters: [
      { name: "type", type: "string", description: "Component type (app, service, extension)" },
      { name: "name", type: "string", description: "Component name" }
    ],
    body: `forge generate service --name=user
forge generate controller --name=auth
forge generate extension --name=metrics`,
    response: {
      status: 201,
      body: `✓ Generated service at services/user.go
✓ Generated controller at controllers/auth.go  
✓ Generated extension at extensions/metrics.go`
    }
  }
];
