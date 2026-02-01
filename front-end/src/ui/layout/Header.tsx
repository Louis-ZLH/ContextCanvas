import { Sun, Terminal, ScrollText } from "lucide-react";

export type ThemeName = "saas" | "cyber" | "paper";

interface HeaderProps {
  theme: ThemeName;
  onSetTheme: (theme: ThemeName) => void;
}

export function Header({ theme, onSetTheme }: HeaderProps) {
  return (
    <header className="h-14 border-b border-main bg-header flex items-center justify-between px-4 z-10">
      <div className="flex flex-col">
        <div className="flex items-center gap-2 text-secondary text-xs">
          <span>Workspace</span>
          <span className="text-[10px]">&gt;</span>
          <span>Frontend Learning</span>
        </div>
        <h1
          className="font-semibold leading-tight text-sm"
          style={{ color: "var(--text-primary)" }}
        >
          React Flow Architecture
        </h1>
      </div>

      <div className="flex items-center gap-3">
        {/* Theme switcher */}
        <div className="flex rounded-md p-0.5 border border-main node-bg">
          <button
            onClick={() => onSetTheme("saas")}
            title="SaaS Theme"
            className="w-7 h-7 flex items-center justify-center rounded transition"
            style={{
              backgroundColor:
                theme === "saas" ? "var(--accent-light)" : "transparent",
              color:
                theme === "saas" ? "var(--accent)" : "var(--text-secondary)",
            }}
          >
            <Sun size={14} />
          </button>
          <button
            onClick={() => onSetTheme("cyber")}
            title="Cyber Theme"
            className="w-7 h-7 flex items-center justify-center rounded transition"
            style={{
              backgroundColor:
                theme === "cyber" ? "var(--accent-light)" : "transparent",
              color:
                theme === "cyber" ? "var(--accent)" : "var(--text-secondary)",
            }}
          >
            <Terminal size={14} />
          </button>
          <button
            onClick={() => onSetTheme("paper")}
            title="Paper Theme"
            className="w-7 h-7 flex items-center justify-center rounded transition"
            style={{
              backgroundColor:
                theme === "paper" ? "var(--accent-light)" : "transparent",
              color:
                theme === "paper" ? "var(--accent)" : "var(--text-secondary)",
            }}
          >
            <ScrollText size={14} />
          </button>
        </div>
      </div>
    </header>
  );
}
