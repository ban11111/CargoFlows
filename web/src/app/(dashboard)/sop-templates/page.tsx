"use client";

import { useQueries, useQuery } from "@tanstack/react-query";
import { Camera, ChevronRight, ClipboardCheck, Plus, RotateCw, TriangleAlert } from "lucide-react";
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
import { localizedText } from "@/lib/sop";

type CaptureSOPSummary = components["schemas"]["CaptureSOPSummary"];
interface Category { id: number; name: string; name_en: string }

export default function SopTemplatesPage() {
  const { language } = useLanguage();
  const [categoryID, setCategoryID] = useState("");
  const [search, setSearch] = useState("");
  const listQuery = useQuery({
    queryKey: ["capture-sops", "include-all", categoryID],
    queryFn: () => apiRequest<{ data: CaptureSOPSummary[] }>(`/capture-sops?include_all=true${categoryID ? `&category_id=${categoryID}` : ""}`),
  });
  const categoriesQuery = useQuery({ queryKey: ["categories"], queryFn: () => apiRequest<{ data: Category[] }>("/categories") });
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
    search: "搜索 SOP 或分类", empty: "没有符合条件的拍摄 SOP。", draft: "有草稿", published: "最新发布", archived: "已归档", required: "必拍", optional: "选拍", open: "打开版本",
    listError: "无法载入 SOP 列表，请重试。", retryList: "重试载入 SOP 列表", categoryError: "无法载入分类；仍可按 SOP 名称浏览。", retryCategory: "重试载入分类",
    detailError: "该 SOP 的版本详情载入失败。", retryDetail: "重试载入", loading: "正在载入…",
  } : {
    title: "Product capture SOPs", description: "Maintain reproducible product capture views frozen by version.", create: "Create capture SOP", list: "SOP list", all: "All categories",
    search: "Search SOP or category", empty: "No capture SOPs match these filters.", draft: "Draft available", published: "Latest published", archived: "Archived", required: "required", optional: "optional", open: "Open version",
    listError: "The SOP list could not be loaded. Try again.", retryList: "Retry SOP list", categoryError: "Categories could not be loaded; browse by SOP name instead.", retryCategory: "Retry categories",
    detailError: "This SOP's version details could not be loaded.", retryDetail: "Retry details for", loading: "Loading…",
  };

  return <div className="space-y-5">
    <header className="flex flex-col gap-3 border-b border-border pb-4 sm:flex-row sm:items-end sm:justify-between">
      <div><h1 className="text-2xl font-semibold tracking-tight">{text.title}</h1><p className="mt-1 text-sm text-muted-foreground">{text.description}</p></div>
      <Button asChild className="min-h-11"><Link href="/sop-templates/new"><Plus className="h-4 w-4" />{text.create}</Link></Button>
    </header>

    {categoriesQuery.isError ? <LoadError action={text.retryCategory} message={text.categoryError} onRetry={() => categoriesQuery.refetch()} /> : null}

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
          </article>;
        })}</div> : null}
      </CardContent>
    </Card>
  </div>;
}

function LoadError({ message, action, onRetry }: { message: string; action: string; onRetry: () => unknown }) {
  return <div className="flex flex-wrap items-center gap-3 rounded-md border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert"><TriangleAlert className="h-4 w-4" /><span>{message}</span><Button aria-label={action} className="min-h-11" onClick={onRetry} size="sm" variant="secondary"><RotateCw className="h-3.5 w-3.5" />{action}</Button></div>;
}
