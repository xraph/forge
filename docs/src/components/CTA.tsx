import { Button } from "@/components/ui/button";
import { ArrowRight } from "lucide-react";

export const CTA = () => {
  return (
    <section className="relative py-32 overflow-hidden">
      {/* Gradient Background */}
      <div className="absolute inset-0 gradient-radiant" />
      
      <div className="container relative mx-auto px-4 sm:px-6 lg:px-8 text-center">
        <div className="text-sm font-semibold tracking-wide text-foreground/70 mb-4">
          GET STARTED
        </div>
        <h2 className="text-5xl sm:text-6xl lg:text-7xl font-black mb-6 leading-tight max-w-4xl mx-auto">
          Ready to dive in? Start your free trial today.
        </h2>
        <p className="text-xl text-foreground/80 max-w-2xl mx-auto mb-12">
          Get the cheat codes for building distributed backends and unlock your team's potential.
        </p>
        
        <Button size="lg" className="bg-primary text-primary-foreground hover:bg-primary/90 text-base px-8 py-6 h-auto">
          Get Started
          <ArrowRight className="ml-2 w-5 h-5" />
        </Button>
      </div>
    </section>
  );
};
