import { Fragment } from "react";
import { NavLink, Outlet } from "react-router-dom";
import { cn } from "@/lib/utils";
import { adminNavGroups, adminPath } from "./adminNav";

/** Administration pages share a top navigation bar; the app sidebar stays as it is. */
export function AdminLayout() {
  return (
    <div className="min-w-0">
      <AdminNavBar />
      <Outlet />
    </div>
  );
}

export function AdminNavBar() {
  return (
    <nav
      aria-label="Administration"
      className="sticky -top-6 z-10 -mx-4 -mt-6 mb-6 border-b border-border bg-background/95 px-4 py-2 backdrop-blur md:-top-8 md:-mx-8 md:-mt-8 md:px-8"
    >
      <div className="flex items-center gap-1 gap-y-1.5 overflow-x-auto [scrollbar-width:none] md:flex-wrap md:overflow-visible [&::-webkit-scrollbar]:hidden">
        {adminNavGroups.map((g, gi) => (
          <Fragment key={g.id}>
            {gi > 0 && <span aria-hidden className="mx-1.5 h-5 w-px shrink-0 bg-border" />}
            <div role="group" aria-label={g.label} title={g.label} className="flex shrink-0 items-center gap-1">
              {g.links.map(({ to, label, icon: Icon }) => (
                <NavLink
                  key={to}
                  to={adminPath(to)}
                  end={to === "."}
                  className={({ isActive }) =>
                    cn(
                      "flex shrink-0 items-center gap-2 whitespace-nowrap rounded-lg px-3 py-1.5 text-sm font-medium text-muted transition-colors hover:bg-surface-2/70 hover:text-foreground",
                      isActive && "bg-surface-2 text-foreground [&>svg]:text-accent"
                    )
                  }
                >
                  <Icon className="h-4 w-4 shrink-0" />
                  {label}
                </NavLink>
              ))}
            </div>
          </Fragment>
        ))}
      </div>
    </nav>
  );
}
