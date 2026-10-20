import { NavLink, Outlet, useLocation } from "react-router-dom";
import { cn } from "@/lib/utils";
import { adminGroupFor, adminNavGroups, adminPath } from "./adminNav";

/** Administration pages share a top navigation bar; the app sidebar stays as it is. */
export function AdminLayout() {
  return (
    <div className="min-w-0">
      <AdminNavBar />
      <Outlet />
    </div>
  );
}

/** Five section buttons on one line, with the active section's pages below. */
export function AdminNavBar() {
  const { pathname } = useLocation();
  const active = adminGroupFor(pathname);
  return (
    <nav
      aria-label="Administration"
      className="sticky -top-6 z-10 -mx-4 -mt-6 mb-6 border-b border-border bg-background/95 px-4 pb-2 pt-2 backdrop-blur md:-top-8 md:-mx-8 md:-mt-8 md:px-8"
    >
      <div className="flex items-center gap-1 overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
        {adminNavGroups.map((g) => {
          const Icon = g.icon;
          const on = g.id === active.id;
          return (
            <NavLink
              key={g.id}
              to={adminPath(g.links[0].to)}
              end
              aria-current={on ? "page" : undefined}
              className={cn(
                "flex shrink-0 items-center gap-2 whitespace-nowrap rounded-lg px-3.5 py-2 text-sm font-semibold text-muted transition-colors hover:bg-surface-2/70 hover:text-foreground",
                on && "bg-surface-2 text-foreground [&>svg]:text-accent"
              )}
            >
              <Icon className="h-4 w-4 shrink-0" />
              {g.label}
            </NavLink>
          );
        })}
      </div>
      {active.links.length > 1 && (
        <div role="group" aria-label={`${active.label} pages`} className="mt-1.5 flex items-center gap-1 overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
          {active.links.map(({ to, label }) => (
            <NavLink
              key={to}
              to={adminPath(to)}
              end={to === "."}
              className={({ isActive }) =>
                cn(
                  "shrink-0 whitespace-nowrap rounded-full px-3 py-1 text-xs font-medium text-muted transition-colors hover:bg-surface-2/70 hover:text-foreground",
                  isActive && "bg-accent/15 text-foreground"
                )
              }
            >
              {label}
            </NavLink>
          ))}
        </div>
      )}
    </nav>
  );
}
