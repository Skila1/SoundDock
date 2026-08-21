import { useState } from "react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Select } from "@/components/ui/select";
import { PageHeader } from "@/components/ui/empty";
import { toast } from "sonner";
import type { User } from "@/types/api";

export function ProfilePage({ user, onRefresh }: { user: User; onRefresh: () => void }) {
  const [display, setDisplay] = useState(user.display_name || "");
  const [rg, setRg] = useState(user.replaygain_mode || "off");
  const [xf, setXf] = useState(String(user.crossfade_seconds || 0));
  return (
    <div className="max-w-lg">
      <PageHeader title="Profile" />
      <form
        className="space-y-4 rounded-xl border border-border bg-surface-1 p-5"
        onSubmit={async (e) => {
          e.preventDefault();
          await api.patch("/api/v1/me", { display_name: display, replaygain_mode: rg, crossfade_seconds: Number(xf) });
          toast.success("Settings saved");
          onRefresh();
        }}
      >
        <Field label="Display name"><Input value={display} onChange={(e) => setDisplay(e.target.value)} /></Field>
        <Field label="ReplayGain">
          <Select value={rg} onValueChange={setRg} options={[{ value: "off", label: "Off" }, { value: "track", label: "Track" }, { value: "album", label: "Album" }]} />
        </Field>
        <Field label="Crossfade (seconds)"><Input type="number" min={0} max={12} value={xf} onChange={(e) => setXf(e.target.value)} /></Field>
        <Button type="submit">Save</Button>
      </form>
      <form
        className="mt-6 space-y-4 rounded-xl border border-border bg-surface-1 p-5"
        onSubmit={async (e) => {
          e.preventDefault();
          const fd = new FormData(e.currentTarget);
          await api.post("/api/v1/me/password", { current: fd.get("current"), new: fd.get("next") });
          toast.success("Password updated");
          e.currentTarget.reset();
        }}
      >
        <h2 className="font-semibold">Password</h2>
        <Field label="Current"><Input name="current" type="password" autoComplete="current-password" required /></Field>
        <Field label="New"><Input name="next" type="password" autoComplete="new-password" required /></Field>
        <Button type="submit">Change password</Button>
      </form>
      <div className="mt-6 flex gap-2">
        <Button variant="outline" onClick={() => (window.location.href = "/settings/connected")}>Connected services</Button>
        <Button variant="outline" onClick={() => window.open("/api/v1/me/export")}>Export my data</Button>
        <Button variant="ghost" onClick={() => api.post("/api/v1/auth/logout-all").then(() => toast.success("Sessions revoked"))}>Revoke other sessions</Button>
      </div>
    </div>
  );
}
