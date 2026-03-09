import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type DragEvent,
} from "react";
import {
  ReactFlow,
  useReactFlow,
  ReactFlowProvider,
  type Connection,
  type Node as BaseNode,
  type Edge as BaseEdge,
  type NodeChange,
  type EdgeChange,
} from "@xyflow/react";
import { nanoid } from "@reduxjs/toolkit";
import { useLocation } from "react-router";
import { toast } from "react-hot-toast";
import { Upload, FileUp, Loader2 } from "lucide-react";
import ChatNode from "../../ui/canvas/ChatNode";
import ResourceNode from "../../ui/canvas/ResourceNode";
import CustomEdge from "../../ui/canvas/CustomEdge";
import { CanvasControls } from "../../ui/canvas/CanvasControls";
import Dagre from "@dagrejs/dagre";
import { useSelector, useDispatch } from "react-redux";
import {
  type Node,
  type Edge,
  type Command,
  type AtomicOp,
  interactiveNodeUpdate,
  interactiveEdgeUpdate,
  onConnect,
  updateNodes,
  deleteNodesAndEdges,
  addNode,
  deleteNode,
  patchNodeData,
  undo,
  redo,
  executeCommand,
  executeCommandAndMaximize,
  setMaximizedNode,
  toggleShowControls,
} from "../../feature/canvas/canvasSlice";
import { useSyncCanvas } from "../../feature/canvas/useSyncCanvas";
import {
  isFileAccepted,
  isOldOfficeFormat,
  uploadFile,
} from "../../service/file";
import type { RootState } from "../../store";

const nodeTypes = {
  chatNode: ChatNode,
  resourceNode: ResourceNode,
};

const edgeTypes = {
  "custom-edge": CustomEdge,
};

const HIDDEN_STYLE: React.CSSProperties = {
  visibility: "hidden",
  pointerEvents: "none",
};
const DELETE_KEYS = ["Backspace", "Delete"];
const RESOURCE_NODE_WIDTH = 230;
const GAP = 50;
const RESOURCE_NODE_SPACING = 100;

