"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, CheckCircle2, ClipboardCheck, Eye, FilePenLine, LoaderCircle, RotateCw, Search, Send, Sparkles, X } from "lucide-react";
import { useMemo, useState } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { apiRequest } from "@/lib/api";
import type { components } from "@/lib/openapi-types";

type Job = components["schemas"]["AIJob"];
type Item = components["schemas"]["AIJobItem"];
type Result = components["schemas"]["AITextResult"];
type Preview = components["schemas"]["AITextApplicationPreview"];
type Application = components["schemas"]["AITextApplicationResult"];
type Structured = Record<string, unknown>;

type Text = typeof zhText;

const zhText = {
  eyebrow: "文字内容工作台", title: "候选、审核与正式应用", description: "生成结果保持原样留档。你可以编辑副本、选择唯一有效候选，并在查看差异后写入正式平台内容。",
  loading: "正在载入文字候选…", loadError: "文字候选载入失败。", retry: "重试", empty: "文字槽位已完成，但暂时没有可审核候选。",
  candidate: "候选", effective: "当前采用", raw: "原始生成", edited: "已编辑", titleField: "商品标题", keywords: "关键词（逗号分隔）", shortDescription: "短描述", longDescription: "长描述", sellingPoints: "卖点（每行一条）", searchKeywords: "搜索关键词（逗号分隔）",
  save: "保存编辑", approve: "批准候选", reject: "拒绝候选", preview: "预览应用", apply: "应用到正式内容", applied: "已应用", saving: "正在保存", actionError: "操作失败。请检查内容或重试。",
  states: { candidate: "待审核", approved: "已批准", rejected: "已拒绝" }, before: "应用前", after: "应用后", revision: "Revision", noFormalContent: "尚无正式内容", previewHelp: "应用只更新当前槽位对应的正式字段，并创建不可变 revision。",
  sourceHint: "来源字段仅用于审计提示；服务端会按冻结商品数据重新校验。", generated: "AI 原稿", changed: "编辑稿",
};

const enText: Text = {
  eyebrow: "Text content workbench", title: "Candidates, review, and formal application", description: "Generated output stays immutable. Edit a copy, select one effective candidate, then review the diff before writing formal platform content.",
  loading: "Loading text candidates…", loadError: "Text candidates could not be loaded.", retry: "Retry", empty: "The text slots finished, but there are no reviewable candidates yet.",
  candidate: "Candidate", effective: "Effective", raw: "Raw generation", edited: "Edited", titleField: "Product title", keywords: "Keywords (comma separated)", shortDescription: "Short description", longDescription: "Long description", sellingPoints: "Selling points (one per line)", searchKeywords: "Search keywords (comma separated)",
  save: "Save edit", approve: "Approve candidate", reject: "Reject candidate", preview: "Preview application", apply: "Apply to formal content", applied: "Applied", saving: "Saving", actionError: "The action failed. Check the content or try again.",
  states: { candidate: "Review", approved: "Approved", rejected: "Rejected" }, before: "Before", after: "After", revision: "Revision", noFormalContent: "No formal content yet", previewHelp: "Application updates only the formal fields for this slot and creates an immutable revision.",
  sourceHint: "Source fields are audit hints only; the server revalidates against the frozen product data.", generated: "AI draft", changed: "Edited draft",
};

