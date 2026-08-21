import { useMemo, useState } from "react";
import { cn } from "@/lib/utils";

const EXP = new Uint8Array(512);
const LOG = new Uint8Array(256);
(() => {
  let x = 1;
  for (let i = 0; i < 255; i++) {
    EXP[i] = x;
    LOG[x] = i;
    x <<= 1;
    if (x & 0x100) x ^= 0x11d;
  }
  for (let i = 255; i < 512; i++) EXP[i] = EXP[i - 255];
})();

const gfMul = (a: number, b: number) => (a && b ? EXP[LOG[a] + LOG[b]] : 0);

function rsGenerator(ec: number) {
  let poly = [1];
  for (let i = 0; i < ec; i++) {
    const next = new Array(poly.length + 1).fill(0);
    for (let j = 0; j < poly.length; j++) {
      next[j] ^= poly[j];
      next[j + 1] ^= gfMul(poly[j], EXP[i]);
    }
    poly = next;
  }
  return poly;
}

function rsEncode(data: number[], ec: number) {
  const gen = rsGenerator(ec);
  const res = new Array(data.length + ec).fill(0);
  for (let i = 0; i < data.length; i++) res[i] = data[i];
  for (let i = 0; i < data.length; i++) {
    const coef = res[i];
    if (!coef) continue;
    for (let j = 0; j < gen.length; j++) res[i + j] ^= gfMul(gen[j], coef);
  }
  return res.slice(data.length);
}

/** ECC-M block layout: [g1 count, g1 data, g2 count, g2 data, ec per block] */
const BLOCKS_M: Array<[number, number, number, number, number] | null> = [
  null,
  [1, 16, 0, 0, 10],
  [1, 28, 0, 0, 16],
  [1, 44, 0, 0, 26],
  [2, 32, 0, 0, 18],
  [2, 43, 0, 0, 24],
  [4, 27, 0, 0, 16],
  [4, 31, 0, 0, 18],
  [2, 38, 2, 39, 22],
  [3, 36, 2, 37, 22],
  [4, 43, 1, 44, 26]
];

const ALIGN: number[][] = [
  [],
  [],
  [6, 18],
  [6, 22],
  [6, 26],
  [6, 30],
  [6, 34],
  [6, 22, 38],
  [6, 24, 42],
  [6, 26, 46],
  [6, 28, 50]
];

const VERSION_BITS = [0, 0, 0, 0, 0, 0, 0, 0x07c94, 0x085bc, 0x09a99, 0x0a4d3];
const FORMAT_M = [0x5412, 0x5125, 0x5e7c, 0x5b4b, 0x45f9, 0x40ce, 0x4f97, 0x4aa0];

function sizeOf(v: number) {
  return 21 + 4 * (v - 1);
}

function inFinder(r: number, c: number, size: number) {
  return (r < 9 && c < 9) || (r < 9 && c >= size - 8) || (r >= size - 8 && c < 9);
}

function inAlign(r: number, c: number, v: number) {
  const pos = ALIGN[v];
  for (const y of pos) {
    for (const x of pos) {
      if (inFinder(y, x, sizeOf(v))) continue;
      if (Math.abs(r - y) <= 2 && Math.abs(c - x) <= 2) return true;
    }
  }
  return false;
}

function reserved(r: number, c: number, v: number) {
  const size = sizeOf(v);
  if (inFinder(r, c, size)) return true;
  if (r === 6 || c === 6) return true;
  if (inAlign(r, c, v)) return true;
  if (r === 8 && (c < 9 || c >= size - 8)) return true;
  if (c === 8 && (r < 9 || r >= size - 8)) return true;
  if (v >= 7) {
    if (r < 6 && c >= size - 11) return true;
    if (c < 6 && r >= size - 11) return true;
  }
  return false;
}

function placeFinders(mod: number[][], size: number) {
  const stamp = (sr: number, sc: number) => {
    for (let r = -1; r <= 7; r++) {
      for (let c = -1; c <= 7; c++) {
        const rr = sr + r;
        const cc = sc + c;
        if (rr < 0 || cc < 0 || rr >= size || cc >= size) continue;
        const ring = r === -1 || c === -1 || r === 7 || c === 7;
        const core = r >= 2 && r <= 4 && c >= 2 && c <= 4;
        const border = r === 0 || c === 0 || r === 6 || c === 6;
        mod[rr][cc] = ring ? 0 : core || border ? 1 : 0;
      }
    }
  };
  stamp(0, 0);
  stamp(0, size - 7);
  stamp(size - 7, 0);
}

function placeAlign(mod: number[][], v: number) {
  const pos = ALIGN[v];
  for (const y of pos) {
    for (const x of pos) {
      if (inFinder(y, x, sizeOf(v))) continue;
      for (let r = -2; r <= 2; r++) {
        for (let c = -2; c <= 2; c++) {
          const a = Math.max(Math.abs(r), Math.abs(c));
          mod[y + r][x + c] = a === 1 ? 0 : 1;
        }
      }
    }
  }
}

