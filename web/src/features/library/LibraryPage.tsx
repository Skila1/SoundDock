import { NavLink, Outlet, Link, useLocation } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Globe, Upload } from "lucide-react";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/ui/empty";
import { cn } from "@/lib/utils";
import { api } from "@/lib/api";
import { hasPerm } from "@/lib/perms";
import type { User } from "@/types/api";

const tabs = [
  { to: "/library", label: "Tracks", end: true },
  { to: "/library/albums", label: "Albums" },
  { to: "/library/artists", label: "Artists" },
  { to: "/library/favourites", label: "Favourites" }
];

function AddActions() {
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/api/v1/me") });
  const canAdd = hasPerm(me.data, "library.upload");
  const canImport = hasPerm(me.data, "library.import_url") || hasPerm(me.data, "library.upload");
  if (!canAdd && !canImport) return null;
  return (
    <div className="flex flex-wrap gap-2">
      {canAdd && (
        <Button asChild size="sm" variant="secondary">
          <Link to="/library/add">
            <Upload className="h-4 w-4" /> Add music
          </Link>
        </Button>
      )}
      {canImport && (
        <Button asChild size="sm" variant="secondary">
          <Link to="/library/import">
            <Globe className="h-4 w-4" /> Import
          </Link>
        </Button>
      )}
    </div>
  );
}

export function LibraryLayout() {
  const loc = useLocation();
  const tool = loc.pathname === "/library/add" || loc.pathname === "/library/import" || loc.pathname === "/library/sources";
  return (
    <div>
      {tool ? (
        <div className="mb-4 flex justify-end">
          <AddActions />
        </div>
      ) : (
        <PageHeader
          title="Library"
          description="Your catalogue. Add files or import from a direct URL."
          actions={<AddActions />}
        />
      )}
      <div className="mb-5 flex flex-wrap gap-1">
        {tabs.map((it) => (
          <NavLink
            key={it.to}
            to={it.to}
            end={it.end}
            className={({ isActive }) =>
              cn("rounded-full px-3 py-1 text-sm", isActive ? "bg-accent text-[#04140a]" : "bg-surface-2 text-muted hover:text-foreground")
            }
          >
            {it.label}
          </NavLink>
        ))}
      </div>
      <Outlet />
    </div>
  );
}