export function TextResultWorkbench({ job, language }: { job: Job; language: "zh" | "en" }) {
  const text = language === "zh" ? zhText : enText;
  const queryClient = useQueryClient();
  const textItems = useMemo(() => job.items.filter((item) => item.kind === "title" || item.kind === "seo_description"), [job.items]);
  const itemByID = useMemo(() => new Map(textItems.map((item) => [item.public_id, item])), [textItems]);
  const key = ["ai-text-results", job.public_id, job.updated_at] as const;
  const results = useQuery({
    queryKey: key,
    queryFn: () => apiRequest<{ data: Result[] }>(`/ai-jobs/${encodeURIComponent(job.public_id)}/text-results`),
    enabled: textItems.length > 0,
    refetchInterval: job.status === "queued" || job.status === "running" ? 2000 : false,
  });
  const [preview, setPreview] = useState<{ resultID: string; value: Preview } | null>(null);
  const [applied, setApplied] = useState<Record<string, Application>>({});

  const replaceResult = (next: Result) => queryClient.setQueryData<{ data: Result[] }>(key, (current) => ({ data: (current?.data ?? []).map((value) => value.public_id === next.public_id ? next : value.job_item_id === next.job_item_id && next.effective ? { ...value, effective: false } : value) }));
  const edit = useMutation({
    mutationFn: ({ itemID, resultID, structured }: { itemID: string; resultID: string; structured: Structured }) => apiRequest<Result>(resultPath(job.public_id, itemID, resultID), { method: "PATCH", body: JSON.stringify({ structured }) }),
    onSuccess(next) { replaceResult(next); setPreview(null); },
  });
  const transition = useMutation({
    mutationFn: ({ itemID, resultID, action }: { itemID: string; resultID: string; action: "approve" | "reject" }) => apiRequest<Result>(`${resultPath(job.public_id, itemID, resultID)}/${action}`, { method: "POST" }),
    onSuccess(next) { replaceResult(next); setPreview(null); },
  });
  const loadPreview = useMutation({
    mutationFn: ({ itemID, resultID }: { itemID: string; resultID: string }) => apiRequest<Preview>(`${resultPath(job.public_id, itemID, resultID)}/application-preview`),
    onSuccess(value, variables) { setPreview({ resultID: variables.resultID, value }); },
  });
  const apply = useMutation({
    mutationFn: ({ itemID, resultID }: { itemID: string; resultID: string }) => apiRequest<Application>(`${resultPath(job.public_id, itemID, resultID)}/apply`, { method: "POST" }),
    onSuccess(value, variables) {
      setApplied((current) => ({ ...current, [variables.resultID]: value }));
      queryClient.setQueryData<{ data: Result[] }>(key, (current) => ({ data: (current?.data ?? []).map((result) => result.public_id === variables.resultID ? { ...result, applied_at: new Date().toISOString() } : result) }));
    },
  });
  const actionError = edit.isError || transition.isError || loadPreview.isError || apply.isError;

  if (textItems.length === 0) return null;
  return <section className="space-y-4" aria-labelledby="text-workbench-title">
    <header className="border-b border-border pb-4"><p className="flex items-center gap-2 text-xs font-semibold uppercase tracking-[0.14em] text-primary"><Sparkles className="h-4 w-4" />{text.eyebrow}</p><h2 className="mt-2 text-xl font-semibold tracking-tight" id="text-workbench-title">{text.title}</h2><p className="mt-1 max-w-3xl text-sm leading-6 text-muted-foreground">{text.description}</p></header>
    {results.isLoading ? <div className="flex min-h-32 items-center justify-center gap-2 rounded-lg border border-border text-sm text-muted-foreground" role="status"><LoaderCircle className="h-4 w-4 animate-spin motion-reduce:animate-none" />{text.loading}</div> : null}
    {results.isError ? <div className="rounded-lg border border-danger/30 bg-danger/5 p-4" role="alert"><p className="text-sm text-danger">{text.loadError}</p><Button className="mt-3 min-h-11" onClick={() => results.refetch()} variant="secondary"><RotateCw className="h-4 w-4" />{text.retry}</Button></div> : null}
    {results.data?.data.length === 0 ? <div className="rounded-lg border border-dashed border-border p-8 text-center text-sm text-muted-foreground">{text.empty}</div> : null}
    <div className="grid gap-5 2xl:grid-cols-2">{results.data?.data.map((result) => <CandidateCard actionError={actionError} applied={applied[result.public_id]} busy={edit.isPending || transition.isPending || loadPreview.isPending || apply.isPending} item={itemByID.get(result.job_item_id)} key={result.public_id} locales={job.output_locales ?? [job.locale]} localized={job.snapshot_schema === "cargoflows_product_generation_v2"} onApply={(itemID) => apply.mutate({ itemID, resultID: result.public_id })} onEdit={(itemID, structured) => edit.mutate({ itemID, resultID: result.public_id, structured })} onPreview={(itemID) => loadPreview.mutate({ itemID, resultID: result.public_id })} onTransition={(itemID, action) => transition.mutate({ itemID, resultID: result.public_id, action })} preview={preview?.resultID === result.public_id ? preview.value : null} result={result} slotName={itemByID.get(result.job_item_id)?.slot_snapshot.name[language] ?? result.kind} text={text} />)}</div>
  </section>;
}

