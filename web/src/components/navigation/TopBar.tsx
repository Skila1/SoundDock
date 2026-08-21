import { ArrowLeft, ArrowRight, Menu, Moon, Search, Sun } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { useTheme } from "@/stores/theme";
import { useUi } from "@/stores/ui";
import { api } from "@/lib/api";
import type { User } from "@/types/api";

export function TopBar({ title, user }: { title?: string; user: User }) {
  const nav = useNavigate();
  const theme = useTheme();
  const ui = useUi();
  return (
    <header className="sticky top-0 z-20 flex h-14 items-center gap-2 border-b border-border bg-background/80 px-3 backdrop-blur md:px-6">
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
          <DropdownMenuItem onSelect={() => api.post("/api/v1/auth/logout").then(() => location.reload())}>Log out</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </header>
  );
}
