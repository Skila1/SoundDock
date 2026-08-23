import { useEffect, useState, type KeyboardEvent as ReactKeyboardEvent } from "react";
import { useNavigate } from "react-router-dom";
import type { LucideIcon } from "lucide-react";
import {
  Disc3,
  CircleHelp,
  Globe,
  Heart,
  Home,
  Library,
  Link2,
  ListMusic,
  MessageCircle,
  Mic2,
  Moon,
  Music,
  Rows3,
  Search,
  Settings,
  Sun,
  Upload,
  UserRound
} from "lucide-react";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Artwork } from "@/components/media/Artwork";
import { api } from "@/lib/api";
import { artworkUrl } from "@/lib/utils";
import type { SearchHit } from "@/types/api";
import { useUi } from "@/stores/ui";
import { usePlayer } from "@/stores/player";
import { useTheme } from "@/stores/theme";
import { usePrefs } from "@/stores/prefs";
import { SOUNDDOCK_DISCORD_INVITE } from "@/lib/community";

const NAV = [
  { to: "/", label: "Home", icon: Home },
  { to: "/search", label: "Search", icon: Search },
  { to: "/artists", label: "Artists", icon: Mic2 },
  { to: "/albums", label: "Albums", icon: Disc3 },
  { to: "/tracks", label: "Tracks", icon: Music },
  { to: "/playlists", label: "Playlists", icon: ListMusic },
  { to: "/favourites", label: "Favourites", icon: Heart },
  { to: "/library", label: "Libraries", icon: Library },
  { to: "/upload", label: "Upload", icon: Upload },
  { to: "/import", label: "Remote Import", icon: Globe },
  { to: "/settings/connected", label: "Connected Services", icon: Link2 },
  { to: "/profile", label: "Profile", icon: UserRound },
  { to: "/admin", label: "Administration", icon: Settings }
];

type RecentRow = { kind: "recent"; key: string; label: string };
type HitRow = { kind: "hit"; key: string; hit: SearchHit };
type NavRow = { kind: "nav"; key: string; to: string; label: string; icon: LucideIcon };
type ActionRow = { kind: "action"; key: string; label: string; icon: LucideIcon; run: () => void };
type Row = RecentRow | HitRow | NavRow | ActionRow;

