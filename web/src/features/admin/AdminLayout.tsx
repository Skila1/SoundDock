import { Outlet } from "react-router-dom";

export function AdminLayout() {
  return (
    <div className="min-w-0">
      <Outlet />
    </div>
  );
}
