import {
  Layers,
  Clock,
  Star,
  Plus,
  BookOpen,
  Database,
  FileCode,
  Settings,
  Network,
} from "lucide-react";
import { NavItem } from "./NavItem";

export function Sidebar() {
  return (
    <aside className="w-64 flex flex-col border-r border-main bg-sidebar z-20">
      {/* Logo */}
      <div className="h-14 flex items-center px-4 border-b border-main">
        <div className="w-8 h-8 rounded-lg flex items-center justify-center mr-3 bg-accent">
          <Network size={16} className="text-white" />
        </div>
        <span
          className="font-bold text-lg tracking-tight"
          style={{ color: "var(--text-primary)" }}
        >
          ContextGraph
        </span>
      </div>

      {/* Nav */}
      <div className="flex-1 overflow-y-auto py-4 px-2 space-y-6">
        <div>
          <div className="px-2 mb-2 text-xs font-semibold uppercase tracking-wider text-secondary">
            Workspace
          </div>
          <div className="space-y-0.5">
            <NavItem icon={<Layers size={14} />} label="All Graphs" active />
            <NavItem icon={<Clock size={14} />} label="Recent" />
            <NavItem icon={<Star size={14} />} label="Favorites" />
          </div>
        </div>

        <div>
          <div className="flex items-center justify-between px-2 mb-2">
            <div className="text-xs font-semibold uppercase tracking-wider text-secondary">
              Knowledge Base
            </div>
            <button className="text-secondary hover:text-primary">
              <Plus size={12} />
            </button>
          </div>
          <div className="space-y-0.5">
            <NavItem icon={<BookOpen size={14} />} label="React Docs.pdf" />
            <NavItem icon={<Database size={14} />} label="Backend API" />
            <NavItem icon={<FileCode size={14} />} label="Prompts V1" />
          </div>
        </div>
      </div>

      {/* User */}
      <div className="p-4 border-t border-main">
        <div className="flex items-center gap-3 p-2 rounded-md cursor-pointer">
          <div className="w-8 h-8 rounded-full bg-linear-to-tr from-blue-400 to-purple-500" />
          <div className="flex-1 overflow-hidden">
            <p
              className="font-medium truncate text-sm"
              style={{ color: "var(--text-primary)" }}
            >
              User Developer
            </p>
            <p className="text-xs text-secondary truncate">Pro Plan</p>
          </div>
          <Settings size={14} className="text-secondary" />
        </div>
      </div>
    </aside>
  );
}
