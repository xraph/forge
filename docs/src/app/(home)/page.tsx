import { Hero } from "@/components/Hero";
import { Features } from "@/components/Features";
import { BentoShowcase } from "@/components/BentoShowcase";
import { MetricsDashboard } from "@/components/MetricsDashboard";
import { CodePlayground } from "@/components/CodePlayground";
import { UseCases } from "@/components/UseCases";
import { ApiExplorer } from "@/components/ApiExplorer";
import { CaseStudiesCarousel } from "@/components/CaseStudiesCarousel";
import { GitHubStats } from "@/components/GitHubStats";
import { Testimonials } from "@/components/Testimonials";
import { Newsletter } from "@/components/Newsletter";
import { CTA } from "@/components/CTA";
import { ParticlesBackground } from "@/components/ParticlesBackground";

/**
 * Main Home Page Component
 * Combines all sections into a comprehensive landing page
 */
export default function HomePage() {
  return (
    <main className="min-h-screen relative">
      <ParticlesBackground />
      <Hero />
      <GitHubStats noTopBorder />
      <Features />
      <BentoShowcase />
      <MetricsDashboard />
      <CodePlayground />
      <UseCases />
      <ApiExplorer />
      {/* <CaseStudiesCarousel /> */}
      {/* <Testimonials /> */}
      <Newsletter />
      {/* <CTA /> */}
    </main>
  );
}
