import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Progress } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { formatBytes } from "@/lib/utils";
import { toast } from "sonner";

type Quotas = {
  default_user_bytes: number;
  default_library_bytes: number;
  users: { user_id: string; max_bytes: number }[];
  libraries: { library_id: string; max_bytes: number }[];
  library_usage?: Record<string, number>;
  user_usage?: Record<string, number>;
};

export function AdminQuotas() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["admin-quotas"], queryFn: () => api.get<Quotas>("/api/v1/admin/quotas") });
  const libs = useQuery({ queryKey: ["libraries"], queryFn: () => api.get<any[]>("/api/v1/libraries") });
  const users = useQuery({ queryKey: ["admin-users"], queryFn: () => api.get<any[]>("/api/v1/admin/users") });
  const [userBytes, setUserBytes] = useState("0");
  const [libBytes, setLibBytes] = useState("0");
  const [userCaps, setUserCaps] = useState<Record<string, string>>({});
  const [libCaps, setLibCaps] = useState<Record<string, string>>({});
  useEffect(() => {
    if (!q.data) return;
    setUserBytes(String(q.data.default_user_bytes || 0));
    setLibBytes(String(q.data.default_library_bytes || 0));
    const u: Record<string, string> = {};
    (q.data.users || []).forEach((row) => { u[row.user_id] = String(row.max_bytes); });
    setUserCaps(u);
    const l: Record<string, string> = {};
    (q.data.libraries || []).forEach((row) => { l[row.library_id] = String(row.max_bytes); });
    setLibCaps(l);
  }, [q.data]);
  const usageL = q.data?.library_usage || {};
  const usageU = q.data?.user_usage || {};
  return (
    <div>
      <PageHeader title="Quotas" description="0 means unlimited. Per-user caps apply to uploads; per-library caps apply to track files." />
      <form
        className="max-w-xl space-y-4"
        onSubmit={async (e) => {
          e.preventDefault();
          await api.put("/api/v1/admin/quotas", {
            default_user_bytes: Number(userBytes) || 0,
            default_library_bytes: Number(libBytes) || 0,
            users: (users.data || []).filter((u: any) => userCaps[u.id] != null && userCaps[u.id] !== "").map((u: any) => ({ user_id: u.id, max_bytes: Number(userCaps[u.id]) || 0 })),
            libraries: (libs.data || []).filter((l: any) => libCaps[l.id] != null && libCaps[l.id] !== "").map((l: any) => ({ library_id: l.id, max_bytes: Number(libCaps[l.id]) || 0 }))
          });
          toast.success("Quotas saved");
          qc.invalidateQueries({ queryKey: ["admin-quotas"] });
        }}
      >
        <Field label="Default user upload cap (bytes)" hint="Applies when a user has no override.">
          <Input type="number" min={0} value={userBytes} onChange={(e) => setUserBytes(e.target.value)} />
        </Field>
        <Field label="Default library cap (bytes)">
          <Input type="number" min={0} value={libBytes} onChange={(e) => setLibBytes(e.target.value)} />
        </Field>
        <h3 className="font-semibold">Libraries</h3>
        <ul className="max-h-[min(22rem,40vh)] space-y-2 overflow-y-auto rounded-xl border border-border p-2 scrollbar-thin">
          {(libs.data || []).map((l: any) => {
            const used = usageL[l.id] || 0;
            const cap = Number(libCaps[l.id] ?? q.data?.default_library_bytes ?? 0) || 0;
            const pct = cap > 0 ? Math.min(100, (used / cap) * 100) : 0;
            return (
              <li key={l.id} className="rounded-lg border border-border bg-surface-1 px-3 py-2">
                <div className="mb-2 flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <div className="truncate font-medium">{l.name}</div>
                    <div className="text-xs text-subtle">{formatBytes(used)} used{cap ? ` / ${formatBytes(cap)}` : ""}</div>
                  </div>
                  <Input className="w-32" type="number" min={0} placeholder="override" value={libCaps[l.id] ?? ""} onChange={(e) => setLibCaps({ ...libCaps, [l.id]: e.target.value })} />
                </div>
                {cap > 0 && <Progress value={pct} />}
              </li>
            );
          })}
        </ul>
        <h3 className="font-semibold">Users</h3>
        <ul className="max-h-[min(22rem,40vh)] space-y-2 overflow-y-auto rounded-xl border border-border p-2 scrollbar-thin">
          {(users.data || []).map((u: any) => {
            const used = usageU[u.id] || 0;
            const cap = Number(userCaps[u.id] ?? q.data?.default_user_bytes ?? 0) || 0;
            return (
              <li key={u.id} className="flex items-center justify-between gap-3 rounded-lg border border-border bg-surface-1 px-3 py-2">
                <div className="min-w-0">
                  <div className="truncate font-medium">{u.display_name || u.username}</div>
                  <div className="text-xs text-subtle">{formatBytes(used)} uploaded{cap ? ` / ${formatBytes(cap)}` : ""}</div>
                </div>
                <Input className="w-32 shrink-0" type="number" min={0} placeholder="override" value={userCaps[u.id] ?? ""} onChange={(e) => setUserCaps({ ...userCaps, [u.id]: e.target.value })} />
              </li>
            );
          })}
        </ul>
        <Button type="submit">Save quotas</Button>
      </form>
    </div>
  );
}
