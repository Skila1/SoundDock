/**
 * Puts Home sections in the user's saved order. Sections missing from the saved
 * order (new ones, or none saved yet) keep their default place after it.
 */
export function orderSections<T extends { id: string }>(sections: T[], saved?: string[]): T[] {
  if (!saved?.length) return sections;
  const rank = new Map(saved.map((id, i) => [id, i]));
  return sections
    .map((s, i) => ({ s, i, r: rank.get(s.id) }))
    .sort((a, b) => {
      if (a.r !== undefined && b.r !== undefined) return a.r - b.r;
      if (a.r !== undefined) return -1;
      if (b.r !== undefined) return 1;
      return a.i - b.i;
    })
    .map((x) => x.s);
}

/**
 * Moves `id` so it sits just before `beforeId` (or last when `beforeId` is null)
 * and returns the new full order.
 */
export function moveSection(order: string[], id: string, beforeId: string | null): string[] {
  if (id === beforeId) return order;
  const rest = order.filter((x) => x !== id);
  const at = beforeId === null ? rest.length : rest.indexOf(beforeId);
  if (at < 0) return order;
  rest.splice(at, 0, id);
  return rest;
}
