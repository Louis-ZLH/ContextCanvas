# NewCanvas Page Redesign: 3 Entry Buttons

## 背景

NewCanvas 页面目前有 2 张卡片："Blank Canvas" 和 "Use Templates"（占位）。将它们替换为 3 个有意义的入口：
1. **Blank Canvas** — 不变
2. **Ask a Question** — 创建 canvas + ChatNode，自动最大化
3. **Begin with Resources** — 文件选择 modal，创建 canvas + ResourceNodes + ChatNode（通过 edges 连接），自动最大化 ChatNode

## 需求

- 用户可以从 3 种方式创建 canvas：空白画布、提问、选择资源文件开始
- "Ask a Question" 创建后直接进入 ChatNode 最大化视图，可立即输入
- "Begin with Resources" 弹出文件选择 modal，支持搜索、分页、多选（最多 10 个文件）
- 所有初始化操作支持原子 Undo（Ctrl+Z 一次撤销全部初始节点/边）
- 无后端 API 变更，所有节点/边通过现有 `executeCommand` + `useSyncCanvas` 同步

## 关键设计决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 初始节点创建时机 | 导航后在 Canvas 组件内 via `location.state` | 避免在 NewCanvas 中处理 canvas 未创建完成的状态；与现有 command pattern 一致 |
| FilePickerModal 状态管理 | 组件内部 `useState` | 无需全局状态，modal 生命周期短 |
| 多节点创建方式 | 单个 `Command` 包含多个 `AtomicOp` | 保证 undo 时原子回退 |
| ChatNode 自动最大化 | `requestAnimationFrame` 延迟 | 等待 ReactFlow 渲染节点后再操作 |
| `pendingModeRef` 传递导航 state | `mutate()` 调用级别的 `onSuccess` | 避免 mutation 定义级别的 `onSuccess` 无法区分不同 handler |
| 最大文件选择数量 | 10 | 防止垂直排列过长导致布局溢出 |
| ID 生成方式 | `nanoid()`（from `@reduxjs/toolkit`） | 与现有代码一致，避免混用 `crypto.randomUUID()` |
| 初始化 effect 时序保障 | `isFullSyncing` guard + `hasInitializedRef` guard + `requestAnimationFrame` 内 `fitView()` | 确保 loadCanvas 完成后才创建节点，`hasInitializedRef` 显式防止 effect 重复执行，且节点创建后视口正确适配 |

## 涉及文件清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `front-end/src/view/canvas/FilePickerModal.tsx` | 新建 | 文件选择 Modal 组件 |
| `front-end/src/view/canvas/NewCanvas.tsx` | 修改 | 3 入口按钮 + Modal 集成 |
| `front-end/src/view/canvas/index.tsx` | 修改 | `LayoutFlowInner` 增加 `location.state` 处理 + 初始化 effect |

## 前端改动

### 1. NEW: `front-end/src/view/canvas/FilePickerModal.tsx`

创建文件选择 modal 组件。

**Props 接口：**

```tsx
interface FilePickerModalProps {
  isOpen: boolean;
  onClose: () => void;
  onConfirm: (fileIds: string[]) => void;
}
```

**依赖：**
- `Modal` 组件（`ui/common/Modal.tsx`，props: `isOpen, onClose, title, children, footer, width`）
- `fileListQueryOptions`（from `query/file.ts`），使用 `enabled: isOpen` 仅在打开时请求
- `FileTypeIcon`（from `ui/canvas/ResourceNode/FileTypeIcon.tsx`）
- `getFileCategoryFromMime`（from `service/file.ts`）用于判断文件类型
- `BASE_URL`（from `util/api.ts`）用于拼接图片缩略图 URL

**核心实现：**

