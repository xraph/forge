"use client";

import { motion } from "framer-motion";
import { Terminal, Command, Menu, Play } from "lucide-react";
import { CodeBlock } from "./code-block";

const cliCode = `package main

import "github.com/xraph/forge/cli"

func main() {
    app := cli.New(cli.Config{
        Name: "ops-tool",
        Desc: "Internal operations CLI",
    })

    app.AddCommand(cli.NewCommand(
        "deploy",
        "Deploy to environment",
        func(ctx cli.Context) error {
            env := ctx.String("env")
            spinner := ctx.Spinner("Deploying...")
            
            if err := deploy(env); err != nil {
                 return err
            }
            
            spinner.Success("Deployed to " + env)
            return nil
        },
        cli.WithFlag(cli.StringFlag("env", "e", "Target environment")),
    ))

    app.Run()
}`;

const highlights = [
  {
    icon: Command,
    label: "Type-Safe Flags",
    desc: "Robust flag parsing with strict typing and validation.",
  },
  {
    icon: Menu,
    label: "Interactive Prompts",
    desc: "Rich selection, confirmation, and text input prompts.",
  },
  {
    icon: Play,
    label: "Animations",
    desc: "Built-in spinners, progress bars, and colored output.",
  },
];

export function CLISection() {
  return (
    <section className="w-full">
      <motion.div
        initial={{ opacity: 0, y: 32 }}
        whileInView={{ opacity: 1, y: 0 }}
        viewport={{ once: true, margin: "-80px" }}
        transition={{ duration: 0.6, ease: "easeOut" }}
        className="group relative overflow-hidden border border-fd-border bg-fd-card"
      >
        <div className="absolute inset-0 bg-fd-card z-0" />
        <div className="metal-shader absolute inset-0 opacity-40 z-0 pointer-events-none" />
        <div className="noise-overlay absolute inset-0 pointer-events-none opacity-50" />

        <div className="relative z-10 grid lg:grid-cols-2 gap-0">
          {/* Text side */}
          <div className="p-8 lg:p-12 flex flex-col justify-center">
            <div className="inline-flex items-center gap-2 border border-orange-500/30 bg-orange-500/10 px-3 py-1 text-xs font-medium text-orange-600 dark:text-orange-400 mb-6 w-fit">
              <Terminal className="size-3" />
              CLI Framework
            </div>

            <h2 className="text-3xl font-bold tracking-tight mb-4">
              Build Enterprise CLIs
            </h2>
            <p className="text-fd-muted-foreground text-lg mb-8 leading-relaxed">
              Don't just build an API—ship the tool to use it. Forge includes a
              complete framework for building beautiful, interactive
              command-line applications.
            </p>

            <div className="space-y-4">
              {highlights.map((h) => (
                <div
                  key={h.label}
                  className="flex items-start gap-3 p-2 transition-colors hover:bg-fd-accent/50 rounded-lg -ml-2"
                >
                  <div className="mt-1 size-8 flex items-center justify-center rounded-lg bg-orange-500/10 border border-orange-500/20 text-orange-500 shrink-0">
                    <h.icon className="size-4" />
                  </div>
                  <div>
                    <h3 className="font-semibold text-sm text-fd-foreground">
                      {h.label}
                    </h3>
                    <p className="text-sm text-fd-muted-foreground">{h.desc}</p>
                  </div>
                </div>
              ))}
            </div>
          </div>

          {/* Code side */}
          <div className="border-t lg:border-t-0 lg:border-l border-fd-border bg-zinc-950 p-6 lg:p-12 flex items-center overflow-hidden">
            <div className="relative w-full transform transition-transform group-hover:scale-[1.01] duration-500">
              <div className="absolute -inset-1 bg-gradient-to-r from-orange-500/10 to-amber-500/10 blur opacity-0 group-hover:opacity-40 transition duration-500" />
              <CodeBlock code={cliCode} filename="main.go" />
            </div>
          </div>
        </div>
      </motion.div>
    </section>
  );
}
