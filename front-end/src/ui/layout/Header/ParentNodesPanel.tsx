import { useState, useRef, useEffect } from "react";
import { Mail, MessageSquare, FileText } from "lucide-react";
import { selectParentNodesOfMaximized } from "../../../feature/canvas/canvasSlice";
import { useAppSelector } from "../../../hooks";
import { ChatParentItem } from "./ChatParentItem";
import { ResourceItem } from "./ResourceItem";

export function ParentNodesPanel() {
  const maximizedNodeId = useAppSelector((s) => s.canvas.maximizedNodeId);
  const { chatParents, resourceParents } = useAppSelector(selectParentNodesOfMaximized);
  const parentCount = chatParents.length + resourceParents.length;

  const [panelOpenForId, setPanelOpenForId] = useState<string | null>(null);
  const panelOpen = panelOpenForId !== null && panelOpenForId === maximizedNodeId;
  const panelRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!panelOpen) return;
    const handler = (e: MouseEvent) => {
      if (panelRef.current && !panelRef.current.contains(e.target as HTMLElement)) {
        setPanelOpenForId(null);
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [panelOpen]);

  if (!maximizedNodeId || parentCount === 0) return null;

  return (
    <div className="relative" ref={panelRef}>
      <button
        onClick={() => setPanelOpenForId((prev) => prev === maximizedNodeId ? null : maximizedNodeId)}
        className="relative flex items-center gap-2 p-2 sm:px-4 sm:py-1.5 rounded-full border border-main node-bg text-sm cursor-pointer transition-colors hover:opacity-80"
        style={{ color: "var(--text-primary)" }}
      >
        <Mail size={16} style={{ color: "var(--accent)" }} />
        <span className="hidden sm:inline">{parentCount} parent node{parentCount !== 1 ? "s" : ""} connected</span>
        <span
          className="sm:hidden absolute -top-1.5 -right-1.5 min-w-4 h-4 rounded-full text-[10px] font-bold flex items-center justify-center"
          style={{ backgroundColor: "var(--accent)", color: "#fff" }}
        >
          {parentCount}
        </span>
      </button>

      {panelOpen && (
        <div
          className="absolute right-0 top-full mt-2 w-80 rounded-xl border border-main node-bg shadow-lg overflow-hidden"
          style={{ zIndex: 50 }}
        >
          {chatParents.length > 0 && (
            <div>
              <div className="px-4 py-2 border-b border-main flex items-center gap-2">
                <MessageSquare size={14} style={{ color: "var(--accent)" }} />
                <span className="text-xs font-semibold uppercase tracking-wider text-secondary">
                  Chats ({chatParents.length})
                </span>
              </div>
              <div className="max-h-48 overflow-y-auto">
                {chatParents.map((node) => (
                  <ChatParentItem key={node.id} nodeId={node.id} />
                ))}
              </div>
            </div>
          )}

          {resourceParents.length > 0 && (
            <div>
              <div className="px-4 py-2 border-b border-main flex items-center gap-2">
                <FileText size={14} style={{ color: "var(--accent)" }} />
                <span className="text-xs font-semibold uppercase tracking-wider text-secondary">
                  Resources ({resourceParents.length})
                </span>
              </div>
              <div className="max-h-48 overflow-y-auto">
                {resourceParents.map((node) => (
                  <ResourceItem key={node.id} fileId={node.data?.fileId} />
                ))}
              </div>
            </div>
          )}

          {chatParents.length === 0 && resourceParents.length === 0 && (
            <div className="px-4 py-6 text-center text-sm text-secondary">
              No parent nodes
            </div>
          )}
        </div>
      )}
    </div>
  );
}
