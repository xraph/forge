import { ArrowLeft, Hammer } from "lucide-react";
import Link from "next/link";
import { ForgeBuilder } from "@/components/forgery/forge-builder";

export const metadata = {
  title: "Forge Builder — Generate Your App",
  description:
    "Interactive code generator for building Forge applications with extension and feature selection.",
};

export default function BuilderPage() {
  return (
    <main className="container max-w-(--fd-layout-width) mx-auto px-6 py-16 md:py-24">
      {/* Back link */}
      <Link
        href="/forgery"
        className="inline-flex items-center gap-1.5 text-sm text-fd-muted-foreground hover:text-fd-foreground transition-colors mb-8"
      >
        <ArrowLeft className="size-3.5" />
        Back to Forgery
      </Link>

      {/* Header */}
      <div className="mb-12">
        <div className="inline-flex items-center gap-2 border border-fd-border bg-fd-card/80 backdrop-blur-sm px-3 py-1.5 text-xs font-medium text-fd-muted-foreground mb-4">
          <Hammer className="size-3" />
          Code Generator
        </div>
        <h1 className="text-4xl font-bold mb-4 tracking-tight">
          Forge Builder
        </h1>
        <p className="text-lg text-fd-muted-foreground max-w-2xl">
          Select the extensions you need and get ready-to-run Go boilerplate.
          Includes both built-in Forge extensions and Forgery ecosystem
          packages.
        </p>
      </div>

      {/* Builder */}
      <ForgeBuilder />
    </main>
  );
}