export function LayoutFlowInner() {
  useSyncCanvas();
  const dispatch = useDispatch();
  const {
    fitView,
    getNodes,
    getEdges,
    screenToFlowPosition,
    getViewport,
    setViewport,
  } = useReactFlow();

  const canvasId: string | null = useSelector(
    (s: RootState) => s.canvas.canvasId,
  );
  const { nodes, edges } = useSelector((state: RootState) => state.canvas);

  const theme = useSelector((state: RootState) => state.user.theme);
  const showControls = useSelector(
    (state: RootState) => state.canvas.showControls,
  );
  const isFullSyncing = useSelector(
    (state: RootState) => state.canvas.isFullSyncing,
  );
  const maximizedNodeId = useSelector(
    (s: RootState) => s.canvas.maximizedNodeId,
  );

  const location = useLocation();
  const hasInitializedRef = useRef(false);
  const nodesRef = useRef<Node[]>([]);

  // ---- 键盘快捷键：Undo / Redo ----
  // 当 ChatNode 最大化时 showControls 为 false，此时跳过注册，等恢复后自动重新监听
  useEffect(() => {
    if (!showControls) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey;
      if (!mod || e.key.toLowerCase() !== "z") return;

      // 避免在 input / textarea / contentEditable 中触发
      const target = e.target as HTMLElement;
      if (
        target.tagName === "INPUT" ||
        target.tagName === "TEXTAREA" ||
        target.isContentEditable
      )
        return;

      e.preventDefault();
      if (e.shiftKey) {
        dispatch(redo());
      } else {
        dispatch(undo());
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [dispatch, showControls]);

  // ---- 将画布容器尺寸写入 CSS 变量，供子节点动态读取 ----
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const observer = new ResizeObserver(([entry]) => {
      const { width, height } = entry.contentRect;
      el.style.setProperty("--canvas-w", `${width}px`);
      el.style.setProperty("--canvas-h", `${height}px`);
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  // ---- 文件拖入视觉反馈 ----
  const [fileDragActive, setFileDragActive] = useState(false);
  const [dragFileCount, setDragFileCount] = useState(0);
  const dragCounterRef = useRef(0); // 计数器：解决子元素 dragenter/leave 冒泡
  const ghostRef = useRef<HTMLDivElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  //处理节点移动
  const handleNodesChange = useCallback(
    (changes: NodeChange[]) => {
      const filtered = changes.filter((change) => change.type !== "remove");
      if (filtered.length > 0) {
        dispatch(interactiveNodeUpdate(filtered));
      }
    },
    [dispatch],
  );

  //处理边变化（选择）
  const handleEdgesChange = useCallback(
    (changes: EdgeChange[]) => {
      const filtered = changes.filter((change) => change.type !== "remove");
      if (filtered.length > 0) {
        dispatch(interactiveEdgeUpdate(filtered));
      }
    },
    [dispatch],
  );

  //处理边连接
  const handleConnect = useCallback(
    (connection: Connection) => {
      dispatch(onConnect(connection));
    },
    [dispatch],
  );

  //处理布局，整理节点位置
  const onLayout = useCallback(
    (direction: "TB" | "LR") => {
      const currentNodes = getNodes();
      const currentEdges = getEdges();
      const layouted = getLayoutedElements(currentNodes, currentEdges, {
        direction,
      });

      const beginNodes: Node[] = [];
      const endNodes: Node[] = [];

      for (const layoutNode of layouted.nodes) {
        const rawNode = currentNodes.find((n) => n.id === layoutNode.id);
        if (!rawNode) continue;

        const beginNode: Node = {
          id: rawNode.id,
          type: rawNode.type as "chatNode" | "resourceNode",
          position: { ...rawNode.position },
          data: { ...rawNode.data },
        };
        const endNode: Node = {
          id: layoutNode.id,
          type: layoutNode.type as "chatNode" | "resourceNode",
          position: { ...layoutNode.position },
          data: { ...layoutNode.data },
        };
        if (
          beginNode.position.x !== endNode.position.x ||
          beginNode.position.y !== endNode.position.y
        ) {
          beginNodes.push(beginNode);
          endNodes.push(endNode);
        }
      }

      if (beginNodes.length > 0) {
        dispatch(updateNodes({ beginNodes, endNodes }));
      }
      window.requestAnimationFrame(() => {
        fitView();
      });
    },
    [getNodes, getEdges, dispatch, fitView],
  );

  //处理节点拖拽开始
  const onNodeDragStart = useCallback(
    (_event: React.MouseEvent, _node: BaseNode, nodes: BaseNode[]) => {
      nodesRef.current = nodes.map((n) => ({
        id: n.id,
        type: n.type as "chatNode" | "resourceNode",
        position: { ...n.position },
        data: { ...n.data },
      }));
    },
    [nodesRef],
  );

  //处理节点拖拽结束
  const onNodeDragStop = useCallback(
    (_event: React.MouseEvent, _node: BaseNode, nodes: BaseNode[]) => {
      const beginNodesMap = new Map<string, Node>(
        nodesRef.current.map((n) => [n.id, n]),
      );

      const beginNodes: Node[] = [];
      const endNodes: Node[] = [];

      for (const curNode of nodes) {
        const prevNode = beginNodesMap.get(curNode.id);
        if (!prevNode) continue;

        if (
          prevNode.position.x === curNode.position.x &&
          prevNode.position.y === curNode.position.y
        ) {
          continue;
        }

        const endNode: Node = {
          id: curNode.id,
          type: curNode.type as "chatNode" | "resourceNode",
          position: { ...curNode.position },
          data: { ...curNode.data },
        };
        beginNodes.push(prevNode);
        endNodes.push(endNode);
      }
      if (beginNodes.length > 0) {
        dispatch(updateNodes({ beginNodes, endNodes }));
      }
    },
    [dispatch],
  );

  // ---- 文件拖入：容器 dragenter / dragleave ----
  const onContainerDragEnter = useCallback(
    (event: DragEvent<HTMLDivElement>) => {
      if (!event.dataTransfer.types.includes("Files")) return;
      dragCounterRef.current++;
      if (dragCounterRef.current === 1) {
        setDragFileCount(event.dataTransfer.items.length);
        setFileDragActive(true);
      }
    },
    [],
  );

  const onContainerDragLeave = useCallback(
    (event: DragEvent<HTMLDivElement>) => {
      if (!event.dataTransfer.types.includes("Files")) return;
      dragCounterRef.current--;
      if (dragCounterRef.current === 0) {
        setFileDragActive(false);
      }
    },
    [],
  );

  // ---- 拖拽放置：内部节点 / 外部文件 ----
  const onDragOver = useCallback((event: DragEvent) => {
    event.preventDefault();
    const isFile = event.dataTransfer.types.includes("Files");
    event.dataTransfer.dropEffect = isFile ? "copy" : "move";

    // 更新虚影位置（直接 DOM 操作，避免 60fps state 更新）
    if (isFile && ghostRef.current && containerRef.current) {
      const rect = containerRef.current.getBoundingClientRect();
      ghostRef.current.style.transform = `translate(${event.clientX - rect.left + 16}px, ${event.clientY - rect.top + 16}px)`;
    }
  }, []);

  const onDrop = useCallback(
    (event: DragEvent) => {
      event.preventDefault();

      // 重置文件拖入状态
      setFileDragActive(false);
      dragCounterRef.current = 0;

      // ① 内部拖拽（从工具栏拖 ChatNode）
      const internalType = event.dataTransfer.getData("application/reactflow");
      if (internalType === "chatNode") {
        const position = screenToFlowPosition({
          x: event.clientX,
          y: event.clientY,
        });
        dispatch(addNode({ type: "chatNode", position, data: {} }));
        return;
      }

      // ② 外部文件拖放 → 创建 Resource 节点
      const files = Array.from(event.dataTransfer.files);
      if (files.length === 0) return;

      // 过滤不支持的文件
      const oldOffice = files.filter((f) => isOldOfficeFormat(f));
      if (oldOffice.length > 0) {
        toast.error(
          "不支持旧版 Office 格式（.doc/.xls/.ppt），请转换为 .docx/.xlsx/.pptx 后重新上传",
        );
      }
      const accepted = files.filter((f) => isFileAccepted(f));
      const otherRejected = files.length - oldOffice.length - accepted.length;
      if (otherRejected > 0) {
        toast.error(
          `${otherRejected} files rejected, only support images, pdfs, docs, spreadsheets, and text files`,
        );
      }
      if (accepted.length === 0) return;

      for (let i = 0; i < accepted.length; i++) {
        const file = accepted[i];
        const nodeId = nanoid();

        // 多文件时向右下方依次偏移，避免完全重叠
        const position = screenToFlowPosition({
          x: event.clientX + i * 40,
          y: event.clientY + i * 40,
        });

        // 先创建 uploading 状态的节点（可 undo）
        dispatch(
          addNode({
            id: nodeId,
            type: "resourceNode",
            position,
            data: {
              // 什么都不传
            },
          }),
        );

        // 异步上传，完成后 patch 节点数据（不进 undo 栈）
        (async () => {
          try {
            const { success, message, data } = await uploadFile(file);
            if (!success || !data) {
              throw new Error(message);
            }

            //利用sync完成bindFileIdToNode操作
            if (!data.fileId) {
              throw new Error("File ID is missing");
            }
            dispatch(
              patchNodeData({
                id: nodeId,
                data: { fileId: data.fileId },
              }),
            );
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
    [dispatch, screenToFlowPosition],
  );

  // ---- 点击按钮在 viewport 中心创建节点 ----
  const onAddNode = useCallback(
    (nodeType: "chatNode") => {
      const { x, y, zoom } = getViewport();
      const reactFlowBounds = document
        .querySelector(".react-flow")
        ?.getBoundingClientRect();
      if (!reactFlowBounds) return;

      const centerX = -x / zoom + reactFlowBounds.width / 2 / zoom;
      const centerY = -y / zoom + reactFlowBounds.height / 2 / zoom;

      // 随机偏移防止反复点击节点重叠
      const offsetX = (Math.random() - 0.5) * 60;
      const offsetY = (Math.random() - 0.5) * 60;

      dispatch(
        addNode({
          type: nodeType,
          position: { x: centerX + offsetX, y: centerY + offsetY },
          data: {},
        }),
      );
    },
    [dispatch, getViewport],
  );

  // ---- 点击上传按钮：在 viewport 中心创建 ResourceNode ----
  const onUploadFile = useCallback(
    (files: File[]) => {
      const oldOffice = files.filter((f) => isOldOfficeFormat(f));
      if (oldOffice.length > 0) {
        toast.error(
          "不支持旧版 Office 格式（.doc/.xls/.ppt），请转换为 .docx/.xlsx/.pptx 后重新上传",
        );
      }
      const accepted = files.filter((f) => isFileAccepted(f));
      const otherRejected = files.length - oldOffice.length - accepted.length;
      if (otherRejected > 0) {
        toast.error(
          `${otherRejected} files rejected, only support images, pdfs, docs, spreadsheets, and text files`,
        );
      }
      if (accepted.length === 0) return;

      const { x, y, zoom } = getViewport();
      const reactFlowBounds = document
        .querySelector(".react-flow")
        ?.getBoundingClientRect();
      if (!reactFlowBounds) return;

      const centerX = -x / zoom + reactFlowBounds.width / 2 / zoom;
      const centerY = -y / zoom + reactFlowBounds.height / 2 / zoom;

      for (let i = 0; i < accepted.length; i++) {
        const file = accepted[i];
        const nodeId = nanoid();

        const offsetX = (Math.random() - 0.5) * 60 + i * 40;
        const offsetY = (Math.random() - 0.5) * 60 + i * 40;

        dispatch(
          addNode({
            id: nodeId,
            type: "resourceNode",
            position: { x: centerX + offsetX, y: centerY + offsetY },
            data: {},
          }),
        );

        (async () => {
          try {
            const { success, message, data } = await uploadFile(file);
            if (!success || !data) {
              throw new Error(message);
            }
            if (!data.fileId) {
              throw new Error("File ID is missing");
            }
            dispatch(
              patchNodeData({
                id: nodeId,
                data: { fileId: data.fileId },
              }),
            );
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
    [dispatch, getViewport],
  );

  //处理节点和边删除
  const onDelete = useCallback(
    ({ nodes, edges }: { nodes: BaseNode[]; edges: BaseEdge[] }) => {
      const nodesToDelete: Node[] = nodes.map((n) => ({
        id: n.id,
        type: n.type as "chatNode" | "resourceNode",
        position: { ...n.position },
        data: { ...n.data },
      }));
      const edgesToDelete: Edge[] = edges.map((e) => ({
        id: e.id,
        source: e.source,
        target: e.target,
        type: "custom-edge",
      }));
      dispatch(
        deleteNodesAndEdges({ nodes: nodesToDelete, edges: edgesToDelete }),
      );
    },
    [dispatch],
  );

  const skipFitViewForCanvasRef = useRef<string | null>(null);
  const viewportOverrideRef = useRef<{ x: number; y: number; zoom: number } | null>(null);

  // Set skip flag during render (before any effects) so fitView is blocked on first mount too
  const locState = location.state as { initialMode?: "ask" | "resources" } | null;
  if (locState?.initialMode && canvasId && skipFitViewForCanvasRef.current !== canvasId) {
    skipFitViewForCanvasRef.current = canvasId;
  }

  useEffect(() => {
    if (canvasId && skipFitViewForCanvasRef.current !== canvasId) {
      fitView();
    }
  }, [fitView, canvasId]);

  // ---- Initial setup from location.state (ask / resources mode) ----
  useEffect(() => {
    if (hasInitializedRef.current) return;

    const state = location.state as {
      initialMode?: "ask" | "resources";
      fileIds?: string[];
    } | null;

    if (!state?.initialMode || !canvasId) return;
    if (isFullSyncing) return;

    hasInitializedRef.current = true;
    skipFitViewForCanvasRef.current = canvasId;
    window.history.replaceState({}, "");

    if (state.initialMode === "ask") {
      const chatNodeId = nanoid();
      const chatNode: Node = {
        id: chatNodeId,
        type: "chatNode",
        position: { x: 0, y: 0 },
        data: {},
      };
      const cmd: Command = {
        canvas_id: canvasId,
        timeStamp: Date.now(),
        forward: [{ type: "create_node", data: chatNode }],
        backward: [{ type: "delete_node", data: chatNode }],
      };
      dispatch(executeCommandAndMaximize({ cmd, nodeId: chatNodeId }));
      const rect = containerRef.current!.getBoundingClientRect();
      const cw = rect.width;
      const ch = rect.height;
      const nodeW = cw / 1.1;
      const nodeH = ch / 1.1;
      const zoom = Math.min(cw / nodeW, ch / nodeH) * 1.02;
      const x = (cw - nodeW * zoom) / 2 - 15;
      const y = (ch - nodeH * zoom) / 2;
      viewportOverrideRef.current = { x, y, zoom };
    }

    if (state.initialMode === "resources" && state.fileIds?.length) {
      const chatNodeId = nanoid();
      const chatNode: Node = {
        id: chatNodeId,
        type: "chatNode",
        position: { x: RESOURCE_NODE_WIDTH + GAP, y: 0 },
        data: {},
      };

      const fileIds = state.fileIds;
      const count = fileIds.length;
      const startY = -((count - 1) * RESOURCE_NODE_SPACING) / 2;

      const forward: AtomicOp[] = [];
      const backward: AtomicOp[] = [];

      forward.push({ type: "create_node", data: chatNode });

      const resourceNodes: Node[] = fileIds.map((fileId, i) => ({
        id: nanoid(),
        type: "resourceNode" as const,
        position: { x: 0, y: startY + i * RESOURCE_NODE_SPACING },
        data: { fileId },
      }));
      for (const rn of resourceNodes) {
        forward.push({ type: "create_node", data: rn });
      }

      const newEdges: Edge[] = resourceNodes.map((rn) => ({
        id: nanoid(),
        source: rn.id,
        target: chatNodeId,
        type: "custom-edge" as const,
      }));
      for (const edge of newEdges) {
        forward.push({ type: "create_edge", data: edge });
      }

      for (const edge of newEdges) {
        backward.unshift({ type: "delete_edge", data: edge });
      }
      for (const rn of resourceNodes) {
        backward.unshift({ type: "delete_node", data: rn });
      }
      backward.unshift({ type: "delete_node", data: chatNode });

      const cmd: Command = {
        canvas_id: canvasId,
        timeStamp: Date.now(),
        forward,
        backward,
      };
      dispatch(executeCommand(cmd));
      requestAnimationFrame(() => {
        fitView({ padding: 1 });
      });
    }
  }, [canvasId, isFullSyncing, dispatch, fitView, location.state]);

  // Apply viewport override on every mount (survives StrictMode remount)
  useEffect(() => {
    if (viewportOverrideRef.current) {
      setViewport(viewportOverrideRef.current, { duration: 0 });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // ---- Defensive: reset maximizedNodeId if the node was removed (e.g. undo) ----
  useEffect(() => {
    if (maximizedNodeId && !nodes.find((n) => n.id === maximizedNodeId)) {
      dispatch(setMaximizedNode(null));
      dispatch(toggleShowControls(true));
    }
  }, [nodes, maximizedNodeId, dispatch]);
  
  return (
    <div
      ref={containerRef}
      className="w-full h-full bg-canvas relative"
      onDragEnter={onContainerDragEnter}
      onDragLeave={onContainerDragLeave}
    >
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        onNodesChange={handleNodesChange}
        onEdgesChange={handleEdgesChange}
        onConnect={handleConnect}
        onNodeDragStart={onNodeDragStart}
        onNodeDragStop={onNodeDragStop}
        onDelete={onDelete}
        deleteKeyCode={showControls ? DELETE_KEYS : null}
        onDragOver={onDragOver}
        onDrop={onDrop}
        minZoom={0.4}
        maxZoom={2}
      >
        <div style={showControls ? undefined : HIDDEN_STYLE}>
          <CanvasControls
            onLayout={onLayout}
            onAddNode={onAddNode}
            onUploadFile={onUploadFile}
            theme={theme}
          />
        </div>
      </ReactFlow>

      {/* ── Full Sync Loading Overlay ── */}
      {isFullSyncing && (
        <div className="absolute inset-0 z-50 flex items-center justify-center bg-black/30 backdrop-blur-sm rounded-xl">
          <div className="flex flex-col items-center gap-3">
            <Loader2
              size={36}
              className="animate-spin"
              style={{ color: "var(--accent)" }}
            />
            <p
              className="text-sm font-medium"
              style={{ color: "var(--text-primary)" }}
            >
              Syncing from server...
            </p>
          </div>
        </div>
      )}

      {/* ── File Drop Overlay + Ghost ── */}
      {fileDragActive && (
        <>
          {/* 全屏半透明提示遮罩 */}
          <div
            className="absolute inset-0 z-50 pointer-events-none rounded-xl
            border-2 border-dashed border-accent/40 bg-accent/5 backdrop-blur-[1px]
            flex items-center justify-center transition-opacity duration-150"
          >
            <div className="flex flex-col items-center gap-3">
              <div className="w-16 h-16 rounded-full bg-accent/10 flex items-center justify-center">
                <Upload size={28} style={{ color: "var(--accent)" }} />
              </div>
              <p
                className="text-sm font-semibold"
                style={{ color: "var(--accent)" }}
              >
                Drop files to create resource nodes
              </p>
              <p className="text-xs text-secondary">
                Images · PDF · Excel · Documents
              </p>
            </div>
          </div>

          {/* 光标跟随虚影 */}
          <div
            ref={ghostRef}
            className="absolute top-0 left-0 z-51 pointer-events-none"
          >
            <div
              className="node-card rounded-lg px-3 py-2 shadow-lg
              flex items-center gap-2 opacity-85 border border-accent/30"
            >
              <FileUp size={14} style={{ color: "var(--accent)" }} />
              <span
                className="text-xs font-medium whitespace-nowrap"
                style={{ color: "var(--text-primary)" }}
              >
                {dragFileCount} {dragFileCount === 1 ? "file" : "files"}
              </span>
            </div>
          </div>
        </>
      )}
    </div>
  );
}

function Canvas() {
  return (
    <ReactFlowProvider>
      <LayoutFlowInner />
    </ReactFlowProvider>
  );
}

export default Canvas;

const getLayoutedElements = (
  // eslint-disable-next-line
  nodes: any[],
  // eslint-disable-next-line
  edges: any[],
  options: { direction: "TB" | "LR" },
) => {
  const g = new Dagre.graphlib.Graph().setDefaultEdgeLabel(() => ({}));
  g.setGraph({ rankdir: options.direction });

  edges.forEach((edge) => g.setEdge(edge.source, edge.target));
  nodes.forEach((node) =>
    g.setNode(node.id, {
      ...node,
      width: node.measured?.width ?? 0,
      height: node.measured?.height ?? 0,
    }),
  );

  Dagre.layout(g);

  return {
    nodes: nodes.map((node) => {
      const position = g.node(node.id);
      const x = position.x - (node.measured?.width ?? 0) / 2;
      const y = position.y - (node.measured?.height ?? 0) / 2;
      return { ...node, position: { x, y } };
    }),
    edges,
  };
};
