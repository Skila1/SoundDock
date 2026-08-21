import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { Radio as RadioIcon } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { EmptyState, PageHeader } from "@/components/ui/empty";
import { usePlayer } from "@/stores/player";
import { toast } from "sonner";
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
  const artists = useQuery({ queryKey: ["artists"], queryFn: () => api.get<{ id: string; name: string }[]>("/api/v1/artists"), enabled: kind === "artist" });
  const albums = useQuery({ queryKey: ["albums"], queryFn: () => api.get<{ id: string; title: string }[]>("/api/v1/albums"), enabled: kind === "album" });
  const tracks = useQuery({
    queryKey: ["tracks-radio"],
    queryFn: () => api.get<{ id: string; title: string }[]>("/api/v1/tracks"),
    enabled: kind === "track"
  });

  const seedOptions = useMemo(() => {
    if (kind === "library") return (seeds.data?.libraries || []).map((x) => ({ value: x.id, label: x.name }));
    if (kind === "artist") return (artists.data || []).slice(0, 200).map((x) => ({ value: x.id, label: x.name }));
    if (kind === "album") return (albums.data || []).slice(0, 200).map((x) => ({ value: x.id, label: x.title }));
    if (kind === "track") return (tracks.data || []).slice(0, 200).map((x) => ({ value: x.id, label: x.title }));
    if (kind === "genre") return (seeds.data?.genres || []).map((x) => ({ value: x.id || x.name, label: x.name }));
    if (kind === "decade") return (seeds.data?.decades || []).map((d) => ({ value: String(d), label: `${d}s` }));
    return [];
  }, [kind, seeds.data, artists.data, albums.data, tracks.data]);

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
        description="Station picks from your library. Enqueue uses your queue — SoundDock never writes playback sessions from this page."
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
            {seedOptions.length ? (
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
      {!ids.length && (
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

export function RadioStationPage() {
  return <RadioPage />;
}
