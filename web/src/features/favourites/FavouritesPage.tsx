import { useQuery } from "@tanstack/react-query";
import { Heart } from "lucide-react";
import { api } from "@/lib/api";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { MediaCard } from "@/components/media/MediaCard";
import { TrackList } from "@/components/media/TrackList";
import { EmptyState } from "@/components/ui/empty";
import { usePlayer } from "@/stores/player";
import type { Album, Artist, Favourite, Playlist, Track } from "@/types/api";

export function FavouritesPage() {
  const play = usePlayer((s) => s.playTracks);
  const q = useQuery({
    queryKey: ["favourites"],
    queryFn: async () => {
      const favs = await api.get<Favourite[]>("/api/v1/favourites");
      const tracks: Track[] = [];
      const albums: Album[] = [];
      const artists: Artist[] = [];
      const playlists: Playlist[] = [];
      for (const f of favs || []) {
        try {
          if (f.type === "track") tracks.push(await api.get<Track>(`/api/v1/tracks/${f.id}`));
          if (f.type === "album") albums.push(await api.get<Album>(`/api/v1/albums/${f.id}`));
          if (f.type === "artist") artists.push(await api.get<Artist>(`/api/v1/artists/${f.id}`));
          if (f.type === "playlist") playlists.push(await api.get<Playlist>(`/api/v1/playlists/${f.id}`));
        } catch { /* missing entity */ }
      }
      return { tracks, albums, artists, playlists };
    }
  });
  const d = q.data;
  const empty = !d || (!d.tracks.length && !d.albums.length && !d.artists.length && !d.playlists.length);
  return (
    <div>
      {empty && !q.isLoading && <EmptyState icon={Heart} title="Nothing favourited yet." description="Heart albums, artists, tracks, and playlists to find them here." />}
      {d && !empty && (
        <Tabs defaultValue="tracks">
          <TabsList className="mb-4">
            <TabsTrigger value="tracks">Tracks ({d.tracks.length})</TabsTrigger>
            <TabsTrigger value="albums">Albums ({d.albums.length})</TabsTrigger>
            <TabsTrigger value="artists">Artists ({d.artists.length})</TabsTrigger>
            <TabsTrigger value="playlists">Playlists ({d.playlists.length})</TabsTrigger>
          </TabsList>
          <TabsContent value="tracks"><TrackList tracks={d.tracks} onPlay={(i) => play([d.tracks[i].id])} /></TabsContent>
          <TabsContent value="albums"><div className="grid grid-cols-2 gap-4 sm:grid-cols-4">{d.albums.map((a) => <MediaCard key={a.id} className="max-w-none min-w-0" to={`/albums/${a.id}`} id={a.id} title={a.title} kind="album" />)}</div></TabsContent>
          <TabsContent value="artists"><div className="grid grid-cols-2 gap-4 sm:grid-cols-4">{d.artists.map((a) => <MediaCard key={a.id} className="max-w-none min-w-0" to={`/artists/${a.id}`} id={a.id} title={a.name} kind="artist" />)}</div></TabsContent>
          <TabsContent value="playlists"><div className="grid grid-cols-2 gap-4 sm:grid-cols-4">{d.playlists.map((p) => <MediaCard key={p.id} className="max-w-none min-w-0" to={`/playlists/${p.id}`} id={p.id} title={p.name} kind="playlist" />)}</div></TabsContent>
        </Tabs>
      )}
    </div>
  );
}
