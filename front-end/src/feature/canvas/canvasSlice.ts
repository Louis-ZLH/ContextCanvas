import { createSlice, createSelector, type PayloadAction, nanoid } from "@reduxjs/toolkit";
import { applyNodeChanges, applyEdgeChanges, type NodeChange, type EdgeChange, type Connection } from "@xyflow/react";
import type { Node, Edge, Command, CanvasState, AtomicOp } from "./types";
import { emptyDelta, applyCommand, applyOps, mergeOpsToDelta } from "./canvasOps";
import type { syncResponse, fullSyncResponse } from "../../service/type";

// export types for external use
export type { Node, Edge, NodeData, FileCategory, AtomicOp, Command, CanvasState, GraphDelta } from "./types";

const initialState: CanvasState = {
  canvasId: null,
  title: "",
  showControls: true,
  maximizedNodeId: null,
  nodes: [],
  edges: [],
  version: 0,
  undoStack: [],
  redoStack: [],
  maxHistory: 50,
  pendingDelta: emptyDelta(),
  syncStatus: "idle",
  syncFailCount: 0,
  isFullSyncing: false,
};

const canvasSlice = createSlice({
  name: "canvas",
  initialState,
  reducers: {
    loadCanvas(
      state, 
      action: PayloadAction<{
        canvasId: string;
        title: string;
        nodes: Node[];
        edges: Edge[];
        version: number;
      }>
    ) {
      const { canvasId, title, nodes, edges, version } = action.payload;
      state.canvasId = canvasId;
      state.title = title;
      state.nodes = nodes;
      state.edges = edges;
      state.version = version;
      // 加载时清空所有历史
      state.undoStack = [];
      state.redoStack = [];
      state.pendingDelta = emptyDelta();
      state.showControls = true;
      state.maximizedNodeId = null;
    },
    /** 执行一个可撤销的操作 ，增，删*/
    executeCommand(state, action: PayloadAction<Command>) {
      applyCommand(state, action.payload);
    },
    undo(state) {
      const cmd = state.undoStack.pop();
      if (!cmd) return;

      applyOps(state, cmd.backward);
      mergeOpsToDelta(state, state.pendingDelta, cmd.backward);
      state.redoStack.push(cmd);
    },
    redo(state) {
      const cmd = state.redoStack.pop();
      if (!cmd) return;

      applyOps(state, cmd.forward);
      mergeOpsToDelta(state, state.pendingDelta, cmd.forward);
      state.undoStack.push(cmd);
    },
    // ---------- 同步相关 ----------

    /** flush 前取走 delta，同时重置 */
    consumeDelta(state) {
      state.pendingDelta = emptyDelta();
      state.syncStatus = "syncing";
    },
    syncSuccess(state, action: PayloadAction<syncResponse>) {
      const { version } = action.payload;
      state.syncStatus = "idle";
      state.syncFailCount = 0;
      state.version = version;
    },
    syncError(state) {
      state.syncStatus = "idle";
      state.syncFailCount += 1;
    },
    startFullSync(state) {
      state.isFullSyncing = true;
    },
    fullSyncDone(state, action: PayloadAction<fullSyncResponse>) {
      const { version } = action.payload;
      state.version = version;
      state.pendingDelta = emptyDelta();
      state.syncStatus = "idle";
      state.syncFailCount = 0;
      state.undoStack = [];
      state.redoStack = [];
      state.isFullSyncing = false;
    },
    replaceFromServer(
      state,
      action: PayloadAction<{ nodes: Node[]; edges: Edge[]; version: number }>
    ) {
      const { nodes, edges, version } = action.payload;
      state.nodes = nodes;
      state.edges = edges;
      state.version = version;
      state.undoStack = [];
      state.redoStack = [];
      state.pendingDelta = emptyDelta();
      state.syncStatus = "idle";
      state.syncFailCount = 0;
      state.isFullSyncing = false;
    },
    updateTitle(state, action: PayloadAction<string>) {
      state.title = action.payload;
    },

    // ---------- 节点操作 ----------
    interactiveNodeUpdate: (state, action: PayloadAction<NodeChange[]>) => {
      // 这里依然借助 applyNodeChanges 处理复杂的坐标计算
      state.nodes = applyNodeChanges(action.payload, state.nodes) as Node[];
    },
    interactiveEdgeUpdate: (state, action: PayloadAction<EdgeChange[]>) => {
      state.edges = applyEdgeChanges(action.payload, state.edges) as Edge[];
    },
    addNodeWithEdge: (state, action: PayloadAction<{ node: Omit<Node, "id"> & { id?: string }; targetNodeId: string }>) => {
      const { node: payload, targetNodeId } = action.payload;
      const node: Node = {
        id: payload.id ?? nanoid(),
        type: payload.type,
        position: payload.position,
        data: payload.data,
      };
      const edge: Edge = {
        id: nanoid(),
        source: node.id,
        target: targetNodeId,
        type: "custom-edge",
      };
      const cmd: Command = {
        canvas_id: state.canvasId!,
        timeStamp: Date.now(),
        forward: [
          { type: "create_node", data: node },
          { type: "create_edge", data: edge },
        ],
        backward: [
          { type: "delete_edge", data: edge },
          { type: "delete_node", data: node },
        ],
      };
      applyCommand(state, cmd);
    },
    addNode: (state, action: PayloadAction<Omit<Node, "id"> & { id?: string }>) => {
      const node : Node = {
        id: action.payload.id ?? nanoid(),
        type: action.payload.type,
        position: action.payload.position,
        data: action.payload.data,
      }
      const cmd: Command = {
        canvas_id: state.canvasId!,
        timeStamp: Date.now(),
        forward: [{ type: "create_node", data: node }],
        backward: [{ type: "delete_node", data: node }],
      }
      applyCommand(state, cmd);
    },
    deleteNode: (state, action: PayloadAction<string>) => {
      const node = state.nodes.find((n) => n.id === action.payload);
      if (!node) return;

      const forwardOps: AtomicOp[] = [];
      const backwardOps: AtomicOp[] = [];
      forwardOps.push({ type: "delete_node", data: node });
      backwardOps.push({ type: "create_node", data: node });

      const relatedEdges = state.edges.filter((e) => e.source === node.id || e.target === node.id);
      for (const edge of relatedEdges) {
        forwardOps.push({ type: "delete_edge", data: edge });
        backwardOps.push({ type: "create_edge", data: edge });
      }
      const cmd: Command = {
        canvas_id: state.canvasId!,
        timeStamp: Date.now(),
        forward: forwardOps.reverse(),
        backward: backwardOps,
      }
      applyCommand(state, cmd);
    },
    updateNode: (state, action: PayloadAction<[beginNode: Node, endNode: Node]>) => {
      const [beginNode, endNode] = action.payload;
      const cmd: Command = {
        canvas_id: state.canvasId!,
        timeStamp: Date.now(),
        forward: [{ type: "update_node", data: endNode }],
        backward: [{ type: "update_node", data: beginNode }],
      }
      applyCommand(state, cmd);
    },
    updateNodes: (state, action: PayloadAction<{beginNodes: Node[], endNodes: Node[]}>) => {
      const { beginNodes, endNodes } = action.payload;
      if (beginNodes.length !== endNodes.length) {
        throw new Error("beginNodes and endNodes must have the same length");
      }
      if(beginNodes.length === 0) return;
      const cmd: Command = {
        canvas_id: state.canvasId!,
        timeStamp: Date.now(),
        forward: endNodes.map((node) => ({ type: "update_node", data: node })),
        backward: beginNodes.map((node) => ({ type: "update_node", data: node })),
      }
      applyCommand(state, cmd);
    },
    // ---------- 边操作 ----------
    onConnect: (state, action: PayloadAction<Connection>) => {
      // 防自环
      if(action.payload.source === action.payload.target) return;
      // 防空
      if(!action.payload.source || !action.payload.target) return;
      // 防重复
      if (state.edges.some((e) => e.source === action.payload.source && e.target === action.payload.target)) {
        return;
      }
      const edge : Edge = {
        id: nanoid(),
        source: action.payload.source,
        target: action.payload.target,
        type: "custom-edge",
      }
      const cmd: Command = {
        canvas_id: state.canvasId!,
        timeStamp: Date.now(),
        forward: [{ type: "create_edge", data: edge }],
        backward: [{ type: "delete_edge", data: edge }],
      }
      applyCommand(state, cmd);
    },
    onDisconnect: (state, action: PayloadAction<string>) => {
      const edge : Edge = {
        id: action.payload,
        source: state.edges.find((e) => e.id === action.payload)?.source ?? "",
        target: state.edges.find((e) => e.id === action.payload)?.target ?? "",
        type: "custom-edge",
      }
      const cmd: Command = {
        canvas_id: state.canvasId!,
        timeStamp: Date.now(),
        forward: [{ type: "delete_edge", data: edge }],
        backward: [{ type: "create_edge", data: edge }],
      }
      applyCommand(state, cmd);
    },
    /** 仅更新 node.data 的部分字段，不进 undo 栈 / delta（用于上传状态更新等服务端驱动的变更） */
    patchNodeData: (state, action: PayloadAction<{ id: string; data: Record<string, unknown> }>) => {
      const node = state.nodes.find((n) => n.id === action.payload.id);
      if (node) {
        node.data = { ...node.data, ...action.payload.data };
        // 利用sync完成bindFileIdToNode操作
        // 加入delta的updatedNodes中
        // state.pendingDelta.updatedNodes.push(node);
        const op : AtomicOp = { type: "update_node", data: node };
        mergeOpsToDelta(state, state.pendingDelta, [op]);
        // 注意：这里没有加入undoStack，因为这是服务端驱动的变更，不应该被用户撤销
      }
    },
    deleteNodesAndEdges: (state, action: PayloadAction<{nodes: Node[], edges: Edge[]}>) => {
      const { nodes, edges } = action.payload;
      if (nodes.length === 0 && edges.length === 0) return;
      const forwardOps: AtomicOp[] = [];
      const backwardOps: AtomicOp[] = [];
      for (const node of nodes) {
        forwardOps.push({ type: "delete_node", data: node });
        backwardOps.push({ type: "create_node", data: node });
      }
      for (const edge of edges) {
        forwardOps.push({ type: "delete_edge", data: edge });
        backwardOps.push({ type: "create_edge", data: edge });
      }
      const relatedEdges = state.edges.filter((e) => nodes.some((n) => n.id === e.source || n.id === e.target));
      for (const edge of relatedEdges) {
        if (edges.some((e) => e.id === edge.id)) continue;
        forwardOps.push({ type: "delete_edge", data: edge });
        backwardOps.push({ type: "create_edge", data: edge });
      }
      const cmd: Command = {
        canvas_id: state.canvasId!,
        timeStamp: Date.now(),
        forward: forwardOps.reverse(), //仅方便语意进行reverse，先删边，后删节点；先创建节点，后创建边
        backward: backwardOps,
      }
      applyCommand(state, cmd);
    },
    toggleShowControls: (state, action: PayloadAction<boolean | undefined>) => {
      if (action.payload === undefined) {
        state.showControls = !state.showControls;
      } else {
        state.showControls = action.payload;
      }
    },
    setMaximizedNode: (state, action: PayloadAction<string | null>) => {
      state.maximizedNodeId = action.payload;
    },
    /** 一次性：创建节点 + 最大化 + 隐藏控件，避免多次 dispatch 在 StrictMode 下的竞态 */
    executeCommandAndMaximize(
      state,
      action: PayloadAction<{ cmd: Command; nodeId: string }>
    ) {
      applyCommand(state, action.payload.cmd);
      state.maximizedNodeId = action.payload.nodeId;
      state.showControls = false;
    },
  },
});

