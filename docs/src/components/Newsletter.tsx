'use client'

import { useState } from "react";
import { Mail } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { toast } from "sonner";

export const Newsletter = () => {
  const [email, setEmail] = useState("");

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    toast("Thanks for subscribing!", {
      description: "You'll receive updates about Forge in your inbox.",
    });
    setEmail("");
  };

  return (
    <section className="relative py-32 overflow-hidden">
      <div className="absolute inset-0 gradient-mesh opacity-30" />

      <div className="container relative mx-auto px-4 sm:px-6 lg:px-8">
        <div className="max-w-3xl mx-auto text-center">
          <div className="inline-flex p-4 rounded-2xl bg-primary/10 border border-primary/20 mb-8">
            <Mail className="w-8 h-8 text-primary" />
          </div>

          <h2 className="text-5xl sm:text-6xl font-black mb-6">
            Stay in the loop.
          </h2>
          <p className="text-xl text-muted-foreground mb-12">
            Get the latest updates, tutorials, and release notes delivered to
            your inbox.
          </p>

          <form
            onSubmit={handleSubmit}
            className="flex flex-col sm:flex-row gap-4 max-w-xl mx-auto"
          >
            <Input
              type="email"
              placeholder="your@email.com"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
              className="flex-1 h-14 text-lg"
            />
            <Button type="submit" size="lg" className="h-14 px-8">
              Subscribe
            </Button>
          </form>

          <p className="mt-6 text-sm text-muted-foreground">
            Join 10,000+ developers. Unsubscribe anytime.
          </p>
        </div>
      </div>
    </section>
  );
};
