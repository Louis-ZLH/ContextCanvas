import { LandingHeader } from "../ui/landing/LandingHeader";
import { Footer } from "../ui/landing/Footer";
import { GuideContent } from "../ui/guide/GuideContent";

export default function GuidePage() {
  return (
    <div
      className="relative antialiased overflow-x-hidden bg-[#fdfbf7] selection:bg-orange-200/40 h-dvh overflow-y-auto text-stone-800 ModernScroller"
      style={{ fontFamily: "'Georgia', 'Cambria', 'Times New Roman', serif" }}
    >
      <div className="fixed -inset-[100vh] bg-[#fdfbf7] -z-10 pointer-events-none"></div>
      <div className="paper-texture fixed inset-0"></div>

      <LandingHeader />

      <main className="relative z-10 pt-16">
        {/* Hero */}
        <div className="pt-16 pb-12 border-b border-stone-200">
          <div className="max-w-7xl mx-auto px-6 text-center">
            <div
              className="inline-flex items-center gap-2 px-3 py-1.5 rounded-full bg-orange-50 border border-orange-200 text-xs text-orange-700 tracking-wider mb-6"
              style={{ fontFamily: "'Inter', system-ui, sans-serif" }}
            >
              <svg className="w-3 h-3" fill="currentColor" viewBox="0 0 8 8">
                <circle cx="4" cy="4" r="3" />
              </svg>
              Quick Start Guide
            </div>
            <h1 className="text-4xl md:text-5xl font-extrabold tracking-tight mb-4">
              Learn{" "}
              <span className="text-orange-600">ContextCanvas</span>
            </h1>
            <p
              className="text-stone-500 max-w-xl mx-auto text-lg leading-relaxed"
              style={{ fontFamily: "'Inter', system-ui, sans-serif" }}
            >
              Everything you need to master spatial AI conversations — from
              creating your first canvas to exporting your work.
            </p>
          </div>
        </div>

        <GuideContent />
        <Footer />
      </main>
    </div>
  );
}
