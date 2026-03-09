import { useState, useCallback, useRef, useMemo, memo } from "react";
import { Handle, Position, useReactFlow, type Viewport } from "@xyflow/react";
import { useSelector, useDispatch, useStore } from "react-redux";
import type { ThemeName } from "../../../feature/user/userSlice";
import { deleteNode, toggleShowControls, setMaximizedNode, addNodeWithEdge, patchNodeData } from "../../../feature/canvas/canvasSlice";
import { nanoid } from "@reduxjs/toolkit";
import { toast } from "react-hot-toast";
import { isFileAccepted, isFileTooLarge, isOldOfficeFormat, uploadFile } from "../../../service/file";
import { queryClient } from "../../../query";
import ChatNodeHeader from "./ChatNodeHeader";
import WelcomeScreen from "./WelcomeScreen";
import MessageList from "./Message/MessageList";
import ChatInput from "./ChatInput";
import ChatNodeLoading from "./ChatNodeLoading";
import ChatNodeError from "./ChatNodeError";
import { useConversationLoader } from "../../../feature/chat/useConversationLoader";
import { makeSelectCurrentThreadIdsByConversationId } from "../../../feature/chat/chatSlice";
import type { RootState } from "../../../store";
import { shallowEqual } from "react-redux";
import { createContext } from "react";
import { useChatStream } from "../../../feature/chat/useChatStream";


// eslint-disable-next-line react-refresh/only-export-components
export const streamContext = createContext<ReturnType<typeof useChatStream> | null>(null);
// eslint-disable-next-line react-refresh/only-export-components
export const modelsContext = createContext<{
  modelIndex: number;
  changeModelIndex: (index: number) => void;
}>({
  modelIndex: 0,
  changeModelIndex: () => {},
});

