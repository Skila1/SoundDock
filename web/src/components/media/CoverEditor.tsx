import { useRef, useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ImagePlus, RotateCcw } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { cn } from "@/lib/utils";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { useArtworkVersion } from "@/stores/artwork";

type CoverKind = "track" | "album" | "artist";

const MAX_BYTES = 20 << 20;

const refreshKeys = ["album", "albums", "artist", "artists", "track", "track-meta", "tracks", "home", "search", "personal-library"];

/**
 * Wraps a cover with a "Change cover" menu: upload your own image or go back to
 * the embedded one. The server checks library write access on every change.
 */
export function CoverEditor({
  kind,
  id,
  canEdit,
  className,
  children
}: {
  kind: CoverKind;
  id: string;
  canEdit: boolean;
  className?: string;
  children: ReactNode;
}) {
  const qc = useQueryClient();
  const fileRef = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);
  const noun = kind === "artist" ? "image" : "cover";

  const refresh = () => {
    useArtworkVersion.getState().bump();
    for (const key of refreshKeys) void qc.invalidateQueries({ queryKey: [key] });
  };

  const upload = async (file: File) => {
    if (!file.type.startsWith("image/")) {
      toast.error("Choose an image file (JPEG, PNG, WebP or GIF).");
      return;
    }
    if (file.size > MAX_BYTES) {
      toast.error("That image is larger than 20 MB.");
      return;
    }
    setBusy(true);
    try {
      const fd = new FormData();
      fd.append("file", file);
      await api.post(`/api/v1/${kind}s/${id}/artwork`, fd);
      toast.success(kind === "artist" ? "Artist image updated" : "Cover updated");
      refresh();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : `Could not update the ${noun}`);
    } finally {
      setBusy(false);
    }
  };

  const reset = async () => {
    setBusy(true);
    try {
      const res = await api.del<{ removed?: number }>(`/api/v1/${kind}s/${id}/artwork`);
      toast.success(res?.removed ? `Custom ${noun} removed` : `No custom ${noun} to remove`);
      refresh();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : `Could not remove the ${noun}`);
    } finally {
      setBusy(false);
    }
  };

  if (!canEdit) return <div className={className}>{children}</div>;

  return (
    <div className={cn("group relative", className)}>
      {children}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            disabled={busy}
            className="absolute inset-x-2 bottom-2 flex items-center justify-center gap-1.5 rounded-full bg-black/65 px-3 py-1.5 text-xs font-semibold text-white opacity-100 backdrop-blur transition focus-visible:opacity-100 disabled:opacity-60 md:opacity-0 md:group-hover:opacity-100"
          >
            <ImagePlus className="h-3.5 w-3.5" />
            {busy ? "Saving…" : kind === "artist" ? "Change image" : "Change cover"}
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="center">
          <DropdownMenuItem onSelect={() => fileRef.current?.click()}>
            <ImagePlus className="h-4 w-4" /> Upload image…
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => void reset()}>
            <RotateCcw className="h-4 w-4" /> Remove custom {noun}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <input
        ref={fileRef}
        type="file"
        accept="image/jpeg,image/png,image/webp,image/gif"
        className="hidden"
        onChange={(e) => {
          const file = e.target.files?.[0];
          e.target.value = "";
          if (file) void upload(file);
        }}
      />
    </div>
  );
}
