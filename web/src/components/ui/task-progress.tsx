import { CheckCircle2, Loader2, XCircle } from "lucide-react";
import { cn } from "@/lib/utils";

export type TaskState = {
  label: string;
  done: number;
  total: number;
  failed?: number;
  status: "running" | "done" | "error";
  detail?: string;
};

/** Inline progress for long operations (imports, matching, downloads). */
export function TaskProgress({ task, className, onDismiss }: { task: TaskState | null; className?: string; onDismiss?: () => void }) {
  if (!task) return null;
  const pct = task.total > 0 ? Math.min(100, Math.round((task.done / task.total) * 100)) : task.status === "done" ? 100 : 0;
  const Icon = task.status === "running" ? Loader2 : task.status === "done" ? CheckCircle2 : XCircle;
  return (
    <div className={cn("rounded-xl border border-border bg-surface-1 p-3", className)} role="status" aria-live="polite">
      <div className="flex items-center gap-2 text-sm">
        <Icon className={cn("h-4 w-4 shrink-0", task.status === "running" && "animate-spin text-accent", task.status === "done" && "text-accent", task.status === "error" && "text-destructive")} />
        <span className="min-w-0 flex-1 truncate font-medium">{task.label}</span>
        <span className="shrink-0 tabular-nums text-xs text-muted">
          {task.total > 0 ? `${task.done.toLocaleString()} / ${task.total.toLocaleString()}` : task.status === "running" ? "Starting…" : ""}
        </span>
        {onDismiss && task.status !== "running" && (
          <button type="button" className="text-xs text-subtle hover:text-foreground" onClick={onDismiss}>
            Dismiss
          </button>
        )}
      </div>
      <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-surface-3">
        <div
          className={cn("h-full rounded-full transition-[width] duration-300", task.status === "error" ? "bg-destructive" : "bg-accent", task.total === 0 && task.status === "running" && "w-1/3 animate-pulse")}
          style={task.total > 0 || task.status !== "running" ? { width: `${pct}%` } : undefined}
        />
      </div>
      {(task.detail || !!task.failed) && (
        <p className="mt-1.5 text-xs text-subtle">
          {task.detail}
          {task.failed ? `${task.detail ? " · " : ""}${task.failed} could not be found` : ""}
        </p>
      )}
    </div>
  );
}
