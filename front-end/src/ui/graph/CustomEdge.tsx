import { useState } from "react";
import {
  BaseEdge,
  EdgeLabelRenderer,
  getBezierPath,
  useReactFlow,
  type EdgeProps,
} from "@xyflow/react";
import { Trash2 } from "lucide-react";


export default function CustomEdge({
  id,
  sourceX,
  sourceY,
  sourcePosition,
  targetX,
  targetY,
  targetPosition,
  data,
}: EdgeProps) {
  const { deleteElements } = useReactFlow();
  const isDashed = data?.dashed === true;
  const [hovered, setHovered] = useState(false);

  const [path, labelX, labelY] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  });

  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        interactionWidth={0}
        style={{
          stroke: "var(--edge-stroke)",
          strokeWidth: "var(--edge-width)",
          opacity: hovered ? 1 : ("var(--edge-opacity)" as unknown as number),
          strokeDasharray: isDashed
            ? "var(--edge-dasharray)"
            : undefined,
          transition: "opacity 0.2s",
          pointerEvents: "none",
        }}
        markerEnd="url(#custom-arrow)"
      />
      {/* Invisible wider path on top for hover detection */}
      <path
        d={path}
        fill="none"
        stroke="transparent"
        strokeWidth={20}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
        style={{ pointerEvents: "stroke" }}
      />
      <EdgeLabelRenderer>
        <button
          onClick={() => deleteElements({ edges: [{ id }] })}
          onMouseEnter={() => setHovered(true)}
          onMouseLeave={() => setHovered(false)}
          style={{
            position: "absolute",
            transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`,
            pointerEvents: "all",
            opacity: hovered ? 1 : 0,
            transition: "opacity 0.2s, background-color 0.2s",
          }}
          className="nodrag nopan w-8 h-8 rounded-md flex items-center justify-center
                     cursor-pointer bg-gray-200 border border-gray-400
                     shadow-sm hover:bg-gray-300 hover:border-gray-400"
          title="Delete edge"
        >
          <Trash2
            size={16}
            style={{ color: "var(--text-secondary)" }}
          />
        </button>
      </EdgeLabelRenderer>
    </>
  );
}
