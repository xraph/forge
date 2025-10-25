'use client'

import { useState } from "react";
import { Play } from "lucide-react";
import { Button } from "@/components/ui/button";

const playgroundExamples = [
  {
    title: "Hello World",
    code: `package main

import "github.com/xraph/forge"

func main() {
    app := forge.New()
    
    app.Get("/", func(c *forge.Context) {
        c.JSON(200, map[string]string{
            "message": "Hello, Forge!",
        })
    })
    
    app.Run(":8080")
}`
  },
  {
    title: "REST API",
    code: `package main

import "github.com/xraph/forge"

type User struct {
    ID    string \`json:"id"\`
    Name  string \`json:"name"\`
    Email string \`json:"email"\`
}

func main() {
    app := forge.New()
    
    app.Get("/users/:id", func(c *forge.Context) {
        user := User{
            ID:    c.Param("id"),
            Name:  "John Doe",
            Email: "john@example.com",
        }
        c.JSON(200, user)
    })
    
    app.Run(":8080")
}`
  },
  {
    title: "Middleware",
    code: `package main

import "github.com/xraph/forge"

func Logger() forge.HandlerFunc {
    return func(c *forge.Context) {
        c.Logger.Info("Request received",
            "method", c.Request.Method,
            "path", c.Request.URL.Path,
        )
        c.Next()
    }
}

func main() {
    app := forge.New()
    app.Use(Logger())
    
    app.Get("/", func(c *forge.Context) {
        c.String(200, "Hello!")
    })
    
    app.Run(":8080")
}`
  }
];

export const CodePlayground = () => {
  const [selectedExample, setSelectedExample] = useState(0);
  const [output] = useState(`{"message": "Hello, Forge!"}`);

  return (
    <section className="relative py-32 bg-secondary/30">
      <div className="container mx-auto px-4 sm:px-6 lg:px-8">
        <div className="text-center max-w-3xl mx-auto mb-20">
          <h2 className="text-5xl sm:text-6xl font-black mb-6">
            Try it yourself.
          </h2>
          <p className="text-xl text-muted-foreground">
            Explore interactive examples and see how easy it is to build with Forge.
          </p>
        </div>

        <div className="max-w-5xl mx-auto">
          <div className="flex gap-4 mb-6 overflow-x-auto pb-2">
            {playgroundExamples.map((example, index) => (
              <Button
                key={index}
                variant={selectedExample === index ? "default" : "outline"}
                onClick={() => setSelectedExample(index)}
                className="whitespace-nowrap"
              >
                {example.title}
              </Button>
            ))}
          </div>

          <div className="grid lg:grid-cols-2 gap-6">
            <div className="rounded-2xl border border-border bg-card overflow-hidden">
              <div className="flex items-center justify-between px-6 py-4 border-b border-border">
                <span className="font-semibold">main.go</span>
                <Button size="sm" className="gap-2">
                  <Play className="w-4 h-4" />
                  Run
                </Button>
              </div>
              <pre className="p-6 text-sm overflow-x-auto">
                <code>{playgroundExamples[selectedExample].code}</code>
              </pre>
            </div>

            <div className="rounded-2xl border border-border bg-card overflow-hidden">
              <div className="px-6 py-4 border-b border-border">
                <span className="font-semibold">Output</span>
              </div>
              <pre className="p-6 text-sm text-green-600 dark:text-green-400">
                <code>{output}</code>
              </pre>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
};
