import { useState, useCallback } from "react";
import { Handle, Position } from "@xyflow/react";
import { PlusCircle, ArrowUp, Maximize2, GitBranch } from "lucide-react";
import { useSelector } from "react-redux";
import MarkdownRenderer from "../MarkdownRenderer";
import type { ThemeName } from "../../feature/user/userSlice";

export interface ChatMessage {
  role: "user" | "ai";
  content: string;
}

export interface ChatNodeData {
  label?: string;
  icon?: "start" | "branch";
  messages?: ChatMessage[];
  inheritedContext?: boolean;
  [key: string]: unknown;
}

function ChatNode({ data }: { data: ChatNodeData }) {
  const [inputValue, setInputValue] = useState("");
  const theme = useSelector(
    (state: { user: { theme: ThemeName } }) => state.user.theme,
  );

  const messages: ChatMessage[] = data.messages ?? [
    { role: "user", content: "React Flow 的核心概念是什么？" },
    {
      role: "ai",
      content:
        "核心概念主要是 **Nodes**（节点）、**Edges**（连线）和 **Handles**（句柄）。它是基于状态驱动的。",
    },
  ];

  const label = data.label ?? "Chat";
  const icon = data.icon ?? "start";
  const inheritedContext = data.inheritedContext ?? false;

  const onInputChange = useCallback(
    (evt: React.ChangeEvent<HTMLInputElement>) => {
      setInputValue(evt.target.value);
    },
    [],
  );

  return (
    <div className="node-card rounded-xl flex flex-col w-[350px] h-[450px] text-sm">
      {/* Header */}
      <div className="h-10 border-b border-main flex items-center justify-between px-3 cursor-move">
        <div className="flex items-center gap-2">
          {icon === "branch" && (
            <GitBranch size={12} className="text-secondary" />
          )}
          <span className="text-xs font-bold text-secondary uppercase tracking-wider">
            {label}
          </span>
        </div>
        <Maximize2
          size={12}
          className="text-secondary hover:text-primary cursor-pointer"
          style={{ color: "var(--text-secondary)" }}
        />
      </div>

      {/* Messages */}
      <div
        className={
          "nodrag nopan flex-1 overflow-y-auto p-4 space-y-4 nowheel select-text cursor-default" +
          (theme === "cyber"
            ? " CyberScroller"
            : theme === "saas"
              ? " ModernScroller"
              : "")
        }
      >
        {inheritedContext && (
          <div className="text-[10px] text-center text-secondary mb-2">
            --- Context Inherited ---
          </div>
        )}

        {messages.map((msg, i) =>
          msg.role === "user" ? (
            <div key={i} className="flex justify-end">
              <div className="bubble-user max-w-[85%] p-2.5 rounded-2xl rounded-tr-sm text-sm cursor-text">
                {msg.content}
              </div>
            </div>
          ) : (
            <div key={i} className="flex justify-start max-w-[85%] cursor-text">
              <MarkdownRenderer content={msg.content} theme={theme} />
            </div>
          ),
        )}
      </div>

      {/* Input */}
      <div className="nodrag nopan p-3 border-t border-main">
        <div className="flex items-center gap-2 p-2 rounded border border-main node-bg">
          <PlusCircle
            size={16}
            className="text-secondary cursor-pointer flex-shrink-0"
            style={{ color: "var(--text-secondary)" }}
          />
          <input
            type="text"
            placeholder="Reply..."
            value={inputValue}
            onChange={onInputChange}
            className="nodrag flex-1 bg-transparent text-sm outline-none"
            style={{ color: "var(--text-primary)" }}
          />
          <ArrowUp
            size={16}
            className="cursor-pointer flex-shrink-0"
            style={{ color: "var(--accent)" }}
          />
        </div>
      </div>

      {/* Handles */}
      <Handle
        type="target"
        position={Position.Left}
        className="custom-handle custom-handle-target"
        style={{ top: 30, left: -6 }}
      />
      <Handle
        type="source"
        position={Position.Right}
        className="custom-handle custom-handle-source"
        style={{ top: 30, right: -6 }}
      />
    </div>
  );
}

export default ChatNode;