export function CommandSearch() {
  const ui = useUi();
  const nav = useNavigate();
  const play = usePlayer((s) => s.playTracks);
  const playing = usePlayer((s) => s.playing);
  const control = usePlayer((s) => s.control);
  const theme = useTheme();
  const prefs = usePrefs();
  const [q, setQ] = useState("");
  const [hits, setHits] = useState<SearchHit[]>([]);
  const [i, setI] = useState(0);
  const recents = JSON.parse(localStorage.getItem("sd-recent-search") || "[]") as string[];

  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        ui.set({ commandOpen: true });
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [ui]);

  useEffect(() => {
    if (!q.trim()) {
      setHits([]);
      return;
    }
    const t = setTimeout(() => {
      api.get<{ results: SearchHit[] }>(`/api/v1/search?q=${encodeURIComponent(q)}&limit=12`).then((r) => {
        setHits(r.results || []);
        setI(0);
      });
    }, 180);
    return () => clearTimeout(t);
  }, [q]);

  const close = () => ui.set({ commandOpen: false });

  const remember = (term: string) => {
    if (!term.trim()) return;
    const next = [term, ...recents.filter((x) => x !== term)].slice(0, 8);
    localStorage.setItem("sd-recent-search", JSON.stringify(next));
  };

  const goHit = (h: SearchHit) => {
    remember(q);
    close();
    if (h.type === "track") play([h.id]);
    else nav(`/${h.type}s/${h.id}`);
  };

  const actions: ActionRow[] = [
    {
      kind: "action",
      key: "act-theme",
      label: theme.theme === "dark" ? "Switch to light theme" : "Switch to dark theme",
      icon: theme.theme === "dark" ? Sun : Moon,
      run: () => {
        theme.setTheme(theme.theme === "dark" ? "light" : "dark");
        close();
      }
    },
    {
      kind: "action",
      key: "act-density",
      label: prefs.density === "compact" ? "Use comfortable density" : "Use compact density",
      icon: Rows3,
      run: () => {
        prefs.toggleDensity();
        close();
      }
    },
    {
      kind: "action",
      key: "act-play",
      label: playing ? "Pause playback" : "Resume playback",
      icon: Music,
      run: () => {
        control(playing ? "pause" : "resume");
        close();
      }
    },
    {
      kind: "action",
      key: "act-help",
      label: "Help",
      icon: CircleHelp,
      run: () => {
        window.open(SOUNDDOCK_DISCORD_INVITE, "_blank", "noopener,noreferrer");
        close();
      }
    },
    {
      kind: "action",
      key: "act-discord",
      label: "Discord server",
      icon: MessageCircle,
      run: () => {
        window.open(SOUNDDOCK_DISCORD_INVITE, "_blank", "noopener,noreferrer");
        close();
      }
    }
  ];

  const needle = q.trim().toLowerCase();
  const navRows: NavRow[] = NAV.filter((n) => !needle || n.label.toLowerCase().includes(needle)).map((n) => ({
    kind: "nav",
    key: "nav-" + n.to,
    to: n.to,
    label: n.label,
    icon: n.icon
  }));
  const actionRows: ActionRow[] = actions.filter((a) => !needle || a.label.toLowerCase().includes(needle));

  const recentRows: RecentRow[] = !q
    ? recents.map((r) => ({ kind: "recent" as const, key: "recent-" + r, label: r }))
    : [];
  const hitRows: HitRow[] = hits.map((h) => ({ kind: "hit" as const, key: h.type + h.id, hit: h }));
  const rows: Row[] = [...recentRows, ...hitRows, ...navRows, ...actionRows];

  useEffect(() => {
    setI(0);
  }, [q, hits.length, navRows.length, actionRows.length]);

  useEffect(() => {
    if (ui.commandOpen) setI(0);
  }, [ui.commandOpen]);

  const activate = (row: Row) => {
    if (row.kind === "recent") setQ(row.label);
    else if (row.kind === "hit") goHit(row.hit);
    else if (row.kind === "nav") {
      remember(q);
      close();
      nav(row.to);
    } else row.run();
  };

  const onKey = (e: ReactKeyboardEvent<HTMLInputElement>) => {
    if (!rows.length) return;
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setI((n) => (n + 1) % rows.length);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setI((n) => (n - 1 + rows.length) % rows.length);
    } else if (e.key === "Enter") {
      e.preventDefault();
      const row = rows[i];
      if (row) activate(row);
    }
  };

  return (
    <Dialog open={ui.commandOpen} onOpenChange={(v) => ui.set({ commandOpen: v })}>
      <DialogContent className="p-0" title="Search">
        <Input
          autoFocus
          value={q}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={onKey}
          placeholder="Search tracks, albums, artists…"
          className="border-0 bg-transparent"
        />
        <div className="max-h-80 overflow-auto border-t border-border p-2">
          {recentRows.map((r) => {
            const idx = rows.indexOf(r);
            return (
              <button
                key={r.key}
                className={`block w-full rounded px-2 py-2 text-left text-sm text-muted hover:bg-surface-2 ${idx === i ? "bg-surface-2" : ""}`}
                onClick={() => activate(r)}
                onMouseEnter={() => setI(idx)}
              >
                {r.label}
              </button>
            );
          })}
          {hitRows.map((r) => {
            const idx = rows.indexOf(r);
            const h = r.hit;
            return (
              <button
                key={r.key}
                className={`flex w-full items-center gap-3 rounded-md px-2 py-2 text-left ${idx === i ? "bg-surface-2" : "hover:bg-surface-2"}`}
                onClick={() => activate(r)}
                onMouseEnter={() => setI(idx)}
              >
                <div className="h-9 w-9 overflow-hidden rounded">
                  <Artwork src={artworkUrl(h.type, h.id, "thumb")} id={h.id} name={h.title} kind={h.type} size="sm" />
                </div>
                <div className="min-w-0">
                  <div className="truncate text-sm" dangerouslySetInnerHTML={{ __html: highlight(h.title, q) }} />
                  <div className="text-xs text-muted">{h.type} · {h.artist || h.album || ""}</div>
                </div>
              </button>
            );
          })}
          {(navRows.length > 0 || actionRows.length > 0) && (
            <>
              {(recentRows.length > 0 || hitRows.length > 0) && (
                <div className="px-2 pb-1 pt-2 text-[10px] font-semibold uppercase tracking-wide text-subtle">Navigate</div>
              )}
              {navRows.map((r) => {
                const idx = rows.indexOf(r);
                const Icon = r.icon;
                return (
                  <button
                    key={r.key}
                    className={`flex w-full items-center gap-3 rounded-md px-2 py-2 text-left text-sm ${idx === i ? "bg-surface-2" : "hover:bg-surface-2"}`}
                    onClick={() => activate(r)}
                    onMouseEnter={() => setI(idx)}
                  >
                    <Icon className="h-4 w-4 shrink-0 text-muted" />
                    <span>{r.label}</span>
                  </button>
                );
              })}
              {actionRows.length > 0 && (
                <div className="px-2 pb-1 pt-2 text-[10px] font-semibold uppercase tracking-wide text-subtle">Actions</div>
              )}
              {actionRows.map((r) => {
                const idx = rows.indexOf(r);
                const Icon = r.icon;
                return (
                  <button
                    key={r.key}
                    className={`flex w-full items-center gap-3 rounded-md px-2 py-2 text-left text-sm ${idx === i ? "bg-surface-2" : "hover:bg-surface-2"}`}
                    onClick={() => activate(r)}
                    onMouseEnter={() => setI(idx)}
                  >
                    <Icon className="h-4 w-4 shrink-0 text-muted" />
                    <span>{r.label}</span>
                  </button>
                );
              })}
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function highlight(text: string, q: string) {
  if (!q) return text;
  const re = new RegExp(`(${q.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")})`, "ig");
  return text.replace(re, "<mark class='bg-accent/30 text-inherit'>$1</mark>");
}
