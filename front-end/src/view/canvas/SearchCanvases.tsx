import { useState, useEffect, useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "react-router";
import { Search, SearchX, ChevronLeft, ChevronRight, X } from "lucide-react";
import { searchCanvasQueryOptions } from "../../query/canvas";
import type { CanvasSearchItem } from "../../service/type";

const PAGE_LIMIT = 20;

function useDebounce(value: string, delay: number) {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delay);
    return () => clearTimeout(timer);
  }, [value, delay]);
  return debounced;
}

function escapeRegex(str: string) {
  return str.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function HighlightText({ text, keyword }: { text: string; keyword: string }) {
  if (!keyword) return <>{text}</>;

  const regex = new RegExp(`(${escapeRegex(keyword)})`, "gi");
  const parts = text.split(regex);

  return (
    <>
      {parts.map((part, i) =>
        part.toLowerCase() === keyword.toLowerCase() ? (
          <span key={i} className="text-accent font-semibold">{part}</span>
        ) : (
          <span key={i}>{part}</span>
        )
      )}
    </>
  );
}

const MATCH_TYPE_CONFIG: Record<CanvasSearchItem["matchType"], { label: string; className: string }> = {
  title: {
    label: "Canvas Title",
    className: "bg-accent/15 text-accent border-accent/30",
  },
  conversation: {
    label: "Conversation",
    className: "bg-blue-500/15 text-blue-500 border-blue-500/30",
  },
  content: {
    label: "Message",
    className: "bg-secondary/15 text-secondary border-secondary/30",
  },
};

export default function SearchCanvases() {
  const navigate = useNavigate();
  const [page, setPage] = useState(1);
  const [keyword, setKeyword] = useState("");
  const [searchFocused, setSearchFocused] = useState(false);
  const debouncedKeyword = useDebounce(keyword, 300);

  // Reset page when keyword changes
  useEffect(() => {
    setPage(1);
  }, [debouncedKeyword]);

  const { data: result, isLoading } = useQuery(
    searchCanvasQueryOptions({ keyword: debouncedKeyword, page, limit: PAGE_LIMIT })
  );

  const searchData = result?.data;
  const results = searchData?.results ?? [];
  const total = searchData?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_LIMIT));

  const handleSearchChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    setKeyword(e.target.value);
  }, []);

  const clearSearch = useCallback(() => {
    setKeyword("");
    setPage(1);
  }, []);

  const handleResultClick = (canvasId: string) => {
    navigate(`/canvas/${canvasId}`);
  };

  const showLoading = isLoading && debouncedKeyword.length > 0;
  const showEmpty = !isLoading && debouncedKeyword.length > 0 && results.length === 0;
  const showPrompt = debouncedKeyword.length === 0;
  const showResults = !isLoading && results.length > 0;

  return (
    <div className="w-full h-full bg-canvas overflow-y-auto">
      <div className="max-w-4xl mx-auto px-6 py-8">
        {/* Header */}
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4 mb-8">
          <h1 className="text-3xl font-bold text-primary">Search Canvases</h1>
        </div>

        {/* Search bar */}
        <div className={`relative w-full mb-8 transition-all duration-300 ${searchFocused ? "scale-[1.01]" : ""}`}>
          <Search size={18} className={`absolute left-4 top-1/2 -translate-y-1/2 transition-colors duration-200 ${searchFocused ? "text-accent" : "text-secondary"}`} />
          <input
            type="text"
            value={keyword}
            onChange={handleSearchChange}
            onFocus={() => setSearchFocused(true)}
            onBlur={() => setSearchFocused(false)}
            placeholder="Search by canvas title, conversation title, or message content..."
            className="w-full pl-11 pr-10 py-3 rounded-xl border border-main bg-node-bg text-primary text-base placeholder:text-secondary focus:outline-none focus:border-accent focus:ring-1 focus:ring-(--accent)/70 transition-all duration-200"
            autoFocus
          />
          {keyword && (
            <button
              onClick={clearSearch}
              className="absolute right-3 top-1/2 -translate-y-1/2 text-secondary hover:text-primary transition-colors cursor-pointer"
            >
              <X size={16} />
            </button>
          )}
        </div>

        {/* Prompt: no keyword entered */}
        {showPrompt && (
          <div className="flex flex-col items-center justify-center py-24 gap-4">
            <Search size={48} className="text-secondary" />
            <p className="text-secondary text-lg">Enter a keyword to search across all canvases</p>
          </div>
        )}

        {/* Loading skeleton */}
        {showLoading && (
          <div className="space-y-3">
            {Array.from({ length: 6 }).map((_, i) => (
              <div key={i} className="rounded-xl border border-main bg-node-bg p-4 animate-pulse">
                <div className="flex items-center justify-between mb-2">
                  <div className="h-5 w-1/3 rounded bg-secondary/10" />
                  <div className="h-4 w-20 rounded bg-secondary/10" />
                </div>
                <div className="flex items-center gap-2">
                  <div className="h-5 w-20 rounded-full bg-secondary/10" />
                  <div className="h-4 w-2/3 rounded bg-secondary/10" />
                </div>
              </div>
            ))}
          </div>
        )}

        {/* Empty state: keyword but no results */}
        {showEmpty && (
          <div className="flex flex-col items-center justify-center py-24 gap-4">
            <SearchX size={48} className="text-secondary" />
            <p className="text-secondary text-lg">No results found for "{debouncedKeyword}"</p>
            <button
              onClick={clearSearch}
              className="text-sm text-accent hover:underline cursor-pointer"
            >
              Clear search
            </button>
          </div>
        )}

        {/* Results list */}
        {showResults && (
          <>
            <p className="text-sm text-secondary mb-4">
              {total} result{total !== 1 ? "s" : ""} found
            </p>
            <div className="space-y-3">
              {results.map((item: CanvasSearchItem) => {
                const config = MATCH_TYPE_CONFIG[item.matchType];
                return (
                  <div
                    key={item.canvasId}
                    onClick={() => handleResultClick(item.canvasId)}
                    className="rounded-xl border border-main bg-node-bg p-4 cursor-pointer transition-all duration-200 hover:border-accent/50 hover:shadow-md"
                  >
                    {/* Row 1: Title + Date */}
                    <div className="flex items-start justify-between gap-4 mb-2">
                      <h3 className="text-base font-medium text-primary truncate flex-1">
                        <HighlightText text={item.title} keyword={debouncedKeyword} />
                      </h3>
                      <time className="text-xs text-secondary whitespace-nowrap mt-0.5">
                        {new Date(item.updatedAt).toLocaleDateString()}
                      </time>
                    </div>
                    {/* Row 2: Badge + Snippet */}
                    <div className="flex items-center gap-2">
                      <span className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium border ${config.className}`}>
                        {config.label}
                      </span>
                      {item.matchType !== "title" && (
                        <p className="text-sm text-secondary truncate flex-1">
                          <HighlightText text={item.matchText} keyword={debouncedKeyword} />
                        </p>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>

            {/* Pagination */}
            {totalPages > 1 && (
              <div className="flex items-center justify-center gap-4 mt-8">
                <button
                  disabled={page <= 1}
                  onClick={() => setPage((p) => p - 1)}
                  className="flex items-center gap-1 px-3 py-1.5 rounded-lg border border-main text-sm text-primary disabled:opacity-40 disabled:cursor-not-allowed hover:bg-(--node-bg) hover:border-(--accent)/50 hover:text-(--accent) active:scale-95 transition-all duration-200 cursor-pointer"
                >
                  <ChevronLeft size={16} />
                  Prev
                </button>
                <span className="text-sm text-secondary">
                  {page} / {totalPages}
                </span>
                <button
                  disabled={page >= totalPages}
                  onClick={() => setPage((p) => p + 1)}
                  className="flex items-center gap-1 px-3 py-1.5 rounded-lg border border-main text-sm text-primary disabled:opacity-40 disabled:cursor-not-allowed hover:bg-(--node-bg) hover:border-(--accent)/50 hover:text-(--accent) active:scale-95 transition-all duration-200 cursor-pointer"
                >
                  Next
                  <ChevronRight size={16} />
                </button>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
