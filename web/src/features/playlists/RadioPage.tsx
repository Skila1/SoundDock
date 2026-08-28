import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { Radio as RadioIcon } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { EmptyState, PageHeader, QueryError } from "@/components/ui/empty";
import { usePlayer } from "@/stores/player";
import { toast } from "sonner";
import type { SearchHit } from "@/types/api";
import type { RadioKind, RadioResponse, RadioSeeds } from "./types";

const kinds: { value: RadioKind; label: string }[] = [
  { value: "quick_mix", label: "Quick Mix" },
  { value: "library", label: "Library" },
  { value: "artist", label: "Artist" },
  { value: "album", label: "Album" },
  { value: "track", label: "Track" },
  { value: "genre", label: "Genre" },
  { value: "decade", label: "Decade" }
];

function radioUrl(kind: string, seed?: string, extra?: Record<string, string>) {
  const q = new URLSearchParams({ kind, limit: "20", ...extra });
  if (seed) q.set("seed_id", seed);
  return `/api/v1/radio?${q.toString()}`;
}

export function RadioPage() {
  const nav = useNavigate();
  const { kind: kindParam, seedId } = useParams();
  const [sp] = useSearchParams();
  const play = usePlayer((s) => s.playTracks);
  const add = usePlayer((s) => s.add);
  const initialKind = (kindParam || sp.get("kind") || "quick_mix") as RadioKind;
  const [kind, setKind] = useState<RadioKind>(kinds.some((k) => k.value === initialKind) ? initialKind : "quick_mix");
  const [seed, setSeed] = useState(seedId || sp.get("seed_id") || "");
  const [genre, setGenre] = useState(sp.get("genre") || "");
  const [decade, setDecade] = useState(sp.get("decade") || "");
  const [ids, setIds] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);

  const seeds = useQuery({ queryKey: ["radio-seeds"], queryFn: () => api.get<RadioSeeds>("/api/v1/radio/seeds") });
  const typeaheadKinds = kind === "artist" || kind === "album" || kind === "track";

  const seedOptions = useMemo(() => {
    if (kind === "library") return (seeds.data?.libraries || []).map((x) => ({ value: x.id, label: x.name }));
    if (kind === "genre") return (seeds.data?.genres || []).map((x) => ({ value: x.id || x.name, label: x.name }));
    if (kind === "decade") return (seeds.data?.decades || []).map((d) => ({ value: String(d), label: `${d}s` }));
    return [];
  }, [kind, seeds.data]);

  const load = async () => {
    setBusy(true);
    try {
      const extra: Record<string, string> = {};
      let seedId = seed;
      if (kind === "genre") {
        const g = seeds.data?.genres.find((x) => x.id === seed || x.name === seed || x.name === genre);
        if (g?.id) seedId = g.id;
        else extra.genre = genre || seed;
      }
      if (kind === "decade") extra.decade = decade || seed;
      if (kind === "quick_mix") seedId = "";
      const res = await api.get<RadioResponse>(radioUrl(kind, seedId, extra));
      setIds(res.track_ids || []);
      if (!res.track_ids?.length) toast.message("No tracks matched this radio seed.");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Radio failed");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div>
      <PageHeader
        title="Radio"
        description="Station picks from your library. Enqueue uses your queue - SoundDock never writes playback sessions from this page."
        actions={
          <Button variant="secondary" onClick={() => nav("/playlists")}>Playlists</Button>
        }
      />
      <div className="mb-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <Field label="Kind">
          <Select value={kind} onValueChange={(v) => { setKind(v as RadioKind); setSeed(""); setIds([]); }} options={kinds} />
        </Field>
        {kind !== "quick_mix" && kind !== "decade" && (
          <Field label="Seed">
            {typeaheadKinds ? (
              <SeedTypeahead kind={kind} value={seed} onChange={setSeed} />
            ) : seedOptions.length ? (
              <Select value={seed || "_none"} onValueChange={(v) => setSeed(v === "_none" ? "" : v)} options={[{ value: "_none", label: "Choose…" }, ...seedOptions]} />
            ) : (
              <Input value={seed} onChange={(e) => setSeed(e.target.value)} placeholder="Seed id" />
            )}
          </Field>
        )}
        {kind === "genre" && (
          <Field label="Genre name">
            <Input value={genre} onChange={(e) => setGenre(e.target.value)} placeholder="Rock" />
          </Field>
        )}
        {kind === "decade" && (
          <Field label="Decade">
            <Select
              value={decade}
              onValueChange={setDecade}
              options={(seeds.data?.decades || [1980, 1990, 2000, 2010, 2020]).map((d) => ({ value: String(d), label: `${d}s` }))}
            />
          </Field>
        )}
      </div>
      <div className="mb-8 flex flex-wrap gap-2">
        <Button onClick={load} disabled={busy}>{busy ? "Picking…" : "Get station"}</Button>
        <Button variant="secondary" disabled={!ids.length} onClick={() => play(ids)}>Play</Button>
        <Button variant="ghost" disabled={!ids.length} onClick={() => add(ids).then(() => toast.success("Added to queue"))}>Add to queue</Button>
        <Button
          variant="ghost"
          onClick={async () => {
            await api.post("/api/v1/radio/refresh", {
              kind,
              seed_id: kind === "decade" || kind === "quick_mix" ? undefined : seed || undefined,
              decade: kind === "decade" ? Number(decade || seed) : undefined,
              limit: 50
            });
            toast.success("radio.refresh queued");
          }}
        >
          Refresh job
        </Button>
      </div>
      {seeds.isError && <QueryError message={seeds.error instanceof Error ? seeds.error.message : undefined} onRetry={() => seeds.refetch()} />}
      {!ids.length && !seeds.isError && (
        <EmptyState
          icon={RadioIcon}
          title={kind === "quick_mix" ? "Start a Quick Mix from your library." : "Pick a seed and get a station."}
          description="Artist, album, track, genre, decade, or library radio. Clients enqueue with POST /api/v1/me/queue/add."
          action={{ label: "Get station", onClick: load }}
        />
      )}
      {!!ids.length && (
        <p className="text-sm text-muted">{ids.length} track ids selected. Play or add to queue to start listening.</p>
      )}
    </div>
  );
}

