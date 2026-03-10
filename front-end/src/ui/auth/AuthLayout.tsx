import type { ReactNode } from "react";
import { Link } from "react-router";
import { BookOpen, PenTool, Layers, Sparkles } from "lucide-react";

interface AuthLayoutProps {
  children: ReactNode;
}

export function AuthLayout({ children }: AuthLayoutProps) {
  return (
    <div className="min-h-dvh w-full flex bg-[#fdfbf7] text-stone-800 selection:bg-orange-200/60 overflow-hidden" style={{ fontFamily: "'Georgia', 'Cambria', 'Times New Roman', serif" }}>
      {/* Left Panel - Visuals (Hidden on mobile) */}
      <div className="hidden lg:flex lg:w-1/2 xl:w-5/12 flex-col justify-between relative bg-[#f5f0e8] border-r border-stone-200 p-12 overflow-hidden">
        {/* Subtle paper texture */}
        <div className="paper-texture"></div>

        {/* Logo Area */}
        <div className="relative z-10">
          <Link to="/" className="flex items-center gap-3 group w-fit">
            <div className="relative flex h-8 w-8 items-center justify-center bg-orange-50 rounded-lg border border-orange-200 group-hover:border-orange-400 transition-colors">
               <BookOpen className="w-5 h-5 text-orange-600" />
            </div>
            <span className="font-extrabold text-2xl tracking-tight text-stone-800" style={{ fontFamily: "'Inter', system-ui, sans-serif" }}>
              Context<span className="text-orange-600">Canvas</span>
            </span>
          </Link>
        </div>

        {/* Center Visual */}
        <div className="relative z-10 flex-1 flex flex-col justify-center items-center">
            {/* Abstract Canvas Visualization - Paper style */}
            <div className="relative w-full max-w-sm aspect-square mb-8">
                {/* Center Node */}
                <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-32 h-32 bg-white rounded-full border-2 border-stone-200 flex items-center justify-center animate-pulse-slow" style={{ boxShadow: '4px 4px 0px rgba(41, 37, 36, 0.1)' }}>
                    <Layers className="w-12 h-12 text-orange-600 opacity-80" />
                </div>

                {/* Orbiting Nodes */}
                {[
                  { icon: PenTool, delay: "0s", pos: "top-0 left-1/2 -translate-x-1/2", animation: "animate-float" },
                  { icon: Sparkles, delay: "2s", pos: "bottom-12 right-0", animation: "animate-float" },
                  { icon: BookOpen, delay: "4s", pos: "bottom-12 left-0", animation: "animate-float" },
                ].map((item, idx) => (
                    <div key={idx} className={`absolute ${item.pos} ${item.animation}`} style={{ animationDelay: item.delay }}>
                         <div className="w-14 h-14 bg-white rounded-2xl border border-stone-200 flex items-center justify-center" style={{ boxShadow: '3px 3px 0px rgba(41, 37, 36, 0.08)' }}>
                            <item.icon className="w-6 h-6 text-stone-500" />
                         </div>
                    </div>
                ))}

                 {/* Connecting Lines (Decorative SVG) */}
                 <svg className="absolute inset-0 w-full h-full pointer-events-none opacity-20 text-stone-400" viewBox="0 0 100 100">
                    <path d="M50 50 L50 15" stroke="currentColor" strokeWidth="0.5" strokeDasharray="4 4" />
                    <path d="M50 50 L80 80" stroke="currentColor" strokeWidth="0.5" strokeDasharray="4 4" />
                    <path d="M50 50 L20 80" stroke="currentColor" strokeWidth="0.5" strokeDasharray="4 4" />
                    <circle cx="50" cy="50" r="30" stroke="currentColor" strokeWidth="0.3" fill="none" strokeDasharray="3 3" />
                 </svg>
            </div>

            <div className="text-center max-w-md">
                <h3 className="text-3xl font-bold text-stone-800 mb-4 tracking-tight">Your Ideas, Beautifully Connected</h3>
                <p className="text-stone-500 leading-relaxed text-lg">
                    Transform scattered thoughts into organized knowledge canvases. A spatial workspace for your best thinking.
                </p>
            </div>
        </div>

        {/* Footer / Copyright */}
        <div className="relative z-10 flex justify-between items-end text-xs text-stone-400" style={{ fontFamily: "'Inter', system-ui, sans-serif" }}>
           <span>&copy; 2026 ContextCanvas Inc.</span>
           <span>Made with care</span>
        </div>
      </div>

      {/* Right Panel - Auth Form */}
      <div className="flex-1 flex flex-col relative overflow-y-auto bg-[#fdfbf7]">
        {/* Mobile Header (only visible on small screens) */}
         <div className="lg:hidden p-6 flex justify-between items-center absolute top-0 left-0 w-full z-20">
            <Link to="/" className="flex items-center gap-2">
               <div className="h-6 w-6 bg-orange-50 rounded-md flex items-center justify-center border border-orange-200">
                 <BookOpen className="w-4 h-4 text-orange-600" />
               </div>
               <span className="font-bold text-lg text-stone-800" style={{ fontFamily: "'Inter', system-ui, sans-serif" }}>Context<span className="text-orange-600">Canvas</span></span>
            </Link>
         </div>

        {/* Top Navigation (Desktop) */}
        <div className="hidden lg:flex justify-end p-8 absolute top-0 right-0 w-full z-20">
            <Link to="/" className="group flex items-center gap-2 text-sm text-stone-400 hover:text-stone-700 transition-colors px-4 py-2 rounded-full hover:bg-stone-100 border border-transparent hover:border-stone-200" style={{ fontFamily: "'Inter', system-ui, sans-serif" }}>
                <span>&larr; Back to Home</span>
            </Link>
        </div>

        <main className="flex-1 flex items-center justify-center p-6 lg:p-12 relative z-10">
           <div className="w-full max-w-md animate-in fade-in slide-in-from-bottom-8 duration-700">
             {children}
           </div>
        </main>

        {/* Mobile footer note */}
        <div className="lg:hidden p-6 text-center text-[10px] text-stone-400" style={{ fontFamily: "'Inter', system-ui, sans-serif" }}>
          Secure connection
        </div>
      </div>
    </div>
  );
}
