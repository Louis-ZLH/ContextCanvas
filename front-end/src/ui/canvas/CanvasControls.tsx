import { type DragEvent, useCallback, useRef, memo } from "react";
import {
  Background,
  BackgroundVariant,
  Controls,
  MiniMap,
  Panel,
} from "@xyflow/react";
import { MessageSquarePlus, FileUp } from "lucide-react";
import type { ThemeName } from "../../feature/user/userSlice";

interface CanvasControlsProps {
  onLayout: (direction: "TB" | "LR") => void;
  onAddNode: (type: "chatNode") => void;
  onUploadFile: (files: File[]) => void;
  theme: ThemeName;
}

export const CanvasControls = memo(function CanvasControls({ onLayout, onAddNode, onUploadFile, theme }: CanvasControlsProps) {
  const fileInputRef = useRef<HTMLInputElement>(null);

  const onDragStart = useCallback(
    (event: DragEvent, nodeType: "chatNode") => {
      event.dataTransfer.setData("application/reactflow", nodeType);
      event.dataTransfer.effectAllowed = "move";
    },
    [],
  );

  const btnClass = `nopan cursor-pointer px-3 py-1.5 rounded-md text-xs font-medium node-card transition-[filter] ${
    theme === "dark" ? "hover:brightness-150" : "hover:brightness-95"
  }`;

  return (
    <>
      {/* Custom arrow marker that uses theme colors */}
      <svg style={{ position: 'absolute', top: 0, left: 0, pointerEvents: 'none' }}>
        <defs>
          <marker
            id="custom-arrow"
            markerWidth="10"
            markerHeight="7"
            refX="9"
            refY="3.5"
            orient="auto"
          >
            <polygon
              points="0 0, 10 3.5, 0 7"
              style={{ fill: "var(--edge-stroke)" }}
            />
          </marker>
        </defs>
      </svg>

      <Panel position="top-left">
        <div className="flex gap-2">
          {/* 布局按钮 */}
          <button onClick={() => onLayout("TB")} className={btnClass}>
            Vertical
          </button>
          <button onClick={() => onLayout("LR")} className={btnClass}>
            Horizontal
          </button>

          <div className="w-px bg-main mx-1" />

          {/* 添加 Chat 节点：可拖拽 + 可点击 */}
          <button
            className={btnClass + " flex items-center gap-1.5 cursor-grab active:cursor-grabbing"}
            draggable
            onDragStart={(e) => onDragStart(e, "chatNode")}
            onClick={() => onAddNode("chatNode")}
            title="Drag to canvas to place, or click to create at center"
          >
            <MessageSquarePlus size={14} />
            Chat
          </button>

          {/* 上传文件按钮 */}
          <input
            ref={fileInputRef}
            type="file"
            multiple
            accept=".pdf,.png,.jpg,.jpeg,.gif,.webp,.svg,.txt,.md,.docx,.xlsx,.pptx,.csv,.json"
            className="hidden"
            onChange={(e) => {
              const files = Array.from(e.target.files ?? []);
              if (files.length > 0) onUploadFile(files);
              e.target.value = "";
            }}
          />
          <button
            className={btnClass + " flex items-center gap-1.5"}
            onClick={() => fileInputRef.current?.click()}
            title="Upload file and create resource node"
          >
            <FileUp size={14} />
            Upload
          </button>
        </div>
      </Panel>
      <Background
        variant={BackgroundVariant.Dots}
        gap={20}
        size={1}
        color="var(--text-secondary)"
        style={{ opacity: 0.3 }}
      />
      <Controls />
      <MiniMap
        position="top-right"
        // Dynamic mask color based on theme
        maskColor={
          theme === "dark"
            ? "rgba(33, 33, 33, 0.7)" // Dark mask
            : "rgba(243, 244, 246, 0.7)" // Light mask for saas/paper
        }
        // Dynamic node color based on theme
        nodeColor={
          theme === "dark"
            ? "#333333" // Neutral dark for nodes
            : "#e5e7eb" // Gray-200 for saas/paper nodes
        }
      />
    </>
  );
});
