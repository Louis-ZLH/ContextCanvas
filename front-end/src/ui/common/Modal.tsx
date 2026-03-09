import { X } from "lucide-react";
import { useEffect, useCallback } from "react";
import { createPortal } from "react-dom";
import { useSelector } from "react-redux";
import type { ThemeName } from "../../feature/user/userSlice";

interface ModalProps {
  isOpen: boolean;
  onClose: () => void;
  title?: React.ReactNode;
  children: React.ReactNode;
  footer?: React.ReactNode;
  width?: string;
}

export function Modal({ isOpen, onClose, title, children, footer, width = "max-w-md" }: ModalProps) {
  const theme = useSelector(
    (state: { user: { theme: ThemeName } }) => state.user.theme
  );

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        onClose();
      }
    },
    [onClose]
  );

  useEffect(() => {
    if (isOpen) {
      document.addEventListener("keydown", handleKeyDown);
      document.body.style.overflow = "hidden";
    }
    return () => {
      document.removeEventListener("keydown", handleKeyDown);
      document.body.style.overflow = "";
    };
  }, [isOpen, handleKeyDown]);

  if (!isOpen) return null;

  const modalContent = (
    <div
      className="fixed inset-0 z-[9999] flex items-center justify-center p-4"
      onClick={onClose}
    >
      {/* 遮罩层 */}
      <div
        className={`absolute inset-0 transition-opacity duration-300 ${
          theme === "dark"
            ? "bg-black/70 backdrop-blur-sm"
            : theme === "paper"
              ? "bg-stone-900/40"
              : "bg-black/50"
        }`}
      />

      {/* Modal 主体 */}
      <div
        onClick={(e) => e.stopPropagation()}
        className={`relative w-full ${width} flex flex-col rounded-2xl shadow-2xl transform transition-all duration-300 animate-in fade-in zoom-in-95 ${
          theme === "dark"
            ? "bg-zinc-900 border border-zinc-700 text-zinc-100"
            : theme === "paper"
              ? "bg-[#fdfbf7] border border-stone-300 text-stone-800"
              : "bg-white border border-gray-200 text-gray-800"
        }`}
      >
        {/* 关闭按钮 */}
        <button
          onClick={onClose}
          className={`absolute top-4 right-4 z-10 p-2 rounded-full transition-colors cursor-pointer ${
            theme === "dark"
              ? "hover:bg-white/10 text-zinc-400 hover:text-white"
              : theme === "paper"
                ? "hover:bg-stone-200 text-stone-500 hover:text-stone-800"
                : "hover:bg-gray-100 text-gray-400 hover:text-gray-600"
          }`}
        >
          <X size={20} />
        </button>

        {/* Header */}
        {title && (
          <div className="p-6 pb-2 shrink-0">
            <h3 className="text-xl font-bold">{title}</h3>
          </div>
        )}

        {/* Content */}
        <div className="p-6">
          {children}
        </div>

        {/* Footer */}
        {footer && (
          <div className={`p-4 border-t flex justify-end gap-3 rounded-b-2xl shrink-0 ${
            theme === "dark" ? "border-zinc-800 bg-zinc-900/50" 
            : theme === "paper" ? "border-stone-200 bg-stone-50"
            : "border-gray-100 bg-gray-50"
          }`}>
            {footer}
          </div>
        )}
      </div>
    </div>
  );

  return createPortal(modalContent, document.body);
}
