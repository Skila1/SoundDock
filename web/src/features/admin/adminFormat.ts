export type ByteUnit = "MB" | "GB" | "TB";

const UNIT: Record<ByteUnit, number> = {
  MB: 1024 ** 2,
  GB: 1024 ** 3,
  TB: 1024 ** 4
};

export function bytesFromParts(n: number, unit: ByteUnit): number {
  if (!Number.isFinite(n) || n <= 0) return 0;
  return Math.round(n * UNIT[unit]);
}

/** Picks the largest unit that keeps the number at or above 1. */
export function splitBytes(bytes: number | null | undefined): { value: number; unit: ByteUnit } {
  if (!bytes || bytes <= 0) return { value: 0, unit: "GB" };
  for (const unit of ["TB", "GB", "MB"] as ByteUnit[]) {
    if (bytes >= UNIT[unit]) return { value: Math.round((bytes / UNIT[unit]) * 100) / 100, unit };
  }
  return { value: Math.round((bytes / UNIT.MB) * 100) / 100, unit: "MB" };
}

export function capLabel(bytes: number | null | undefined) {
  if (!bytes) return "Unlimited";
  const { value, unit } = splitBytes(bytes);
  return `${value} ${unit}`;
}