```tsx
import { useState, useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { Modal } from "../../ui/common/Modal";
import { fileListQueryOptions } from "../../query/file";
import { FileTypeIcon } from "../../ui/canvas/ResourceNode/FileTypeIcon";
import { getFileCategoryFromMime } from "../../service/file";
import { BASE_URL } from "../../util/api";

const MAX_SELECTION = 10;
const PAGE_SIZE = 12;

export function FilePickerModal({ isOpen, onClose, onConfirm }: FilePickerModalProps) {
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [keyword, setKeyword] = useState("");
  const [debouncedKeyword, setDebouncedKeyword] = useState("");
  const [page, setPage] = useState(1);

  // Reset on re-open
  useEffect(() => {
    if (isOpen) {
      setSelectedIds(new Set());
      setKeyword("");
      setDebouncedKeyword("");
      setPage(1);
    }
  }, [isOpen]);

  // 300ms debounce for search
  useEffect(() => {
    const timer = setTimeout(() => {
      setDebouncedKeyword(keyword);
      setPage(1); // 搜索时重置分页
    }, 300);
    return () => clearTimeout(timer);
  }, [keyword]);

  const { data, isLoading } = useQuery({
    ...fileListQueryOptions({ page, limit: PAGE_SIZE, keyword: debouncedKeyword }),
    enabled: isOpen,  // 仅在 modal 打开时发起请求
  });

  const files = data?.data?.files ?? [];
  const total = data?.data?.total ?? 0;
  const totalPages = Math.ceil(total / PAGE_SIZE);

  const toggleSelect = (fileId: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(fileId)) {
        next.delete(fileId);
      } else if (next.size < MAX_SELECTION) {
        next.add(fileId);
      }
      return next;
    });
  };

  const handleConfirm = () => {
    onConfirm(Array.from(selectedIds));
  };

  // ... render Modal with:
  // - title="Select Files"
  // - width="max-w-3xl"
  // - Search input
  // - 3-column grid: files.map → checkbox + thumbnail/icon + filename
  //   - Image files: <img src={`${BASE_URL}/api/file/${file.fileId}`} onError={(e) => { e.currentTarget.style.display = "none"; e.currentTarget.nextElementSibling?.removeAttribute("hidden"); }} />
  //     附带 hidden 的 <FileTypeIcon /> 作为 fallback，图片加载失败时显示
  //   - Others: <FileTypeIcon fileType={getFileCategoryFromMime(file.contentType, file.filename)} />
  // - Pagination controls (page / totalPages)
  // - footer: selection count + Confirm button (disabled when selectedIds.size === 0)
  // - Empty state: isLoading → skeleton; files.length === 0 → 提示信息
}
```

**空状态处理：**
- `isLoading` → 显示 skeleton 占位
- `files.length === 0 && keyword` → "No files matching your search"
- `files.length === 0 && !keyword` → "No files uploaded yet. Upload files in My Resources to get started."

### 2. MODIFY: `front-end/src/view/canvas/NewCanvas.tsx`

**Import 变更：**
- 移除: `LayoutTemplate`
- 新增: `MessageCircle, FolderOpen` from lucide-react
- 新增: `useState, useRef` from react
- 新增: `FilePickerModal` from `./FilePickerModal`

**Grid 布局：** `md:grid-cols-2 lg:grid-cols-3`

**Mutation 改造：**

将导航逻辑从 mutation 定义级别的 `onSuccess` 移到 `mutate()` 调用级别，以便区分不同 handler：

