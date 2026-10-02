import { api } from "@/lib/api";

export type Quotas = {
  default_user_bytes: number;
  default_library_bytes: number;
  users: { user_id: string; max_bytes: number }[];
  libraries: { library_id: string; max_bytes: number }[];
  library_usage?: Record<string, number>;
  user_usage?: Record<string, number>;
};

export const quotasKey = ["admin-quotas"] as const;

export function fetchQuotas() {
  return api.get<Quotas>("/api/v1/admin/quotas");
}

type QuotaPatch = {
  defaultUserBytes?: number;
  defaultLibraryBytes?: number;
  /** null removes the override so the default applies. */
  user?: { id: string; bytes: number | null };
  library?: { id: string; bytes: number | null };
};

/** Pure merge used by saveQuotas; exported for tests. */
export function mergeQuotas(cur: Quotas, patch: QuotaPatch) {
  const users = (cur.users || []).filter((u) => u.user_id !== patch.user?.id);
  if (patch.user && patch.user.bytes != null) users.push({ user_id: patch.user.id, max_bytes: patch.user.bytes });
  const libraries = (cur.libraries || []).filter((l) => l.library_id !== patch.library?.id);
  if (patch.library && patch.library.bytes != null) libraries.push({ library_id: patch.library.id, max_bytes: patch.library.bytes });
  return {
    default_user_bytes: patch.defaultUserBytes ?? cur.default_user_bytes ?? 0,
    default_library_bytes: patch.defaultLibraryBytes ?? cur.default_library_bytes ?? 0,
    users,
    libraries
  };
}

/**
 * The quotas endpoint replaces everything at once, so read the latest values
 * and change only what this page edits.
 */
export async function saveQuotas(patch: QuotaPatch) {
  const cur = await fetchQuotas();
  await api.put("/api/v1/admin/quotas", mergeQuotas(cur, patch));
}

export function overrideFor(list: { max_bytes: number }[] | undefined, match: (row: any) => boolean): number | null {
  const row = (list || []).find(match);
  return row ? row.max_bytes : null;
}
