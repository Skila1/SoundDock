import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { toast } from "sonner";

type Demo = { seeded?: boolean; library_id?: string | null; track_count?: number };

export function AdminDemo() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["admin-demo"], queryFn: () => api.get<Demo>("/api/v1/admin/demo") });
  const d = q.data || {};
  return (
    <div>
      <PageHeader title="Demo library" description="Creates a small synthetic library of generated tones. Never runs at startup." />
      <div className="mb-4 flex flex-wrap gap-2">
        <Badge tone={d.seeded ? "success" : "neutral"}>{d.seeded ? "Demo present" : "Not seeded"}</Badge>
        {d.seeded && <Badge>{d.track_count ?? 0} tracks</Badge>}
      </div>
      <div className="max-w-lg space-y-3 rounded-xl border border-border bg-surface-1 p-4">
        <p className="text-sm text-muted">Three short WAV tones (A4, C5, E5) in a library named Demo. Role grants are added alongside existing role grants. No copyrighted audio is included.</p>
        <div className="flex flex-wrap gap-2">
          <Button onClick={async () => {
            try {
              const r = await api.post<Demo & { already_seeded?: boolean }>("/api/v1/admin/demo");
              toast.success(r.already_seeded ? "Demo library already exists" : "Demo library created");
              qc.invalidateQueries({ queryKey: ["admin-demo"] });
              qc.invalidateQueries({ queryKey: ["libraries"] });
            } catch (e) {
              toast.error(e instanceof Error ? e.message : "Could not seed demo library");
            }
          }}>Create demo library</Button>
          <Button variant="secondary" disabled={!d.seeded} onClick={async () => {
            if (!window.confirm("Remove the Demo library and its generated files?")) return;
            try {
              await api.del("/api/v1/admin/demo");
              toast.success("Demo library removed");
              qc.invalidateQueries({ queryKey: ["admin-demo"] });
              qc.invalidateQueries({ queryKey: ["libraries"] });
            } catch (e) {
              toast.error(e instanceof Error ? e.message : "Could not remove demo library");
            }
          }}>Remove demo library</Button>
        </div>
      </div>
    </div>
  );
}
