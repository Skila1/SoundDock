import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Badge } from "@/components/ui/misc";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { relativeTime } from "@/lib/utils";
import { formatTimestamp, requestLogsHref } from "./logFilters";

type ErrorRow = {
  id?: string;
  source?: string;
  class?: string;
  type?: string;
  category?: string;
  message?: string;
  last_error?: string;
  error?: string;
  at?: string;
  created_at?: string;
  status?: string;
  request_id?: string;
  job_id?: string;
};

const sources = [
  { value: "all", label: "All sources" },
  { value: "oplog", label: "Server errors" },
  { value: "job", label: "Jobs" },
  { value: "job_attempt", label: "Job attempts" },
  { value: "discord_playback", label: "Discord playback" },
  { value: "discord_gateway", label: "Discord bot" },
  { value: "acquisition", label: "Downloads" },
  { value: "scan_file", label: "Library scans" },
  { value: "webhook", label: "Webhooks" },
  { value: "external_account", label: "Provider accounts" },
  { value: "external_playlist", label: "Playlist imports" },
  { value: "external_sync", label: "Playlist sync" }
];

const sourceLabel = Object.fromEntries(sources.map((s) => [s.value, s.label]));

export function ErrorsTab() {
  const [source, setSource] = useState("all");
  const [text, setText] = useState("");
  const [search, setSearch] = useState("");
  useEffect(() => {
    const t = window.setTimeout(() => setSearch(text), 400);
    return () => window.clearTimeout(t);
  }, [text]);
  const q = useQuery({
    queryKey: ["admin-errors", source, search],
    queryFn: () => {
      const p = new URLSearchParams({ limit: "100" });
      if (source !== "all") p.set("source", source);
      if (search.trim()) p.set("q", search.trim());
      return api.get<{ items?: ErrorRow[] }>(`/api/v1/admin/errors?${p.toString()}`);
    },
    refetchInterval: 15000
  });
  const items = q.data?.items || [];
  return (
    <div>
      <p className="mb-3 text-sm text-muted">Server exceptions and failures from jobs, Discord, downloads, scans, webhooks, and playlist providers.</p>
      <div className="mb-3 flex flex-wrap gap-2">
        <Input className="min-w-[14rem] flex-1" placeholder="Search errors" value={text} onChange={(e) => setText(e.target.value)} aria-label="Search errors" />
        <Select className="w-52" value={source} onValueChange={setSource} options={sources} />
      </div>
      <ul className="divide-y divide-border rounded-xl border border-border">
        {items.map((err, i) => {
          const at = err.at || err.created_at;
          return (
            <li key={err.id || `${err.source}-${at}-${i}`} className="px-4 py-3 text-sm">
              <div className="flex flex-wrap items-center gap-2">
                <Badge tone="danger">{sourceLabel[err.source || ""] || err.source || err.type || "error"}</Badge>
                {err.class && <span className="text-xs text-muted">{err.class}</span>}
                <span className="ml-auto text-xs text-subtle" title={formatTimestamp(at)}>{relativeTime(at)}</span>
              </div>
              <p className="mt-1 break-words text-destructive">{err.message || err.last_error || err.error}</p>
              {err.request_id && (
                <Link className="mt-1 inline-block text-xs text-accent hover:underline" to={requestLogsHref(err.request_id)}>View request activity</Link>
              )}
            </li>
          );
        })}
        {!items.length && <li className="px-4 py-10 text-center text-sm text-muted">{q.isLoading ? "Loading…" : "No errors match."}</li>}
      </ul>
    </div>
  );
}
