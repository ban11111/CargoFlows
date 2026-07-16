"use client";

import type { ColumnDef } from "@tanstack/react-table";
import { Bot, Play } from "lucide-react";
import { DataTable } from "@/components/data-table";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useLanguage } from "@/lib/i18n";
import { aiJobs } from "@/lib/mock-data";
import type { AiJob, AiJobStatus } from "@/lib/types";
import { formatDateTime } from "@/lib/utils";

function JobBadge({ status, label }: { status: AiJobStatus; label: string }) {
  const variant = status === "failed" ? "danger" : status === "completed" ? "success" : "warning";
  return <Badge variant={variant}>{label}</Badge>;
}

export default function AiJobsPage() {
  const { t } = useLanguage();
  const labels: Record<AiJobStatus, string> = {
    pending: t("pendingSubmit"),
    queued: t("queued"),
    running: t("running"),
    completed: t("completed"),
    failed: t("failed"),
  };
  const columns: ColumnDef<AiJob>[] = [
    { accessorKey: "id", header: t("job") },
    { accessorKey: "skuCode", header: t("sku") },
    { accessorKey: "targetPlatform", header: t("platform") },
    { accessorKey: "inputAssets", header: t("inputAssets") },
    {
      accessorKey: "status",
      header: t("status"),
      cell: ({ row }) => <JobBadge status={row.original.status} label={labels[row.original.status]} />,
    },
    {
      accessorKey: "createdAt",
      header: t("createdAt"),
      cell: ({ row }) => formatDateTime(row.original.createdAt),
    },
  ];

  return (
    <div className="space-y-6">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
        <div>
          <h1 className="text-2xl font-semibold">{t("aiJobs")}</h1>
          <p className="mt-1 text-sm text-muted-foreground">{t("aiJobsDesc")}</p>
        </div>
        <Button>
          <Play className="h-4 w-4" />
          {t("newJob")}
        </Button>
      </div>
      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <Bot className="h-4 w-4 text-primary" />
            <CardTitle>{t("jobList")}</CardTitle>
          </div>
        </CardHeader>
        <CardContent>
          <DataTable columns={columns} data={aiJobs} searchPlaceholder={t("searchJobs")} />
        </CardContent>
      </Card>
    </div>
  );
}
