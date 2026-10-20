import { api } from "@/lib/api";

export type BasedOnSource = { id: string; name?: string; artwork?: string };

/**
 * Starts a "Create playlist based on" copy of a provider playlist: a SoundDock
 * playlist with the same name, songs and cover. Songs missing from the library
 * are downloaded and join the playlist straight away. Returns the job ID.
 */
export async function createPlaylistBasedOn(provider: string, pl: BasedOnSource): Promise<string | undefined> {
  const r = await api.post<{ job_id?: string }>(`/api/v1/providers/${provider}/playlists/${encodeURIComponent(pl.id)}/import`, {
    mode: "once",
    copy: true,
    name: pl.name,
    artwork: pl.artwork || undefined
  });
  return r?.job_id;
}
