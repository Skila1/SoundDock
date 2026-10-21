import { useEffect } from "react";
import { useSearchParams } from "react-router-dom";
import { PageHeader } from "@/components/ui/empty";
import { AdminTabs } from "../adminUi";
import { JobsPanel } from "./JobsPanel";
import { LogStream } from "./LogStream";
import { NowPlaying } from "./NowPlaying";
import { legacyActivityParams, type ActivityTab } from "./streamModel";

export { activityTabs, type ActivityTab } from "./streamModel";

export function AdminActivity() {
  const [params, setParams] = useSearchParams();
  const legacy = legacyActivityParams(params)?.toString();

  // Old links (?tab=live, jobs, audit, errors) land on the new tab and filter.
  useEffect(() => {
    if (legacy != null) setParams(new URLSearchParams(legacy), { replace: true });
  }, [legacy, setParams]);

  return (
    <div>
      <PageHeader title="Activity" description="Who is listening, what the server is working on, and a searchable record of everything that happened." />
      {legacy == null && (
        <AdminTabs<ActivityTab>
          fallback="activity"
          tabs={[
            {
              id: "activity",
              label: "Activity",
              content: (
                <div className="space-y-6">
                  <NowPlaying />
                  <JobsPanel />
                </div>
              )
            },
            { id: "logs", label: "Logs", content: <LogStream /> }
          ]}
        />
      )}
    </div>
  );
}
