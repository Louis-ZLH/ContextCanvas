import { useCallback } from "react";
import {
  ReactFlow,
  addEdge,
  Background,
  BackgroundVariant,
  Controls,
  MiniMap,
  useReactFlow,
  Panel,
  ReactFlowProvider,
  useNodesState,
  useEdgesState,
} from "@xyflow/react";
import ChatNode from "../../ui/graph/ChatNode";
import SourceNode from "../../ui/graph/SourceNode";
import CustomEdge from "../../ui/graph/CustomEdge";
import Dagre from "@dagrejs/dagre";

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

const initialNodes = [
  {
    id: "chatNode1",
    type: "chatNode",
    position: { x: 100, y: 80 },
    data: {
      label: "Start: React Basics",
      icon: "start",
      messages: [
        { role: "user", content: "React Flow 的核心概念是什么？" },
        {
          role: "ai",
          content:
            "核心概念主要是 **Nodes**（节点）、**Edges**（连线）和 **Handles**（句柄）。它是基于状态驱动的。",
        },
      ],
    },
  },
  {
    id: "chatNode2",
    type: "chatNode",
    position: { x: 620, y: 250 },
    data: {
      label: "Branch: Custom Node",
      icon: "branch",
      inheritedContext: true,
      messages: [
        { role: "user", content: "那怎么自定义一个带有输入框的 Node？" },
        {
          role: "ai",
          content: [
            "你需要注册 `nodeTypes`，然后创建自定义组件：",
            "",
            "```tsx",
            "function TextNode({ data }) {",
            "  return (",
            '    <div className="custom-node">',
            "      <input",
            '        className="nodrag"',
            "        value={data.label}",
            "      />",
            "    </div>",
            "  );",
            "}",
            "```",
            "",
            "记得给 `input` 添加 `nodrag` 类名，否则拖拽会冲突。",
          ].join("\n"),
        },
      ],
    },
  },
  {
    id: "sourceNode1",
    type: "sourceNode",
    position: { x: 80, y: 620 },
    data: {
      title: "React Flow Documentation",
      fileName: "react-flow-docs-v11.pdf",
      fileType: "PDF",
      fileSize: "2.4 MB",
      pageCount: "48 pages",
      excerpt:
        '"A node in React Flow is a React component. It can be as simple as a div or as complex as an interactive chart..."',
      excerptSource: "— Page 12, Section 3.1",
      tags: ["#react-flow", "#nodes", "#custom"],
      addedTime: "Added 2 days ago",
      referenceCount: 2,
    },
  },
];

const initialEdges = [
  {
    id: "chat1-chat2",
    source: "chatNode1",
    target: "chatNode2",
    type: "custom-edge",
  },
  {
    id: "source1-chat1",
    source: "sourceNode1",
    target: "chatNode1",
    type: "custom-edge",
    data: { dashed: true },
  },
];

const nodeTypes = {
  chatNode: ChatNode,
  sourceNode: SourceNode,
};

const edgeTypes = {
  "custom-edge": CustomEdge,
};

export function LayoutFlowInner() {
  const { fitView, getNodes, getEdges } = useReactFlow();
  const [nodes, setNodes, onNodesChange] = useNodesState(initialNodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(initialEdges);

  const onConnect = useCallback(
    //@ts-expect-error connection type
    (connection) => {
      const edge = { ...connection, type: "custom-edge" };
      setEdges((eds) => addEdge(edge, eds));
    },
    [setEdges],
  );

  const onLayout = useCallback(
    (direction: "TB" | "LR") => {
      const currentNodes = getNodes();
      const currentEdges = getEdges();
      const layouted = getLayoutedElements(currentNodes, currentEdges, {
        direction,
      });

      setNodes([...layouted.nodes]);
      setEdges([...layouted.edges]);

      fitView();
    },
    [getNodes, getEdges, setNodes, setEdges, fitView],
  );

  return (
    <div className="w-full h-full bg-canvas">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={onConnect}
        fitView
      >
        {/* Custom arrow marker that uses theme colors */}
        <svg>
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
            <button
              onClick={() => onLayout("TB")}
              className="px-3 py-1.5 rounded-md text-xs font-medium node-card"
              style={{ color: "var(--text-primary)" }}
            >
              Vertical
            </button>
            <button
              onClick={() => onLayout("LR")}
              className="px-3 py-1.5 rounded-md text-xs font-medium node-card"
              style={{ color: "var(--text-primary)" }}
            >
              Horizontal
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
        <MiniMap position="top-right" />
      </ReactFlow>
    </div>
  );
}

function Graph() {
  return (
    <ReactFlowProvider>
      <LayoutFlowInner />
    </ReactFlowProvider>
  );
}

// eslint-disable-next-line
export async function loader() {
  console.log("Loading graph with ID");
  return null;
}

export default Graph;
