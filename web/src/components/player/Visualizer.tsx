import { useEffect, useRef } from "react";
import { getAnalyser } from "@/stores/player";

export function Visualizer({ active }: { active: boolean }) {
  const ref = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    if (!active) return;
    const canvas = ref.current;
    if (!canvas) return;
    const analyser = getAnalyser();
    if (!analyser) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    const data = new Uint8Array(analyser.frequencyBinCount);
    let raf = 0;
    const draw = () => {
      analyser.getByteFrequencyData(data);
      const { width, height } = canvas;
      ctx.clearRect(0, 0, width, height);
      const n = 32;
      const gap = 2;
      const bw = (width - gap * (n - 1)) / n;
      for (let i = 0; i < n; i++) {
        const v = data[Math.floor((i / n) * data.length)] / 255;
        const h = Math.max(2, v * height);
        ctx.fillStyle = `hsla(142, 64%, ${40 + v * 30}%, 0.9)`;
        ctx.fillRect(i * (bw + gap), height - h, bw, h);
      }
      raf = requestAnimationFrame(draw);
    };
    raf = requestAnimationFrame(draw);
    return () => cancelAnimationFrame(raf);
  }, [active]);

  if (!active) return null;
  return <canvas ref={ref} width={560} height={72} className="mt-4 h-[72px] w-full rounded-md bg-surface-2" aria-hidden />;
}
