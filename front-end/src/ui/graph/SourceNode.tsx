import { Handle, Position } from "@xyflow/react";
import {
  FileText,
  Scale,
  Files,
  Clock,
  Link,
  MoreVertical,
} from "lucide-react";

export interface SourceNodeData {
  title?: string;
  fileName?: string;
  fileType?: string;
  fileSize?: string;
  pageCount?: string;
  excerpt?: string;
  excerptSource?: string;
  tags?: string[];
  addedTime?: string;
  referenceCount?: number;
  [key: string]: unknown;
}

function SourceNode({ data }: { data: SourceNodeData }) {
  const title = data.title ?? "React Flow Documentation";
  const fileName = data.fileName ?? "react-flow-docs-v11.pdf";
  const fileType = data.fileType ?? "PDF";
  const fileSize = data.fileSize ?? "2.4 MB";
  const pageCount = data.pageCount ?? "48 pages";
  const excerpt =
    data.excerpt ??
    '"A node in React Flow is a React component. It can be as simple as a div or as complex as an interactive chart..."';
  const excerptSource = data.excerptSource ?? "— Page 12, Section 3.1";
  const tags = data.tags ?? ["#react-flow", "#nodes", "#custom"];
  const addedTime = data.addedTime ?? "Added 2 days ago";
  const referenceCount = data.referenceCount ?? 2;

  return (
    <div className="source-node rounded-xl flex flex-col w-[320px] text-sm">
      {/* Header */}
      <div className="source-header h-10 border-b border-main flex items-center justify-between px-3 cursor-move rounded-t-xl">
        <div className="flex items-center gap-2">
          <FileText size={12} className="source-meta-icon" />
          <span className="text-xs font-bold uppercase tracking-wider text-secondary">
            Source
          </span>
        </div>
        <div className="flex items-center gap-2">
          <span className="source-tag text-[10px] font-semibold px-1.5 py-0.5 rounded">
            {fileType}
          </span>
          <MoreVertical
            size={12}
            className="text-secondary hover:text-primary cursor-pointer"
            style={{ color: "var(--text-secondary)" }}
          />
        </div>
      </div>

      {/* Content */}
      <div className="p-4 space-y-3">
        {/* File info */}
        <div className="flex items-start gap-3">
          <div
            className="w-10 h-12 rounded-md flex items-center justify-center flex-shrink-0 bg-accent-light"
          >
            <FileText size={18} className="source-meta-icon" />
          </div>
          <div className="flex-1 min-w-0">
            <p
              className="font-semibold text-sm truncate"
              style={{ color: "var(--text-primary)" }}
            >
              {title}
            </p>
            <p className="text-xs text-secondary mt-0.5">{fileName}</p>
            <div className="flex items-center gap-3 mt-1.5 text-[11px] text-secondary">
              <span className="flex items-center gap-1">
                <Scale size={10} /> {fileSize}
              </span>
              <span className="flex items-center gap-1">
                <Files size={10} /> {pageCount}
              </span>
            </div>
          </div>
        </div>

        {/* Highlighted excerpt */}
        <div className="source-highlight rounded-md p-2.5 text-xs leading-relaxed">
          <p className="italic" style={{ color: "var(--text-primary)" }}>
            {excerpt}
          </p>
          <p className="text-[10px] text-secondary mt-1.5 not-italic">
            {excerptSource}
          </p>
        </div>

        {/* Tags */}
        <div className="flex flex-wrap gap-1.5">
          {tags.map((tag) => (
            <span
              key={tag}
              className="source-tag text-[10px] px-2 py-0.5 rounded-full font-medium"
            >
              {tag}
            </span>
          ))}
        </div>

        {/* Footer meta */}
        <div className="flex items-center justify-between text-[11px] text-secondary pt-1 border-t border-main">
          <span className="flex items-center gap-1">
            <Clock size={10} /> {addedTime}
          </span>
          <span className="flex items-center gap-1">
            <Link size={10} /> {referenceCount} references
          </span>
        </div>
      </div>

      {/* Source handle (right only) */}
      <Handle
        type="source"
        position={Position.Right}
        className="custom-handle custom-handle-source"
        style={{ top: 20, right: -6 }}
      />
    </div>
  );
}

export default SourceNode;