```tsx
type PendingMode =
  | { mode: "blank" }
  | { mode: "ask" }
  | { mode: "resources"; fileIds: string[] };

export default function NewCanvas() {
  const navigate = useNavigate();
  const [isFilePickerOpen, setIsFilePickerOpen] = useState(false);
  const pendingModeRef = useRef<PendingMode | null>(null);

  const { mutate: createCanvas, isPending } = useMutation({
    mutationFn: createCanvasService,
    // onSuccess 移除 — 改为在每个 mutate() 调用中处理
    onError: (error) => {
      pendingModeRef.current = null;
      if (error instanceof Error) {
        toast.error(error.message);
      } else {
        toast.error("Failed to create canvas");
      }
    },
  });

  const handleCreateBlankCanvas = () => {
    pendingModeRef.current = { mode: "blank" };
    createCanvas(undefined, {
      onSuccess: (data) => {
        if (data.success && data.data) {
          navigate(`/canvas/${data.data.id}`);
          queryClient.invalidateQueries({ queryKey: ["canvas", "list"] });
        }
      },
    });
  };

  const handleAskQuestion = () => {
    pendingModeRef.current = { mode: "ask" };
    createCanvas(undefined, {
      onSuccess: (data) => {
        if (data.success && data.data) {
          navigate(`/canvas/${data.data.id}`, {
            state: { initialMode: "ask" },
          });
          queryClient.invalidateQueries({ queryKey: ["canvas", "list"] });
        }
      },
    });
  };

  const handleBeginWithResources = () => {
    setIsFilePickerOpen(true);
  };

  const handleFilesSelected = (fileIds: string[]) => {
    setIsFilePickerOpen(false);
    pendingModeRef.current = { mode: "resources", fileIds };
    createCanvas(undefined, {
      onSuccess: (data) => {
        if (data.success && data.data) {
          navigate(`/canvas/${data.data.id}`, {
            state: { initialMode: "resources", fileIds },
          });
          queryClient.invalidateQueries({ queryKey: ["canvas", "list"] });
        }
      },
    });
  };

  // 3 个按钮共享 isPending 状态，mutation 进行中全部 disabled
  // ...
}
```

**3 张卡片：**
| 卡片 | Icon | 标题 | 描述 | handler |
|------|------|------|------|---------|
| Blank Canvas | `Plus` (不变) | Blank Canvas | Start from a blank slate... | `handleCreateBlankCanvas` |
| Ask a Question | `MessageCircle` | Ask a Question | Start a conversation with AI... | `handleAskQuestion` |
| Begin with Resources | `FolderOpen` | Begin with Resources | Select files as context... | `handleBeginWithResources` |

**Render FilePickerModal：**
```tsx
<FilePickerModal
  isOpen={isFilePickerOpen}
  onClose={() => setIsFilePickerOpen(false)}
  onConfirm={handleFilesSelected}
/>
```

### 3. MODIFY: `front-end/src/view/canvas/index.tsx` (Canvas component)

在 `LayoutFlowInner` 中增加 `useEffect` 处理 `location.state`。

**新增 imports：**
```tsx
import { useRef } from "react";
import { useLocation } from "react-router";
import { nanoid } from "@reduxjs/toolkit";
import { executeCommand, setMaximizedNode, toggleShowControls } from "../../feature/canvas/canvasSlice";
import type { Command, AtomicOp } from "../../feature/canvas/types";
```

**常量（来源于现有代码）：**
```tsx
const RESOURCE_NODE_WIDTH = 230;  // 来自 ChatNode/index.tsx
const GAP = 50;
const RESOURCE_NODE_SPACING = 100;  // 垂直间距
```

**Initial setup effect（放在现有 fitView effect 之后）：**

> **注意**：该 effect 声明在 fitView effect 之后，React 按声明顺序执行同依赖 effects。
> fitView effect 先执行时画布可能为空，因此本 effect 在 `requestAnimationFrame` 中补充调用 `fitView()`。

