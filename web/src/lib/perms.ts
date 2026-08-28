export type PermUser = {
  is_admin?: boolean;
  permissions?: string[];
};

export function hasPerm(user: PermUser | null | undefined, perm: string): boolean {
  if (!user || !perm) return false;
  if (user.is_admin) return true;
  return (user.permissions || []).includes(perm);
}
