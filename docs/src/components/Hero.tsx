import { Button } from "@/components/ui/button";
import { ArrowRight } from "lucide-react";
import { LineShadowText } from "./ui/line-shadow-text";

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
            </LineShadowText>{" "} your backends your way.
          </h1>
          
          <p className="text-xl sm:text-2xl text-foreground/80 max-w-2xl mb-10 leading-relaxed">
            Forge is an opinionated distributed backend framework with everything in between. 
            Dependency injection, routing, middleware, OpenAPI, and more.
          </p>
          
          <div className="flex flex-col sm:flex-row gap-4 mb-12">
            <Button size="lg">
              Get Started
              <ArrowRight className="ml-2 w-4 h-4" />
            </Button>
            <Button size="lg" variant="outline">
              View Documentation
            </Button>
          </div>

          <div className="inline-flex items-center gap-2 text-sm text-muted-foreground">
            <code className="px-3 py-2 bg-white/50 rounded-lg font-mono border border-border">
              go get github.com/xraph/forge
            </code>
          </div>
        </div>
      </div>
    </section>
  );
};
