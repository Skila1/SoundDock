import { NavLink, Outlet, Link, useLocation } from "react-router-dom";
import { Globe, Upload } from "lucide-react";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/ui/empty";
import { cn } from "@/lib/utils";

const tabs = [
  { to: "/library", label: "Tracks", end: true },
  { to: "/library/albums", label: "Albums" },
  { to: "/library/artists", label: "Artists" },
  { to: "/library/favourites", label: "Favourites" }
];

function AddActions() {
  return (
    <div className="flex flex-wrap gap-2">
      <Button asChild size="sm" variant="secondary">
        <Link to="/library/add">
          <Upload className="h-4 w-4" /> Add music
        </Link>
      </Button>
      <Button asChild size="sm" variant="secondary">
        <Link to="/library/import">
          <Globe className="h-4 w-4" /> Import
        </Link>
      </Button>
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
