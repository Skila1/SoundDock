import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@/lib/api";
import type { Favourite } from "@/types/api";

/** Favourite state and toggle for one song; keeps Home and lists in sync. */
export function useFavouriteTrack(id: string | null | undefined) {
  const qc = useQueryClient();
  const favs = useQuery({ queryKey: ["favourites"], queryFn: () => api.get<Favourite[]>("/api/v1/favourites") });
  const fav = !!id && (favs.data || []).some((f) => f.type === "track" && f.id === id);
  const toggle = async () => {
    if (!id) return;
    const on = !fav;
    qc.setQueryData<Favourite[]>(["favourites"], (old) =>
      on ? [{ type: "track", id } as Favourite, ...(old || [])] : (old || []).filter((f) => !(f.type === "track" && f.id === id))
    );
    try {
      await api.post("/api/v1/favourites", { type: "track", id, on });
      toast.success(on ? "Added to favourites" : "Removed from favourites");
    } catch {
      toast.error("Could not update favourites");
    } finally {
      void qc.invalidateQueries({ predicate: (q) => q.queryKey[0] === "favourites" || q.queryKey[0] === "home" });
    }
  };
  return { fav, toggle };
}
