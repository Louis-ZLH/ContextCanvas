import { useNavigate, useLocation, Link } from "react-router";

const navLinks = [
  { label: "Home", to: "/" },
  { label: "Features", to: "#features" },
  { label: "Engine", to: "#engine" },
  { label: "Pricing", to: "#pricing" },
  { label: "Docs", to: "#docs" },
  { label: "Guide", to: "/guide" },
];

export function LandingHeader() {
  const navigate = useNavigate();
  const location = useLocation();

  function handleNavClick(e: React.MouseEvent, target: string) {
    e.preventDefault();
    if (target === "/") {
      if (location.pathname === "/") {
        document.querySelector(".ModernScroller")?.scrollTo({ top: 0, behavior: "smooth" });
      } else {
        navigate("/");
      }
      return;
    } else if (target.startsWith("/")) {
      navigate(target);
    } else if (location.pathname === "/") {
      const el = document.getElementById(target.replace("#", ""));
      if (el) el.scrollIntoView({ behavior: "smooth", block: "start" });
    } else {
      const id = target.replace("#", "");
      navigate("/");
      const tryScroll = (attempts: number) => {
        const el = document.getElementById(id);
        if (el) {
          el.scrollIntoView({ behavior: "smooth", block: "start" });
        } else if (attempts > 0) {
          requestAnimationFrame(() => tryScroll(attempts - 1));
        }
      };
      requestAnimationFrame(() => tryScroll(20));
    }
  }

  return (
    <nav className="fixed top-0 w-full z-50 h-16 bg-[#fdfbf7]/90 backdrop-blur-sm border-b border-stone-200">
      <div className="max-w-7xl mx-auto px-6 h-full flex items-center justify-between">
        <Link
          to="/"
          className="flex items-center gap-2 font-extrabold tracking-tight text-xl"
          style={{ fontFamily: "'Inter', system-ui, sans-serif" }}
        >
          <div className="relative flex h-3 w-3">
            <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-orange-500 opacity-75"></span>
            <span className="relative inline-flex rounded-full h-3 w-3 bg-orange-500"></span>
          </div>
          <span className="text-stone-800">
            Context<span className="text-orange-600">Canvas</span>
          </span>
        </Link>

        <div className="hidden md:flex items-center gap-8 text-sm text-stone-500" style={{ fontFamily: "'Inter', system-ui, sans-serif" }}>
          {navLinks.map((link) => (
            <a
              key={link.label}
              href={link.to}
              onClick={(e) => handleNavClick(e, link.to)}
              className="hover:text-orange-600 transition-colors relative group cursor-pointer"
            >
              <span>{link.label}</span>
              <span className="absolute -bottom-1 left-0 w-0 h-px bg-orange-500 transition-[width] group-hover:w-full"></span>
            </a>
          ))}
        </div>

        <div className="flex items-center gap-2 sm:gap-4 text-xs sm:text-sm whitespace-nowrap" style={{ fontFamily: "'Inter', system-ui, sans-serif" }}>
          <a
            href="https://github.com/Louis-ZLH/ContextGraph"
            target="_blank"
            rel="noopener noreferrer"
            className="hidden sm:block text-stone-400 hover:text-stone-800 transition-colors duration-300"
            aria-label="GitHub"
          >
            <svg className="h-5 w-5" fill="currentColor" viewBox="0 0 32 32">
              <path fillRule="evenodd" clipRule="evenodd" d="M16 0C7.16 0 0 7.16 0 16C0 23.08 4.58 29.06 10.94 31.18C11.74 31.32 12.04 30.84 12.04 30.42C12.04 30.04 12.02 28.78 12.02 27.44C8 28.18 6.96 26.46 6.64 25.56C6.46 25.1 5.68 23.68 5 23.3C4.44 23 3.64 22.26 4.98 22.24C6.24 22.22 7.14 23.4 7.44 23.88C8.88 26.3 11.18 25.62 12.1 25.2C12.24 24.16 12.66 23.46 13.12 23.06C9.56 22.66 5.84 21.28 5.84 15.16C5.84 13.42 6.46 11.98 7.48 10.86C7.32 10.46 6.76 8.82 7.64 6.62C7.64 6.62 8.98 6.2 12.04 8.26C13.32 7.9 14.68 7.72 16.04 7.72C17.4 7.72 18.76 7.9 20.04 8.26C23.1 6.18 24.44 6.62 24.44 6.62C25.32 8.82 24.76 10.46 24.6 10.86C25.62 11.98 26.24 13.4 26.24 15.16C26.24 21.3 22.5 22.66 18.94 23.06C19.52 23.56 20.02 24.52 20.02 26.02C20.02 28.16 20 29.88 20 30.42C20 30.84 20.3 31.34 21.1 31.18C27.42 29.06 32 23.06 32 16C32 7.16 24.84 0 16 0Z" />
            </svg>
          </a>
          <button
            onClick={() => navigate("/login")}
            className="cursor-pointer px-3 sm:px-4 py-2 text-stone-600 font-medium hover:text-orange-600 transition-colors duration-300"
          >
            Sign in
          </button>
          <button
            onClick={() => navigate("/canvas")}
            className="cursor-pointer px-3 sm:px-5 py-2 bg-orange-500 text-white rounded-full font-semibold hover:bg-orange-600 active:scale-95 transition-all duration-300 shadow-sm hover:shadow-md"
          >
            Get Started &rarr;
          </button>
        </div>
      </div>
    </nav>
  );
}
