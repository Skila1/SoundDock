import { useCallback, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Eye, Globe, Library, ListPlus, Lock, MoreHorizontal, Play, Trash2 } from "lucide-react";
import { api } from "@/lib/api";
import { TrackList } from "@/components/media/TrackList";
import { LibraryToolbar, LocalSearch, TrackGrid } from "@/components/media/LibraryToolbar";
import { Select } from "@/components/ui/select";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { ConfirmDialog } from "@/components/ui/alert-dialog";
import { EmptyState, PageHeader, QueryError } from "@/components/ui/empty";
import { Badge, Skeleton } from "@/components/ui/misc";
import { usePlayer } from "@/stores/player";
import { useLibraryView } from "@/stores/libraryView";
import type { PersonalLibraryResponse, PersonalLibraryTrack, PublicUserProfile, User } from "@/types/api";
import { toast } from "sonner";

const SORTS = [
  { value: "recent", label: "Recently added" },
  { value: "first", label: "First added" },
  { value: "title", label: "Title" },
  { value: "artist", label: "Artist" },
  { value: "album", label: "Album" },
  { value: "year", label: "Year" },
  { value: "plays", label: "Most requested" },
  { value: "duration", label: "Length" }
];

const text = (v: unknown) => (typeof v === "string" ? v : "").toLocaleLowerCase();
const artistOf = (t: PersonalLibraryTrack) => t.artists?.map((a) => a.name).join(", ") || t.artist || "";
const time = (v?: string) => (v ? Date.parse(v) || 0 : 0);

function compare(sort: string): (a: PersonalLibraryTrack, b: PersonalLibraryTrack) => number {
  const str = (f: (t: PersonalLibraryTrack) => string) => (a: PersonalLibraryTrack, b: PersonalLibraryTrack) =>
    f(a).localeCompare(f(b), undefined, { sensitivity: "base", numeric: true });
  const inAlbum = (a: PersonalLibraryTrack, b: PersonalLibraryTrack) =>
    str((t) => t.album || "")(a, b) || (a.disc_number || 0) - (b.disc_number || 0) || (a.track_number || 0) - (b.track_number || 0);
  switch (sort) {
    case "first":
      return (a, b) => time(a.first_requested_at) - time(b.first_requested_at);
    case "title":
      return str((t) => t.title || "");
    case "artist":
      return (a, b) => str(artistOf)(a, b) || inAlbum(a, b);
    case "album":
      return inAlbum;
    case "year":
      return (a, b) => (b.year || 0) - (a.year || 0);
    case "plays":
      return (a, b) => (b.request_count || 0) - (a.request_count || 0);
    case "duration":
      return (a, b) => (b.duration_ms || 0) - (a.duration_ms || 0);
    default:
      return (a, b) => time(b.last_requested_at) - time(a.last_requested_at);
  }
}

