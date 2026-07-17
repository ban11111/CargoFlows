import type { components } from "@/lib/openapi-types";

type Job = components["schemas"]["AIJob"];

export function jobPollingInterval(job: Job | undefined): 2000 | false {
  return job?.items.some((item) => item.status === "queued" || item.status === "running") ? 2000 : false;
}
