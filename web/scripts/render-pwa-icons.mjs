import { createWriteStream, mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { deflateSync } from "node:zlib";

const root = join(dirname(fileURLToPath(import.meta.url)), "..", "public", "icons");
mkdirSync(root, { recursive: true });

const GREEN = [0x1d, 0xb9, 0x54, 0xff];
const INK = [0x0b, 0x0f, 0x14, 0xff];
const CLEAR = [0, 0, 0, 0];

function crc32(buf) {
  let c = ~0;
  for (const b of buf) {
    c ^= b;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
  }
  return ~c >>> 0;
}

function chunk(type, data) {
  const out = Buffer.alloc(8 + data.length + 4);
  out.writeUInt32BE(data.length, 0);
  out.write(type, 4);
  data.copy(out, 8);
  const crcBuf = Buffer.concat([Buffer.from(type), data]);
  out.writeUInt32BE(crc32(crcBuf), 8 + data.length);
  return out;
}

function writePng(path, size, paint) {
  const raw = Buffer.alloc((size * 4 + 1) * size);
  for (let y = 0; y < size; y++) {
    const row = y * (size * 4 + 1);
    raw[row] = 0;
    for (let x = 0; x < size; x++) {
      const px = paint(x, y, size);
      const o = row + 1 + x * 4;
      raw[o] = px[0];
      raw[o + 1] = px[1];
      raw[o + 2] = px[2];
      raw[o + 3] = px[3];
    }
  }
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(size, 0);
  ihdr.writeUInt32BE(size, 4);
  ihdr[8] = 8;
  ihdr[9] = 6;
  const png = Buffer.concat([
    Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
    chunk("IHDR", ihdr),
    chunk("IDAT", deflateSync(raw)),
    chunk("IEND", Buffer.alloc(0))
  ]);
  const ws = createWriteStream(path);
  ws.end(png);
}

function distRoundRect(x, y, size, pad, radius) {
  const min = pad;
  const max = size - 1 - pad;
  const cx = x < min + radius ? min + radius : x > max - radius ? max - radius : x;
  const cy = y < min + radius ? min + radius : y > max - radius ? max - radius : y;
  if (x >= min + radius && x <= max - radius && y >= min && y <= max) return -1;
  if (y >= min + radius && y <= max - radius && x >= min && x <= max) return -1;
  const dx = x - cx;
  const dy = y - cy;
  return Math.hypot(dx, dy) - radius;
}

function waveDist(x, y, size, pad, lift) {
  const inner = size - pad * 2;
  const t = (x - pad) / inner;
  const cx = 0.5 + (t - 0.5) * 1.15;
  const amp = inner * 0.11;
  const yy = pad + inner * lift + Math.sin(cx * Math.PI) * -amp;
  return Math.abs(y - yy);
}

function paintIcon(x, y, size, { pad, stroke }) {
  const radius = Math.round(size * 0.18);
  const d = distRoundRect(x, y, size, pad, radius);
  if (d > 0.6) return CLEAR;
  const onWave =
    waveDist(x, y, size, pad + size * 0.12, 0.62) < stroke ||
    waveDist(x, y, size, pad + size * 0.12, 0.48) < stroke ||
    waveDist(x, y, size, pad + size * 0.12, 0.34) < stroke;
  if (d > -1 && d <= 0.6) return GREEN;
  return onWave ? INK : GREEN;
}

writePng(join(root, "icon-192.png"), 192, (x, y, s) => paintIcon(x, y, s, { pad: 0, stroke: 5 }));
writePng(join(root, "icon-512.png"), 512, (x, y, s) => paintIcon(x, y, s, { pad: 0, stroke: 13 }));
writePng(join(root, "icon-512-maskable.png"), 512, (x, y, s) => paintIcon(x, y, s, { pad: 64, stroke: 10 }));
writePng(join(root, "apple-touch-icon.png"), 180, (x, y, s) => paintIcon(x, y, s, { pad: 0, stroke: 5 }));
console.log("wrote PWA icons to", root);
