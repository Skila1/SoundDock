import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { ChevronRight, type LucideIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import type { PillTone } from "./overviewModel";

const dot: Record<PillTone, string> = {
  ok: "bg-success",
  warn: "bg-warning",
  fail: "bg-destructive"
};

const ring: Record<PillTone, string> = {
  ok: "border-border hover:border-success/40",
  warn: "border-warning/40 bg-warning/5 hover:border-warning/70",
  fail: "border-destructive/40 bg-destructive/5 hover:border-destructive/70"
};

/** One health item: a coloured dot and a label, linking to where it is fixed. */
export function StatusPill({ tone, label, to, title }: { tone: PillTone; label: string; to: string; title?: string }) {
  return (
    <Link
      to={to}
      title={title}
      className={cn("inline-flex min-w-0 items-center gap-2 rounded-full border bg-surface-1 px-3 py-1.5 text-sm transition-colors", ring[tone])}
    >
      <span className={cn("h-2 w-2 shrink-0 rounded-full", dot[tone])} aria-hidden />
      <span className="truncate">{label}</span>
      <span className="sr-only">{tone === "ok" ? "(healthy)" : tone === "warn" ? "(needs a look)" : "(failing)"}</span>
    </Link>
  );
}

/** A clickable number tile. Max four per page. */
export function Tile({ label, value, hint, icon: Icon, to, tone }: { label: string; value: ReactNode; hint?: ReactNode; icon: LucideIcon; to: string; tone?: PillTone }) {
  return (
    <Link
      to={to}
      className={cn(
        "group block min-w-0 rounded-2xl border border-border bg-surface-1 p-4 shadow-sm transition-colors hover:bg-surface-2/50 sm:p-5",
        tone === "warn" && "border-warning/40",
        tone === "fail" && "border-destructive/40"
      )}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="truncate text-xs font-semibold uppercase tracking-wider text-subtle">{label}</span>
        <span
          className={cn(
            "flex h-8 w-8 shrink-0 items-center justify-center rounded-xl bg-accent/10 text-accent",
            tone === "warn" && "bg-warning/10 text-warning",
            tone === "fail" && "bg-destructive/10 text-destructive"
          )}
        >
          <Icon className="h-4 w-4" />
        </span>
      </div>
      <div className="tabular mt-2 truncate text-2xl font-bold tracking-tight sm:text-3xl">{value}</div>
      {hint && (
        <div className="mt-1 flex items-center gap-1 text-xs text-subtle">
          <span className="truncate">{hint}</span>
          <ChevronRight className="h-3 w-3 shrink-0 opacity-0 transition-opacity group-hover:opacity-100" />
        </div>
      )}
    </Link>
  );
}
