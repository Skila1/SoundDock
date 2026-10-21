import { useState, type ReactNode } from "react";
import { ChevronDown } from "lucide-react";
import { cn } from "@/lib/utils";

/** A collapsed group for settings most people never touch. */
export function Expander({
  title,
  description,
  children,
  defaultOpen = false,
  open: controlled,
  onOpenChange,
  id,
  className
}: {
  title: string;
  description?: ReactNode;
  children: ReactNode;
  defaultOpen?: boolean;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  id?: string;
  className?: string;
}) {
  const [own, setOwn] = useState(defaultOpen);
  const open = controlled ?? own;
  const toggle = () => {
    setOwn(!open);
    onOpenChange?.(!open);
  };
  return (
    <section id={id} className={cn("scroll-mt-20 rounded-2xl border border-border bg-surface-1 shadow-sm", className)}>
      <button
        type="button"
        className="flex w-full items-center justify-between gap-3 px-5 py-4 text-left"
        aria-expanded={open}
        onClick={toggle}
      >
        <span className="min-w-0">
          <span className="block text-sm font-semibold">{title}</span>
          {description && <span className="block text-xs text-subtle">{description}</span>}
        </span>
        <ChevronDown className={cn("h-4 w-4 shrink-0 text-muted transition-transform", open && "rotate-180")} />
      </button>
      {open && <div className="border-t border-border px-5 py-4">{children}</div>}
    </section>
  );
}

/** Small number box that sits inside a sentence. */
export function InlineNumber({ value, onChange, min = 0, step, label, className, disabled }: { value: string; onChange: (v: string) => void; min?: number; step?: string; label: string; className?: string; disabled?: boolean }) {
  return (
    <input
      type="number"
      inputMode="decimal"
      min={min}
      step={step}
      aria-label={label}
      disabled={disabled}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className={cn(
        "mx-1 inline-block h-8 w-16 rounded-lg border border-border bg-surface-2 px-2 text-center text-sm font-semibold text-foreground outline-none transition focus:border-accent/60 focus:ring-2 focus:ring-accent/20 disabled:opacity-60",
        className
      )}
    />
  );
}

/** Used/free bar. Turns amber above 85% and red above 95%. */
export function UsageBar({ used, total, className }: { used: number; total: number; className?: string }) {
  const pct = total > 0 ? Math.min(100, (used / total) * 100) : 0;
  return (
    <div className={cn("h-2 w-full overflow-hidden rounded-full bg-surface-3", className)} role="meter" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(pct)}>
      <div
        className={cn("h-full rounded-full transition-all", pct >= 95 ? "bg-destructive" : pct >= 85 ? "bg-warning" : "bg-accent")}
        style={{ width: `${pct}%` }}
      />
    </div>
  );
}

/** One quiet line of facts separated by dots. */
export function StatusLine({ items }: { items: (ReactNode | false | null | undefined)[] }) {
  const shown = items.filter(Boolean);
  return (
    <p className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm text-muted">
      {shown.map((it, i) => (
        <span key={i} className="flex items-center gap-2">
          {i > 0 && <span aria-hidden className="text-subtle">·</span>}
          {it}
        </span>
      ))}
    </p>
  );
}

/** Relative time that also understands the future ("in 3h"). */
export function whenLabel(iso?: string | null) {
  if (!iso) return "-";
  const diff = new Date(iso).getTime() - Date.now();
  const past = diff < 0;
  const min = Math.round(Math.abs(diff) / 60000);
  let s: string;
  if (min < 1) return past ? "just now" : "any minute";
  if (min < 60) s = `${min}m`;
  else if (min < 60 * 24) s = `${Math.round(min / 60)}h`;
  else if (min < 60 * 24 * 14) s = `${Math.round(min / 60 / 24)}d`;
  else return new Date(iso).toLocaleDateString();
  return past ? `${s} ago` : `in ${s}`;
}

const GB = 1024 ** 3;

export function toGB(bytes?: number | null) {
  if (!bytes) return "0";
  const n = bytes / GB;
  return n >= 10 ? String(Math.round(n)) : String(Math.round(n * 100) / 100);
}

export function fromGB(s: string) {
  const n = Number(s);
  if (!n || n < 0) return 0;
  return Math.round(n * GB);
}
