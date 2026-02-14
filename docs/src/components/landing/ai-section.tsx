"use client";

import Link from "next/link";
import { motion } from "framer-motion";
import {
  ArrowRight,
  Bot,
  Brain,
  MessageSquare,
  Shield,
  Sparkles,
  Workflow,
} from "lucide-react";
import { CodeBlock } from "./code-block";

const highlights = [
  {
    icon: Brain,
    label: "LLM Abstraction",
    desc: "OpenAI, Anthropic, Ollama & more",
  },
  { icon: Bot, label: "ReAct Agents", desc: "Autonomous reasoning & tool use" },
  {
    icon: MessageSquare,
    label: "Structured Output",
    desc: "Type-safe JSON schema responses",
  },
  {
    icon: Workflow,
    label: "RAG & Workflows",
    desc: "Retrieval pipelines & multi-step flows",
  },
  {
    icon: Shield,
    label: "Guardrails",
    desc: "Content filtering & cost limits",
  },
  { icon: Sparkles, label: "Streaming", desc: "Real-time token streaming" },
];

const aiCode = `agent := ai.NewReActAgent(ai.AgentConfig{
    Name:   "assistant",
    Model:  llm.NewOpenAI("gpt-4o"),
    Tools:  []ai.Tool{searchTool, calcTool},
    Memory: ai.NewConversationMemory(10),
})

result, err := agent.Run(ctx, "Analyze Q4 revenue")`;

export function AISection() {
  return (
    <section className="container max-w-(--fd-layout-width) mx-auto px-4 sm:px-6">
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
          <div className="p-5 sm:p-8 lg:p-10 flex flex-col justify-center">
            <div className="inline-flex items-center gap-2 border border-purple-500/30 bg-purple-500/10 px-3 py-1 text-xs font-medium text-purple-600 dark:text-purple-400 mb-6 w-fit">
              <Sparkles className="size-3" />
              AI SDK
            </div>
            <h2 className="text-xl sm:text-2xl lg:text-3xl font-bold tracking-tight mb-3 sm:mb-4">
              AI-First Framework
            </h2>
            <p className="text-sm sm:text-base text-fd-muted-foreground leading-relaxed mb-4 sm:mb-6">
              Build intelligent applications with a unified LLM abstraction
              layer. From simple text generation to autonomous agents with
              tools, memory, and RAG pipelines.
            </p>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-2.5 mb-6">
              {highlights.map((h) => (
                <div
                  key={h.label}
                  className="flex items-start gap-2 p-2 transition-colors hover:bg-fd-accent/50"
                >
                  <h.icon className="size-3.5 text-purple-500 mt-0.5 flex-shrink-0" />
                  <div>
                    <div className="text-sm font-medium leading-tight">
                      {h.label}
                    </div>
                    <div className="text-xs text-fd-muted-foreground mt-0.5">
                      {h.desc}
                    </div>
                  </div>
                </div>
              ))}
            </div>

            <Link
              href="/docs/ai-sdk"
              className="inline-flex items-center gap-2 text-sm font-semibold text-purple-600 dark:text-purple-400 hover:text-purple-500 transition-colors w-fit"
            >
              Explore AI SDK
              <ArrowRight className="size-4" />
            </Link>
          </div>

          {/* Code side */}
          <div className="border-t lg:border-t-0 lg:border-l border-fd-border bg-zinc-950 p-4 sm:p-6 lg:p-8 flex items-center">
            <div className="relative w-full transform transition-transform group-hover:scale-[1.01] duration-500">
              <div className="absolute -inset-1 bg-gradient-to-r from-purple-500/10 to-violet-500/10 blur opacity-0 group-hover:opacity-40 transition duration-500" />
              <CodeBlock code={aiCode} filename="agent.go" />
            </div>
          </div>
        </div>
      </motion.div>
    </section>
  );
}
