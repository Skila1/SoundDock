import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { Library, Plus, Trash2, User as UserIcon, UsersRound } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/alert-dialog";
import { EmptyState, PageHeader, QueryError } from "@/components/ui/empty";
import { Select } from "@/components/ui/select";
import { cn } from "@/lib/utils";
import type { LibraryGrant, LibraryGrantsStrict } from "@/types/api";
import { SettingSwitch, errorMessage } from "../adminUi";
import { useAdminUsers, useGroups, userLabel } from "./iamShared";

type LibraryRow = { id: string; name: string };
const ACTIONS = [
  { id: "read", label: "Browse" },
  { id: "stream", label: "Play" },
  { id: "write", label: "Change" }
] as const;

const grantsKey = (lib: string) => ["admin-grants", lib] as const;

/** Library access as a page: pick a library, tick who may browse, play and change it. */
export function AdminAccess() {
  return (
    <div>
      <PageHeader title="Library access" description="Choose who can browse, play and change each library. Groups give access to everyone in them." />
      <AccessMatrix />
    </div>
  );
}

export function AccessMatrix({ initialLibrary = "" }: { initialLibrary?: string }) {
  const qc = useQueryClient();
  const [params, setParams] = useSearchParams();
  const libs = useQuery({ queryKey: ["libraries"], queryFn: () => api.get<LibraryRow[]>("/api/v1/libraries") });
  const users = useAdminUsers();
  const groups = useGroups();
  const strict = useQuery({ queryKey: ["library-grants-strict"], queryFn: () => api.get<LibraryGrantsStrict>("/api/v1/admin/library-grants-strict") });
  const libList = libs.data || [];
  const lib = params.get("lib") || initialLibrary || libList[0]?.id || "";
  const setLib = (id: string) => {
    const next = new URLSearchParams(params);
    next.set("lib", id);
    setParams(next, { replace: true });
  };
  const allGrants = useQueries({
    queries: libList.map((l) => ({ queryKey: grantsKey(l.id), queryFn: () => api.get<LibraryGrant[]>(`/api/v1/admin/libraries/${l.id}/grants`) }))
  });
  const grantsFor = (id: string) => allGrants[libList.findIndex((l) => l.id === id)];
  const current = grantsFor(lib);
  const grants = useMemo(() => sortGrants(current?.data || []), [current?.data]);
  const [removing, setRemoving] = useState<LibraryGrant | null>(null);
  const [adding, setAdding] = useState("");
  useEffect(() => setAdding(""), [lib]);

  const granted = new Set(grants.map((g) => (g.kind === "role" ? `role:${g.role_id || g.role}` : `user:${g.user_id || g.username}`)));
  const addOptions = [
    ...(groups.data || []).filter((g) => !granted.has(`role:${g.id}`) && !granted.has(`role:${g.name}`)).map((g) => ({ value: `role:${g.id}`, label: `Group: ${g.name}` })),
    ...(users.data || []).filter((u) => !granted.has(`user:${u.id}`) && !granted.has(`user:${u.username}`)).map((u) => ({ value: `user:${u.id}`, label: userLabel(u) }))
  ];

  const invalidate = () => qc.invalidateQueries({ queryKey: grantsKey(lib) });

  async function setActions(g: LibraryGrant, action: string, on: boolean) {
    const cur = g.actions || [];
    const next = on ? [...cur.filter((x) => x !== action), action] : cur.filter((x) => x !== action);
    try {
      await api.patch(`/api/v1/admin/libraries/${lib}/grants/${g.id}`, { actions: next });
      await invalidate();
    } catch (e) {
      toast.error(errorMessage(e, "Could not change access"));
    }
  }

  async function add(value: string) {
    setAdding(value);
    const [kind, id] = value.split(":");
    try {
      await api.post(`/api/v1/admin/libraries/${lib}/grants`, kind === "role" ? { role_id: id, actions: ["read", "stream"] } : { user_id: id, actions: ["read", "stream"] });
      toast.success("Access added");
      await invalidate();
    } catch (e) {
      toast.error(errorMessage(e, "Could not add access"));
    } finally {
      setAdding("");
    }
  }

  if (libs.isError) return <QueryError message={errorMessage(libs.error, "Could not load libraries")} onRetry={() => libs.refetch()} />;

  return (
    <div className="space-y-5">
      <section className="rounded-2xl border border-border bg-surface-1 px-5 py-3 shadow-sm">
        <SettingSwitch
          label="Strict permissions"
          description="On: only the boxes you tick count. Off: anyone listed can always browse and play, even with nothing ticked."
          checked={!!strict.data?.library_grants_strict}
          onChange={async (v) => {
            try {
              await api.put("/api/v1/admin/library-grants-strict", { library_grants_strict: v });
              toast.success(v ? "Strict permissions on" : "Strict permissions off");
              qc.invalidateQueries({ queryKey: ["library-grants-strict"] });
            } catch (err) {
              toast.error(errorMessage(err, "Could not change strict mode"));
            }
          }}
        />
      </section>

      {!libs.isLoading && !libList.length ? (
        <EmptyState icon={Library} title="No libraries yet" description="Add a library first, then choose who can use it." />
      ) : (
        <>
          <div className="-mx-1 flex gap-2 overflow-x-auto px-1 pb-1 scrollbar-thin" role="tablist" aria-label="Libraries">
            {libList.map((l) => {
              const n = grantsFor(l.id)?.data?.length;
              const active = l.id === lib;
              return (
                <button
                  key={l.id}
                  type="button"
                  role="tab"
                  aria-selected={active}
                  onClick={() => setLib(l.id)}
                  className={cn(
                    "flex shrink-0 items-center gap-2 rounded-full px-3.5 py-1.5 text-sm font-medium ring-1 ring-inset transition-colors",
                    active ? "bg-accent text-[#04140a] ring-accent" : "bg-surface-1 text-muted ring-border hover:text-foreground"
                  )}
                >
                  <Library className="h-3.5 w-3.5" />
                  {l.name}
                  {n != null && <span className={cn("tabular text-xs", active ? "opacity-70" : "text-subtle")}>{n}</span>}
                </button>
              );
            })}
          </div>

          <div className="overflow-hidden rounded-2xl border border-border bg-surface-1 shadow-sm">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-border bg-surface-2/60 text-xs uppercase tracking-wider text-subtle">
                <tr>
                  <th className="px-4 py-3 font-semibold">Who</th>
                  {ACTIONS.map((a) => <th key={a.id} className="w-16 px-2 py-3 text-center font-semibold sm:w-24">{a.label}</th>)}
                  <th className="w-12 px-2 py-3"><span className="sr-only">Remove</span></th>
                </tr>
              </thead>
              <tbody>
                {grants.map((g) => {
                  const isGroup = g.kind === "role";
                  const label = (isGroup ? g.role : g.username) || "Unknown";
                  return (
                    <tr key={g.id} className="border-t border-border first:border-t-0">
                      <td className="px-4 py-3">
                        <div className="flex min-w-0 items-center gap-2.5">
                          <span className={cn("flex h-7 w-7 shrink-0 items-center justify-center rounded-full", isGroup ? "bg-accent/10 text-accent" : "bg-surface-2 text-muted")}>
                            {isGroup ? <UsersRound className="h-3.5 w-3.5" /> : <UserIcon className="h-3.5 w-3.5" />}
                          </span>
                          <span className="min-w-0">
                            <span className="block truncate font-medium">{label}</span>
                            <span className="block text-xs text-subtle">{isGroup ? (label === "User" ? "Everyone with an account" : "Group") : "Person"}</span>
                          </span>
                        </div>
                      </td>
                      {ACTIONS.map((a) => (
                        <td key={a.id} className="px-2 py-3 text-center">
                          <input
                            type="checkbox"
                            className="h-4 w-4 accent-[var(--sd-accent)]"
                            aria-label={`${a.label} for ${label}`}
                            checked={(g.actions || []).includes(a.id)}
                            onChange={(e) => void setActions(g, a.id, e.target.checked)}
                          />
                        </td>
                      ))}
                      <td className="px-2 py-3 text-right">
                        <Button size="icon" variant="ghost" className="h-8 w-8" aria-label={`Remove ${label}`} onClick={() => setRemoving(g)}>
                          <Trash2 />
                        </Button>
                      </td>
                    </tr>
                  );
                })}
                {!grants.length && (
                  <tr>
                    <td colSpan={5} className="px-4 py-8 text-center text-sm text-muted">{current?.isLoading ? "Loading..." : "Nobody has access to this library yet."}</td>
                  </tr>
                )}
              </tbody>
            </table>
            {lib && addOptions.length > 0 && (
              <div className="flex flex-wrap items-center gap-2 border-t border-border bg-surface-2/30 px-4 py-3">
                <Plus className="h-4 w-4 text-subtle" />
                <div className="min-w-0 flex-1 sm:max-w-xs">
                  <Select value={adding} onValueChange={(v) => void add(v)} options={addOptions} placeholder="Give a group or person access" />
                </div>
              </div>
            )}
          </div>
          <p className="text-xs text-subtle">Browse shows the library in lists and search. Play lets them stream it. Change lets them upload, edit and remove songs.</p>
        </>
      )}

      <ConfirmDialog
        open={!!removing}
        onOpenChange={(v) => { if (!v) setRemoving(null); }}
        title={`Remove ${removing?.kind === "role" ? removing?.role : removing?.username}?`}
        description={removing?.kind === "role" ? "Everyone who gets this library only through this group loses it." : "They lose this library unless one of their groups still has it."}
        confirmLabel="Remove access"
        destructive
        onConfirm={async () => {
          if (!removing) return;
          try {
            await api.del(`/api/v1/admin/libraries/${lib}/grants/${removing.id}`);
            toast.success("Access removed");
            await invalidate();
          } catch (e) {
            toast.error(errorMessage(e, "Could not remove access"));
          }
        }}
      />
    </div>
  );
}

/** Groups first (User group on top), then people, each alphabetical. */
function sortGrants(list: LibraryGrant[]) {
  const rank = (g: LibraryGrant) => (g.kind === "role" ? (g.role === "User" ? 0 : 1) : 2);
  const name = (g: LibraryGrant) => ((g.kind === "role" ? g.role : g.username) || "").toLowerCase();
  return [...list].sort((a, b) => rank(a) - rank(b) || name(a).localeCompare(name(b)));
}