function placeTiming(mod: number[][], size: number) {
  for (let i = 8; i < size - 8; i++) {
    mod[6][i] = i % 2 === 0 ? 1 : 0;
    mod[i][6] = i % 2 === 0 ? 1 : 0;
  }
}

function placeFormat(mod: number[][], size: number, bits: number) {
  const bit = (i: number) => (bits >> (14 - i)) & 1;
  for (let i = 0; i < 6; i++) {
    mod[8][i] = bit(i);
    mod[i][8] = bit(14 - i);
  }
  mod[8][7] = bit(6);
  mod[8][8] = bit(7);
  mod[7][8] = bit(8);
  for (let i = 0; i < 8; i++) mod[8][size - 1 - i] = bit(i);
  for (let i = 0; i < 7; i++) mod[size - 1 - i][8] = bit(14 - i);
  mod[size - 8][8] = 1;
}

function placeVersion(mod: number[][], v: number) {
  if (v < 7) return;
  const bits = VERSION_BITS[v];
  const size = sizeOf(v);
  for (let i = 0; i < 18; i++) {
    const bit = (bits >> i) & 1;
    const r = Math.floor(i / 3);
    const c = i % 3;
    mod[r][size - 11 + c] = bit;
    mod[size - 11 + c][r] = bit;
  }
}

function maskAt(mask: number, r: number, c: number) {
  switch (mask) {
    case 0:
      return (r + c) % 2 === 0;
    case 1:
      return r % 2 === 0;
    case 2:
      return c % 3 === 0;
    case 3:
      return (r + c) % 3 === 0;
    case 4:
      return (Math.floor(r / 2) + Math.floor(c / 3)) % 2 === 0;
    case 5:
      return ((r * c) % 2) + ((r * c) % 3) === 0;
    case 6:
      return (((r * c) % 2) + ((r * c) % 3)) % 2 === 0;
    default:
      return (((r + c) % 2) + ((r * c) % 3)) % 2 === 0;
  }
}

function penalty(mod: number[][]) {
  const n = mod.length;
  let s = 0;
  for (let r = 0; r < n; r++) {
    let run = 1;
    for (let c = 1; c <= n; c++) {
      if (c < n && mod[r][c] === mod[r][c - 1]) run++;
      else {
        if (run >= 5) s += 3 + (run - 5);
        run = 1;
      }
    }
  }
  for (let c = 0; c < n; c++) {
    let run = 1;
    for (let r = 1; r <= n; r++) {
      if (r < n && mod[r][c] === mod[r - 1][c]) run++;
      else {
        if (run >= 5) s += 3 + (run - 5);
        run = 1;
      }
    }
  }
  for (let r = 0; r < n - 1; r++) {
    for (let c = 0; c < n - 1; c++) {
      const v = mod[r][c];
      if (v === mod[r][c + 1] && v === mod[r + 1][c] && v === mod[r + 1][c + 1]) s += 3;
    }
  }
  let dark = 0;
  for (let r = 0; r < n; r++) for (let c = 0; c < n; c++) dark += mod[r][c];
  s += Math.floor(Math.abs(dark * 20 - n * n * 10) / (n * n)) * 10;
  return s;
}

function bitstream(bytes: number[], v: number) {
  const blocks = BLOCKS_M[v]!;
  const dataCw = blocks[0] * blocks[1] + blocks[2] * blocks[3];
  const countBits = v >= 10 ? 16 : 8;
  const bits: number[] = [];
  const push = (val: number, n: number) => {
    for (let i = n - 1; i >= 0; i--) bits.push((val >> i) & 1);
  };
  push(0b0100, 4);
  push(bytes.length, countBits);
  for (const b of bytes) push(b, 8);
  const capacity = dataCw * 8;
  const term = Math.min(4, capacity - bits.length);
  push(0, term);
  while (bits.length % 8) bits.push(0);
  const pads = [0xec, 0x11];
  let p = 0;
  while (bits.length < capacity) push(pads[p++ % 2], 8);
  bits.length = capacity;
  const cw: number[] = [];
  for (let i = 0; i < bits.length; i += 8) {
    let b = 0;
    for (let j = 0; j < 8; j++) b = (b << 1) | bits[i + j];
    cw.push(b);
  }
  return cw;
}

function interleave(v: number, data: number[]) {
  const [n1, d1, n2, d2, ec] = BLOCKS_M[v]!;
  const blocks: { data: number[]; ecc: number[] }[] = [];
  let off = 0;
  for (let i = 0; i < n1; i++) {
    const slice = data.slice(off, off + d1);
    off += d1;
    blocks.push({ data: slice, ecc: rsEncode(slice, ec) });
  }
  for (let i = 0; i < n2; i++) {
    const slice = data.slice(off, off + d2);
    off += d2;
    blocks.push({ data: slice, ecc: rsEncode(slice, ec) });
  }
  const out: number[] = [];
  const maxD = Math.max(d1, d2);
  for (let i = 0; i < maxD; i++) for (const b of blocks) if (i < b.data.length) out.push(b.data[i]);
  for (let i = 0; i < ec; i++) for (const b of blocks) out.push(b.ecc[i]);
  return out;
}

