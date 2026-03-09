import { useState, useEffect } from "react";
import { useQuery, useQueryClient, keepPreviousData } from "@tanstack/react-query";
import { Search, ChevronLeft, ChevronRight, X, FolderOpen } from "lucide-react";
import { Modal } from "../../ui/common/Modal";
import { fileListQueryOptions } from "../../query/file";
import { FileTypeIcon } from "../../ui/canvas/ResourceNode/FileTypeIcon";
import { getFileCategoryFromMime, formatFileSize } from "../../service/file";
import { BASE_URL } from "../../util/api";
import type { FileListItem } from "../../service/type";

const MAX_SELECTION = 10;
const PAGE_SIZE = 9;

interface FilePickerModalProps {
  isOpen: boolean;
  onClose: () => void;
  onConfirm: (fileIds: string[]) => void;
}

export function FilePickerModal({ isOpen, onClose, onConfirm }: FilePickerModalProps) {
  const queryClient = useQueryClient();
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [keyword, setKeyword] = useState("");
  const [debouncedKeyword, setDebouncedKeyword] = useState("");
  const [page, setPage] = useState(1);

  // Reset state on re-open (render-time state adjustment, no ref needed)
  const [prevIsOpen, setPrevIsOpen] = useState(false);
  if (isOpen && !prevIsOpen) {
    setSelectedIds(new Set());
    setKeyword("");
    setDebouncedKeyword("");
    setPage(1);
  }
  if (isOpen !== prevIsOpen) {
    setPrevIsOpen(isOpen);
  }
  
  // 300ms debounce for search
  useEffect(() => {
    const timer = setTimeout(() => {
      setDebouncedKeyword(keyword);
      setPage(1);
    }, 300);
    return () => clearTimeout(timer);
  }, [keyword]);

  const { data, isLoading } = useQuery({
    ...fileListQueryOptions({ page, limit: PAGE_SIZE, keyword: debouncedKeyword }),
    enabled: isOpen,
    placeholderData: keepPreviousData,
  });

  const files = data?.data?.files ?? [];
  const total = data?.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  // Prefetch adjacent pages
  useEffect(() => {
    if (!isOpen || isLoading) return;
    if (page < totalPages) {
      queryClient.prefetchQuery(
        fileListQueryOptions({ page: page + 1, limit: PAGE_SIZE, keyword: debouncedKeyword })
      );
    }
    if (page > 1) {
      queryClient.prefetchQuery(
        fileListQueryOptions({ page: page - 1, limit: PAGE_SIZE, keyword: debouncedKeyword })
      );
    }
  }, [isOpen, page, totalPages, debouncedKeyword, isLoading, queryClient]);

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

  const isImage = (file: FileListItem) =>
    getFileCategoryFromMime(file.contentType, file.filename) === "image";

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title="Select Files"
      width="max-w-3xl"
      footer={
        <div className="flex items-center justify-between w-full">
          <span className="text-sm text-secondary">
            {selectedIds.size} / {MAX_SELECTION} selected
          </span>
          <div className="flex gap-3">
            <button
              onClick={onClose}
              className="px-4 py-2 rounded-lg border border-main text-sm text-primary hover:bg-(--node-bg) transition-colors cursor-pointer"
            >
              Cancel
            </button>
            <button
              onClick={handleConfirm}
              disabled={selectedIds.size === 0}
              className="px-4 py-2 rounded-lg bg-(--accent) text-white text-sm font-medium disabled:opacity-40 disabled:cursor-not-allowed hover:opacity-90 active:scale-95 transition-all cursor-pointer"
            >
              Confirm
            </button>
          </div>
        </div>
      }
    >
      {/* Search input */}
      <div className="relative mb-4">
        <Search
          size={16}
          className="absolute left-3 top-1/2 -translate-y-1/2 text-secondary"
        />
        <input
          type="text"
          value={keyword}
          onChange={(e) => setKeyword(e.target.value)}
          placeholder="Search files..."
          className="w-full pl-9 pr-9 py-2 rounded-lg border border-main bg-(--input-bg) text-primary text-sm placeholder:text-secondary focus:outline-none focus:border-(--accent) focus:ring-1 focus:ring-(--accent)/70 transition-all"
        />
        {keyword && (
          <button
            onClick={() => setKeyword("")}
            className="absolute right-3 top-1/2 -translate-y-1/2 text-secondary hover:text-primary transition-colors cursor-pointer"
          >
            <X size={14} />
          </button>
        )}
      </div>

      {/* Content area */}
      <div className="h-[430px]">
        {/* Loading skeleton */}
        {isLoading && (
          <div className="grid grid-cols-3 gap-3">
            {Array.from({ length: PAGE_SIZE }).map((_, i) => (
              <div
                key={i}
                className="rounded-lg border border-main bg-(--node-bg) p-3 animate-pulse"
              >
                <div className="h-16 w-full rounded bg-secondary/10 mb-2" />
                <div className="h-4 w-3/4 rounded bg-secondary/10" />
              </div>
            ))}
          </div>
        )}

        {/* Empty: no files at all */}
        {!isLoading && files.length === 0 && !keyword && (
          <div className="flex flex-col items-center justify-center py-16 gap-3">
            <FolderOpen size={40} className="text-secondary" />
            <p className="text-secondary text-sm text-center">
              No files uploaded yet.
              <br />
              Upload files in My Resources to get started.
            </p>
          </div>
        )}

        {/* Empty: search no results */}
        {!isLoading && files.length === 0 && keyword && (
          <div className="flex flex-col items-center justify-center py-16 gap-3">
            <Search size={40} className="text-secondary" />
            <p className="text-secondary text-sm">
              No files matching "{keyword}"
            </p>
          </div>
        )}

        {/* File grid */}
        {!isLoading && files.length > 0 && (
          <div className="grid grid-cols-3 gap-3">
            {files.map((file) => {
              const selected = selectedIds.has(file.fileId);
              const atLimit = selectedIds.size >= MAX_SELECTION && !selected;

              return (
                <button
                  key={file.fileId}
                  onClick={() => toggleSelect(file.fileId)}
                  disabled={atLimit}
                  className={`relative flex flex-col items-center gap-2 p-3 rounded-lg border text-left transition-all cursor-pointer ${
                    selected
                      ? "border-(--accent) bg-(--accent)/10 ring-1 ring-(--accent)/50"
                      : "border-main bg-(--node-bg) hover:border-(--accent)/50 hover:shadow-sm"
                  } ${atLimit ? "opacity-40 cursor-not-allowed" : ""}`}
                >
                  {/* Checkbox indicator */}
                  <div
                    className={`absolute top-2 right-2 w-5 h-5 rounded-md border-2 flex items-center justify-center transition-colors ${
                      selected
                        ? "bg-(--accent) border-(--accent)"
                        : "border-secondary/40"
                    }`}
                  >
                    {selected && (
                      <svg
                        width="12"
                        height="12"
                        viewBox="0 0 12 12"
                        fill="none"
                      >
                        <path
                          d="M2.5 6L5 8.5L9.5 3.5"
                          stroke="white"
                          strokeWidth="2"
                          strokeLinecap="round"
                          strokeLinejoin="round"
                        />
                      </svg>
                    )}
                  </div>

                  {/* Thumbnail / Icon */}
                  <div className="w-full h-16 flex items-center justify-center overflow-hidden rounded">
                    {isImage(file) ? (
                      <ImageThumbnail file={file} />
                    ) : (
                      <FileTypeIcon
                        fileType={getFileCategoryFromMime(
                          file.contentType,
                          file.filename
                        )}
                        size={32}
                      />
                    )}
                  </div>

                  {/* Filename + size */}
                  <div className="w-full min-w-0">
                    <p className="text-xs text-primary truncate">
                      {file.filename}
                    </p>
                    <p className="text-xs text-secondary">
                      {formatFileSize(file.fileSize)}
                    </p>
                  </div>
                </button>
              );
            })}
          </div>
        )}
      </div>

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="flex items-center justify-center gap-4 mt-4 pt-4 border-t border-main">
          <button
            disabled={page <= 1 || isLoading}
            onClick={() => setPage((p) => p - 1)}
            className="flex items-center gap-1 px-3 py-1.5 rounded-lg border border-main text-sm text-primary disabled:opacity-40 disabled:cursor-not-allowed hover:bg-(--node-bg) hover:border-(--accent)/50 hover:text-(--accent) active:scale-95 transition-all cursor-pointer"
          >
            <ChevronLeft size={16} />
            Prev
          </button>
          <span className="text-sm text-secondary">
            {page} / {totalPages}
          </span>
          <button
            disabled={page >= totalPages || isLoading}
            onClick={() => setPage((p) => p + 1)}
            className="flex items-center gap-1 px-3 py-1.5 rounded-lg border border-main text-sm text-primary disabled:opacity-40 disabled:cursor-not-allowed hover:bg-(--node-bg) hover:border-(--accent)/50 hover:text-(--accent) active:scale-95 transition-all cursor-pointer"
          >
            Next
            <ChevronRight size={16} />
          </button>
        </div>
      )}
    </Modal>
  );
}

/** Image thumbnail with fallback to FileTypeIcon on error */
function ImageThumbnail({ file }: { file: FileListItem }) {
  const [failed, setFailed] = useState(false);

  if (failed) {
    return (
      <FileTypeIcon
        fileType={getFileCategoryFromMime(file.contentType, file.filename)}
        size={32}
      />
    );
  }

  return (
    <img
      src={`${BASE_URL}/api/file/${file.fileId}`}
      alt={file.filename}
      loading="lazy"
      className="max-h-16 max-w-full object-contain rounded"
      onError={() => setFailed(true)}
    />
  );
}
