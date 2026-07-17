"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertCircle, ArrowLeft, Box, CheckCircle2, FileLock2, Image as ImageIcon, RefreshCw } from "lucide-react";
import Link from "next/link";
import { useParams } from "next/navigation";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { jobPollingInterval } from "@/lib/ai-job-polling";
import { apiRequest } from "@/lib/api";
import { useLanguage } from "@/lib/i18n";
import type { components } from "@/lib/openapi-types";
import { formatDateTime } from "@/lib/utils";

type Job = components["schemas"]["AIJob"];
type JobStatus = Job["status"];
type ItemStatus = components["schemas"]["AIJobItem"]["status"];

function statusVariant(status: JobStatus | ItemStatus) {
  if (status === "completed") return "success" as const;
  if (status === "failed" || status === "cancelled") return "danger" as const;
  if (status === "partial") return "neutral" as const;
  return "warning" as const;
}

export default function AIJobDetailPage() {
  const { jobId } = useParams<{ jobId: string }>();
  const { language, t } = useLanguage();
  const zh = language === "zh";
  const query = useQuery({
    queryKey: ["ai-jobs", jobId],
    queryFn: () => apiRequest<Job>(`/ai-jobs/${encodeURIComponent(jobId)}`),
    refetchInterval: ({ state }) => jobPollingInterval(state.data),
  });
  const labels: Record<JobStatus | ItemStatus, string> = { queued: t("queued"), running: t("running"), partial: t("partial"), completed: t("completed"), failed: t("failed"), cancelled: t("cancelled") };

  if (query.isLoading) return <div className="h-72 animate-pulse rounded-lg bg-muted" aria-label={t("aiJobDetailLoading")} />;
  if (query.isError || !query.data) return <div className="flex flex-col items-center gap-3 rounded-lg border border-danger/30 bg-danger/5 p-10 text-center" role="alert"><AlertCircle className="h-6 w-6 text-danger" /><p className="text-sm text-danger">{t("aiJobDetailLoadError")}</p><Button className="min-h-11" onClick={() => query.refetch()} variant="secondary"><RefreshCw className="h-4 w-4" />{t("retry")}</Button></div>;

  const job = query.data;
  const snapshot = job.input_snapshot;
  const active = jobPollingInterval(job) !== false;
  const completed = job.items.filter((item) => item.status === "completed").length;
  const progress = job.items.length ? Math.round((completed / job.items.length) * 100) : 0;

  return <div className="space-y-6">
    <div className="flex items-start gap-3"><Button asChild aria-label={t("backToAIJobs")} className="min-h-11 min-w-11" size="icon" variant="ghost"><Link href="/ai-jobs"><ArrowLeft className="h-4 w-4" /></Link></Button><div className="min-w-0 flex-1"><div className="flex flex-wrap items-center gap-2"><h1 className="text-2xl font-semibold">{snapshot.sku.code} · {zh ? "AI 任务" : "AI job"}</h1><Badge variant={statusVariant(job.status)}>{labels[job.status]}</Badge></div><p className="mt-1 break-all font-mono text-xs text-muted-foreground">{job.public_id}</p></div></div>

    <Card><CardHeader><div className="flex items-center justify-between gap-3"><CardTitle>{t("aiJobProgress")}</CardTitle><span className="text-sm tabular-nums text-muted-foreground">{completed}/{job.items.length}</span></div></CardHeader><CardContent><div aria-label={`${progress}%`} aria-valuemax={100} aria-valuemin={0} aria-valuenow={progress} className="h-2 overflow-hidden rounded-full bg-muted" role="progressbar"><div className="h-full bg-primary transition-[width] motion-reduce:transition-none" style={{ width: `${progress}%` }} /></div><p aria-live="polite" className="mt-3 flex items-center gap-2 text-sm text-muted-foreground">{active ? <><RefreshCw className="h-4 w-4 animate-spin motion-reduce:animate-none" />{t("activePolling")}</> : <><CheckCircle2 className="h-4 w-4 text-success" />{t("pollingStopped")}</>}</p></CardContent></Card>

    <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_360px]">
      <section className="space-y-3" aria-labelledby="items-title"><div className="flex items-center gap-2"><Box className="h-4 w-4 text-primary" /><h2 className="font-semibold" id="items-title">{t("selectedItems")}</h2></div>{job.items.map((item) => <article className="rounded-lg border border-border bg-card p-4" key={item.public_id}><div className="flex flex-wrap items-start justify-between gap-3"><div><h3 className="font-medium">{item.slot_snapshot.name[zh ? "zh" : "en"]}</h3><p className="mt-1 text-xs text-muted-foreground">{item.slot_key} · {item.kind}</p></div><Badge variant={statusVariant(item.status)}>{labels[item.status]}</Badge></div><dl className="mt-4 grid gap-3 text-sm sm:grid-cols-3"><div><dt className="text-muted-foreground">{t("approvedAssets")}</dt><dd className="mt-1 font-medium tabular-nums">{item.selected_input_asset_ids.length}</dd></div><div><dt className="text-muted-foreground">{t("attemptCount")}</dt><dd className="mt-1 font-medium tabular-nums">{item.attempt_count}</dd></div><div><dt className="text-muted-foreground">{t("createdAt")}</dt><dd className="mt-1 font-medium">{formatDateTime(item.created_at)}</dd></div></dl>{item.safe_error ? <div className="mt-4 rounded-md border border-danger/30 bg-danger/5 px-3 py-2 text-sm text-danger" role="alert"><span className="font-medium">{t("safeError")}：</span>{item.safe_error}</div> : null}</article>)}</section>

      <aside className="xl:sticky xl:top-20 xl:self-start"><Card><CardHeader><div className="flex items-center gap-2"><FileLock2 className="h-4 w-4 text-primary" /><CardTitle>{t("snapshotSummary")}</CardTitle></div></CardHeader><CardContent className="space-y-4"><p className="text-xs leading-5 text-muted-foreground">{t("snapshotRedactedHelp")}</p><dl className="divide-y divide-border text-sm"><Summary label={zh ? "商品" : "Product"} value={snapshot.product.name} /><Summary label={t("brand")} value={snapshot.product.brand || "—"} /><Summary label={t("category")} value={zh ? snapshot.product.category.name_zh : snapshot.product.category.name_en} /><Summary label={t("spec")} value={[snapshot.sku.color, snapshot.sku.size].filter(Boolean).join(" / ") || "—"} /><Summary label={t("platform")} value={snapshot.target_platform} /><Summary label={t("locale")} value={snapshot.locale} /><Summary label={t("sopVersion")} value={`${snapshot.sop.name[zh ? "zh" : "en"]} · V${snapshot.sop.version_number}`} /><Summary label={t("templateVersion")} value={`V${snapshot.template.version_number}`} /><Summary label={t("approvedAssets")} value={`${snapshot.selected_assets.length} ${t("approvedImages")}`} /><Summary label={t("createdAt")} value={formatDateTime(job.created_at)} /></dl><div className="flex items-start gap-2 rounded-md bg-muted p-3 text-xs text-muted-foreground"><ImageIcon className="mt-0.5 h-4 w-4 shrink-0" />{zh ? "图片仅按数量和审核状态汇总。" : "Images are summarized only by count and approval state."}</div></CardContent></Card></aside>
    </div>
  </div>;
}

function Summary({ label, value }: { label: string; value: string }) {
  return <div className="grid grid-cols-[120px_minmax(0,1fr)] gap-3 py-3 first:pt-0 last:pb-0"><dt className="text-muted-foreground">{label}</dt><dd className="min-w-0 break-words text-right font-medium">{value}</dd></div>;
}
