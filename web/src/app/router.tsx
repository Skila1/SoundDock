import { lazy, Suspense } from "react";
import { useQuery } from "@tanstack/react-query";
import { Navigate, Route, Routes } from "react-router-dom";
import { api } from "@/lib/api";
import { AppShell } from "@/app/layout/AppShell";
import { BootScreen, ForbiddenPage, NotFoundPage } from "@/app/errors";
import { LoginPage } from "@/features/auth/AuthPages";
import { HomePage } from "@/features/home/HomePage";
import { Skeleton } from "@/components/ui/misc";
import type { User } from "@/types/api";

const SearchPage = lazy(() => import("@/features/search/SearchPage").then((m) => ({ default: m.SearchPage })));
const ArtistsPage = lazy(() => import("@/features/artists/ArtistsPage").then((m) => ({ default: m.ArtistsPage })));
const ArtistPage = lazy(() => import("@/features/artists/ArtistPage").then((m) => ({ default: m.ArtistPage })));
const AlbumsPage = lazy(() => import("@/features/albums/AlbumsPage").then((m) => ({ default: m.AlbumsPage })));
const AlbumPage = lazy(() => import("@/features/albums/AlbumPage").then((m) => ({ default: m.AlbumPage })));
const TracksPage = lazy(() => import("@/features/tracks/TracksPage").then((m) => ({ default: m.TracksPage })));
const PlaylistsPage = lazy(() => import("@/features/playlists/PlaylistsPage").then((m) => ({ default: m.PlaylistsPage })));
const PlaylistPage = lazy(() => import("@/features/playlists/PlaylistPage").then((m) => ({ default: m.PlaylistPage })));
const ConnectedServicesPage = lazy(() => import("@/features/settings/ConnectedServicesPage").then((m) => ({ default: m.ConnectedServicesPage })));
const FavouritesPage = lazy(() => import("@/features/favourites/FavouritesPage").then((m) => ({ default: m.FavouritesPage })));
const LibrariesPage = lazy(() => import("@/features/library/LibrariesPage").then((m) => ({ default: m.LibrariesPage })));
const UploadPage = lazy(() => import("@/features/upload/UploadPage").then((m) => ({ default: m.UploadPage })));
const ImportPage = lazy(() => import("@/features/imports/ImportPage").then((m) => ({ default: m.ImportPage })));
const ProfilePage = lazy(() => import("@/features/profile/ProfilePage").then((m) => ({ default: m.ProfilePage })));
const AdminLayout = lazy(() => import("@/features/admin/AdminLayout").then((m) => ({ default: m.AdminLayout })));
const AdminOverview = lazy(() => import("@/features/admin/AdminOverview").then((m) => ({ default: m.AdminOverview })));
const AdminUsers = lazy(() => import("@/features/admin/AdminPages").then((m) => ({ default: m.AdminUsers })));
const AdminRoles = lazy(() => import("@/features/admin/AdminPages").then((m) => ({ default: m.AdminRoles })));
const AdminStorage = lazy(() => import("@/features/admin/AdminPages").then((m) => ({ default: m.AdminStorage })));
const AdminLibraries = lazy(() => import("@/features/admin/AdminPages").then((m) => ({ default: m.AdminLibraries })));
const AdminJobs = lazy(() => import("@/features/admin/AdminPages").then((m) => ({ default: m.AdminJobs })));
const AdminBackups = lazy(() => import("@/features/admin/AdminPages").then((m) => ({ default: m.AdminBackups })));
const AdminDatabase = lazy(() => import("@/features/admin/AdminPages").then((m) => ({ default: m.AdminDatabase })));
const AdminDiscord = lazy(() => import("@/features/admin/AdminPages").then((m) => ({ default: m.AdminDiscord })));
const AdminIntegrations = lazy(() => import("@/features/admin/AdminPages").then((m) => ({ default: m.AdminIntegrations })));
const AdminExternal = lazy(() => import("@/features/admin/AdminPages").then((m) => ({ default: m.AdminExternalProviders })));
const AdminWebhooks = lazy(() => import("@/features/admin/AdminPages").then((m) => ({ default: m.AdminWebhooks })));
const AdminMetadata = lazy(() => import("@/features/admin/AdminPages").then((m) => ({ default: m.AdminMetadata })));
const AdminTranscode = lazy(() => import("@/features/admin/AdminPages").then((m) => ({ default: m.AdminTranscode })));
const AdminRetention = lazy(() => import("@/features/admin/AdminPages").then((m) => ({ default: m.AdminRetention })));
const AdminSecurity = lazy(() => import("@/features/admin/AdminPages").then((m) => ({ default: m.AdminSecurity })));
const AdminLogs = lazy(() => import("@/features/admin/AdminPages").then((m) => ({ default: m.AdminLogs })));
const AdminCloudflare = lazy(() => import("@/features/admin/AdminPages").then((m) => ({ default: m.AdminCloudflare })));
const AdminUpdates = lazy(() => import("@/features/admin/AdminPages").then((m) => ({ default: m.AdminUpdates })));