function CandidateCard({ result, item, slotName, text, preview, applied, busy, actionError, locales, localized, onEdit, onTransition, onPreview, onApply }: { result: Result; item?: Item; slotName: string; text: Text; preview: Preview | null; applied?: Application; busy: boolean; actionError: boolean; locales: string[]; localized: boolean; onEdit: (itemID: string, structured: Structured) => void; onTransition: (itemID: string, action: "approve" | "reject") => void; onPreview: (itemID: string) => void; onApply: (itemID: string) => void }) {
  const payload = (result.edited_structured ?? result.raw_structured) as Structured;
  const [drafts, setDrafts] = useState(() => toLocalizedDrafts(result.kind, payload, locales, localized));
  const dirty = !sameStructured(fromLocalizedDrafts(result.kind, drafts, payload, locales, localized), fromLocalizedDrafts(result.kind, toLocalizedDrafts(result.kind, payload, locales, localized), payload, locales, localized));
  if (!item) return null;
  const locked = result.state === "rejected" || Boolean(result.applied_at);
  const stateVariant = result.state === "approved" ? "success" : result.state === "rejected" ? "danger" : "warning";
  return <Card className={result.effective ? "border-primary/50 shadow-[0_12px_35px_-28px_rgba(16,83,73,0.8)]" : ""}>
    <CardHeader><div className="flex flex-wrap items-start justify-between gap-3"><div><p className="text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground">{slotName} · {text.candidate} {result.candidate_index}</p><CardTitle className="mt-1">{result.kind === "title" ? text.titleField : text.searchKeywords}</CardTitle></div><div className="flex flex-wrap gap-2"><Badge variant={stateVariant}>{text.states[result.state]}</Badge>{result.effective ? <Badge variant="neutral"><Check className="h-3 w-3" />{text.effective}</Badge> : null}{result.applied_at ? <Badge variant="success"><Send className="h-3 w-3" />{text.applied}</Badge> : null}</div></div></CardHeader>
    <CardContent className="space-y-4">
      <div className="flex items-center gap-2 text-xs text-muted-foreground">{result.edited_structured ? <><FilePenLine className="h-3.5 w-3" />{text.changed}</> : <><Sparkles className="h-3.5 w-3" />{text.generated}</>}</div>
      <div className="space-y-5">{locales.map((locale) => <section className="rounded-lg border border-border bg-muted/20 p-4" key={locale}><h4 className="mb-3 text-sm font-semibold">{locale === "en" ? "English" : "简体中文"}</h4>{result.kind === "title" ? <TitleEditor draft={drafts[locale]} idPrefix={`${result.public_id}-${locale}`} locked={locked} setDraft={(draft) => setDrafts({ ...drafts, [locale]: draft })} text={text} /> : <SEOEditor draft={drafts[locale]} idPrefix={`${result.public_id}-${locale}`} locked={locked} setDraft={(draft) => setDrafts({ ...drafts, [locale]: draft })} text={text} />}</section>)}</div>
      <p className="text-xs leading-5 text-muted-foreground">{text.sourceHint}</p>
      {actionError ? <p className="rounded-md border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert">{text.actionError}</p> : null}
      <div className="flex flex-wrap gap-2 border-t border-border pt-4"><Button className="min-h-11" disabled={busy || locked || !dirty} onClick={() => onEdit(item.public_id, fromLocalizedDrafts(result.kind, drafts, payload, locales, localized))} variant="secondary">{busy ? <LoaderCircle className="h-4 w-4 animate-spin motion-reduce:animate-none" /> : <FilePenLine className="h-4 w-4" />}{text.save}</Button>{result.state !== "approved" && !locked ? <Button className="min-h-11" disabled={busy || dirty} onClick={() => onTransition(item.public_id, "approve")}><ClipboardCheck className="h-4 w-4" />{text.approve}</Button> : null}{!locked ? <Button className="min-h-11" disabled={busy || dirty} onClick={() => onTransition(item.public_id, "reject")} variant="outline"><X className="h-4 w-4" />{text.reject}</Button> : null}{result.state === "approved" && result.effective && !result.applied_at ? <Button className="min-h-11" disabled={busy || dirty} onClick={() => onPreview(item.public_id)} variant="outline"><Eye className="h-4 w-4" />{text.preview}</Button> : null}</div>
      {preview && !dirty ? <PreviewPanel applied={applied} busy={busy} onApply={() => onApply(item.public_id)} preview={preview} text={text} /> : null}
    </CardContent>
  </Card>;
}

