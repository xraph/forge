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
import { Footer } from "@/components/landing/footer";

export default function HomePage() {
  return (
    <main className="flex flex-col items-center overflow-x-hidden relative">
      <Hero />
      <FeatureBento />
      <div className="w-full space-y-10 sm:space-y-16 py-12 sm:py-16 md:py-24">
        <div className="container max-w-(--fd-layout-width) mx-auto px-4 sm:px-6">
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
      <Footer />
    </main>
  );
}
