import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Badge } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { Switch } from "@/components/ui/switch";
import { toast } from "sonner";

export function AdminMaintenance() {
  const qc = useQueryClient();
  const m = useQuery({ queryKey: ["admin-maintenance"], queryFn: () => api.get<{ maintenance: boolean }>("/api/v1/admin/maintenance") });
  const a = useQuery({ queryKey: ["admin-announcement"], queryFn: () => api.get<{ announcement: string; maintenance: boolean }>("/api/v1/admin/announcement") });
  const [on, setOn] = useState(false);
  const [text, setText] = useState("");
  useEffect(() => { if (m.data) setOn(!!m.data.maintenance); }, [m.data]);
  useEffect(() => { if (a.data) setText(a.data.announcement || ""); }, [a.data]);
  return (
    <div>
      <PageHeader title="Maintenance" description="Blocks library, user, config, upload, metadata, and delete writes. Queue, listen history, scrobbles, and stream stay available. /healthz stays 200." />
      <div className="mb-4 flex flex-wrap gap-2">
        <Badge tone={on ? "warning" : "success"}>{on ? "Maintenance on" : "Live"}</Badge>
      </div>
      <div className="mb-6 flex max-w-lg items-center justify-between rounded-xl border border-border bg-surface-1 p-4">
        <div>
          <div className="text-sm font-medium">Maintenance mode</div>
          <p className="text-xs text-subtle">Users can still play, skip, and scrobble. Admins can turn this off here.</p>
        </div>
        <Switch checked={on} onCheckedChange={async (v) => {
          setOn(v);
          try {
            await api.put("/api/v1/admin/maintenance", { maintenance: v });
            toast.success(v ? "Maintenance on" : "Maintenance off");
            qc.invalidateQueries({ queryKey: ["admin-maintenance"] });
            qc.invalidateQueries({ queryKey: ["admin-announcement"] });
            qc.invalidateQueries({ queryKey: ["admin-health-detail"] });
          } catch (e) {
            setOn(!v);
            toast.error(e instanceof Error ? e.message : "Could not update maintenance");
          }
        }} />
      </div>
      <form className="max-w-lg space-y-3 rounded-xl border border-border bg-surface-1 p-4" onSubmit={async (e) => {
        e.preventDefault();
        await api.put("/api/v1/admin/announcement", { announcement: text });
        toast.success("Announcement saved");
        qc.invalidateQueries({ queryKey: ["admin-announcement"] });
      }}>
        <h3 className="font-semibold">Announcement</h3>
        <p className="text-sm text-muted">Shown in the app shell banner after the integrator mounts GET /api/v1/announcement.</p>
        <Field label="Message">
          <Input value={text} onChange={(e) => setText(e.target.value)} placeholder="Optional banner text" />
        </Field>
        <Button type="submit">Save announcement</Button>
      </form>
    </div>
  );
}
