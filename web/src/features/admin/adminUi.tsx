import { useState, type ReactNode } from "react";
import { useSearchParams } from "react-router-dom";
import { MoreHorizontal, type LucideIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn } from "@/lib/utils";
import { bytesFromParts, splitBytes, type ByteUnit } from "./adminFormat";

/** Tab state kept in ?tab= so links and reloads land on the same tab. */
export function useTabParam<T extends string>(tabs: readonly T[], fallback: T): [T, (t: T) => void] {
  const [params, setParams] = useSearchParams();
  const raw = params.get("tab") as T | null;
  const current = raw && tabs.includes(raw) ? raw : fallback;
  const set = (t: T) => {
    const next = new URLSearchParams(params);
    if (t === fallback) next.delete("tab");
    else next.set("tab", t);
    setParams(next, { replace: true });
  };
  return [current, set];
}

export type TabDef<T extends string> = { id: T; label: string; content: ReactNode; badge?: ReactNode };

/** Page-level tabs synced to the URL. Content mounts only when its tab is active. */
export function AdminTabs<T extends string>({ tabs, fallback }: { tabs: readonly TabDef<T>[]; fallback: T }) {
  const ids = tabs.map((t) => t.id);
  const [tab, setTab] = useTabParam(ids, fallback);
  return (
    <Tabs value={tab} onValueChange={(v) => setTab(v as T)}>
      <TabsList className="mb-5 w-full overflow-x-auto sm:w-fit">
        {tabs.map((t) => (
          <TabsTrigger key={t.id} value={t.id} className="flex shrink-0 items-center gap-2">
            {t.label}
            {t.badge}
          </TabsTrigger>
        ))}
      </TabsList>
      {tabs.map((t) => (
        <TabsContent key={t.id} value={t.id} className="outline-none">
          {tab === t.id ? t.content : null}
        </TabsContent>
      ))}
    </Tabs>
  );
}

export function Card({ title, description, actions, children, className }: { title?: ReactNode; description?: ReactNode; actions?: ReactNode; children?: ReactNode; className?: string }) {
  return (
    <section className={cn("rounded-2xl border border-border bg-surface-1 p-5 shadow-sm", className)}>
      {(title || actions) && (
        <div className="mb-3 flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            {title && <h2 className="text-base font-semibold tracking-tight">{title}</h2>}
            {description && <p className="mt-0.5 text-sm text-muted">{description}</p>}
          </div>
          {actions && <div className="flex flex-wrap gap-2">{actions}</div>}
        </div>
      )}
      {children}
    </section>
  );
}

export function StatCard({ label, value, hint, icon: Icon, tone }: { label: string; value: ReactNode; hint?: ReactNode; icon?: LucideIcon; tone?: "warning" | "danger" }) {
  return (
    <div className={cn("rounded-2xl border border-border bg-surface-1 p-5 shadow-sm", tone === "warning" && "border-warning/40", tone === "danger" && "border-destructive/40")}>
      <div className="flex items-center justify-between gap-3 text-muted">
        <span className="text-xs font-semibold uppercase tracking-wider text-subtle">{label}</span>
        {Icon && (
          <span className={cn("flex h-8 w-8 items-center justify-center rounded-xl bg-accent/10 text-accent", tone === "warning" && "bg-warning/10 text-warning", tone === "danger" && "bg-destructive/10 text-destructive")}>
            <Icon className="h-4 w-4" />
          </span>
        )}
      </div>
      <div className="tabular mt-2 truncate text-3xl font-bold tracking-tight">{value}</div>
      {hint && <div className="mt-1 truncate text-xs text-subtle">{hint}</div>}
    </div>
  );
}

/** A labelled switch that saves as soon as it changes. */
export function SettingSwitch({ label, description, checked, onChange, disabled }: { label: string; description?: ReactNode; checked: boolean; onChange: (v: boolean) => void; disabled?: boolean }) {
  return (
    <div className={cn("flex items-center justify-between gap-4 py-2", disabled && "opacity-60")}>
      <div className="min-w-0">
        <div className="text-sm font-medium">{label}</div>
        {description && <p className="text-xs text-subtle">{description}</p>}
      </div>
      <Switch checked={checked} onCheckedChange={(v) => { if (!disabled) onChange(v); }} />
    </div>
  );
}

export type RowAction = { label: string; icon?: LucideIcon; onSelect: () => void; destructive?: boolean; disabled?: boolean; separatorBefore?: boolean };

/** Secondary row actions behind a "⋯" button. */
export function RowMenu({ actions, label = "More actions" }: { actions: RowAction[]; label?: string }) {
  const visible = actions.filter(Boolean);
  if (!visible.length) return null;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button size="icon" variant="ghost" className="h-8 w-8" aria-label={label}>
          <MoreHorizontal className="h-4 w-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {visible.map((a) => (
          <div key={a.label}>
            {a.separatorBefore && <DropdownMenuSeparator />}
            <DropdownMenuItem
              disabled={a.disabled}
              className={cn(a.destructive && "text-destructive", a.disabled && "pointer-events-none opacity-50")}
              onSelect={() => a.onSelect()}
            >
              {a.icon && <a.icon className="h-4 w-4" />}
              {a.label}
            </DropdownMenuItem>
          </div>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

const unitOptions = [
  { value: "MB", label: "MB" },
  { value: "GB", label: "GB" },
  { value: "TB", label: "TB" }
];

/** Size input in MB/GB/TB backed by a byte count. Empty means "no value". */
export function ByteInput({ value, onChange, placeholder, allowEmpty }: { value: number | null; onChange: (bytes: number | null) => void; placeholder?: string; allowEmpty?: boolean }) {
  const parts = splitBytes(value);
  const [unit, setUnit] = useState<ByteUnit>(parts.unit);
  const shown = value == null ? "" : String(Math.round((value / unitBytes(unit)) * 100) / 100);
  return (
    <div className="flex gap-2">
      <Input
        className="w-28"
        type="number"
        min={0}
        step="any"
        inputMode="decimal"
        placeholder={placeholder}
        value={shown}
        onChange={(e) => {
          const raw = e.target.value;
          if (raw === "") {
            onChange(allowEmpty ? null : 0);
            return;
          }
          onChange(bytesFromParts(Number(raw), unit));
        }}
      />
      <Select className="w-20" value={unit} onValueChange={(u) => setUnit(u as ByteUnit)} options={unitOptions} />
    </div>
  );
}

function unitBytes(u: ByteUnit) {
  return bytesFromParts(1, u);
}

/** Bottom bar shown while a form has unsaved changes. */
export function SaveBar({ dirty, saving, onSave, onReset, label = "Save changes" }: { dirty: boolean; saving?: boolean; onSave: () => void; onReset?: () => void; label?: string }) {
  if (!dirty) return null;
  return (
    <div className="sticky bottom-3 z-10 mt-6 flex items-center justify-between gap-3 rounded-xl border border-border bg-surface-2/95 px-4 py-3 shadow-card backdrop-blur">
      <span className="text-sm text-muted">You have unsaved changes.</span>
      <div className="flex gap-2">
        {onReset && <Button variant="ghost" size="sm" disabled={saving} onClick={onReset}>Discard</Button>}
        <Button size="sm" disabled={saving} onClick={onSave}>{saving ? "Saving…" : label}</Button>
      </div>
    </div>
  );
}

export function errorMessage(e: unknown, fallback: string) {
  return e instanceof Error && e.message ? e.message : fallback;
}
