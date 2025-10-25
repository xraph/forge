import { Button } from "@/components/ui/button";
import { ArrowRight } from "lucide-react";

export const Navigation = () => {
  return (
    <nav className="fixed top-0 left-0 right-0 z-50 bg-background/80 backdrop-blur-md border-b border-border">
      <div className="container mx-auto px-4 sm:px-6 lg:px-8">
        <div className="flex items-center justify-between h-16">
          <div className="flex items-center gap-8">
            <a href="/" className="flex items-center gap-2 text-xl font-bold">
              ⚒️ Forge
            </a>
            <div className="hidden md:flex items-center gap-6">
              <a href="/#features" className="text-sm text-muted-foreground hover:text-foreground transition-colors">
                Features
              </a>
              <a href="/changelog" className="text-sm text-muted-foreground hover:text-foreground transition-colors">
                Changelog
              </a>
              <a href="/roadmap" className="text-sm text-muted-foreground hover:text-foreground transition-colors">
                Roadmap
              </a>
              <a href="#" className="text-sm text-muted-foreground hover:text-foreground transition-colors">
                Documentation
              </a>
            </div>
          </div>
          
          <div className="flex items-center gap-4">
            <Button variant="ghost" size="sm">
              Login
            </Button>
            <Button size="sm" className="bg-primary text-primary-foreground hover:bg-primary/90">
              Get Started
              <ArrowRight className="ml-2 w-4 h-4" />
            </Button>
          </div>
        </div>
      </div>
    </nav>
  );
};
