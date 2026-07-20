"use client";

import { useQuery } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { Bot, Filter, Play } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { DataTable } from "@/components/data-table";
import { ErrorNotice } from "@/components/error-notice";
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
  const { language, t } = useLanguage();
  const zh = language === "zh";
  const [creator, setCreator] = useState("");
  const [model, setModel] = useState("");
  const [apiMode, setAPIMode] = useState("");
  const query = new URLSearchParams();
  if (creator.trim()) query.set("created_by", creator.trim());
  if (model.trim()) query.set("model", model.trim());
  if (apiMode) query.set("api_mode", apiMode);
  const jobsQuery = useQuery({
    queryKey: ["ai-jobs", creator, model, apiMode],
    queryFn: () => apiRequest<{ data: AiJob[] }>(`/ai-jobs${query.size ? `?${query.toString()}` : ""}`),
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
    { accessorFn: (row) => `${row.created_by?.name ?? ""} ${row.created_by?.email ?? ""}`, id: "creator", header: zh ? "发起人" : "Created by", cell: ({ row }) => row.original.created_by?.public_id ? <div><p className="font-medium">{row.original.created_by.name}</p><p className="text-xs text-muted-foreground">{row.original.created_by.email}</p></div> : <span className="text-muted-foreground">{zh ? "旧任务未记录" : "Not recorded for legacy job"}</span> },
    { accessorFn: (row) => `${row.model_snapshot?.text_model ?? ""} ${row.model_snapshot?.image_responses_model ?? ""} ${row.model_snapshot?.image_generation_model ?? ""}`, id: "models", header: zh ? "模型" : "Models", cell: ({ row }) => row.original.model_snapshot?.text_model ? <div className="space-y-1 font-mono text-xs"><p>{row.original.model_snapshot.text_model}</p><p className="text-muted-foreground">{row.original.model_snapshot.image_api_mode === "images" ? row.original.model_snapshot.image_generation_model : row.original.model_snapshot.image_responses_model} · {row.original.model_snapshot.image_api_mode}</p></div> : <span className="text-muted-foreground">{zh ? "旧任务未记录" : "Not recorded"}</span> },
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
          <p className="mb-2 text-[11px] font-bold uppercase tracking-[0.16em] text-primary">CargoFlows · Automation</p>
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
          <div className="mb-5 grid gap-3 rounded-lg border border-border bg-muted/30 p-4 md:grid-cols-[1fr_1fr_180px]" aria-label={zh ? "任务筛选" : "Job filters"}>
            <label className="space-y-1 text-sm"><span className="flex items-center gap-1 font-medium"><Filter className="h-3.5 w-3.5" />{zh ? "发起人" : "Creator"}</span><input className="min-h-11 w-full rounded-md border border-input bg-background px-3" onChange={(event) => setCreator(event.target.value)} placeholder={zh ? "姓名、邮箱或用户 ID" : "Name, email, or user ID"} value={creator} /></label>
            <label className="space-y-1 text-sm"><span className="font-medium">{zh ? "模型" : "Model"}</span><input className="min-h-11 w-full rounded-md border border-input bg-background px-3 font-mono" onChange={(event) => setModel(event.target.value)} placeholder="gpt-5.6 / gpt-image-2" value={model} /></label>
            <label className="space-y-1 text-sm"><span className="font-medium">{zh ? "图片调用方式" : "Image API mode"}</span><select className="min-h-11 w-full rounded-md border border-input bg-background px-3" onChange={(event) => setAPIMode(event.target.value)} value={apiMode}><option value="">{zh ? "全部" : "All"}</option><option value="responses">Responses</option><option value="images">Images API</option></select></label>
          </div>
          {jobsQuery.isLoading ? <div className="h-32 animate-pulse rounded-lg bg-muted" aria-label={t("aiJobsLoading")} /> : null}
          {jobsQuery.isError ? <ErrorNotice actionLabel={t("retry")} message={t("aiJobsLoadError")} onAction={() => jobsQuery.refetch()} title={zh ? "任务列表加载失败" : "Could not load AI jobs"} /> : null}
          {jobsQuery.isSuccess ? <DataTable columns={columns} data={jobsQuery.data.data} searchPlaceholder={t("searchJobs")} /> : null}
        </CardContent>
      </Card>
    </div>
  );
}
