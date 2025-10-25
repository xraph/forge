export const bentoItems = [
  {
    title: "Clean & Expressive",
    description: "Write beautiful, maintainable code with intuitive APIs",
    gradient: "from-blue-500/20 to-cyan-500/20",
    type: "code" as const
  },
  {
    title: "Lightning Fast",
    description: "Handle 100k+ requests per second",
    metric: "100k+",
    label: "req/sec",
    gradient: "from-purple-500/20 to-pink-500/20",
    type: "metric" as const
  },
  {
    title: "Built-in Observability",
    description: "Monitor everything out of the box",
    gradient: "from-orange-500/20 to-red-500/20",
    type: "monitoring" as const
  },
  {
    title: "Enterprise Security",
    description: "Production-grade security by default",
    gradient: "from-green-500/20 to-emerald-500/20",
    type: "security" as const
  },
  {
    title: "Rich Ecosystem",
    description: "Integrate with your favorite tools",
    gradient: "from-yellow-500/20 to-orange-500/20",
    type: "integrations" as const
  }
];

export const codeExample = `package main

import "github.com/xraph/forge"

func main() {
    app := forge.New()
    
    app.Get("/hello", func(c *forge.Context) {
        c.JSON(200, map[string]string{
            "message": "Hello from Forge!",
        })
    })
    
    app.Run(":8080")
}`;
