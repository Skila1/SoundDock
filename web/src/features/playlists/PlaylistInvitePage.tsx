import { useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/ui/empty";
import { toast } from "sonner";

export function PlaylistInvitePage() {
  const [sp] = useSearchParams();
  const nav = useNavigate();
  const token = sp.get("token") || "";
  const [name, setName] = useState("Playlist");
  const [pid, setPid] = useState("");
  const [err, setErr] = useState("");

  useEffect(() => {
    if (!token) {
      setErr("Missing invite token");
      return;
    }
    api.get<{ playlist_id: string; name: string }>(`/api/v1/playlists/invite?token=${encodeURIComponent(token)}`)
      .then((r) => {
        setName(r.name);
        setPid(r.playlist_id);
      })
      .catch((e) => setErr(e instanceof Error ? e.message : "Invalid invite"));
  }, [token]);

  return (
    <div>
      <PageHeader title="Playlist invite" description={err || `Join “${name}” as a collaborator.`} />
      <div className="flex gap-2">
        <Button
          disabled={!!err || !token}
          onClick={async () => {
            const r = await api.post<{ playlist_id: string }>("/api/v1/playlists/invite/accept", { token });
            toast.success("Joined playlist");
            nav(`/playlists/${r.playlist_id || pid}`);
          }}
        >
          Accept invite
        </Button>
        <Button variant="ghost" onClick={() => nav("/playlists")}>Cancel</Button>
      </div>
    </div>
  );
}
