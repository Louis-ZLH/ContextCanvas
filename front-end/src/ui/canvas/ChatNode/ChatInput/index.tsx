import { useState, useRef, useCallback, useEffect, useContext, memo } from "react";
import { X, Loader2 } from "lucide-react";
import PlusMenu from "./PlusMenu";
import ModelMenu from "./ModelMenu";
import VoiceButton from "./VoiceButton";
import SendButton from "./SendButton";
import StopButton from "./StopButton";
import { useSelector, useDispatch } from "react-redux";
import { selectCurrentLeafIdByConversationId, selectConversationHasCreated, loadConversations, loadMessages } from "../../../../feature/chat/chatSlice";
import type { RootState } from "../../../../store";
import { streamContext } from "../index";
import { MODELS } from "../constant";
import { modelsContext } from "../index";
import { toast } from "react-hot-toast";
import { createConversation } from "../../../../service/chat";
import type { Conversation, Message } from "../../../../feature/chat/types";

const MAX_CONTENT_LENGTH = 15000;

interface ChatInputProps {
  conversationId: string;
  inputValue: string;
  onInputValueChange: (value: string) => void;
  isMaximized: boolean;
  isBottom?: boolean;
  onUploadFile?: (files: File[]) => void;
}

function ChatInput({
  conversationId,
  inputValue,
  onInputValueChange,
  isMaximized,
  isBottom = true,
  onUploadFile,
}: ChatInputProps) {
  const hasContent = !!inputValue.trim();
  const isTooLong = inputValue.length > MAX_CONTENT_LENGTH;
  const dispatch = useDispatch();
  const { modelIndex, changeModelIndex } = useContext(modelsContext)!;
  const [isMultiline, setIsMultiline] = useState(false);
  const [isCreating, setIsCreating] = useState(false);
  const [mode, setMode] = useState<null | "Study & Learn" | "Web Search">(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const isSendingRef = useRef(false);
  const { send, stop, isStreaming } = useContext(streamContext)!;
  const conversationHasCreated = useSelector((state: RootState) => selectConversationHasCreated(state, conversationId));
  const currentLeafId = useSelector((state: RootState) => selectCurrentLeafIdByConversationId(state, conversationId));
  const canvasId = useSelector((state: RootState) => state.canvas.canvasId);

  const handleSend = useCallback(async () => {
    const content = inputValue.trim();
    if (!content || isStreaming || isSendingRef.current || isTooLong) return;
    if (!canvasId) {
      toast.error("Canvas not found");
      return;
    }
    if (!conversationHasCreated || !currentLeafId) {
      // 如果没有rootMessageId和currentLeafId，则认为会话未创建，需要创建会话。
      isSendingRef.current = true;
      setIsCreating(true);
      try {
        const { success, message, data } = await createConversation(conversationId, content, canvasId); //传content让AI生成title
        if (!success) {
          toast.error(message);
          return;
        }
        // 载入会话和根消息
        dispatch(loadConversations([data?.conversation as Conversation]));
        dispatch(loadMessages([data?.rootMessage as Message]));
        // 使用返回的 currentLeafId，因为闭包中的 currentLeafId 仍是 null
        send(content, modelIndex, data?.conversation?.currentLeafId as string);
        onInputValueChange("");
        setIsMultiline(false);
        const el = textareaRef.current;
        if (el) {
          el.style.height = "auto";
        }
      } finally {
        isSendingRef.current = false;
        setIsCreating(false);
      }
      return;
    }
    send(content, modelIndex, currentLeafId);
    onInputValueChange("");
    setIsMultiline(false);
    const el = textareaRef.current;
    if (el) {
      el.style.height = "auto";
    }
  }, [inputValue, isStreaming, isTooLong, send, modelIndex, currentLeafId, onInputValueChange, conversationHasCreated, dispatch, conversationId, canvasId]);

  useEffect(() => {
    const el = textareaRef.current;
    if (el) {
      el.style.height = "auto";
      el.style.height = Math.min(el.scrollHeight, 120) + "px";
    }
  }, [isMultiline]);

  const handleTextareaChange = useCallback(
    (evt: React.ChangeEvent<HTMLTextAreaElement>) => {
      const value = evt.target.value;
      onInputValueChange(value);
      const el = evt.target;
      el.style.height = "auto";
      const scrollH = el.scrollHeight;
      el.style.height = Math.min(scrollH, 120) + "px";
      setIsMultiline((prev) => {
        if (!value) return false;
        if (prev) return true;
        return scrollH > 42;
      });
    },
    [onInputValueChange],
  );

  const handleKeyDown = useCallback(
    (evt: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (evt.nativeEvent.isComposing) return;
      if (evt.key === "Enter" && !evt.shiftKey) {
        evt.preventDefault();
        handleSend();
      }
    },
    [handleSend],
  );

  const handleVoiceInput = useCallback(
    (text: string) => {
      onInputValueChange(inputValue + text);
      const el = textareaRef.current;
      if (el) {
        requestAnimationFrame(() => {
          el.style.height = "auto";
          el.style.height = Math.min(el.scrollHeight, 120) + "px";
          if (el.scrollHeight > 42) setIsMultiline(true);
        });
      }
    },
    [onInputValueChange, inputValue],
  );

  return (
    <div
      className={`${isBottom ? "absolute bottom-0" : ""} pb-1.5 rounded-t-4xl ${isMaximized ? "" : "left-5 right-5"}`}
      style={{
        borderColor: "var(--border-main)",
        backgroundColor: "var(--node-bg)",
        ...(isMaximized && isBottom
          ? {
              left: "max(1rem, calc((100% - 700px) / 2))",
              right: "max(1rem, calc((100% - 700px) / 2))",
            }
          : {}),
        ...(!isBottom
          ? {
              width: "min(700px, 90%)",
            }
          : {}),
      }}
    >
      <div
        className="flex flex-col rounded-4xl border-[0.5px] shadow-sm nowheel"
        style={{ borderColor: "var(--border-main)", backgroundColor: "var(--input-bg, transparent)" }}
      >
        <div className={isMultiline || mode ? "" : "relative"}>
          <textarea
            ref={textareaRef}
            placeholder="Ask Anything..."
            value={inputValue}
            onChange={handleTextareaChange}
            onKeyDown={handleKeyDown}
            rows={1}
            className={`w-full bg-transparent text-[14px] outline-none resize-none mt-3 mb-1.5 leading-relaxed overflow-wrap:break-word nowheel ${
              isMultiline || mode ? "px-5" : "pl-10 pr-32"
            }`}
            style={{
              color: "var(--text-primary)",
              maxHeight: "120px",
            }}
          />
          {!(isMultiline || mode) && (
            <>
              <PlusMenu position="absolute" expandUp={isBottom} onSelectMode={setMode} onUploadFile={onUploadFile} />
              <div className="absolute right-2 top-1/2 -translate-y-1/2 flex items-center gap-1">
                <ModelMenu models={MODELS} selectedIndex={modelIndex} onSelect={changeModelIndex} expandUp={isBottom} />
                {isStreaming ? <StopButton onClick={stop} /> : isCreating ? <span className="p-1.5 rounded-full" style={{ backgroundColor: "var(--accent)" }}><Loader2 size={14} className="animate-spin" style={{ color: "#fff" }} /></span> : hasContent ? <SendButton onClick={handleSend} disabled={isTooLong} /> : <VoiceButton onTranscript={handleVoiceInput} />}
              </div>
            </>
          )}
        </div>
        <div
          className={`flex items-center justify-between px-2 transition-all duration-300 ease-in-out ${isMultiline || mode ? "overflow-visible" : "overflow-hidden"}`}
          style={{
            maxHeight: isMultiline || mode ? "40px" : "0px",
            paddingBottom: isMultiline || mode ? "6px" : "0px",
            opacity: isMultiline || mode ? 1 : 0,
          }}
        >
          <div className="flex items-center gap-1.5">
            <PlusMenu position="inline" expandUp={isBottom} onSelectMode={setMode} onUploadFile={onUploadFile} />
            {mode && (
              <button
                className="flex items-center gap-1.5 rounded-full px-3 py-1 text-[12px] font-medium cursor-pointer transition-all duration-200 hover:brightness-110 hover:shadow-sm"
                style={{
                  backgroundColor:
                    mode === "Study & Learn"
                      ? "rgba(124, 58, 237, 0.15)"
                      : "rgba(14, 165, 233, 0.15)",
                  color:
                    mode === "Study & Learn" ? "#8b5cf6" : "#0ea5e9",
                  border: `1px solid ${
                    mode === "Study & Learn"
                      ? "rgba(139, 92, 246, 0.3)"
                      : "rgba(14, 165, 233, 0.3)"
                  }`,
                }}
                onClick={() => setMode(null)}
              >
                {mode}
                <X
                  size={12}
                  style={{
                    color:
                      mode === "Study & Learn" ? "#8b5cf6" : "#0ea5e9",
                  }}
                />
              </button>
            )}
          </div>
          <div className="flex items-center gap-1">
            <ModelMenu models={MODELS} selectedIndex={modelIndex} onSelect={changeModelIndex} expandUp={isBottom} />
            {isStreaming ? <StopButton onClick={stop} /> : isCreating ? <span className="p-1.5 rounded-full" style={{ backgroundColor: "var(--accent)" }}><Loader2 size={14} className="animate-spin" style={{ color: "#fff" }} /></span> : hasContent ? <SendButton onClick={handleSend} disabled={isTooLong} /> : <VoiceButton onTranscript={handleVoiceInput} />}
          </div>
        </div>
      </div>
      {isBottom ? <p
        className="text-center p-0.5 text-[10px] leading-none"
        style={{ color: "var(--text-secondary)" }}
      >
        AI may make mistakes. Verify important info.
      </p> : <div className="h-4"></div>
      }
    </div>
  );
}

export default memo(ChatInput);