```tsx
const location = useLocation();
const isFullSyncing = useSelector((state: RootState) => state.canvas.isFullSyncing);
const hasInitializedRef = useRef(false);

useEffect(() => {
  // 显式 guard：防止 isFullSyncing 变化导致 effect 重复执行
  if (hasInitializedRef.current) return;

  const state = location.state as {
    initialMode?: "ask" | "resources";
    fileIds?: string[];
  } | null;

  if (!state?.initialMode || !canvasId) return;

  // 等待全量同步完成，避免在 loadCanvas 未完成时创建节点
  if (isFullSyncing) return;

  hasInitializedRef.current = true;

  // 立即清除 state，防止刷新后重复触发
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
    dispatch(executeCommand(cmd));
    requestAnimationFrame(() => {
      fitView();
      dispatch(setMaximizedNode(chatNodeId));
      dispatch(toggleShowControls(false));
    });
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
    // 垂直居中：ResourceNodes 中心与 ChatNode 的 y=0 对齐
    const startY = -((count - 1) * RESOURCE_NODE_SPACING) / 2;

    const forward: AtomicOp[] = [];
    const backward: AtomicOp[] = [];

    // 1. 创建 ChatNode
    forward.push({ type: "create_node", data: chatNode });

    // 2. 创建 ResourceNodes
    const resourceNodes: Node[] = fileIds.map((fileId, i) => ({
      id: nanoid(),
      type: "resourceNode",
      position: { x: 0, y: startY + i * RESOURCE_NODE_SPACING },
      data: { fileId },
    }));
    for (const rn of resourceNodes) {
      forward.push({ type: "create_node", data: rn });
    }

    // 3. 创建 Edges: ResourceNode -> ChatNode
    const edges: Edge[] = resourceNodes.map((rn) => ({
      id: nanoid(),
      source: rn.id,
      target: chatNodeId,
      type: "custom-edge",
    }));
    for (const edge of edges) {
      forward.push({ type: "create_edge", data: edge });
    }

    // Backward: 先删 edges，再删 nodes（逆序）
    for (const edge of edges) {
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
      fitView();
      dispatch(setMaximizedNode(chatNodeId));
      dispatch(toggleShowControls(false));
    });
  }
}, [canvasId, isFullSyncing]);
```

**Defensive effect（处理 undo 导致 maximizedNodeId 悬空）：**

```tsx
const nodes = useSelector((s: RootState) => s.canvas.nodes);
const maximizedNodeId = useSelector((s: RootState) => s.canvas.maximizedNodeId);

useEffect(() => {
  if (maximizedNodeId && !nodes.find((n) => n.id === maximizedNodeId)) {
    dispatch(setMaximizedNode(null));
    dispatch(toggleShowControls(true));
  }
}, [nodes, maximizedNodeId]);
```

## Node Layout for "Begin with Resources"

```
ResourceNode 0 (0, -100) ──→ ChatNode (280, 0)
ResourceNode 1 (0,    0) ──→ ChatNode (280, 0)
ResourceNode 2 (0,  100) ──→ ChatNode (280, 0)
```

- `RESOURCE_NODE_WIDTH = 230`，`GAP = 50`，所以 ChatNode x = 280
- ResourceNodes 垂直居中于 ChatNode Y 坐标
- `fitView` 自动调整视口

## Sync Flow

Nodes/edges 通过 `executeCommand` 创建后进入 `pendingDelta`，然后 `useSyncCanvas` debounce（2s）通过 `syncCanvas` API 同步。ResourceNodes 的 `data: { fileId }` 通过 `convertNodeToSendStructure` 转换为 `file_id` 发送。**无需后端 API 变更。**

## 潜在问题与注意事项