type Draft = { title: string; keywords: string; shortDescription: string; longDescription: string; sellingPoints: string; searchKeywords: string };

function TitleEditor({ draft, setDraft, text, locked, idPrefix }: { draft: Draft; setDraft: (draft: Draft) => void; text: Text; locked: boolean; idPrefix: string }) {
  return <div className="space-y-3"><Field id={`${idPrefix}-title`} label={text.titleField}><Input disabled={locked} id={`${idPrefix}-title`} maxLength={500} onChange={(event) => setDraft({ ...draft, title: event.target.value })} value={draft.title} /></Field><Field id={`${idPrefix}-keywords`} label={text.keywords}><Input disabled={locked} id={`${idPrefix}-keywords`} onChange={(event) => setDraft({ ...draft, keywords: event.target.value })} value={draft.keywords} /></Field></div>;
}

function SEOEditor({ draft, setDraft, text, locked, idPrefix }: { draft: Draft; setDraft: (draft: Draft) => void; text: Text; locked: boolean; idPrefix: string }) {
  return <div className="space-y-3"><Field id={`${idPrefix}-short`} label={text.shortDescription}><Textarea disabled={locked} id={`${idPrefix}-short`} onChange={(event) => setDraft({ ...draft, shortDescription: event.target.value })} value={draft.shortDescription} /></Field><Field id={`${idPrefix}-points`} label={text.sellingPoints}><Textarea disabled={locked} id={`${idPrefix}-points`} onChange={(event) => setDraft({ ...draft, sellingPoints: event.target.value })} value={draft.sellingPoints} /></Field><Field id={`${idPrefix}-long`} label={text.longDescription}><Textarea disabled={locked} id={`${idPrefix}-long`} onChange={(event) => setDraft({ ...draft, longDescription: event.target.value })} value={draft.longDescription} /></Field><Field id={`${idPrefix}-search`} label={text.searchKeywords}><Input disabled={locked} id={`${idPrefix}-search`} onChange={(event) => setDraft({ ...draft, searchKeywords: event.target.value })} value={draft.searchKeywords} /></Field></div>;
}

function PreviewPanel({ preview, text, onApply, applied, busy }: { preview: Preview; text: Text; onApply: () => void; applied?: Application; busy: boolean }) {
  const localizations = preview.localizations ?? [{ locale: String((preview.after as Structured).locale ?? "zh-CN"), before: preview.before, after: preview.after }];
  const contents = applied ? (applied.contents ?? [applied.content]) : [];
  return <div className="rounded-lg border border-primary/30 bg-primary/[0.035] p-4"><div className="flex items-center gap-2 font-medium"><Search className="h-4 w-4 text-primary" />{text.preview}</div><p className="mt-1 text-xs text-muted-foreground">{text.previewHelp}</p><div className="mt-4 space-y-4">{localizations.map((entry) => { const after = entry.after as Structured; const revision = typeof after.revision === "number" ? after.revision : "—"; return <section key={entry.locale}><h4 className="mb-2 text-sm font-semibold">{entry.locale === "en" ? "English" : "简体中文"}</h4><div className="grid gap-3 sm:grid-cols-2"><Snapshot label={text.before} value={entry.before as Structured} empty={text.noFormalContent} /><Snapshot label={`${text.after} · ${text.revision} ${revision}`} value={after} empty={text.noFormalContent} /></div></section>; })}</div><div className="mt-4 flex justify-end">{applied ? <p className="flex items-center gap-2 text-sm font-medium text-success" role="status"><CheckCircle2 className="h-4 w-4" />{text.applied} · {contents.map((content) => `${content.locale} ${text.revision} ${content.revision}`).join(" · ")}</p> : <Button className="min-h-11" disabled={busy} onClick={onApply}><Send className="h-4 w-4" />{text.apply}</Button>}</div></div>;
}

