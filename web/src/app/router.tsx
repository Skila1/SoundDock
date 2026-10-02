import { lazy, Suspense } from "react";
import { useQuery } from "@tanstack/react-query";
import { Navigate, Route, Routes } from "react-router-dom";
import { api } from "@/lib/api";
import { AppShell } from "@/app/layout/AppShell";
import { BootError, BootScreen, ForbiddenPage, NotFoundPage } from "@/app/errors";
import { DiscordCallbackCatch, isDiscordOAuthCallbackPath, LoginPage, SetupPage } from "@/features/auth/AuthPages";
import { resetClientSession } from "@/features/auth/sessionReset";
import { HomePage } from "@/features/home/HomePage";
import { Skeleton } from "@/components/ui/misc";
import type { User } from "@/types/api";
import { legacyAdminRedirects } from "@/features/admin/adminNav";

const SearchPage = lazy(() => import("@/features/search/SearchPage").then((m) => ({ default: m.SearchPage })));
const ArtistsPage = lazy(() => import("@/features/artists/ArtistsPage").then((m) => ({ default: m.ArtistsPage })));
const ArtistPage = lazy(() => import("@/features/artists/ArtistPage").then((m) => ({ default: m.ArtistPage })));
const AlbumsPage = lazy(() => import("@/features/albums/AlbumsPage").then((m) => ({ default: m.AlbumsPage })));
const AlbumPage = lazy(() => import("@/features/albums/AlbumPage").then((m) => ({ default: m.AlbumPage })));
const TracksPage = lazy(() => import("@/features/tracks/TracksPage").then((m) => ({ default: m.TracksPage })));
const PlaylistsPage = lazy(() => import("@/features/playlists/PlaylistsPage").then((m) => ({ default: m.PlaylistsPage })));
const PlaylistPage = lazy(() => import("@/features/playlists/PlaylistPage").then((m) => ({ default: m.PlaylistPage })));
const PlaylistInvitePage = lazy(() => import("@/features/playlists/PlaylistInvitePage").then((m) => ({ default: m.PlaylistInvitePage })));
const RadioPage = lazy(() => import("@/features/playlists/RadioPage").then((m) => ({ default: m.RadioPage })));
const RadioStationPage = lazy(() => import("@/features/playlists/RadioPage").then((m) => ({ default: m.RadioStationPage })));
const TrackPage = lazy(() => import("@/features/tracks/TrackPage").then((m) => ({ default: m.TrackPage })));
const HistoryPage = lazy(() => import("@/features/history/HistoryPage").then((m) => ({ default: m.HistoryPage })));
const NeverPlayedPage = lazy(() => import("@/features/history/NeverPlayedPage").then((m) => ({ default: m.NeverPlayedPage })));
const RediscoveryPage = lazy(() => import("@/features/history/RediscoveryPage").then((m) => ({ default: m.RediscoveryPage })));
const StatsPage = lazy(() => import("@/features/stats/StatsPage").then((m) => ({ default: m.StatsPage })));
const WrappedPage = lazy(() => import("@/features/wrapped/WrappedPage").then((m) => ({ default: m.WrappedPage })));
const DevicesPage = lazy(() => import("@/features/devices/DevicesPage").then((m) => ({ default: m.DevicesPage })));
const AdminLayout = lazy(() => import("@/features/admin/AdminLayout").then((m) => ({ default: m.AdminLayout })));
const AdminDashboard = lazy(() => import("@/features/admin/AdminDashboard").then((m) => ({ default: m.AdminDashboard })));
const AdminActivity = lazy(() => import("@/features/admin/activity/AdminActivity").then((m) => ({ default: m.AdminActivity })));
const AdminBackups = lazy(() => import("@/features/admin/AdminBackups").then((m) => ({ default: m.AdminBackups })));
const AdminUpdates = lazy(() => import("@/features/admin/AdminUpdates").then((m) => ({ default: m.AdminUpdates })));
const AdminIntegrations = lazy(() => import("@/features/admin/AdminIntegrations").then((m) => ({ default: m.AdminIntegrations })));
const AdminUsers = lazy(() => import("@/features/admin/AdminUsers").then((m) => ({ default: m.AdminUsers })));
const AdminRoles = lazy(() => import("@/features/admin/AdminRoles").then((m) => ({ default: m.AdminRoles })));
const AdminDiscord = lazy(() => import("@/features/admin/AdminDiscord").then((m) => ({ default: m.AdminDiscord })));
const AdminLibraries = lazy(() => import("@/features/admin/AdminLibraries").then((m) => ({ default: m.AdminLibraries })));
const AdminCatalog = lazy(() => import("@/features/admin/AdminCatalog").then((m) => ({ default: m.AdminCatalog })));
const AdminMediaSettings = lazy(() => import("@/features/admin/AdminMediaSettings").then((m) => ({ default: m.AdminMediaSettings })));
const AdminRetention = lazy(() => import("@/features/admin/AdminRetention").then((m) => ({ default: m.AdminRetention })));
const AdminStatsMigration = lazy(() => import("@/features/admin/AdminStatsMigration").then((m) => ({ default: m.AdminStatsMigration })));
const PartyPage = lazy(() => import("@/features/devices/PartyPage").then((m) => ({ default: m.PartyPage })));
const ConnectedServicesPage = lazy(() => import("@/features/settings/ConnectedServicesPage").then((m) => ({ default: m.ConnectedServicesPage })));
const FavouritesPage = lazy(() => import("@/features/favourites/FavouritesPage").then((m) => ({ default: m.FavouritesPage })));
const LibrariesPage = lazy(() => import("@/features/library/LibrariesPage").then((m) => ({ default: m.LibrariesPage })));
const LibraryLayout = lazy(() => import("@/features/library/LibraryPage").then((m) => ({ default: m.LibraryLayout })));
const PersonalLibraryPage = lazy(() => import("@/features/library/PersonalLibraryPage").then((m) => ({ default: m.PersonalLibraryPage })));
const PublicProfilePage = lazy(() => import("@/features/library/PublicProfilePage").then((m) => ({ default: m.PublicProfilePage })));
const UploadPage = lazy(() => import("@/features/upload/UploadPage").then((m) => ({ default: m.UploadPage })));
const ImportPage = lazy(() => import("@/features/imports/ImportPage").then((m) => ({ default: m.ImportPage })));
const ProfilePage = lazy(() => import("@/features/profile/ProfilePage").then((m) => ({ default: m.ProfilePage })));

