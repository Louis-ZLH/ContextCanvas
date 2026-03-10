import {
  Plus,
  FolderOpen,
  Search,
  Settings,
  Network,
  PanelLeftClose,
  PanelLeftOpen,
} from "lucide-react";
import { Link, useLocation } from "react-router";
import type { User } from "../../service/type";
import { useState, useEffect } from "react";
import { useSelector } from "react-redux";
import type { ThemeName } from "../../feature/user/userSlice";
import { UserModal } from "./UserModal";
import { canvasListQueryOptions } from "../../query/canvas";
import { useQuery } from "@tanstack/react-query";
import type { Canvas } from "../../service/type";
import { SidebarCanvasItem } from "./SidebarCanvasItem";

export function Sidebar({ user, isOpen, setIsOpen }: { user: User | null; isOpen: boolean; setIsOpen: (v: boolean) => void }) {
  const [isModalOpen, setIsModalOpen] = useState(false);

  useEffect(() => {
    const mql = window.matchMedia("(min-width: 1024px)");
    const handler = (e: MediaQueryListEvent) => setIsOpen(e.matches);
    mql.addEventListener("change", handler);
    return () => mql.removeEventListener("change", handler);
  }, [setIsOpen]);
  const theme = useSelector(
    (state: { user: { theme: ThemeName } }) => state.user.theme
  );
  const location = useLocation();
  const { data } = useQuery(canvasListQueryOptions);
  const canvasList = data?.data?.canvasList as Canvas[] | undefined;

  return (
    <>
    {/* Mobile backdrop */}
    {isOpen && (
      <div
        className="fixed inset-0 bg-black/40 z-30 lg:hidden"
        onClick={() => setIsOpen(false)}
      />
    )}
    <aside
      className={`${
        isOpen ? "w-64 z-40 lg:z-20 fixed inset-y-0 left-0 lg:relative lg:inset-auto" : "hidden lg:flex w-16 z-20 relative"
      } flex flex-col border-r border-main bg-sidebar transition-[width] duration-100 ease-in-out overflow-hidden whitespace-nowrap`}
    >
      {/* Collapsed State: Only Show Open Button and Avatar */}
      <div
        className={`absolute inset-0 flex flex-col transition-opacity duration-300 ${
          isOpen ? "opacity-0 pointer-events-none z-0" : "opacity-100 z-10"
        }`}
      >
        <div className="h-14 flex items-center justify-center">
          <button
            onClick={() => setIsOpen(true)}
            className={`w-[36px] h-[36px] flex items-center justify-center rounded-lg cursor-pointer text-secondary ${theme === "dark" ? "hover:bg-white/10" : "hover:bg-black/10"}`}
          >
            <PanelLeftOpen size={24} strokeWidth={1.25} />
          </button>
        </div>

        {/* New Canvas Button (Collapsed) */}
        <div className="flex justify-center mt-2">
          <Link
            to="/canvas"
            className={`w-[36px] h-[36px] flex items-center justify-center rounded-lg cursor-pointer text-secondary ${theme === "dark" ? "hover:bg-white/10" : "hover:bg-black/10"}`}
            title="New Canvas"
          >
            <Plus size={24} strokeWidth={1.25} />
          </Link>
        </div>
        
        <div className="flex-1" />
        <div className="p-4 flex justify-center">
          <button
            onClick={() => setIsModalOpen(true)}
            className={`p-2 rounded-lg cursor-pointer transition-colors duration-200 ${theme === "dark" ? "hover:bg-white/10" : "hover:bg-black/10"}`}
          >
            {user?.avatarUrl ? (
              <img
                src={user.avatarUrl}
                alt="User Avatar"
                className="w-8 h-8 rounded-full"
              />
            ) : (
              <div className="w-8 h-8 rounded-full bg-linear-to-tr from-blue-400 to-purple-500" />
            )}
          </button>
        </div>
      </div>

      {/* Expanded State: Full Content */}
      <div
        className={`flex flex-col h-full min-w-64 transition-opacity duration-300 ${
          isOpen ? "opacity-100" : "opacity-0 pointer-events-none"
        }`}
      >
        {/* Logo */}
        <div className="h-14 flex items-center justify-between px-4 border-b border-main">
          <div className="flex items-center">
            <div className="w-8 h-8 rounded-lg flex items-center justify-center mr-3 bg-accent">
              <Network size={16} style={{ color: "var(--node-bg)" }} />
            </div>
            <span
              className="font-bold text-lg tracking-tight"
              style={{ color: "var(--text-primary)" }}
            >
              ContextCanvas
            </span>
          </div>
          <button
            onClick={() => setIsOpen(false)}
            className={`w-[36px] h-[36px] flex items-center justify-center rounded-lg cursor-pointer text-secondary ${theme === "dark" ? "hover:bg-white/10" : "hover:bg-black/10"}`}
          >
            <PanelLeftClose size={24} strokeWidth={1.25} />
          </button>
        </div>

        {/* Nav */}
        <div className={`flex-1 overflow-y-auto pb-4 space-y-6 ${theme === "dark" ? "DarkScroller" : "ModernScroller"}`}>
          {/* Actions Section */}
          <div className="space-y-1 sticky top-0 bg-sidebar pt-4 z-20 shadow-sm">
            <Link
              to="/canvas"
              className={`flex items-center gap-3 px-3 py-2.5 rounded-lg transition-colors duration-200 group text-primary ${theme === "dark" ? "hover:bg-white/10" : "hover:bg-black/5"}`}
            >
              <Plus
                size={18}
                className="text-secondary group-hover:text-accent"
              />
              <span className="text-sm font-medium">New Canvas</span>
            </Link>

            <Link
              to="/canvas/myresource"
              className={`flex items-center gap-3 px-3 py-2.5 rounded-lg transition-colors duration-200 group text-primary ${theme === "dark" ? "hover:bg-white/10" : "hover:bg-black/5"}`}
            >
              <FolderOpen
                size={18}
                className="text-secondary group-hover:text-accent"
              />
              <span className="text-sm font-medium">My Resources</span>
            </Link>

            <Link
              to="/canvas/search"
              className={`flex items-center gap-3 px-3 py-2.5 rounded-lg transition-colors duration-200 group text-primary ${theme === "dark" ? "hover:bg-white/10" : "hover:bg-black/5"}`}
            >
              <Search
                size={18}
                className="text-secondary group-hover:text-accent"
              />
              <span className="text-sm font-medium">Search Canvases</span>
            </Link>
          </div>

          {/* Canvases Section */}
          <div>
            <div className="px-3 mb-3 text-xs font-semibold uppercase tracking-wider text-secondary">
              Canvases
            </div>
            <div className="space-y-1">
              {canvasList?.map((canvas: Canvas) => (
                <SidebarCanvasItem
                  key={canvas.id}
                  canvas={canvas}
                  theme={theme}
                  isActive={location.pathname === `/canvas/${canvas.id}`}
                />
              ))}
            </div>
          </div>
        </div>

        {/* User */}
        <div className="p-4 border-t border-main">
          <button
            onClick={() => setIsModalOpen(true)}
            className={`w-full flex items-center gap-3 p-2 rounded-lg cursor-pointer bg-transparent transition-colors duration-200 ${theme === "dark" ? "hover:bg-white/10" : "hover:bg-black/10"}`}
          >
            {user?.avatarUrl ? (
              <img
                src={user.avatarUrl}
                alt="User Avatar"
                className="w-8 h-8 rounded-full"
              />
            ) : (
              <div className="w-8 h-8 rounded-full bg-linear-to-tr from-blue-400 to-purple-500" />
            )}
            <div className="flex-1 overflow-hidden text-left">
              <p
                className="font-medium truncate text-sm"
                style={{ color: "var(--text-primary)" }}
              >
                {user?.username || "Guest"}
              </p>
              <p className="text-xs text-secondary truncate">
                {user?.plan} Plan
              </p>
            </div>
            <Settings size={14} className="text-secondary" />
          </button>
        </div>
      </div>

      {/* User Modal */}
      <UserModal
        isOpen={isModalOpen}
        onClose={() => setIsModalOpen(false)}
        user={user}
      />
    </aside>
    </>
  );
}