function SeedTypeahead({ kind, value, onChange }: { kind: RadioKind; value: string; onChange: (id: string) => void }) {
  const [q, setQ] = useState("");
  const [picked, setPicked] = useState("");
  const search = useQuery({
    queryKey: ["radio-typeahead", kind, q],
    enabled: q.trim().length > 1,
    queryFn: () => api.get<{ results: SearchHit[] }>(`/api/v1/search?q=${encodeURIComponent(q)}&type=${kind}&limit=12`)
  });
  const hits = (search.data?.results || []).filter((h) => h.type === kind);

  useEffect(() => {
    if (!value) {
      setPicked("");
      setQ("");
    }
  }, [value]);

  return (
    <div className="relative">
      <Input
        value={picked || q}
        onChange={(e) => {
          setPicked("");
          onChange("");
          setQ(e.target.value);
        }}
        placeholder={kind === "track" ? "Search tracks" : kind === "album" ? "Search albums" : "Search artists"}
      />
      {!!hits.length && !picked && (
        <ul className="absolute z-20 mt-1 max-h-56 w-full overflow-auto rounded-md border border-border bg-surface-1 py-1 shadow-lg">
          {hits.map((h) => (
            <li key={h.id}>
              <button
                type="button"
                className="block w-full px-3 py-1.5 text-left text-sm hover:bg-surface-2"
                onClick={() => {
                  onChange(h.id);
                  setPicked(h.title);
                  setQ("");
                }}
              >
                <span className="block truncate">{h.title}</span>
                {h.artist && <span className="block truncate text-xs text-muted">{h.artist}</span>}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

export function RadioStationPage() {
  return <RadioPage />;
}
