import { ArrowLeft, ArrowRight, Check, Menu, Moon, Palette, Rows3, Search, Sun } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { useTheme } from "@/stores/theme";
import { ACCENT_PRESETS, PrefsSync, usePrefs } from "@/stores/prefs";
import { useUi } from "@/stores/ui";
import { api } from "@/lib/api";
import { SOUNDDOCK_DISCORD_INVITE } from "@/lib/community";
import type { User } from "@/types/api";

export function TopBar({ title, user }: { title?: string; user: User }) {
  const nav = useNavigate();
  const theme = useTheme();
  const prefs = usePrefs();
  const ui = useUi();
  return (
      <header className="sd-topbar sticky top-0 z-20 flex h-14 shrink-0 items-center gap-2 border-b border-border bg-background/80 px-3 backdrop-blur md:px-6">
      <PrefsSync />
      <Button size="icon" variant="ghost" className="md:hidden" onClick={() => ui.set({ mobileNav: true })} aria-label="Menu">
        <Menu />
      </Button>
      <Button size="icon" variant="ghost" className="hidden md:inline-flex" onClick={() => nav(-1)} aria-label="Back">
        <ArrowLeft />
      </Button>
      <Button size="icon" variant="ghost" className="hidden md:inline-flex" onClick={() => nav(1)} aria-label="Forward">
        <ArrowRight />
      </Button>
      <div className="min-w-0 flex-1 truncate text-sm font-medium text-muted">{title}</div>
      <Button variant="secondary" className="hidden max-w-sm flex-1 justify-start gap-2 text-muted md:flex" onClick={() => ui.set({ commandOpen: true })}>
        <Search className="h-4 w-4" /> Search library
        <kbd className="ml-auto rounded border border-border px-1.5 text-[10px]">⌘K</kbd>
      </Button>
      <Button size="icon" variant="ghost" className="md:hidden" onClick={() => ui.set({ commandOpen: true })} aria-label="Search">
        <Search />
      </Button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button size="icon" variant="ghost" aria-label="Appearance">
            <Palette />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent className="w-56 p-2">
          <div className="px-1 pb-2 text-[10px] font-semibold uppercase tracking-wide text-subtle">Accent</div>
          <div className="mb-2 flex flex-wrap gap-1.5 px-1">
            {ACCENT_PRESETS.map((a) => (
              <button
                key={a.value}
                type="button"
                title={a.name}
                aria-label={a.name}
                className="flex h-7 w-7 items-center justify-center rounded-full border border-border"
                style={{ background: a.value }}
                onClick={() => prefs.setAccent(a.value)}
              >
                {prefs.accent === a.value && <Check className="h-3.5 w-3.5 text-[#04140a]" />}
              </button>
            ))}
          </div>
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => prefs.toggleDensity()}>
            <Rows3 className="h-4 w-4" />
            {prefs.density === "compact" ? "Comfortable density" : "Compact density"}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <Button size="icon" variant="ghost" onClick={() => theme.setTheme(theme.theme === "dark" ? "light" : "dark")} aria-label="Toggle theme">
        {theme.theme === "dark" ? <Sun /> : <Moon />}
      </Button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button size="icon" variant="secondary" aria-label="Account">
            {(user.display_name || user.username).slice(0, 1).toUpperCase()}
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent>
          <DropdownMenuItem onSelect={() => nav("/profile")}>Profile</DropdownMenuItem>
          {user.is_admin && <DropdownMenuItem onSelect={() => nav("/admin")}>Administration</DropdownMenuItem>}
          <DropdownMenuSeparator />
          <DropdownMenuItem asChild>
            <a href={SOUNDDOCK_DISCORD_INVITE} target="_blank" rel="noopener noreferrer">Help</a>
          </DropdownMenuItem>
          <DropdownMenuItem asChild>
            <a href={SOUNDDOCK_DISCORD_INVITE} target="_blank" rel="noopener noreferrer">Discord server</a>
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => api.post("/api/v1/auth/logout").then(() => location.reload())}>Log out</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </header>
  );
}
