import { PageHeader } from "@/components/ui/empty";
import { AdminTabs } from "../adminUi";
import { AuditTab } from "./AuditTab";
import { ErrorsTab } from "./ErrorsTab";
import { JobsTab } from "./JobsTab";
import { LiveTab } from "./LiveTab";
import { LogsTab } from "./LogsTab";

export const activityTabs = ["live", "jobs", "logs", "audit", "errors"] as const;
export type ActivityTab = (typeof activityTabs)[number];

export function AdminActivity() {
  return (
    <div>
      <PageHeader title="Activity" description="What is happening now, and a searchable record of who did what, when, from where, and whether it worked." />
      <AdminTabs<ActivityTab>
        fallback="live"
        tabs={[
          { id: "live", label: "Live", content: <LiveTab /> },
          { id: "jobs", label: "Jobs & Workers", content: <JobsTab /> },
          { id: "logs", label: "Logs", content: <LogsTab /> },
          { id: "audit", label: "Audit", content: <AuditTab /> },
          { id: "errors", label: "Errors", content: <ErrorsTab /> }
        ]}
      />
    </div>
  );
}
