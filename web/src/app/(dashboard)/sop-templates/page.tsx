"use client";

import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { Archive, Camera, ChevronRight, ClipboardCheck, LoaderCircle, Plus, RotateCcw, RotateCw, TriangleAlert } from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { apiRequest } from "@/lib/api";
import { categoryLabel } from "@/lib/category-label";
import { useLanguage } from "@/lib/i18n";
import type { components } from "@/lib/openapi-types";
import { localizedText, mergeSOPVersion, type SOPVersion } from "@/lib/sop";

type CaptureSOPSummary = components["schemas"]["CaptureSOPSummary"];
interface Category { id: number; name: string; name_en: string }

export default function SopTemplatesPage() {
  const { language } = useLanguage();
  const queryClient = useQueryClient();
  const [categoryID, setCategoryID] = useState("");
  const [search, setSearch] = useState("");
  const listQuery = useQuery({
    queryKey: ["capture-sops", "include-all", categoryID],
    queryFn: () => apiRequest<{ data: CaptureSOPSummary[] }>(`/capture-sops?include_all=true${categoryID ? `&category_id=${categoryID}` : ""}`),
  });
  const categoriesQuery = useQuery({ queryKey: ["categories"], queryFn: () => apiRequest<{ data: Category[] }>("/categories") });
  const archiveVersion = useMutation({
    mutationFn: (version: SOPVersion) => apiRequest<SOPVersion>(`/sop-versions/${version.public_id}/archive`, { method: "POST", body: "{}" }),
    onSuccess: async (next) => {
      queryClient.setQueryData<CaptureSOPSummary>(["capture-sop", next.sop_public_id], (current) => current ? mergeSOPVersion(current, next) : current);
      queryClient.setQueriesData<{ data: CaptureSOPSummary[] }>({ queryKey: ["capture-sops"] }, (current) => current ? { ...current, data: current.data.map((summary) => mergeSOPVersion(summary, next)) } : current);
      queryClient.setQueryData(["sop-version", next.public_id], next);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["capture-sop", next.sop_public_id] }),
        queryClient.invalidateQueries({ queryKey: ["capture-sops"] }),
      ]);
    },
  });
  const restoreVersion = useMutation({
    mutationFn: (version: SOPVersion) => apiRequest<SOPVersion>(`/sop-versions/${version.public_id}/restore`, { method: "POST", body: "{}" }),
    onSuccess: async (next) => {
      queryClient.setQueryData<CaptureSOPSummary>(["capture-sop", next.sop_public_id], (current) => current ? mergeSOPVersion(current, next) : current);
      queryClient.setQueriesData<{ data: CaptureSOPSummary[] }>({ queryKey: ["capture-sops"] }, (current) => current ? { ...current, data: current.data.map((summary) => mergeSOPVersion(summary, next)) } : current);
      queryClient.setQueryData(["sop-version", next.public_id], next);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["capture-sop", next.sop_public_id] }),
        queryClient.invalidateQueries({ queryKey: ["capture-sops"] }),
      ]);
    },
  });
  const listed = listQuery.data?.data ?? [];
  const summaryQueries = useQueries({
    queries: listed.map((sop) => ({
      queryKey: ["capture-sop", sop.public_id],
      queryFn: () => apiRequest<CaptureSOPSummary>(`/capture-sops/${sop.public_id}`),
      initialData: sop,
      initialDataUpdatedAt: 0,
    })),
  });
  const categoryByID = useMemo(() => new Map(categoriesQuery.data?.data?.map((category) => [category.id, category]) ?? []), [categoriesQuery.data]);
  const rows = listed.map((initial, index) => ({ sop: summaryQueries[index]?.data ?? initial, detail: summaryQueries[index] }));
  const filtered = rows.filter(({ sop }) => {
    const latest = [...sop.versions].sort((a, b) => b.version_number - a.version_number)[0];
    const category = categoryByID.get(sop.category_id);
    return `${latest ? localizedText(language, latest.name) : ""} ${category ? categoryLabel(category, language) : ""}`.toLowerCase().includes(search.toLowerCase());
  });
  const text = language === "zh" ? {
    title: "商品拍摄 SOP", description: "维护可复现、按版本冻结的商品拍摄视图。", create: "新建拍摄 SOP", list: "SOP 列表", all: "全部分类",
    search: "搜索 SOP 或分类", empty: "没有符合条件的拍摄 SOP。", draft: "有草稿", published: "最新发布", publishedVersion: "已发布", archived: "已停用", required: "必拍", optional: "选拍", open: "打开版本",
    history: "版本记录", disable: "停用版本", restore: "重新启用", lifecycleError: "无法更新版本状态，请稍后重试。", disableConfirm: "停用后该版本不能用于新的拍摄批次，历史记录仍会保留。继续？", restoreConfirm: "重新启用后，该版本会再次出现在新拍摄批次的可选 SOP 中。继续？",
    listError: "无法载入 SOP 列表，请重试。", retryList: "重试载入 SOP 列表", categoryError: "无法载入分类；仍可按 SOP 名称浏览。", retryCategory: "重试载入分类",
    detailError: "该 SOP 的版本详情载入失败。", retryDetail: "重试载入", loading: "正在载入…",
  } : {
    title: "Product capture SOPs", description: "Maintain reproducible product capture views frozen by version.", create: "Create capture SOP", list: "SOP list", all: "All categories",
    search: "Search SOP or category", empty: "No capture SOPs match these filters.", draft: "Draft available", published: "Latest published", publishedVersion: "Published", archived: "Disabled", required: "required", optional: "optional", open: "Open version",
    history: "Version history", disable: "Disable version", restore: "Re-enable", lifecycleError: "The version status could not be updated. Try again later.", disableConfirm: "Disabling prevents this version from being used in new capture sessions. Existing history is preserved. Continue?", restoreConfirm: "Re-enabling makes this version available for new capture sessions again. Continue?",
    listError: "The SOP list could not be loaded. Try again.", retryList: "Retry SOP list", categoryError: "Categories could not be loaded; browse by SOP name instead.", retryCategory: "Retry categories",
    detailError: "This SOP's version details could not be loaded.", retryDetail: "Retry details for", loading: "Loading…",
  };

  return <div className="space-y-5">
    <header className="flex flex-col gap-3 border-b border-border pb-4 sm:flex-row sm:items-end sm:justify-between">
      <div><p className="mb-2 text-[11px] font-bold uppercase tracking-[0.16em] text-primary">CargoFlows · Capture standards</p><h1 className="text-3xl font-bold tracking-tight text-navy sm:text-4xl">{text.title}</h1><p className="mt-2 text-sm text-muted-foreground">{text.description}</p></div>
      <Button asChild className="min-h-11"><Link href="/sop-templates/new"><Plus className="h-4 w-4" />{text.create}</Link></Button>
    </header>

    {categoriesQuery.isError ? <LoadError action={text.retryCategory} message={text.categoryError} onRetry={() => categoriesQuery.refetch()} /> : null}
    {archiveVersion.isError || restoreVersion.isError ? <p className="rounded-md border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert">{text.lifecycleError}</p> : null}

    <Card>
      <CardHeader><div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between"><CardTitle className="flex items-center gap-2"><ClipboardCheck className="h-4 w-4 text-primary" />{text.list}</CardTitle><div className="flex flex-col gap-2 sm:flex-row"><Input aria-label={text.search} className="h-11 sm:w-64" onChange={(event) => setSearch(event.target.value)} placeholder={text.search} value={search} /><select aria-label={language === "zh" ? "按分类筛选" : "Filter by category"} className="h-11 rounded-md border border-border bg-card px-3 text-sm outline-none focus:border-primary focus:ring-2 focus:ring-primary/20" disabled={categoriesQuery.isError} onChange={(event) => setCategoryID(event.target.value)} value={categoryID}><option value="">{text.all}</option>{categoriesQuery.data?.data.map((category) => <option key={category.id} value={category.id}>{categoryLabel(category, language)}</option>)}</select></div></div></CardHeader>
      <CardContent className="p-0">
        {listQuery.isLoading ? <p className="p-6 text-sm text-muted-foreground" role="status">{text.loading}</p> : null}
        {listQuery.isError ? <div className="p-4"><LoadError action={text.retryList} message={text.listError} onRetry={() => listQuery.refetch()} /></div> : null}
        {listQuery.isSuccess && filtered.length === 0 ? <p className="p-6 text-sm text-muted-foreground">{text.empty}</p> : null}
        {listQuery.isSuccess && filtered.length ? <div className="divide-y divide-border">{filtered.map(({ sop, detail }) => {
          const versions = [...sop.versions].sort((a, b) => b.version_number - a.version_number);
          const draft = versions.find((version) => version.status === "draft");
          const published = versions.find((version) => version.status === "published");
          const archived = versions.find((version) => version.status === "archived");
          const target = draft ?? published ?? versions[0];
          const displayName = target ? localizedText(language, target.name) : sop.public_id;
          const requiredCount = target?.views.filter((view) => view.required).length ?? 0;
          const optionalCount = target?.views.filter((view) => !view.required).length ?? 0;
          return <article className="grid gap-3 p-4 md:grid-cols-[minmax(0,1fr)_auto_auto] md:items-center" key={sop.public_id}>
            <div className="flex min-w-0 items-start gap-3"><span className="grid h-10 w-10 shrink-0 place-items-center rounded-md bg-muted text-primary"><Camera className="h-5 w-5" /></span><div className="min-w-0"><h2 className="truncate font-semibold">{displayName}</h2><p className="mt-0.5 text-sm text-muted-foreground">{categoryByID.has(sop.category_id) ? categoryLabel(categoryByID.get(sop.category_id)!, language) : `#${sop.category_id}`}</p><div className="mt-2 flex flex-wrap gap-2">{draft ? <Badge variant="warning">{text.draft} · V{draft.version_number}</Badge> : null}{published ? <Badge variant="success">{text.published} · V{published.version_number}</Badge> : null}{!draft && !published && archived ? <Badge variant="neutral">{text.archived} · V{archived.version_number}</Badge> : null}</div>{detail?.isError ? <div className="mt-3 flex flex-wrap items-center gap-2 text-xs text-danger"><TriangleAlert className="h-4 w-4" /><span>{text.detailError}</span><Button aria-label={`${text.retryDetail}${language === "zh" ? "" : " "}${displayName}${language === "zh" ? "的版本详情" : ""}`} className="min-h-11" onClick={() => detail.refetch()} size="sm" variant="secondary"><RotateCw className="h-3.5 w-3.5" />{language === "zh" ? "重试" : "Retry"}</Button></div> : null}</div></div>
            <div className="font-mono text-xs tabular-nums text-muted-foreground"><span className="text-foreground">{requiredCount}</span> {text.required} · <span className="text-foreground">{optionalCount}</span> {text.optional}</div>
            {target ? <Button asChild className="min-h-11" variant="secondary"><Link href={`/sop-templates/${sop.public_id}/versions/${target.public_id}`}>{text.open}<ChevronRight className="h-4 w-4" /></Link></Button> : null}
            <section className="border-t border-border pt-3 md:col-span-3" aria-label={`${displayName} · ${text.history}`}><p className="mb-2 text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground">{text.history}</p><div className="grid gap-2">{versions.map((version) => {
              const archiving = archiveVersion.isPending && archiveVersion.variables?.public_id === version.public_id;
              const restoring = restoreVersion.isPending && restoreVersion.variables?.public_id === version.public_id;
              const versionStatus = version.status === "draft" ? text.draft : version.status === "published" ? (version.public_id === published?.public_id ? text.published : text.publishedVersion) : text.archived;
              return <div className="flex flex-col gap-2 rounded-lg border border-border bg-muted/25 p-3 sm:flex-row sm:items-center sm:justify-between" key={version.public_id}><div className="flex min-w-0 flex-wrap items-center gap-2"><span className="font-mono text-xs font-semibold tabular-nums">V{version.version_number}</span><Badge variant={version.status === "draft" ? "warning" : version.status === "published" ? "success" : "neutral"}>{versionStatus}</Badge><span className="truncate text-sm">{localizedText(language, version.name)}</span></div><div className="flex flex-wrap gap-2"><Button asChild size="sm" variant="ghost"><Link href={`/sop-templates/${sop.public_id}/versions/${version.public_id}`}>{text.open} V{version.version_number}</Link></Button>{version.status === "published" ? <Button aria-label={`${text.disable} V${version.version_number}`} disabled={archiveVersion.isPending || restoreVersion.isPending} onClick={() => { if (window.confirm(`${text.disableConfirm} V${version.version_number}`)) archiveVersion.mutate(version); }} size="sm" variant="danger">{archiving ? <LoaderCircle className="h-4 w-4 animate-spin motion-reduce:animate-none" /> : <Archive className="h-4 w-4" />}{text.disable}</Button> : null}{version.status === "archived" ? <Button aria-label={`${text.restore} V${version.version_number}`} disabled={archiveVersion.isPending || restoreVersion.isPending} onClick={() => { if (window.confirm(`${text.restoreConfirm} V${version.version_number}`)) restoreVersion.mutate(version); }} size="sm" variant="secondary">{restoring ? <LoaderCircle className="h-4 w-4 animate-spin motion-reduce:animate-none" /> : <RotateCcw className="h-4 w-4" />}{text.restore}</Button> : null}</div></div>;
            })}</div></section>
          </article>;
        })}</div> : null}
      </CardContent>
    </Card>
  </div>;
}

function LoadError({ message, action, onRetry }: { message: string; action: string; onRetry: () => unknown }) {
  return <div className="flex flex-wrap items-center gap-3 rounded-md border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert"><TriangleAlert className="h-4 w-4" /><span>{message}</span><Button aria-label={action} className="min-h-11" onClick={onRetry} size="sm" variant="secondary"><RotateCw className="h-3.5 w-3.5" />{action}</Button></div>;
}