| 问题 | 严重度 | 说明 | 应对方案 |
|------|--------|------|----------|
| `useEffect` 执行时 canvas 未加载完 | 高 | `loadCanvas` 是异步的，`canvasId` 已设但节点数据可能未就绪 | 在 effect 中增加 `isFullSyncing` guard：`if (isFullSyncing) return;`，确保全量同步完成后再创建节点。同时将 `isFullSyncing` 加入 effect 依赖数组 |
| 用户快速连续点击创建 | 中 | `isPending` 已覆盖 Blank Canvas 按钮 | 3 个按钮共享 `isPending` 状态，mutation 进行中全部 `disabled` |
| FilePickerModal 无文件时 | 低 | 用户未上传任何文件 | 显示空状态提示 + 引导前往 My Resources 上传 |
| Undo 后 `maximizedNodeId` 悬空 | 中 | Undo 删除节点但 `maximizedNodeId` 仍指向已删除节点 | Defensive effect 检查节点是否存在，自动 reset |
| `location.state` 在刷新后丢失 | 低 | 用户在 canvas 页刷新不会重新触发初始化 | 可接受行为，`replaceState` 已清除 state |
| 大量文件选择时布局溢出 | 中 | 选择过多 ResourceNode 垂直排列过长 | 限制最大选择数量为 10 个 |
| `requestAnimationFrame` 可能不够 | 低 | 节点创建后 ReactFlow 可能还未完成渲染 | 如遇问题可改为 `setTimeout(fn, 50)` 或监听 `onNodesChange` |
| `fitView` 与初始化 effect 执行顺序 | 中 | 现有 fitView effect 和初始化 effect 都依赖 `canvasId`，React 按声明顺序执行，fitView 先于节点创建 | 在初始化 effect 的 `requestAnimationFrame` 中补充调用 `fitView()`，确保节点创建后视口正确适配 |
| `isFullSyncing` 变化导致初始化 effect 重复执行 | 中 | `isFullSyncing` 在 effect 依赖中，从 `true→false` 时触发重执行；虽然 `replaceState` 已清除 `location.state` 可隐式防止重复，但依赖副作用的时序不够健壮 | 增加 `hasInitializedRef = useRef(false)` 作为显式 guard，effect 开头检查 `if (hasInitializedRef.current) return;`，成功执行后设为 `true` |
| 图片缩略图加载失败无 fallback | 低 | FilePickerModal 中图片文件使用 `<img>` 展示缩略图，若文件已删除或网络异常则显示为破碎图片 | 给 `<img>` 添加 `onError` handler，失败时隐藏 `<img>` 并显示预置的 hidden `<FileTypeIcon />` 作为 fallback |

## 性能与扩展性说明

- FilePickerModal 使用 `enabled: isOpen` 避免不必要的请求
- 文件列表分页（12/页）控制单次加载量
- `fileListQueryOptions` 已配置 `staleTime: 2min`，避免重复请求
- 未来可考虑：文件缩略图懒加载（`loading="lazy"`）、大量文件时虚拟滚动

## TODO 清单

### Phase 1: FilePickerModal 组件
- [x] 创建 `FilePickerModal.tsx`
- [x] 实现搜索（300ms debounce）、分页（12/页）、多选（最多 10）
- [x] 空状态处理（无文件 / 搜索无结果）
- [x] 主题适配（saas / dark / paper）

### Phase 2: NewCanvas.tsx 改造
- [x] Grid 布局改为 `lg:grid-cols-3`
- [x] 3 个 handler + `mutate()` 级别 `onSuccess` 导航
- [x] 3 张卡片 UI（Blank Canvas / Ask a Question / Begin with Resources）
- [x] 集成 FilePickerModal

### Phase 3: Canvas index.tsx 初始化逻辑
- [x] `useLocation` 读取 `location.state` + `replaceState` 清除
- [x] `ask` mode: 创建 ChatNode + maximize
- [x] `resources` mode: 创建 ResourceNodes + ChatNode + edges + maximize
- [x] Defensive effect: `maximizedNodeId` 校验

### Phase 4: 测试验证
- [ ] Blank Canvas 无回归
- [ ] Ask a Question 完整流程
- [ ] Begin with Resources 完整流程
- [ ] Undo/Redo 原子性验证
- [ ] 3 套主题切换测试（saas / dark / paper）
- [ ] FilePickerModal 边缘情况（无文件 / 搜索无结果 / 关闭 modal）

## 验证方案

