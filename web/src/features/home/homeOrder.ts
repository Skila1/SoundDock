/**
 * Puts Home sections in the user's saved order. A section missing from the saved
 * order (new, or nothing saved yet) goes right after the section that precedes it
 * by default (or last, when it is last by default), so a new section lands
 * where it was designed to sit.
 */
export function orderSections<T extends { id: string }>(sections: T[], saved?: string[]): T[] {
  if (!saved?.length) return sections;
  const byId = new Map(sections.map((s) => [s.id, s]));
  const out = saved.filter((id, i) => byId.has(id) && saved.indexOf(id) === i);
  sections.forEach((s, i) => {
    if (out.includes(s.id)) return;
    if (i === sections.length - 1) {
      out.push(s.id);
      return;
    }
    let at = 0;
    for (let j = i - 1; j >= 0; j--) {
      const k = out.indexOf(sections[j].id);
      if (k >= 0) {
        at = k + 1;
        break;
      }
    }
    out.splice(at, 0, s.id);
  });
  return out.map((id) => byId.get(id)!);
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
