import { create } from "zustand";
import { toast } from "sonner";
import { api } from "@/lib/api";

export type PartyPermissions = {
  add: boolean;
  vote: boolean;
  skip: boolean;
  pause: boolean;
};
export type PartyMember = {
  user_id: string;
  role: "host" | "dj" | "guest" | string;
  name?: string;
};
export type PartyVote = {
  track_id?: string;
  user_id?: string;
  created_at?: string;
};
export type PartyState = {
  session_id: string;
  enabled: boolean;
  host_user_id?: string | null;
  expires_at?: string | null;
  members: PartyMember[];
  votes: PartyVote[];
  permissions?: PartyPermissions;
  current?: {
    track_id: string;
    title: string;
    artist: string;
    status: string;
  } | null;
};

type PartyStore = {
  party: PartyState | null;
  myId: string | null;
  open: boolean;
  joinId: string;
  set: (
    p: Partial<Pick<PartyStore, "party" | "myId" | "open" | "joinId">>,
  ) => void;
};

/** The party the signed-in user is in, shared by the header button and the player. */
export const useParty = create<PartyStore>((set) => ({
  party: null,
  myId: null,
  open: false,
  joinId: "",
  set: (p) => set(p),
}));

/** Session id of a party this user joined as a guest or DJ (not as host). */
export function guestPartySession(): string | null {
  const { party, myId } = useParty.getState();
  if (!party?.enabled || !myId || party.host_user_id === myId) return null;
  return party.session_id;
}

/**
 * Sends songs to the host's party queue when this user is a guest. Returns
 * false when they are not in someone else's party, so callers use their own queue.
 */
export async function queueToParty(
  ids: string[],
  next = false,
  tracks?: unknown[],
): Promise<boolean> {
  const sid = guestPartySession();
  if (!sid || !ids.length) return false;
  try {
    const st = await api.post<PartyState>("/api/v1/me/party/queue", {
      session_id: sid,
      track_ids: ids,
      tracks,
      next,
    });
    useParty.getState().set({ party: st });
    toast.success(
      ids.length === 1
        ? "Added to the party queue"
        : `Added ${ids.length} songs to the party queue`,
    );
  } catch (err) {
    toast.error(
      err instanceof Error ? err.message : "Could not add to the party",
    );
  }
  return true;
}
