'use client'

import { Button } from "@/components/ui/button";
import { ArrowRight, CopyIcon } from "lucide-react";
import { LineShadowText } from "./ui/line-shadow-text";
import Link from "next/link";

export const Hero = () => {
  return (
    <section className="relative pt-32 pb-20 overflow-hidden">
      {/* Gradient Background */}
      <div className="absolute inset-0 gradient-radiant m-2 rounded-2xl" />
      
      {/* Content */}
      <div className="container relative mx-auto px-4 sm:px-6 lg:px-8">
        <div className="max-w-4xl">
          <h1 className="text-5xl sm:text-6xl lg:text-8xl font-black leading-tight mb-8">
            <LineShadowText className="italic" shadowColor='black'>
              Forge
            </LineShadowText>{" "} your backend your way.
          </h1>
          
          <p className="text-xl sm:text-2xl text-foreground/80 max-w-2xl mb-10 leading-relaxed">
            Forge is an opinionated distributed backend framework with everything in between. 
            Dependency injection, routing, middleware, OpenAPI, and more.
          </p>
          
          <div className="flex flex-col sm:flex-row gap-4 mb-12">
            <Link href="/docs/forge/quick-start" className="block cursor-pointer">
              <Button size="lg">
                Get Started
                <ArrowRight className="ml-2 w-4 h-4" />
              </Button>
            </Link>
            <Link href="/docs/forge" className="block cursor-pointer">
              <Button size="lg" variant="outline">
                View Documentation
              </Button>
            </Link>
          </div>

          <div className="inline-flex items-center gap-2 text-sm text-muted-foreground">
            <div className="flex items-center gap-2">
              <code className="px-3 py-2 bg-white/50 rounded-lg font-mono border border-border">
                go get github.com/xraph/forge
              </code>
              <Button
                size="sm"
                variant="ghost"
                className="p-2"
                onClick={() => navigator.clipboard.writeText("go get github.com/xraph/forge")}
              >
                <CopyIcon />
              </Button>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
};
