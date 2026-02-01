import ReactMarkdown from "react-markdown";
import { Prism as SyntaxHighlighter } from "react-syntax-highlighter";
import { vscDarkPlus } from "react-syntax-highlighter/dist/esm/styles/prism";
import { oneLight } from "react-syntax-highlighter/dist/esm/styles/prism";
import remarkGfm from "remark-gfm";
import type { ThemeName } from "../feature/user/userSlice";

interface MarkdownRendererProps {
  content: string;
  theme?: ThemeName;
}

const MarkdownRenderer = ({ content, theme = "saas" }: MarkdownRendererProps) => {
  const isDark = theme === "cyber";
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const codeStyle: any = isDark ? vscDarkPlus : oneLight;

  return (
    <div
      className="prose prose-sm max-w-none"
      style={{
        color: "var(--text-primary)",
        "--tw-prose-body": "var(--text-primary)",
        "--tw-prose-headings": "var(--text-primary)",
        "--tw-prose-bold": "var(--text-primary)",
        "--tw-prose-links": "var(--accent)",
        "--tw-prose-code": "var(--accent)",
        "--tw-prose-quotes": "var(--text-secondary)",
        "--tw-prose-hr": "var(--border-main)",
      } as React.CSSProperties}
    >
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          // eslint-disable-next-line @typescript-eslint/no-explicit-any, @typescript-eslint/no-unused-vars
          code({ className, children, ref, node, ...props }: any) {
            const match = /language-(\w+)/.exec(className || "");
            return match ? (
              <div
                className="rounded-md overflow-hidden my-2 shadow-sm"
                style={{ border: "1px solid var(--border-main)" }}
              >
                <div
                  className="flex justify-between items-center px-3 py-1 text-xs"
                  style={{
                    backgroundColor: isDark ? "#1e293b" : "var(--bg-sidebar)",
                    color: "var(--text-secondary)",
                  }}
                >
                  <span>{match[1]}</span>
                  <button
                    className="cursor-pointer hover:opacity-80"
                    style={{ color: "var(--accent)" }}
                  >
                    Copy
                  </button>
                </div>
                <SyntaxHighlighter
                  style={codeStyle}
                  language={match[1]}
                  PreTag="div"
                  {...props}
                  customStyle={{
                    margin: 0,
                    borderRadius: 0,
                    fontSize: "0.75rem",
                  }}
                >
                  {String(children).replace(/\n$/, "")}
                </SyntaxHighlighter>
              </div>
            ) : (
              <code
                className="px-1 py-0.5 rounded text-xs font-mono"
                style={{
                  backgroundColor: isDark ? "#1e293b" : "var(--accent-light)",
                  color: "var(--accent)",
                }}
                ref={ref}
                {...props}
              >
                {children}
              </code>
            );
          },
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
};

export default MarkdownRenderer;