1. **Blank Canvas**: Click → creates canvas → navigates to empty canvas（无回归）
2. **Ask a Question**: Click → creates canvas → navigates → one ChatNode visible in maximized view → can type immediately
3. **Begin with Resources**: Click → modal opens → search/paginate files → select 2-3 files → confirm → creates canvas → navigates → resource nodes + chat node visible, chat node maximized, resource nodes shown as parent context
4. **Undo**: In both modes, Ctrl+Z undoes the entire initial setup atomically（ChatNode + ResourceNodes + Edges 全部移除，maximized state 自动 reset）
5. **File Picker edge cases**: No files uploaded → shows empty state; search with no results → shows message; close modal → no canvas created
6. **Theme**: 在 saas / dark / paper 三套主题下验证 FilePickerModal 和新卡片的样式

## Review 修正记录

以下是经过代码库对照审查后修正的问题：

| # | 问题 | 严重度 | 修正内容 |
|---|------|--------|----------|
| 1 | Edge 缺少 `type: "custom-edge"` | **高** | `Edge` 接口（`types.ts:15-19`）强制要求 `type: "custom-edge"` 字段。已在 resources 模式的 edge 创建代码中补充 |
| 2 | `BASE_URL` 导入路径错误 | 中 | 实际位置为 `util/api.ts`，非 `service/base.ts`。已修正所有引用 |
| 3 | `crypto.randomUUID()` 与现有代码不一致 | 低 | 现有代码统一使用 `nanoid()`（from `@reduxjs/toolkit`）。已全部替换为 `nanoid()` |
| 4 | `fileListQueryOptions` 的 `enabled` 选项用法不正确 | 低 | `fileListQueryOptions` 返回完整 query options 对象，需 spread 后追加 `enabled`。已修正为 `useQuery({ ...fileListQueryOptions(...), enabled: isOpen })` |
| 5 | `fitView` 与初始化 effect 执行顺序竞争 | 中 | 两个 effect 共享 `canvasId` 依赖，fitView 先执行时画布为空。已在初始化 effect 的 `requestAnimationFrame` 中补充 `fitView()` 调用 |
| 6 | `loadCanvas` 时序问题解决方案不明确 | 高 | 原方案描述模糊。已增加 `isFullSyncing` guard（`if (isFullSyncing) return;`），将其加入 effect 依赖，确保全量同步完成后才创建节点 |
| 7 | 新增 imports 不完整 | 低 | 缺少 `nanoid`（from `@reduxjs/toolkit`）和 `AtomicOp` 类型导入。已补充 |
| 8 | 初始化 effect 依赖 `replaceState` 副作用防重复执行 | 中 | `isFullSyncing` 在依赖数组中，变化时会重新触发 effect；原方案隐式依赖 `replaceState` 已清除 `location.state` 来避免重复，不够健壮。已增加 `hasInitializedRef` 显式 guard 并在 imports 中补充 `useRef` |
| 9 | FilePickerModal 图片缩略图无加载失败 fallback | 低 | 图片文件缩略图通过 `<img>` 加载，若资源不可用会显示破碎图标。已在 `<img>` 上添加 `onError` handler，失败时切换显示 `<FileTypeIcon />` fallback |

## 补充核实记录

以下为额外代码库核实项，均已确认无问题：

| # | 核实项 | 结论 | 依据 |
|---|--------|------|------|
| 1 | `createCanvas(undefined, { onSuccess })` 调用方式 | **无问题** | `createCanvasService` 签名为无参函数（`canvas.ts:11`），React Query 的 `mutate(variables?, options?)` 需传 `undefined` 作为第一个参数才能访问第二个 options 参数，这是标准用法 |
| 2 | `NodeData` 类型兼容性：chatNode `data: {}` 与 resourceNode `data: { fileId }` | **无问题** | `NodeData = { fileId?: string }`（`types.ts:5-7`），`fileId` 为可选字段，空对象 `{}` 和 `{ fileId: string }` 均满足类型约束 |
| 3 | `queryClient` 来源与用法 | **无问题** | 现有代码从模块单例导入 `import { queryClient } from "../../query"`（`NewCanvas.tsx:6`），非 `useQueryClient()` Hook。PRD 代码片段的 `queryClient.invalidateQueries(...)` 写法与现有代码一致 |
