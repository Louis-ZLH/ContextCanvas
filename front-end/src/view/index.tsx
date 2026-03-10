import { LandingHeader } from "../ui/landing/LandingHeader";
import { HeroSection } from "../ui/landing/HeroSection";
import { FeaturesSection } from "../ui/landing/FeaturesSection";
import { EngineSection } from "../ui/landing/EngineSection";
import { PricingSection } from "../ui/landing/PricingSection";
import { DocsSection } from "../ui/landing/DocsSection";
import { Footer } from "../ui/landing/Footer";

export default function LandingPage() {
  return (
    <div className="relative antialiased overflow-x-hidden bg-[#fdfbf7] selection:bg-orange-200/40 h-dvh overflow-y-auto text-stone-800 ModernScroller" style={{ fontFamily: "'Georgia', 'Cambria', 'Times New Roman', serif" }}>
      {/* Background: Large solid color to cover scroll bounce */}
      <div className="fixed -inset-[100vh] bg-[#fdfbf7] -z-10 pointer-events-none"></div>

      {/* Subtle paper texture overlay */}
      <div className="paper-texture fixed inset-0"></div>

      <LandingHeader />

      <main className="relative z-10">
        <HeroSection />
        <FeaturesSection />
        <EngineSection />
        <PricingSection />
        <DocsSection />
        <Footer />
      </main>
    </div>
  );
}
