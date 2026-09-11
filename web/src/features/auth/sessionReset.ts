import { pauseAll } from "@/components/player/audioEngine";
import { queryClient } from "@/app/providers";
import { resetPlayerSessionForTests } from "@/stores/player";

/** Drop the previous account's cache and player session after login, setup, or logout. */
export function resetClientSession() {
  pauseAll();
  queryClient.removeQueries({
    predicate: (q) => q.queryKey[0] !== "setup"
  });
  resetPlayerSessionForTests();
}