function ChatNode({ id, selected }: { id: string; selected?: boolean }) {
  const dispatch = useDispatch(); 
  const [modelIndex, setModelIndex] = useState(0);
  const modelsContextValue = useMemo(() => ({
    modelIndex,
    changeModelIndex: setModelIndex,
  }), [modelIndex]);
  const { send, stop, isStreaming, sendSignalRef } = useChatStream(id);
  const stream = useMemo(() => ({ send, stop, isStreaming, sendSignalRef }), [send, stop, isStreaming, sendSignalRef]);
  const title = useSelector((state: RootState) => state.chat.conversations[id]?.title);
  const { isLoading, error } = useConversationLoader(id);
  // eslint-disable-next-line
  const selectCurrentThreadIdsByConversationId = useMemo(makeSelectCurrentThreadIdsByConversationId, []);
  const threadIds = useSelector((state: RootState) => selectCurrentThreadIdsByConversationId(state, id), shallowEqual);

  const { fitView, getViewport, setViewport } = useReactFlow();
  const store = useStore<RootState>();
  const [inputValue, setInputValue] = useState("");
  const theme = useSelector(
    (state: { user: { theme: ThemeName } }) => state.user.theme,
  );
  const isMaximized = useSelector(
    (state: { canvas: { maximizedNodeId: string | null } }) => state.canvas.maximizedNodeId === id,
  );
  const viewportState = useRef<{ x: number; y: number; zoom: number } | null>(
    null,
  );
  const label = title ?? "New Chat";

  const handleSizeChange = useCallback(() => {
    dispatch(toggleShowControls());
    if (!isMaximized) {
      viewportState.current = getViewport();
      dispatch(setMaximizedNode(id));
      window.requestAnimationFrame(() => {
        fitView({
          nodes: [{ id }],
          padding: -0.1,
          maxZoom: 1.1,
          duration: 500,
        });
      });
    } else {
      dispatch(setMaximizedNode(null));
      if (viewportState.current) {
        setViewport(viewportState.current as Viewport, { duration: 500 });
      } else {
        // Node was created already maximized (ask mode), no saved viewport
        window.requestAnimationFrame(() => {
          fitView({ duration: 500 });
        });
      }
      viewportState.current = null;
    }
  }, [isMaximized, dispatch, fitView, getViewport, setViewport, id]);

  const handleDelete = useCallback(() => {
    dispatch(deleteNode(id));
  }, [dispatch, id]);

  const handleUploadFile = useCallback(
    (files: File[]) => {
      const oldOffice = files.filter((f) => isOldOfficeFormat(f));
      if (oldOffice.length > 0) {
        toast.error("Legacy Office formats (.doc/.xls/.ppt) are not supported. Please convert to .docx/.xlsx/.pptx and re-upload");
      }
      const accepted = files.filter((f) => isFileAccepted(f));
      const tooLarge = accepted.filter((f) => isFileTooLarge(f));
      if (tooLarge.length > 0) {
        toast.error(`${tooLarge.length} file(s) exceed 5MB limit`);
      }
      const validFiles = accepted.filter((f) => !isFileTooLarge(f));
      const otherRejected = files.length - oldOffice.length - accepted.length;
      if (otherRejected > 0) {
        toast.error(`${otherRejected} files rejected, only support images, pdfs, docs, spreadsheets, and text files`);
      }
      // 惰性读取：只在用户上传时才取最新值，不订阅 nodes 变化
      const state = store.getState();
      const nodePosition = state.canvas.nodes.find((n) => n.id === id)?.position;
      const canvasId = state.canvas.canvasId;
      if (validFiles.length === 0 || !nodePosition || !canvasId) return;

      // ChatNode: w-[400px], ResourceNode: w-[230px]
      const RESOURCE_NODE_WIDTH = 230;
      const GAP = 50;

      for (let i = 0; i < validFiles.length; i++) {
        const file = validFiles[i];
        const nodeId = nanoid();

        // 将 ResourceNode 放在 ChatNode 左侧，留出间距让连线清晰
        dispatch(addNodeWithEdge({
          node: {
            id: nodeId,
            type: "resourceNode",
            position: {
              x: nodePosition.x - RESOURCE_NODE_WIDTH - GAP + i * 30,
              y: nodePosition.y + i * 80,
            },
            data: {},
          },
          targetNodeId: id,
        }));

        // 异步上传，完成后 patch 节点数据
        (async () => {
          try {
            const { success, message, data } = await uploadFile(file);
            if (!success || !data) {
              throw new Error(message);
            }
            if (!data.fileId) {
              throw new Error("File ID is missing");
            }
            dispatch(patchNodeData({ id: nodeId, data: { fileId: data.fileId } }));
            queryClient.invalidateQueries({ queryKey: ["file", "list"] });
            queryClient.invalidateQueries({ queryKey: ["file", "storage"] });
          } catch (error: unknown) {
            if (error instanceof Error) {
              toast.error(error.message);
            } else {
              toast.error("Failed to upload file");
            }
            dispatch(deleteNode(nodeId));
          }
        })();
      }
    },
    [dispatch, store, id],
  );

  const isEmpty = threadIds.length === 0;

  return (
    <streamContext.Provider value={stream}>
      <modelsContext.Provider value={modelsContextValue}>
        <div
          className={`node-card rounded-xl flex flex-col text-sm ${isMaximized ? "nodrag nopan relative" : "h-[500px] w-[400px]"} ${selected ? "node-selected" : ""}`}
          style={
            isMaximized
              ? {
                  width: "calc(var(--canvas-w)/1.1 + 10px)",
                  height: "calc(var(--canvas-h)/1.1 + 10px)",
                }
              : undefined
          }
        >
          <ChatNodeHeader
            label={label}
            isMaximized={isMaximized}
            onSizeChange={handleSizeChange}
            onDelete={handleDelete}
          />

          <div className="nodrag nopan nowheel cursor-default flex flex-col flex-1 min-h-0 relative">
            {isLoading ? (
              <ChatNodeLoading />
            ) : error ? (
              <ChatNodeError message={error} />
            ) : isEmpty ? (
              <WelcomeScreen onSuggestionClick={setInputValue}>
                <ChatInput
                conversationId={id}
                inputValue={inputValue}
                onInputValueChange={setInputValue}
                isMaximized={isMaximized}
                isBottom={false}
                onUploadFile={handleUploadFile}
              />
            </WelcomeScreen>
            ) : (
              <MessageList
                threadIds={threadIds}
                theme={theme}
                isMaximized={isMaximized}
              />
            )}

            {!isLoading && !error && !isEmpty && <ChatInput
              conversationId={id}
              inputValue={inputValue}
              onInputValueChange={setInputValue}
              isMaximized={isMaximized}
              isBottom={true}
              onUploadFile={handleUploadFile}
            />}
          </div>

          <Handle
            type="target"
            position={Position.Left}
            className="custom-handle custom-handle-target"
            style={{ top: 30, left: -8 }}
          />
          <Handle
            type="source"
            position={Position.Right}
            className="custom-handle custom-handle-source"
            style={{ top: 30, right: -8 }}
          />
        </div>
      </modelsContext.Provider>
    </streamContext.Provider>
  );
}

export default memo(ChatNode, (prev, next) =>
  prev.id === next.id && prev.selected === next.selected
);
