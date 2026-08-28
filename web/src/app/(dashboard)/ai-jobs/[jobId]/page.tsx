"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Box, CheckCircle2, FileLock2, Image as ImageIcon, RefreshCw, UserRound } from "lucide-react";
import Link from "next/link";
import { useParams } from "next/navigation";

import { TextResultWorkbench } from "@/components/ai/text-result-workbench";
import { ImageResultGallery } from "@/components/ai/image-result-gallery";
import { ErrorNotice } from "@/components/error-notice";
import { aiFailureMessage } from "@/lib/ai-failure-message";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { jobPollingInterval } from "@/lib/ai-job-polling";
import { ApiError, apiRequest } from "@/lib/api";
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

function outputLanguageLabel(locales: string[], zh: boolean) {
  if (locales.length === 2) return zh ? "双语（English → 简体中文）" : "Bilingual (English → Simplified Chinese)";
  return locales[0] === "en" ? "English" : (zh ? "简体中文" : "Simplified Chinese");
}

export default function AIJobDetailPage() {
  const { jobId } = useParams<{ jobId: string }>();
  const { language, t } = useLanguage();
  const zh = language === "zh";
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: ["ai-jobs", jobId],
    queryFn: () => apiRequest<Job>(`/ai-jobs/${encodeURIComponent(jobId)}`),
    refetchInterval: ({ state }) => jobPollingInterval(state.data),
  });
  const regenerateItem = useMutation({
    mutationFn: (itemID: string) => apiRequest<Job>(`/ai-jobs/${encodeURIComponent(jobId)}/items/${encodeURIComponent(itemID)}/regenerate`, { method: "POST" }),
    onSuccess(next) {
      queryClient.setQueryData(["ai-jobs", jobId], next);
      void queryClient.invalidateQueries({ queryKey: ["ai-text-results", jobId] });
      void queryClient.invalidateQueries({ queryKey: ["ai-jobs", jobId, "image-threads"] });
    },
  });
  const labels: Record<JobStatus | ItemStatus, string> = { queued: t("queued"), running: t("running"), partial: t("partial"), completed: t("completed"), failed: t("failed"), cancelled: t("cancelled") };

  if (query.isLoading) return <div className="h-72 animate-pulse rounded-lg bg-muted" aria-label={t("aiJobDetailLoading")} />;
  if (query.isError || !query.data) return <ErrorNotice actionLabel={t("retry")} message={t("aiJobDetailLoadError")} onAction={() => query.refetch()} title={zh ? "任务详情加载失败" : "Could not load job details"} />;

  const job = query.data;
  const creator = job.created_by ?? { public_id: "", name: "", email: "" };
  const modelSnapshot = job.model_snapshot ?? { text_model: "", image_api_mode: "" as const, image_responses_model: "", image_generation_model: "" };
  const snapshot = job.input_snapshot;
  const active = jobPollingInterval(job) !== false;
  const completed = job.items.filter((item) => item.status === "completed").length;
  const progress = job.items.length ? Math.round((completed / job.items.length) * 100) : 0;
	const failedItems = job.items.filter((item) => item.status === "failed");
	const firstFailure = failedItems.find((item) => item.failure)?.failure;
	const brandIcons = snapshot.brand_icons ?? [];

  return <div className="space-y-6">
    <div className="flex items-start gap-3"><Button asChild aria-label={t("backToAIJobs")} className="min-h-11 min-w-11" size="icon" variant="ghost"><Link href="/ai-jobs"><ArrowLeft className="h-4 w-4" /></Link></Button><div className="min-w-0 flex-1"><div className="flex flex-wrap items-center gap-2"><h1 className="text-2xl font-semibold">{snapshot.sku.code} · {zh ? "AI 任务" : "AI job"}</h1><Badge variant={statusVariant(job.status)}>{labels[job.status]}</Badge></div><p className="mt-1 break-all font-mono text-xs text-muted-foreground">{job.public_id}</p></div></div>

    <Card><CardHeader><div className="flex items-center justify-between gap-3"><CardTitle>{t("aiJobProgress")}</CardTitle><span className="text-sm tabular-nums text-muted-foreground">{completed}/{job.items.length}</span></div></CardHeader><CardContent><div aria-label={`${progress}%`} aria-valuemax={100} aria-valuemin={0} aria-valuenow={progress} className="h-2 overflow-hidden rounded-full bg-muted" role="progressbar"><div className="h-full bg-primary transition-[width] motion-reduce:transition-none" style={{ width: `${progress}%` }} /></div><p aria-live="polite" className="mt-3 flex items-center gap-2 text-sm text-muted-foreground">{active ? <><RefreshCw className="h-4 w-4 animate-spin motion-reduce:animate-none" />{t("activePolling")}</> : <><CheckCircle2 className="h-4 w-4 text-success" />{t("pollingStopped")}</>}</p></CardContent></Card>

	{firstFailure ? <ErrorNotice className="-mt-2" labels={failureLabels(zh)} message={`${zh ? `${failedItems.length} 个内容槽位失败。首个错误：` : `${failedItems.length} content slot(s) failed. First error: `}${aiFailureMessage(firstFailure.safe_message, firstFailure.code, zh)}`} recovery={recoveryText(firstFailure.recovery_action, zh)} stage={failureStageText(firstFailure.stage, zh)} title={zh ? "任务执行失败" : "Job execution failed"} /> : null}

	<Card><CardHeader><CardTitle>{zh ? "AI 双层成本" : "AI dual-ledger cost"}</CardTitle></CardHeader><CardContent><div className="grid gap-4 sm:grid-cols-3"><AuditDetail label="Token" value={(job.total_tokens ?? 0).toLocaleString()} /><AuditDetail label={zh ? "任务估算（冻结费率）" : "Task estimate (frozen rate)"} value={`$${Number(job.estimated_amount_usd ?? 0).toFixed(8)}`} /><AuditDetail label={zh ? "供应商聚合成本分摊" : "Reconciled aggregate allocation"} value={`$${Number(job.reconciled_amount_usd ?? 0).toFixed(8)} · ${job.reconciliation_status ?? "pending"}`} /></div><p className="mt-4 rounded-lg bg-muted/60 p-3 text-xs leading-5 text-muted-foreground">{zh ? "实际分摊按同一 UTC 日期成本桶与本地估算比例计算，并非 OpenAI 逐 Request ID 精确收费；reasoning token 是 output token 子集，不会重复计费。" : "The actual allocation is derived from the same UTC-day supplier bucket in proportion to local estimates; it is not exact per-request billing. Reasoning tokens are an output subset and are not billed twice."}</p></CardContent></Card>

	{brandIcons.length ? <Card><CardHeader><div className="flex items-center gap-2"><ImageIcon className="h-4 w-4 text-primary"/><CardTitle>{zh?"冻结的品牌图标参考":"Frozen brand mark references"}</CardTitle></div></CardHeader><CardContent><div className="flex flex-wrap gap-2">{brandIcons.map((icon)=><Badge key={icon.public_id} variant="neutral">{icon.name}</Badge>)}</div><p className="mt-3 text-xs text-muted-foreground">{zh?"任务会保持图标结构，配色可根据生成风格调整。":"The task preserves mark structure while allowing colors to adapt to the generation style."}</p></CardContent></Card>:null}

    <Card><CardHeader><div className="flex items-center gap-2"><UserRound className="h-4 w-4 text-primary" /><CardTitle>{zh ? "任务审计" : "Job audit"}</CardTitle></div></CardHeader><CardContent><dl className="grid gap-4 text-sm sm:grid-cols-2 lg:grid-cols-4"><AuditDetail label={zh ? "发起人" : "Created by"} value={creator.public_id ? creator.name : (zh ? "旧任务未记录" : "Not recorded for legacy job")} /><AuditDetail label={zh ? "邮箱" : "Email"} value={creator.email || "—"} /><AuditDetail label={zh ? "发起时间" : "Created at"} value={formatDateTime(job.created_at)} /><AuditDetail label={zh ? "图片调用方式" : "Image API mode"} value={modelSnapshot.image_api_mode ? (modelSnapshot.image_api_mode === "images" ? "Images API" : "Responses") : (zh ? "旧任务未记录" : "Not recorded")} /><AuditDetail label={zh ? "文字模型" : "Text model"} value={modelSnapshot.text_model || (zh ? "旧任务未记录" : "Not recorded")} mono /><AuditDetail label={zh ? "Responses 编排模型" : "Responses orchestration model"} value={modelSnapshot.image_responses_model || "—"} mono /><AuditDetail label={zh ? "Images API 图像模型" : "Images API image model"} value={modelSnapshot.image_generation_model || "—"} mono /><AuditDetail label={zh ? "发起人 ID" : "Creator ID"} value={creator.public_id || "—"} mono /></dl></CardContent></Card>

    <TextResultWorkbench job={job} language={language} />

    <ImageResultGallery active={active} itemLabels={Object.fromEntries(job.items.map((item) => [item.public_id, item.slot_snapshot.name[zh ? "zh" : "en"]]))} jobID={job.public_id} language={language} refreshKey={job.updated_at} />

    <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_360px]">
      <section className="space-y-3" aria-labelledby="items-title"><div className="flex items-center gap-2"><Box className="h-4 w-4 text-primary" /><h2 className="font-semibold" id="items-title">{t("selectedItems")}</h2></div>{job.items.map((item) => {
        const executions = item.executions ?? [];
        const latest = executions[executions.length - 1];
        const requestedModel = latest?.requested_model || item.failure?.model || "";
        const actualModel = latest?.actual_model || requestedModel;
        const apiMode = latest?.api_mode || item.failure?.api_mode || "";
        const canRegenerate = item.status === "failed";
        const regenerationPending = regenerateItem.isPending && regenerateItem.variables === item.public_id;
        const regenerationFailed = regenerateItem.isError && regenerateItem.variables === item.public_id;
        const failureAction = canRegenerate ? { actionLabel: regenerationPending ? (zh ? "正在重新生成…" : "Regenerating…") : (zh ? "重新生成" : "Regenerate"), actionPending: regenerationPending, onAction: () => regenerateItem.mutate(item.public_id) } : {};
        return <article className="rounded-lg border border-border bg-card p-3" key={item.public_id}><div className="flex flex-wrap items-start justify-between gap-2"><div><h3 className="font-medium leading-5">{item.slot_snapshot.name[zh ? "zh" : "en"]}</h3><p className="mt-0.5 text-xs text-muted-foreground">{item.slot_key} · {item.kind}</p></div><Badge variant={statusVariant(item.status)}>{labels[item.status]}</Badge></div><dl className="mt-3 grid grid-cols-3 gap-2 text-xs"><AuditDetail label={t("attemptCount")} value={String(item.attempt_count)} /><AuditDetail label={zh ? "实际模型" : "Actual model"} value={actualModel || (zh ? "旧任务未记录" : "Not recorded")} mono /><AuditDetail label={zh ? "API 路径" : "API path"} value={apiMode ? apiPath(apiMode, latest?.operation) : (zh ? "旧任务未记录" : "Not recorded")} mono /></dl>{item.failure ? <ErrorNotice {...failureAction} className="mt-3" code={item.failure.code} labels={failureLabels(zh)} message={aiFailureMessage(item.failure.safe_message || item.safe_error, item.failure.code, zh)} recovery={recoveryText(item.failure.recovery_action, zh)} requestId={item.failure.provider_request_id} stage={failureStageText(item.failure.stage, zh)} title={zh ? "失败原因" : "Failure reason"} /> : item.safe_error ? <ErrorNotice {...failureAction} className="mt-3" labels={failureLabels(zh)} message={item.safe_error} recovery={recoveryText(canRegenerate ? "retry_later" : "contact_support", zh)} title={zh ? "失败原因" : "Failure reason"} /> : null}{regenerationFailed ? <p className="mt-2 text-xs leading-5 text-danger" role="alert">{regenerationErrorText(regenerateItem.error, zh)}</p> : null}{executions.length ? <details className="mt-3 rounded-md border border-border"><summary className="cursor-pointer px-3 py-2 text-sm font-medium">{zh ? `执行记录（${executions.length}）` : `Execution history (${executions.length})`}</summary><div className="overflow-x-auto border-t border-border"><table className="w-full min-w-[1050px] text-left text-xs"><thead className="bg-muted/50 text-muted-foreground"><tr><th className="p-3">#</th><th className="p-3">{zh ? "实际模型" : "Actual"}</th><th className="p-3">API / Tier</th><th className="p-3">Request ID</th><th className="p-3">{zh ? "Token 明细" : "Token breakdown"}</th><th className="p-3">{zh ? "估算 USD" : "Estimated USD"}</th><th className="p-3">{zh ? "状态" : "Status"}</th></tr></thead><tbody>{executions.map((execution) => <tr className="border-t border-border align-top" key={execution.public_id}><td className="p-3 tabular-nums">{execution.attempt_number}</td><td className="p-3 font-mono">{execution.actual_model || execution.requested_model || "—"}</td><td className="p-3 font-mono">{apiPath(execution.api_mode, execution.operation)}<span className="block text-muted-foreground">{execution.service_tier}</span></td><td className="p-3 font-mono">{execution.provider_request_id || "—"}</td><td className="p-3 tabular-nums"><p>input text {execution.input_text_tokens}</p><p>cached {execution.cached_input_tokens} · input image {execution.input_image_tokens}</p><p>output text {execution.output_text_tokens} · output image {execution.output_image_tokens}</p><p>reasoning {execution.reasoning_tokens} ⊂ output · total {execution.total_tokens}</p></td><td className="p-3 tabular-nums">${Number(execution.estimated_amount_usd ?? 0).toFixed(8)}<Badge className="mt-1 block w-fit" variant={execution.pricing_status === "priced" ? "success" : "warning"}>{execution.pricing_status}</Badge></td><td className="p-3">{execution.status}</td></tr>)}</tbody></table></div></details> : null}</article>;
      })}</section>

      <aside className="xl:sticky xl:top-20 xl:self-start"><Card><CardHeader><div className="flex items-center gap-2"><FileLock2 className="h-4 w-4 text-primary" /><CardTitle>{t("snapshotSummary")}</CardTitle></div></CardHeader><CardContent className="space-y-4"><p className="text-xs leading-5 text-muted-foreground">{t("snapshotRedactedHelp")}</p><dl className="divide-y divide-border text-sm"><Summary label={zh ? "商品" : "Product"} value={snapshot.product.name} /><Summary label={t("brand")} value={snapshot.product.brand || "—"} /><Summary label={t("category")} value={zh ? snapshot.product.category.name_zh : snapshot.product.category.name_en} /><Summary label={t("spec")} value={[snapshot.sku.color, snapshot.sku.size].filter(Boolean).join(" / ") || "—"} /><Summary label={t("platform")} value={snapshot.target_platform} /><Summary label={zh ? "输出语言" : "Output language"} value={outputLanguageLabel(job.output_locales ?? [job.locale], zh)} /><Summary label={t("sopVersion")} value={`${snapshot.sop.name[zh ? "zh" : "en"]} · V${snapshot.sop.version_number}`} /><Summary label={t("templateVersion")} value={`V${snapshot.template.version_number}`} /><Summary label={t("approvedAssets")} value={`${snapshot.selected_assets.length} ${t("approvedImages")}`} /><Summary label={t("createdAt")} value={formatDateTime(job.created_at)} /></dl><div className="flex items-start gap-2 rounded-md bg-muted p-3 text-xs text-muted-foreground"><ImageIcon className="mt-0.5 h-4 w-4 shrink-0" />{zh ? "图片仅按数量和审核状态汇总。" : "Images are summarized only by count and approval state."}</div></CardContent></Card></aside>
    </div>
  </div>;
}