function Snapshot({ label, value, empty }: { label: string; value: Structured; empty: string }) {
  const entries = Object.entries(value).filter(([key]) => !["public_id", "sku_id", "platform", "locale", "updated_at"].includes(key));
  return <div className="min-w-0 rounded-md border border-border bg-card p-3"><p className="text-xs font-semibold uppercase tracking-[0.1em] text-muted-foreground">{label}</p>{entries.length ? <dl className="mt-2 space-y-2 text-xs">{entries.map(([key, item]) => <div key={key}><dt className="text-muted-foreground">{key.replaceAll("_", " ")}</dt><dd className="mt-0.5 break-words font-medium">{Array.isArray(item) ? item.join(" · ") : String(item ?? "")}</dd></div>)}</dl> : <p className="mt-2 text-xs text-muted-foreground">{empty}</p>}</div>;
}

function Field({ id, label, children }: { id: string; label: string; children: React.ReactNode }) { return <div className="space-y-1.5"><Label htmlFor={id}>{label}</Label>{children}</div>; }
function splitList(value: string) { return value.split(/[,，\n]/).map((item) => item.trim()).filter(Boolean); }
function splitLines(value: string) { return value.split(/\r?\n/).map((item) => item.trim()).filter(Boolean); }
function toDraft(kind: Result["kind"], value: Structured): Draft { return { title: String(value.title ?? ""), keywords: asStrings(value.keywords).join(", "), shortDescription: String(value.short_description ?? ""), longDescription: String(value.long_description ?? ""), sellingPoints: asStrings(value.selling_points).join("\n"), searchKeywords: asStrings(value.search_keywords).join(", ") }; }
function fromDraft(kind: Result["kind"], draft: Draft): Structured { return kind === "title" ? { title: draft.title.trim(), keywords: splitList(draft.keywords) } : { short_description: draft.shortDescription.trim(), selling_points: splitLines(draft.sellingPoints), long_description: draft.longDescription.trim(), search_keywords: splitList(draft.searchKeywords) }; }
function toLocalizedDrafts(kind: Result["kind"], value: Structured, locales: string[], localized: boolean): Record<string, Draft> { const values = localized && isStructured(value.localizations) ? value.localizations : {}; return Object.fromEntries(locales.map((locale) => [locale, toDraft(kind, localized && isStructured(values[locale]) ? values[locale] : value)])); }
function fromLocalizedDrafts(kind: Result["kind"], drafts: Record<string, Draft>, original: Structured, locales: string[], localized: boolean): Structured { const source_fields = asStrings(original.source_fields); if (!localized) return { ...fromDraft(kind, drafts[locales[0]]), source_fields }; return { localizations: Object.fromEntries(locales.map((locale) => [locale, fromDraft(kind, drafts[locale])])), source_fields }; }
function sameStructured(left: Structured, right: Structured) { return JSON.stringify(left) === JSON.stringify(right); }
function asStrings(value: unknown) { return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : []; }
function isStructured(value: unknown): value is Structured { return typeof value === "object" && value !== null && !Array.isArray(value); }
function resultPath(jobID: string, itemID: string, resultID: string) { return `/ai-jobs/${encodeURIComponent(jobID)}/items/${encodeURIComponent(itemID)}/text-results/${encodeURIComponent(resultID)}`; }
