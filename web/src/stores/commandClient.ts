export type ControlBody = {
  action: string;
  extra: Record<string, unknown>;
  command_id: string;
  device_id?: string;
};

export type ControlSender = (body: ControlBody) => Promise<unknown>;

type Receipt = {
  action: string;
  extraKey: string;
  result?: unknown;
  inFlight?: Promise<unknown>;
};

function extraKey(extra: Record<string, unknown>): string {
  const rest: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(extra)) {
    if (k === "command_id") continue;
    rest[k] = v;
  }
  return JSON.stringify(rest);
}

export function newCommandId(): string {
  try {
    return crypto.randomUUID();
  } catch {
    return `cmd-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
  }
}

export function createCommandClient(send: ControlSender) {
  const receipts = new Map<string, Receipt>();

  return {
    newCommandId,
    /** Replay a duplicate command_id with the same payload instead of sending twice. */
    async control(action: string, extra: Record<string, unknown> = {}, deviceId?: string): Promise<unknown> {
      const commandId =
        typeof extra.command_id === "string" && extra.command_id.trim() ? extra.command_id.trim() : newCommandId();
      const extraOut = { ...extra, command_id: commandId };
      const key = extraKey(extraOut);
      const hit = receipts.get(commandId);
      if (hit && hit.action === action && hit.extraKey === key) {
        if (hit.result !== undefined) return hit.result;
        if (hit.inFlight) return hit.inFlight;
      }
      const body: ControlBody = { action, extra: extraOut, command_id: commandId };
      if (deviceId) body.device_id = deviceId;
      const inFlight = send(body).then(
        (result) => {
          receipts.set(commandId, { action, extraKey: key, result });
          return result;
        },
        (err) => {
          receipts.delete(commandId);
          throw err;
        }
      );
      receipts.set(commandId, { action, extraKey: key, inFlight });
      return inFlight;
    },
    peek(commandId: string) {
      return receipts.get(commandId);
    },
    reset() {
      receipts.clear();
    }
  };
}