function Fallback() {
  return (
    <div className="space-y-3">
      <Skeleton className="h-8 w-48" />
      <Skeleton className="h-40 w-full" />
    </div>
  );
}

export function AppRouter() {
  const setup = useQuery({ queryKey: ["setup"], queryFn: () => api.get<{ needed: boolean; discord_only?: boolean }>("/api/v1/setup/status") });
  const me = useQuery({
    queryKey: ["me"],
    queryFn: () => api.get<User>("/api/v1/me"),
    retry: false
  });

  if (setup.isLoading || (me.isLoading && !me.isError)) return <BootScreen />;
  if (me.isError || !me.data) return <LoginPage onDone={() => me.refetch()} />;

  const user = me.data;
  return (
    <Suspense fallback={<Fallback />}>
      <Routes>
        <Route element={<AppShell user={user} />}>
          <Route path="/" element={<HomePage />} />
          <Route path="/search" element={<SearchPage />} />
          <Route path="/artists" element={<ArtistsPage />} />
          <Route path="/artists/:id" element={<ArtistPage />} />
          <Route path="/albums" element={<AlbumsPage />} />
          <Route path="/albums/:id" element={<AlbumPage />} />
          <Route path="/tracks" element={<TracksPage />} />
          <Route path="/playlists" element={<PlaylistsPage />} />
          <Route path="/playlists/:id" element={<PlaylistPage />} />
          <Route path="/settings/connected" element={<ConnectedServicesPage />} />
          <Route path="/favourites" element={<FavouritesPage />} />
          <Route path="/library" element={<LibrariesPage user={user} />} />
          <Route path="/upload" element={<UploadPage />} />
          <Route path="/import" element={<ImportPage />} />
          <Route path="/profile" element={<ProfilePage user={user} onRefresh={() => me.refetch()} />} />
          <Route path="/admin" element={user.is_admin ? <AdminLayout /> : <ForbiddenPage />}>
            <Route index element={<AdminOverview />} />
            <Route path="users" element={<AdminUsers />} />
            <Route path="roles" element={<AdminRoles />} />
            <Route path="storage" element={<AdminStorage />} />
            <Route path="libraries" element={<AdminLibraries />} />
            <Route path="jobs" element={<AdminJobs />} />
            <Route path="backups" element={<AdminBackups />} />
            <Route path="database" element={<AdminDatabase />} />
            <Route path="discord" element={<AdminDiscord />} />
            <Route path="integrations" element={<AdminIntegrations />} />
            <Route path="providers" element={<AdminExternal />} />
            <Route path="webhooks" element={<AdminWebhooks />} />
            <Route path="metadata" element={<AdminMetadata />} />
            <Route path="transcoding" element={<AdminTranscode />} />
            <Route path="retention" element={<AdminRetention />} />
            <Route path="security" element={<AdminSecurity />} />
            <Route path="logs" element={<AdminLogs />} />
            <Route path="cloudflare" element={<AdminCloudflare />} />
            <Route path="updates" element={<AdminUpdates />} />
          </Route>
          <Route path="*" element={<NotFoundPage />} />
        </Route>
        <Route path="*" element={<Navigate to="/" />} />
      </Routes>
    </Suspense>
  );
}