function pickVersion(len: number) {
  for (let v = 1; v <= 10; v++) {
    const [n1, d1, n2, d2] = BLOCKS_M[v]!;
    const cap = n1 * d1 + n2 * d2;
    const countBits = v >= 10 ? 16 : 8;
    const need = Math.ceil((4 + countBits + len * 8 + 4) / 8);
    if (need <= cap) return v;
  }
  return 0;
}

function fillData(base: number[][], v: number, bits: number[]) {
  const size = sizeOf(v);
  let bi = 0;
  let up = true;
  for (let col = size - 1; col > 0; col -= 2) {
    if (col === 6) col--;
    for (let i = 0; i < size; i++) {
      const r = up ? size - 1 - i : i;
      for (let dc = 0; dc < 2; dc++) {
        const c = col - dc;
        if (reserved(r, c, v)) continue;
        const bit = bi < bits.length ? bits[bi++] : 0;
        base[r][c] = bit;
      }
    }
    up = !up;
  }
}

function toBits(cw: number[]) {
  const bits: number[] = [];
  for (const b of cw) for (let i = 7; i >= 0; i--) bits.push((b >> i) & 1);
  return bits;
}

export function qrMatrix(value: string): boolean[][] {
  const bytes = Array.from(new TextEncoder().encode(value || " "));
  const v = pickVersion(bytes.length);
  if (!v) throw new Error("QR payload too long");
  const size = sizeOf(v);
  const template = Array.from({ length: size }, () => new Array<number>(size).fill(0));
  placeFinders(template, size);
  placeAlign(template, v);
  placeTiming(template, size);
  placeVersion(template, v);
  const data = interleave(v, bitstream(bytes, v));
  const bits = toBits(data);
  fillData(template, v, bits);

  let best: number[][] | null = null;
  let bestScore = Infinity;
  let bestMask = 0;
  for (let mask = 0; mask < 8; mask++) {
    const m = template.map((row) => row.slice());
    for (let r = 0; r < size; r++) {
      for (let c = 0; c < size; c++) {
        if (!reserved(r, c, v) && maskAt(mask, r, c)) m[r][c] ^= 1;
      }
    }
    placeFormat(m, size, FORMAT_M[mask]);
    const score = penalty(m);
    if (score < bestScore) {
      bestScore = score;
      best = m;
      bestMask = mask;
    }
  }
  void bestMask;
  return (best || template).map((row) => row.map((cell) => cell === 1));
}

export function QrCode({
  value,
  size = 192,
  className,
  label
}: {
  value: string;
  size?: number;
  className?: string;
  label?: string;
}) {
  const matrix = useMemo(() => {
    try {
      return qrMatrix(value);
    } catch {
      return null;
    }
  }, [value]);
  if (!matrix) {
    return <p className="text-sm text-muted">QR payload is too long to encode.</p>;
  }
  const n = matrix.length;
  const dim = n + 8;
  const paths: string[] = [];
  for (let r = 0; r < n; r++) {
    for (let c = 0; c < n; c++) {
      if (matrix[r][c]) paths.push(`M${c + 4} ${r + 4}h1v1h-1z`);
    }
  }
  return (
    <svg
      className={cn("rounded-md bg-white", className)}
      width={size}
      height={size}
      viewBox={`0 0 ${dim} ${dim}`}
      role="img"
      aria-label={label || "QR code"}
    >
      <rect width={dim} height={dim} fill="#fff" />
      <path d={paths.join("")} fill="#111" />
    </svg>
  );
}

/** Presentational share/pair card. Integrator mounts this; do not import from AppShell/Sidebar. */
export function QrShare({
  value,
  title = "Scan to open",
  caption
}: {
  value?: string;
  title?: string;
  caption?: string;
}) {
  const href = value || (typeof window !== "undefined" ? window.location.href : "");
  const [copied, setCopied] = useState(false);
  return (
    <div className="flex flex-col items-center gap-3 rounded-xl border border-border bg-surface-1 p-4">
      <h2 className="text-sm font-semibold">{title}</h2>
      <QrCode value={href} size={192} label={title} />
      <p className="max-w-[16rem] break-all text-center text-xs text-muted">{caption || href}</p>
      <button
        type="button"
        className="text-xs font-medium text-accent hover:underline"
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(href);
            setCopied(true);
            setTimeout(() => setCopied(false), 1600);
          } catch {
            /* ignore */
          }
        }}
      >
        {copied ? "Copied" : "Copy link"}
      </button>
    </div>
  );
}
