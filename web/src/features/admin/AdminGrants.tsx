import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Select } from "@/components/ui/select";
import { Badge } from "@/components/ui/misc";
import { PageHeader } from "@/components/ui/empty";
import { toast } from "sonner";
import type { LibraryGrant } from "@/types/api";

const ACTION_OPTS = ["read", "stream", "write"] as const;

export function AdminGrants() {
  const qc = useQueryClient();
  const libs = useQuery({ queryKey: ["libraries"], queryFn: () => api.get<any[]>("/api/v1/libraries") });
  const users = useQuery({ queryKey: ["admin-users"], queryFn: () => api.get<any[]>("/api/v1/admin/users") });
  const [lib, setLib] = useState("");
  const [userId, setUserId] = useState("");
  const grants = useQuery({
    queryKey: ["admin-grants", lib],
    queryFn: () => api.get<LibraryGrant[]>(`/api/v1/admin/libraries/${lib}/grants`),
    enabled: !!lib
  });
  const libOptions = (libs.data || []).map((l: any) => ({ value: l.id, label: l.name }));
  const userOptions = (users.data || []).map((u: any) => ({ value: u.id, label: u.display_name || u.username }));

  async function patchActions(g: LibraryGrant, next: string[]) {
    await api.patch(`/api/v1/admin/libraries/${lib}/grants/${g.id}`, { actions: next });
    qc.invalidateQueries({ queryKey: ["admin-grants", lib] });
  }

  return (
    <div>
      <PageHeader
        title="Library grants"
        description="Scoped ACL per library: read (catalogue), stream (playback), write (mutations). Capabilities such as upload still use permissions. A User role grant on a library is why everyone sees it — do not remove that row unless you intend to hide the library from the group."
      />
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
                  <div className="mt-1 flex flex-wrap gap-3 text-xs text-muted">
                    {ACTION_OPTS.map((a) => (
                      <label key={a} className="inline-flex items-center gap-1">
                        <input
                          type="checkbox"
                          checked={(g.actions || []).includes(a)}
                          onChange={(e) => {
                            const cur = g.actions || [];
                            const next = e.target.checked ? [...cur.filter((x) => x !== a), a] : cur.filter((x) => x !== a);
                            patchActions(g, next).catch((err) => toast.error(err instanceof Error ? err.message : "Could not update actions"));
                          }}
                        />
                        {a}
                      </label>
                    ))}
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <Badge tone={g.kind === "role" ? "neutral" : "accent"}>{g.kind}</Badge>
                  <Button size="sm" variant="ghost" onClick={async () => {
                    if (g.kind === "role" && !window.confirm("Remove this role grant? Everyone in the group loses this library until you add the grant back.")) {
                      return;
                    }
                    await api.del(`/api/v1/admin/libraries/${lib}/grants/${g.id}`);
                    toast.success(g.kind === "role" ? "Role grant removed" : "User grant removed");
                    qc.invalidateQueries({ queryKey: ["admin-grants", lib] });
                  }}>Remove</Button>
                </div>
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
}
