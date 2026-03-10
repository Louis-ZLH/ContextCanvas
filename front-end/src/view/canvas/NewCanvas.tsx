import { Plus, MessageCircle, FolderOpen, ArrowRight, Sparkles, Loader2 } from "lucide-react";
import { useState, useRef } from "react";
import { useMutation } from "@tanstack/react-query";
import { createCanvas as createCanvasService } from "../../service/canvas";
import { useNavigate } from "react-router";
import { toast } from "react-hot-toast";
import { queryClient } from "../../query";
import { FilePickerModal } from "./FilePickerModal";

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

  return (
    <div className="w-full h-full bg-canvas flex flex-col items-center justify-center p-4 sm:p-6 animate-fade-in">
      <div className="max-w-5xl w-full space-y-4 sm:space-y-12">
        {/* Header Section */}
        <div className="text-center space-y-2 sm:space-y-4">
          <div className="inline-flex items-center justify-center p-2 sm:p-3 bg-accent/10 rounded-full mb-2 sm:mb-4">
            <Sparkles className="w-5 h-5 sm:w-6 sm:h-6 text-accent" />
          </div>
          <h1 className="text-2xl sm:text-4xl md:text-5xl font-bold text-primary tracking-tight">
            Start Your Creation
          </h1>
          <p className="text-secondary text-sm sm:text-lg md:text-xl max-w-2xl mx-auto">
            Choose a way to start your journey of thought and capture moments of inspiration.
          </p>
        </div>

        {/* Cards Grid */}
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3 sm:gap-8 px-0 sm:px-4">
          {/* Blank Canvas Card */}
          <button
            onClick={handleCreateBlankCanvas}
            disabled={isPending}
            className={`group relative flex flex-col items-start p-4 sm:p-8 h-36 sm:h-80 rounded-2xl sm:rounded-3xl border-2 border-dashed border-main bg-node-bg/50 transition-[transform,box-shadow,background-color,border-color] duration-300 text-left ${
              isPending
                ? "cursor-not-allowed opacity-70"
                : "hover:border-accent hover:bg-node-bg cursor-pointer hover:-translate-y-1 hover:shadow-xl"
            }`}
          >
            {!isPending && (
              <div className="absolute top-4 right-4 sm:top-6 sm:right-6 opacity-0 group-hover:opacity-100 transition-[opacity,transform] duration-300 transform translate-x-2 group-hover:translate-x-0">
                <ArrowRight className="w-5 h-5 sm:w-6 sm:h-6 text-accent" />
              </div>
            )}

            <div className="w-10 h-10 sm:w-16 sm:h-16 rounded-xl sm:rounded-2xl bg-accent/10 flex items-center justify-center mb-auto group-hover:scale-110 transition-transform duration-300">
              {isPending ? (
                <Loader2 className="w-5 h-5 sm:w-8 sm:h-8 text-accent animate-spin" />
              ) : (
                <Plus className="w-5 h-5 sm:w-8 sm:h-8 text-accent" />
              )}
            </div>

            <div className="space-y-1 sm:space-y-2 mt-auto">
              <h3 className="text-lg sm:text-2xl font-bold text-primary group-hover:text-accent transition-colors">
                Blank Canvas
              </h3>
              <p className="text-secondary text-sm sm:text-base leading-relaxed group-hover:text-primary/80 transition-colors">
                Start from a blank slate and unleash your creativity.
                <br className="hidden sm:block" />
                Suitable for brainstorming and free drawing.
              </p>
            </div>
          </button>

          {/* Ask a Question Card */}
          <button
            onClick={handleAskQuestion}
            disabled={isPending}
            className={`group relative flex flex-col items-start p-4 sm:p-8 h-36 sm:h-80 rounded-2xl sm:rounded-3xl border border-main bg-node-bg shadow-sm transition-[transform,box-shadow,background-color,border-color] duration-300 text-left ${
              isPending
                ? "cursor-not-allowed opacity-70"
                : "hover:border-accent hover:shadow-xl cursor-pointer hover:-translate-y-1"
            }`}
          >
            {!isPending && (
              <div className="absolute top-4 right-4 sm:top-6 sm:right-6 opacity-0 group-hover:opacity-100 transition-[opacity,transform] duration-300 transform translate-x-2 group-hover:translate-x-0">
                <ArrowRight className="w-5 h-5 sm:w-6 sm:h-6 text-accent" />
              </div>
            )}

            <div className="w-10 h-10 sm:w-16 sm:h-16 rounded-xl sm:rounded-2xl bg-primary/5 flex items-center justify-center mb-auto group-hover:scale-110 transition-transform duration-300">
              <MessageCircle className="w-5 h-5 sm:w-8 sm:h-8 text-primary group-hover:text-accent transition-colors" />
            </div>

            <div className="space-y-1 sm:space-y-2 mt-auto">
              <h3 className="text-lg sm:text-2xl font-bold text-primary group-hover:text-accent transition-colors">
                Ask a Question
              </h3>
              <p className="text-secondary text-sm sm:text-base leading-relaxed group-hover:text-primary/80 transition-colors">
                Start a conversation with AI to explore ideas.
                <br className="hidden sm:block" />
                Jump straight into a focused chat session.
              </p>
            </div>
          </button>

          {/* Begin with Resources Card */}
          <button
            onClick={handleBeginWithResources}
            disabled={isPending}
            className={`group relative flex flex-col items-start p-4 sm:p-8 h-36 sm:h-80 rounded-2xl sm:rounded-3xl border border-main bg-node-bg shadow-sm transition-[transform,box-shadow,background-color,border-color] duration-300 text-left ${
              isPending
                ? "cursor-not-allowed opacity-70"
                : "hover:border-accent hover:shadow-xl cursor-pointer hover:-translate-y-1"
            }`}
          >
            {!isPending && (
              <div className="absolute top-4 right-4 sm:top-6 sm:right-6 opacity-0 group-hover:opacity-100 transition-[opacity,transform] duration-300 transform translate-x-2 group-hover:translate-x-0">
                <ArrowRight className="w-5 h-5 sm:w-6 sm:h-6 text-accent" />
              </div>
            )}

            <div className="w-10 h-10 sm:w-16 sm:h-16 rounded-xl sm:rounded-2xl bg-primary/5 flex items-center justify-center mb-auto group-hover:scale-110 transition-transform duration-300">
              <FolderOpen className="w-5 h-5 sm:w-8 sm:h-8 text-primary group-hover:text-accent transition-colors" />
            </div>

            <div className="space-y-1 sm:space-y-2 mt-auto">
              <h3 className="text-lg sm:text-2xl font-bold text-primary group-hover:text-accent transition-colors">
                Begin with Resources
              </h3>
              <p className="text-secondary text-sm sm:text-base leading-relaxed group-hover:text-primary/80 transition-colors">
                Select files as context for your conversation.
                <br className="hidden sm:block" />
                AI will use your resources to provide insights.
              </p>
            </div>
          </button>
        </div>
      </div>

      <FilePickerModal
        isOpen={isFilePickerOpen}
        onClose={() => setIsFilePickerOpen(false)}
        onConfirm={handleFilesSelected}
      />
    </div>
  );
}
