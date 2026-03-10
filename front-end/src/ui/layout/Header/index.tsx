import { PanelLeftOpen } from "lucide-react";
import { useAppSelector } from "../../../hooks";
import { Breadcrumb } from "./Breadcrumb";
import { ParentNodesPanel } from "./ParentNodesPanel";

export function Header({ onOpenSidebar, sidebarOpen }: { onOpenSidebar: () => void; sidebarOpen: boolean }) {
  const maximizedNodeId = useAppSelector((s) => s.canvas.maximizedNodeId);
  const chatTitle = useAppSelector((s) => maximizedNodeId ? s.chat.conversations[maximizedNodeId]?.title : null);

  return (
    <header className="h-14 border-b border-main bg-header flex items-center justify-between px-4 z-10 relative">
      <div className="flex items-center gap-2">
        {!sidebarOpen && (
          <button
            onClick={onOpenSidebar}
            className="lg:hidden w-9 h-9 flex items-center justify-center rounded-lg cursor-pointer text-secondary hover:bg-black/5"
          >
            <PanelLeftOpen size={20} strokeWidth={1.25} />
          </button>
        )}
        <div className={maximizedNodeId ? "hidden sm:block" : ""}>
          <Breadcrumb />
        </div>
      </div>

      {chatTitle && (
        <div className="absolute left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 text-sm font-medium truncate max-w-[40%] text-center"
          style={{ color: "var(--text-primary)" }}
        >
          {chatTitle}
        </div>
      )}

      <div className="flex items-center gap-3">
        <ParentNodesPanel />
      </div>
    </header>
  );
}