export function PersonalLibraryPage({ mine, admin, adminDiscord }: { mine?: boolean; admin?: boolean; adminDiscord?: boolean }) {
  const { id, discordID } = useParams();
  const play = usePlayer((s) => s.playTracks);
  const add = usePlayer((s) => s.add);
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [view, patchView] = useLibraryView(mine ? "my-library" : "user-library", { sort: "recent" });
  const layout = view.layout;
  const [term, setTerm] = useState("");
  const [selected, setSelected] = useState<string[]>([]);
  const [clearOpen, setClearOpen] = useState(false);
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
  const path = mine
    ? "/api/v1/me/library"
    : adminDiscord && discordID
      ? `/api/v1/admin/discord-users/${encodeURIComponent(discordID)}/library`
    : admin && id
      ? `/api/v1/admin/users/${id}/library`
      : `/api/v1/users/${id}/library`;
  const profile = useQuery({
    queryKey: ["user-profile", id],
    enabled: !mine && !adminDiscord && !!id,
    queryFn: () => api.get<PublicUserProfile>(`/api/v1/users/${id}`)
  });
  const q = useQuery({
    queryKey: ["personal-library", mine ? "me" : adminDiscord ? discordID : id, admin || adminDiscord ? "admin" : "user"],
    queryFn: () => api.get<PersonalLibraryResponse>(path)
  });
  const all = useMemo(() => q.data?.items || [], [q.data]);
  const genres = useMemo(() => {
    const seen = new Map<string, string>();
    for (const t of all) {
      const g = (t.genre || "").trim();
      if (g && !seen.has(g.toLowerCase())) seen.set(g.toLowerCase(), g);
    }
    return [...seen.values()].sort((a, b) => a.localeCompare(b));
  }, [all]);
  const genre = view.genre && genres.some((g) => g.toLowerCase() === view.genre) ? view.genre : "";
  const items = useMemo(() => {
    const words = term.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean);
    const out = all.filter((t) => {
      if (view.explicit === "clean" && t.explicit) return false;
      if (view.explicit === "explicit" && !t.explicit) return false;
      if (genre && text(t.genre) !== genre) return false;
      if (!words.length) return true;
      const hay = `${text(t.title)} ${text(artistOf(t))} ${text(t.album)} ${text(t.genre)}`;
      return words.every((w) => hay.includes(w));
    });
    const cmp = compare(view.sort || "recent");
    out.sort(view.desc ? (a, b) => cmp(b, a) : cmp);
    return out;
  }, [all, term, view.explicit, view.sort, view.desc, genre]);
  const filtered = items.length !== all.length;
  const ids = useMemo(() => items.map((t) => t.id), [items]);
  const onSelection = useCallback((next: string[]) => setSelected(next), []);
  const title = mine
    ? "My Library"
    : q.data?.owner.display_name || profile.data?.display_name || "Personal library";
  const visibility = q.data?.owner.visibility || profile.data?.personal_library_visibility || "private";

  const removeFromLibrary = async (body: { track_ids?: string[]; all?: boolean }) => {
    try {
      const res = await api.del<{ removed?: number }>("/api/v1/me/library", body);
      const n = res?.removed ?? 0;
      toast.success(n === 1 ? "Removed 1 song from My Library" : `Removed ${n} songs from My Library`);
      setSelected([]);
      void qc.invalidateQueries({ queryKey: ["personal-library"] });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not update My Library");
    }
  };

  return (
    <div>
      <PageHeader
        title={title}
        description={
          mine
            ? "Songs you picked yourself, in SoundDock or with /play in Discord. Playing a whole album, playlist or radio station does not add its songs. Select songs to remove them."
            : "Requested songs for this listener."
        }
        actions={
          <>
            <Badge tone={visibility === "public" ? "success" : "neutral"}>
              {visibility === "public" ? <Globe className="h-3 w-3" /> : <Lock className="h-3 w-3" />}
              {visibility === "public" ? "Public" : "Private"}
            </Badge>
            {mine && selected.length > 0 && (
              <Button variant="secondary" onClick={() => void removeFromLibrary({ track_ids: selected })}>
                <Trash2 /> Remove {selected.length === 1 ? "1 song" : `${selected.length} songs`}
              </Button>
            )}
            {items.length > 0 && (
              <Button variant="secondary" onClick={() => add(ids).then(() => toast.success(filtered ? "Queued results" : "Queued library"))}>
                <ListPlus /> {filtered ? "Queue results" : "Queue all"}
              </Button>
            )}
            {items.length > 0 && (
              <Button onClick={() => play(ids)}>
                <Play className="fill-current" /> {filtered ? `Play ${items.length}` : "Play all"}
              </Button>
            )}
            {mine && (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button size="icon" variant="ghost" aria-label="More library options"><MoreHorizontal /></Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem onSelect={() => navigate("/profile")}><Eye className="h-4 w-4" /> Visibility settings</DropdownMenuItem>
                  {all.length > 0 && (
                    <>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem className="text-destructive" onSelect={() => setClearOpen(true)}><Trash2 className="h-4 w-4" /> Clear My Library</DropdownMenuItem>
                    </>
                  )}
                </DropdownMenuContent>
              </DropdownMenu>
            )}
          </>
        }
      />
      {q.data?.inspecting && (
        <p className="mb-4 rounded-lg border border-border bg-surface-2 px-3 py-2 text-sm text-muted">
          You are viewing this personal library as an administrator. It is not mixed into your own library.
        </p>
      )}
      {q.isError && profile.data && !profile.data.personal_library_visible && (
        <EmptyState
          icon={Library}
          title="This personal library is private."
          description="Only the owner, or an administrator from user management, can open it."
        />
      )}
      {q.isError && !(profile.data && !profile.data.personal_library_visible) && (
        <QueryError message={q.error instanceof Error ? q.error.message : undefined} onRetry={() => q.refetch()} />
      )}
      {all.length > 0 && (
        <LibraryToolbar
          search={<LocalSearch value={term} onChange={setTerm} placeholder={mine ? "Search My Library" : "Search this library"} />}
          sort={{ value: view.sort || "recent", options: SORTS, onChange: (v) => patchView({ sort: v, desc: false }) }}
          desc={!!view.desc}
          onDesc={(d) => patchView({ desc: d })}
          filters={
            <>
              {genres.length > 1 && (
                <Select
                  className="h-9 w-[140px]"
                  value={genre || "all"}
                  onValueChange={(v) => patchView({ genre: v === "all" ? "" : v })}
                  options={[{ value: "all", label: "All genres" }, ...genres.map((g) => ({ value: g.toLowerCase(), label: g }))]}
                />
              )}
              <Select
                className="h-9 w-[128px]"
                value={view.explicit || "all"}
                onValueChange={(v) => patchView({ explicit: v as "all" | "clean" | "explicit" })}
                options={[
                  { value: "all", label: "All songs" },
                  { value: "clean", label: "Clean only" },
                  { value: "explicit", label: "Explicit only" }
                ]}
              />
            </>
          }
          layout={layout}
          onLayout={(l) => patchView({ layout: l })}
          summary={filtered ? `${items.length.toLocaleString()} of ${all.length.toLocaleString()} songs` : `${all.length.toLocaleString()} ${all.length === 1 ? "song" : "songs"}`}
        />
      )}
      {q.isLoading && (
        <div className="space-y-2">
          {Array.from({ length: 8 }).map((_, i) => (
            <Skeleton key={i} className="h-12 w-full" />
          ))}
        </div>
      )}
      {!q.isLoading && !q.isError && !all.length && (
        <EmptyState
          icon={Library}
          title={mine ? "Nothing requested yet." : "This library is empty."}
          description={mine ? "Play or queue a song from search or the catalogue and it will land here." : undefined}
          action={mine ? { label: "Browse the catalogue", onClick: () => navigate("/library") } : undefined}
        />
      )}
      {!q.isLoading && all.length > 0 && !items.length && (
        <EmptyState
          icon={Library}
          title="No songs match."
          description="Try a different search or clear the filters."
          action={{
            label: "Clear filters",
            onClick: () => {
              setTerm("");
              patchView({ genre: "", explicit: "all" });
            }
          }}
        />
      )}
      {items.length > 0 &&
        (layout === "grid" ? (
          <TrackGrid tracks={items} onPlay={(i) => play([ids[i]])} />
        ) : (
          <TrackList
            tracks={items}
            onPlay={(i) => play([ids[i]])}
            onQueue={(t) => add([t.id]).then(() => toast.success("Added to queue"))}
            onNext={(t) => add([t.id], true).then(() => toast.success("Playing next"))}
            onSelectionChange={mine ? onSelection : undefined}
          />
        ))}
      <ConfirmDialog
        open={clearOpen}
        onOpenChange={setClearOpen}
        title="Clear My Library?"
        description="This removes every song from your personal library. The songs stay in the shared catalogue and your playlists."
        confirmLabel="Clear"
        destructive
        onConfirm={() => void removeFromLibrary({ all: true })}
      />
      {mine && me.data && (
        <p className="mt-8 text-center text-xs text-subtle">
          The shared catalogue is still at <Link className="underline" to="/library">Catalogue</Link>.
          Open a track&apos;s menu to add it to one of your playlists.
        </p>
      )}
    </div>
  );
}