function Summary({ label, value }: { label: string; value: string }) {
  return <div className="grid grid-cols-[120px_minmax(0,1fr)] gap-3 py-3 first:pt-0 last:pb-0"><dt className="text-muted-foreground">{label}</dt><dd className="min-w-0 break-words text-right font-medium">{value}</dd></div>;
}

function AuditDetail({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return <div><dt className="text-muted-foreground">{label}</dt><dd className={`mt-1 break-words font-medium ${mono ? "font-mono text-xs" : ""}`}>{value}</dd></div>;
}

function apiPath(mode: string, operation?: string) {
  if (mode === "responses") return "/v1/responses";
  return operation === "edit" ? "/v1/images/edits" : "/v1/images/generations";
}

function recoveryText(action: string, zh: boolean) {
  const copy: Record<string, [string, string]> = {
    review_openai_settings: ["请到 OpenAI 设置检查模型与 API 路径是否兼容，并重新验证凭据。", "Review the model/API path compatibility in OpenAI settings and verify the credential again."],
    retry_later: ["可能是限流、网络或服务暂时不可用，请稍后重试。", "This may be temporary rate limiting, network trouble, or service unavailability. Retry later."],
    adjust_input: ["请检查输入素材和内容要求，调整后创建新任务。", "Review the input assets and content requirements, then create a new job."],
    contact_support: ["请携带页面显示的技术编号联系管理员。", "Contact an administrator with the technical ID shown here."],
    create_new_job: ["配置修正后，请创建新任务；历史任务不会被改写。", "After correcting the configuration, create a new job; history will not be rewritten."],
  };
  const value = copy[action] ?? copy.contact_support;
  return value[zh ? 0 : 1];
}

function failureLabels(zh: boolean) {
	return zh ? { stage: "出错阶段", reason: "具体原因", recovery: "建议处理", diagnostics: "技术诊断信息" } : { stage: "Failed at", reason: "What happened", recovery: "What to do", diagnostics: "Technical details" };
}

function failureStageText(stage: string | undefined, zh: boolean) {
	const copy: Record<string, [string, string]> = {
		input_validation: ["输入校验", "Input validation"], snapshot_validation: ["任务快照校验", "Job snapshot validation"], source_assets: ["输入素材读取", "Source asset loading"], prompt_compilation: ["Prompt 编译", "Prompt compilation"], provider_configuration: ["OpenAI 配置", "OpenAI configuration"], provider_request: ["OpenAI 请求", "OpenAI request"], content_safety: ["内容安全检查", "Content safety check"], result_storage: ["结果存储", "Result storage"], task_execution: ["任务执行", "Task execution"],
	};
	const value = copy[stage ?? "task_execution"] ?? copy.task_execution;
	return value[zh ? 0 : 1];
}

function regenerationErrorText(error: unknown, zh: boolean) {
	if (error instanceof ApiError) {
		return zh ? `重新生成失败：${error.message}（${error.code}）` : `Regeneration failed: ${error.message} (${error.code})`;
	}
	return zh ? "重新生成失败，请稍后重试。" : "Regeneration failed. Try again later.";
}
