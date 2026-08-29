import { queryClient } from "@/app/providers";
import { resetPlayerSessionForTests } from "@/stores/player";

/** Drop the previous account's cache and player session after login or setup. */
export function resetClientSession() {
  queryClient.removeQueries({
    predicate: (q) => q.queryKey[0] !== "setup"
  });
  resetPlayerSessionForTests();
}