export const {
  loadCanvas,
  executeCommand,
  undo,
  redo,
  consumeDelta,
  syncSuccess,
  syncError,
  startFullSync,
  fullSyncDone,
  replaceFromServer,
  updateTitle,
  interactiveNodeUpdate,
  interactiveEdgeUpdate,
  addNodeWithEdge,
  addNode,
  deleteNode,
  updateNode,
  onConnect,
  onDisconnect,
  updateNodes,
  patchNodeData,
  deleteNodesAndEdges,
  toggleShowControls,
  setMaximizedNode,
  executeCommandAndMaximize,
} = canvasSlice.actions;

export default canvasSlice.reducer;

// ────────────── Selectors ──────────────

const selectNodes = (state: { canvas: CanvasState }) => state.canvas.nodes;
const selectEdges = (state: { canvas: CanvasState }) => state.canvas.edges;
const selectMaximizedNodeId = (state: { canvas: CanvasState }) => state.canvas.maximizedNodeId;

/** Stable empty reference — avoids new object allocation when no node is maximized */
const EMPTY_PARENTS: { chatParents: Node[]; resourceParents: Node[] } = {
  chatParents: [],
  resourceParents: [],
};

/** Compare parent sets by id + essential data to skip re-renders when only positions changed */
function parentResultEqual(
  a: { chatParents: Node[]; resourceParents: Node[] },
  b: { chatParents: Node[]; resourceParents: Node[] },
): boolean {
  if (a === b) return true;
  if (a.chatParents.length !== b.chatParents.length) return false;
  if (a.resourceParents.length !== b.resourceParents.length) return false;
  for (let i = 0; i < a.chatParents.length; i++) {
    if (a.chatParents[i].id !== b.chatParents[i].id) return false;
  }
  for (let i = 0; i < a.resourceParents.length; i++) {
    const na = a.resourceParents[i], nb = b.resourceParents[i];
    if (na.id !== nb.id || na.data?.fileId !== nb.data?.fileId) return false;
  }
  return true;
}

/** Returns parent nodes (nodes whose edge targets the maximized node), split by type */
export const selectParentNodesOfMaximized = createSelector(
  [selectNodes, selectEdges, selectMaximizedNodeId],
  (nodes, edges, maximizedNodeId) => {
    if (!maximizedNodeId) return EMPTY_PARENTS;
    const parents = edges
      .filter((e) => e.target === maximizedNodeId)
      .map((e) => nodes.find((n) => n.id === e.source))
      .filter((n): n is Node => n !== undefined);
    return {
      chatParents: parents.filter((n) => n.type === "chatNode"),
      resourceParents: parents.filter((n) => n.type === "resourceNode"),
    };
  },
  {
    memoizeOptions: {
      resultEqualityCheck: parentResultEqual,
    },
  },
);
