"use client";

import { useQuery } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { AlertCircle, Bot, Play, RefreshCw } from "lucide-react";
import Link from "next/link";
import { DataTable } from "@/components/data-table";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { apiRequest } from "@/lib/api";
import { useLanguage } from "@/lib/i18n";
import type { AiJob, AiJobStatus } from "@/lib/types";
import { formatDateTime } from "@/lib/utils";

function JobBadge({ status, label }: { status: AiJobStatus; label: string }) {
  const variant = status === "failed" || status === "cancelled" ? "danger" : status === "completed" ? "success" : status === "partial" ? "neutral" : "warning";
  return <Badge variant={variant}>{label}</Badge>;
}

export default function AiJobsPage() {
  const { t } = useLanguage();
  const jobsQuery = useQuery({
    queryKey: ["ai-jobs"],
    queryFn: () => apiRequest<{ data: AiJob[] }>("/ai-jobs"),
  });
  const labels: Record<AiJobStatus, string> = {
    queued: t("queued"),
    running: t("running"),
    partial: t("partial"),
    completed: t("completed"),
    failed: t("failed"),
    cancelled: t("cancelled"),
  };
  const columns: ColumnDef<AiJob>[] = [
    { accessorKey: "public_id", header: t("job"), cell: ({ row }) => <span className="font-mono text-xs">{row.original.public_id.slice(0, 8)}</span> },
    { accessorFn: (row) => row.input_snapshot.sku.code, id: "sku", header: t("sku"), cell: ({ row }) => <Link className="font-medium text-primary hover:underline" href={`/ai-jobs/${row.original.public_id}`}>{row.original.input_snapshot.sku.code}</Link> },
    { accessorKey: "target_platform", header: t("platform") },
    { accessorFn: (row) => row.input_snapshot.selected_assets.length, id: "inputAssets", header: t("inputAssets") },
    {
      accessorKey: "status",
      header: t("status"),
      cell: ({ row }) => <JobBadge status={row.original.status} label={labels[row.original.status]} />,
    },
    {
      accessorKey: "created_at",
      header: t("createdAt"),
      cell: ({ row }) => formatDateTime(row.original.created_at),
    },
  ];

  return (
    <div className="space-y-6">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
        <div>
          <p className="mb-2 text-[11px] font-bold uppercase tracking-[0.16em] text-primary">CargoFlow · Automation</p>
          <h1 className="text-3xl font-bold tracking-tight text-navy sm:text-4xl">{t("aiJobs")}</h1>
          <p className="mt-2 text-sm text-muted-foreground">{t("aiJobsDesc")}</p>
        </div>
        <Button asChild className="min-h-11"><Link href="/ai-jobs/new"><Play className="h-4 w-4" />{t("newJob")}</Link></Button>
      </div>
      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <Bot className="h-4 w-4 text-primary" />
            <CardTitle>{t("jobList")}</CardTitle>
          </div>
        </CardHeader>
        <CardContent>
          {jobsQuery.isLoading ? <div className="h-32 animate-pulse rounded-lg bg-muted" aria-label={t("aiJobsLoading")} /> : null}
          {jobsQuery.isError ? <div className="flex flex-col items-center gap-3 rounded-lg border border-danger/30 bg-danger/5 p-8 text-center" role="alert"><AlertCircle className="h-5 w-5 text-danger" /><p className="text-sm text-danger">{t("aiJobsLoadError")}</p><Button className="min-h-11" onClick={() => jobsQuery.refetch()} variant="secondary"><RefreshCw className="h-4 w-4" />{t("retry")}</Button></div> : null}
          {jobsQuery.isSuccess ? <DataTable columns={columns} data={jobsQuery.data.data} searchPlaceholder={t("searchJobs")} /> : null}
        </CardContent>
      </Card>
    </div>
  );
}
