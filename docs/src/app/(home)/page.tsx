import { Hero } from "@/components/landing/hero";
import { FeatureBento } from "@/components/landing/feature-bento";
import { AISection } from "@/components/landing/ai-section";
import { VesselSection } from "@/components/landing/vessel-section";
import { ForgeDXSection } from "@/components/landing/forge-dx-section";
import { CLISection } from "@/components/landing/cli-section";
import { GatewaySection } from "@/components/landing/gateway-section";
import { ExtensionsStatsSection } from "@/components/landing/extensions-stats-section";
import { Ecosystem } from "@/components/landing/ecosystem";
import { CTA } from "@/components/landing/cta";
import { SectionHeader } from "@/components/landing/section-header";

export default function HomePage() {
  return (
    <main className="flex flex-col items-center overflow-x-hidden relative">
      {/* Gradient background with grain effect */}
      <div className="flex flex-col items-end absolute -right-60 -top-10 blur-xl z-0 ">
        <div className="h-[10rem] rounded-full w-[60rem] z-1 bg-gradient-to-b blur-[6rem] from-purple-400/40 to-sky-400/40 dark:from-purple-600 dark:to-sky-600"></div>
        <div className="h-[10rem] rounded-full w-[90rem] z-1 bg-gradient-to-b blur-[6rem] from-pink-400/30 to-yellow-300/40 dark:from-pink-900 dark:to-yellow-400"></div>
        <div className="h-[10rem] rounded-full w-[60rem] z-1 bg-gradient-to-b blur-[6rem] from-yellow-400/40 to-sky-400/40 dark:from-yellow-600 dark:to-sky-500"></div>
      </div>
      <div className="absolute inset-0 z-0 bg-noise opacity-30"></div>
      <Hero />
      <FeatureBento />
      <div className="w-full space-y-16 py-16 md:py-24">
        <div className="container max-w-(--fd-layout-width) mx-auto px-6">
          <SectionHeader
            title="Composite Modules"
            description="Built on varous modules."
            className="text-left"
            leftAlign={true}
          />
        </div>
        <AISection />
        <VesselSection />
        <ForgeDXSection />
      </div>
      <CLISection />
      <GatewaySection />
      <ExtensionsStatsSection />
      <Ecosystem />
      <CTA />
    </main>
  );
}