const BOOT_REQUEST_TIMEOUT_MS = 12_000;

function bootRequest<T>(path: string, signal: AbortSignal) {
  const controller = new AbortController();
  const abort = () => controller.abort();
  if (signal.aborted) abort();
  else signal.addEventListener("abort", abort, { once: true });
  const timeout = window.setTimeout(() => controller.abort(), BOOT_REQUEST_TIMEOUT_MS);
  return api.get<T>(path, { signal: controller.signal }).finally(() => {
    window.clearTimeout(timeout);
    signal.removeEventListener("abort", abort);
  });
}

function errorStatus(error: unknown) {
  return typeof error === "object" && error !== null && "status" in error ? Number(error.status) : undefined;
}

function Fallback() {
  return (
    <div className="space-y-3">
      <Skeleton className="h-8 w-48" />
      <Skeleton className="h-40 w-full" />
    </div>
  );
}

export function AppRouter() {
  const setup = useQuery({
    queryKey: ["setup"],
    queryFn: ({ signal }) => bootRequest<{ needed: boolean; discord_enabled?: boolean; discord_configured?: boolean }>("/api/v1/setup/status", signal),
    retry: false
  });
  const me = useQuery({
    queryKey: ["me"],
    queryFn: ({ signal }) => bootRequest<User>("/api/v1/me", signal),
    retry: false
  });

  if (isDiscordOAuthCallbackPath()) {
    return <DiscordCallbackCatch />;
  }
  if (setup.data?.needed) {
    return (
      <SetupPage
        discordConfigured={!!setup.data?.discord_configured}
        onDone={() => {
          resetClientSession();
          void setup.refetch();
          void me.refetch();
        }}
      />
    );
  }
  if (me.data) {
    const user = me.data;
    return (
    <Suspense fallback={<Fallback />}>
      <Routes>
        <Route element={<AppShell user={user} />}>
          <Route path="/" element={<HomePage />} />
          <Route path="/search" element={<SearchPage />} />
          <Route path="/artists/:id" element={<ArtistPage />} />
          <Route path="/albums/:id" element={<AlbumPage />} />
          <Route path="/tracks/:id" element={<TrackPage />} />
          <Route path="/artists" element={<Navigate to="/library/artists" replace />} />
          <Route path="/albums" element={<Navigate to="/library/albums" replace />} />
          <Route path="/tracks" element={<Navigate to="/library" replace />} />
          <Route path="/favourites" element={<Navigate to="/library/favourites" replace />} />
          <Route path="/upload" element={<Navigate to="/library/add" replace />} />
          <Route path="/import" element={<Navigate to="/library/import" replace />} />
          <Route path="/me/library" element={<PersonalLibraryPage mine />} />
          <Route path="/users/:id/library" element={<PersonalLibraryPage />} />
          <Route path="/users/:id" element={<PublicProfilePage />} />
          <Route path="/library" element={<LibraryLayout />}>
            <Route index element={<TracksPage />} />
            <Route path="albums" element={<AlbumsPage />} />
            <Route path="artists" element={<ArtistsPage />} />
            <Route path="favourites" element={<FavouritesPage />} />
            <Route path="add" element={<UploadPage />} />
            <Route path="import" element={<ImportPage />} />
            <Route path="sources" element={<LibrariesPage user={user} />} />
          </Route>
          <Route path="/playlists" element={<PlaylistsPage />} />
          <Route path="/playlists/invite" element={<PlaylistInvitePage />} />
          <Route path="/playlists/:id" element={<PlaylistPage />} />
          <Route path="/radio" element={<RadioPage />} />
          <Route path="/radio/:kind/:seedId" element={<RadioStationPage />} />
          <Route path="/radio/:kind" element={<RadioStationPage />} />
          <Route path="/history" element={<HistoryPage />} />
          <Route path="/history/never-played" element={<NeverPlayedPage />} />
          <Route path="/history/rediscovery" element={<RediscoveryPage />} />
          <Route path="/stats" element={<StatsPage />} />
          <Route path="/wrapped" element={<WrappedPage />} />
          <Route path="/settings/connected" element={<ConnectedServicesPage />} />
          <Route path="/profile" element={<ProfilePage user={user} onRefresh={() => me.refetch()} />} />
          <Route path="/profile/devices" element={<DevicesPage />} />
          <Route path="/profile/party" element={<PartyPage />} />
          <Route path="/admin" element={user.is_admin ? <AdminLayout /> : <ForbiddenPage />}>
            <Route index element={<AdminDashboard />} />
            <Route path="activity" element={<AdminActivity />} />
            <Route path="backups" element={<AdminBackups />} />
            <Route path="updates" element={<AdminUpdates />} />
            <Route path="integrations" element={<AdminIntegrations />} />
            <Route path="users" element={<AdminUsers />} />
            <Route path="users/:id/library" element={<PersonalLibraryPage admin />} />
            <Route path="discord-users/:discordID/library" element={<PersonalLibraryPage adminDiscord />} />
            <Route path="groups" element={<AdminRoles />} />
            <Route path="discord" element={<AdminDiscord />} />
            <Route path="libraries" element={<AdminLibraries />} />
            <Route path="catalog" element={<AdminCatalog />} />
            <Route path="media-settings" element={<AdminMediaSettings />} />
            <Route path="retention" element={<AdminRetention />} />
            <Route path="stats-migration" element={<AdminStatsMigration />} />
            {Object.entries(legacyAdminRedirects).map(([from, to]) => (
              <Route key={from} path={from} element={<Navigate to={to} replace />} />
            ))}
          </Route>
          <Route path="*" element={<NotFoundPage />} />
        </Route>
        <Route path="*" element={<Navigate to="/" />} />
      </Routes>
    </Suspense>
    );
  }
  if (me.isError && errorStatus(me.error) !== 401) {
    return (
      <BootError
        onRetry={() => {
          void setup.refetch();
          void me.refetch();
        }}
      />
    );
  }
  if (me.isError || me.isFetched || setup.isError) {
    return (
      <LoginPage
        discordConfigured={!!setup.data?.discord_configured}
        onDone={() => {
          resetClientSession();
          void me.refetch();
        }}
      />
    );
  }
  return <BootScreen />;
}
