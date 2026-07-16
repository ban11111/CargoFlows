"use client";

import { useQueries, useQuery } from "@tanstack/react-query";
import { Camera, ChevronRight, ClipboardCheck, Plus } from "lucide-react";
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
  const listQuery = useQuery({ queryKey: ["capture-sops", categoryID], queryFn: () => apiRequest<{ data: CaptureSOPSummary[] }>(`/capture-sops${categoryID ? `?category_id=${categoryID}` : ""}`) });
  const categoriesQuery = useQuery({ queryKey: ["categories"], queryFn: () => apiRequest<{ data: Category[] }>("/categories") });
  const summaryQueries = useQueries({ queries: (listQuery.data?.data ?? []).map((sop) => ({ queryKey: ["capture-sop", sop.public_id], queryFn: () => apiRequest<CaptureSOPSummary>(`/capture-sops/${sop.public_id}`), initialData: sop, initialDataUpdatedAt: 0 })) });
  const categoryByID = useMemo(() => new Map(categoriesQuery.data?.data.map((category) => [category.id, category])), [categoriesQuery.data]);
  const summaries = summaryQueries.map((query) => query.data).filter((value): value is CaptureSOPSummary => Boolean(value));
  const filtered = summaries.filter((sop) => {
    const latest = [...sop.versions].sort((a, b) => b.version_number - a.version_number)[0];
    const category = categoryByID.get(sop.category_id);
    return `${latest ? localizedText(language, latest.name) : ""} ${category ? categoryLabel(category, language) : ""}`.toLowerCase().includes(search.toLowerCase());
  });
  const text = language === "zh" ? {
    title: "商品拍摄 SOP", description: "维护可复现、按版本冻结的商品拍摄视图。", create: "新建拍摄 SOP", list: "SOP 列表", all: "全部分类",
    search: "搜索 SOP 或分类", empty: "没有符合条件的拍摄 SOP。", draft: "有草稿", published: "最新发布", required: "必拍", optional: "选拍", open: "打开版本",
  } : {
    title: "Product capture SOPs", description: "Maintain reproducible product capture views frozen by version.", create: "Create capture SOP", list: "SOP list", all: "All categories",
    search: "Search SOP or category", empty: "No capture SOPs match these filters.", draft: "Draft available", published: "Latest published", required: "required", optional: "optional", open: "Open version",
  };

  return <div className="space-y-5">
    <header className="flex flex-col gap-3 border-b border-border pb-4 sm:flex-row sm:items-end sm:justify-between"><div><h1 className="text-2xl font-semibold tracking-tight">{text.title}</h1><p className="mt-1 text-sm text-muted-foreground">{text.description}</p></div><Button asChild className="min-h-11"><Link href="/sop-templates/new"><Plus className="h-4 w-4" />{text.create}</Link></Button></header>
    <Card><CardHeader><div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between"><CardTitle className="flex items-center gap-2"><ClipboardCheck className="h-4 w-4 text-primary" />{text.list}</CardTitle><div className="flex flex-col gap-2 sm:flex-row"><Input aria-label={text.search} className="h-11 sm:w-64" onChange={(e) => setSearch(e.target.value)} placeholder={text.search} value={search} /><select aria-label={language === "zh" ? "按分类筛选" : "Filter by category"} className="h-11 rounded-md border border-border bg-card px-3 text-sm outline-none focus:border-primary focus:ring-2 focus:ring-primary/20" onChange={(e) => setCategoryID(e.target.value)} value={categoryID}><option value="">{text.all}</option>{categoriesQuery.data?.data.map((category) => <option key={category.id} value={category.id}>{categoryLabel(category, language)}</option>)}</select></div></div></CardHeader>
      <CardContent className="p-0">{listQuery.isLoading ? <p className="p-6 text-sm text-muted-foreground" role="status">{language === "zh" ? "正在载入…" : "Loading…"}</p> : filtered.length ? <div className="divide-y divide-border">{filtered.map((sop) => {
        const versions = [...sop.versions].sort((a, b) => b.version_number - a.version_number);
        const draft = versions.find((version) => version.status === "draft");
        const published = versions.find((version) => version.status === "published");
        const target = draft ?? published ?? versions[0];
        const requiredCount = target?.views.filter((view) => view.required).length ?? 0;
        const optionalCount = target?.views.filter((view) => !view.required).length ?? 0;
        return <article className="grid gap-3 p-4 md:grid-cols-[minmax(0,1fr)_auto_auto] md:items-center" key={sop.public_id}><div className="flex min-w-0 items-start gap-3"><span className="grid h-10 w-10 shrink-0 place-items-center rounded-md bg-muted text-primary"><Camera className="h-5 w-5" /></span><div className="min-w-0"><h2 className="truncate font-semibold">{target ? localizedText(language, target.name) : sop.public_id}</h2><p className="mt-0.5 text-sm text-muted-foreground">{categoryByID.has(sop.category_id) ? categoryLabel(categoryByID.get(sop.category_id)!, language) : `#${sop.category_id}`}</p><div className="mt-2 flex flex-wrap gap-2">{draft ? <Badge variant="warning">{text.draft} · V{draft.version_number}</Badge> : null}{published ? <Badge variant="success">{text.published} · V{published.version_number}</Badge> : null}</div></div></div><div className="font-mono text-xs tabular-nums text-muted-foreground"><span className="text-foreground">{requiredCount}</span> {text.required} · <span className="text-foreground">{optionalCount}</span> {text.optional}</div>{target ? <Button asChild className="min-h-11" variant="secondary"><Link href={`/sop-templates/${sop.public_id}/versions/${target.public_id}`}>{text.open}<ChevronRight className="h-4 w-4" /></Link></Button> : null}</article>;
      })}</div> : <p className="p-6 text-sm text-muted-foreground">{text.empty}</p>}</CardContent>
    </Card>
  </div>;
}
