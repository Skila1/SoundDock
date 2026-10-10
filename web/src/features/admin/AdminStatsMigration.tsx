import { PageHeader } from "@/components/ui/empty";
import { AdminTabs } from "./adminUi";
import { ListenComparePanel } from "./ListenCompare";
import { StatsRebuildPanel } from "./StatsRebuild";

/** One-time switch of listening stats to the new data. Linked from the Dashboard while pending. */
export function AdminStatsMigration() {
  return (
    <div>
      <PageHeader title="Stats migration" description="A one-time step that moves Home and Stats onto the new listening data." />
      <AdminTabs
        fallback="rebuild"
        tabs={[
          { id: "rebuild", label: "Rebuild", content: <StatsRebuildPanel /> },
          { id: "compare", label: "Compare", content: <ListenComparePanel /> }
        ]}
      />
    </div>
  );
}
