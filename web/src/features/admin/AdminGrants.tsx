import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Select } from "@/components/ui/select";
import { Badge } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { toast } from "sonner";

type Grant = {
  id: string;
  kind: "role" | "user";
  actions?: string[];
  username?: string | null;
  role?: string | null;
  user_id?: string | null;
};

export function AdminGrants() {
  const qc = useQueryClient();
  const libs = useQuery({ queryKey: ["libraries"], queryFn: () => api.get<any[]>("/api/v1/libraries") });
  const users = useQuery({ queryKey: ["admin-users"], queryFn: () => api.get<any[]>("/api/v1/admin/users") });
  const [lib, setLib] = useState("");
  const [userId, setUserId] = useState("");
  const grants = useQuery({
    queryKey: ["admin-grants", lib],
    queryFn: () => api.get<Grant[]>(`/api/v1/admin/libraries/${lib}/grants`),
    enabled: !!lib
  });
  const libOptions = (libs.data || []).map((l: any) => ({ value: l.id, label: l.name }));
  const userOptions = (users.data || []).map((u: any) => ({ value: u.id, label: u.display_name || u.username }));
  return (
    <div>
      <PageHeader title="Library grants" description="Per-user grants add to role grants. Removing a user grant never deletes Administrator or User role access." />
      <div className="mb-4 max-w-sm">
        <Field label="Library">
          <Select value={lib} onValueChange={setLib} options={libOptions} placeholder="Select library" />
        </Field>
      </div>
      {lib && (
        <>
          <form className="mb-4 flex max-w-lg flex-wrap items-end gap-2" onSubmit={async (e) => {
            e.preventDefault();
            if (!userId) return;
            await api.post(`/api/v1/admin/libraries/${lib}/grants`, { user_id: userId, actions: ["read", "stream"] });
            toast.success("User grant added");
            qc.invalidateQueries({ queryKey: ["admin-grants", lib] });
          }}>
            <Field label="Add user grant">
              <Select value={userId} onValueChange={setUserId} options={userOptions} placeholder="Select user" />
            </Field>
            <Button type="submit">Add</Button>
          </form>
          <ul className="space-y-2">
            {(grants.data || []).map((g) => (
              <li key={g.id} className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-border px-4 py-3">
                <div>
                  <div className="font-medium">{g.kind === "role" ? g.role : g.username}</div>
                  <div className="text-xs text-muted">{(g.actions || []).join(", ")}</div>
                </div>
                <div className="flex items-center gap-2">
                  <Badge tone={g.kind === "role" ? "neutral" : "accent"}>{g.kind}</Badge>
                  {g.kind === "user" && (
                    <Button size="sm" variant="ghost" onClick={async () => {
                      await api.del(`/api/v1/admin/libraries/${lib}/grants/${g.id}`);
                      toast.success("User grant removed");
                      qc.invalidateQueries({ queryKey: ["admin-grants", lib] });
                    }}>Remove</Button>
                  )}
                </div>
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
}
