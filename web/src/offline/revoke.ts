import { clearOfflineCache } from "./cache";
import { revokeOfflineTokens } from "./tokens";

export async function revokeDeviceAndClear() {
  try {
    await revokeOfflineTokens();
  } finally {
    await clearOfflineCache();
  }
}
