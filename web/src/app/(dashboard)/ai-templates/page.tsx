"use client";

import { useQuery } from "@tanstack/react-query";
import { FileStack, LoaderCircle, Plus, RotateCw } from "lucide-react";
import Link from "next/link";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { apiRequest } from "@/lib/api";
import { useLanguage } from "@/lib/i18n";
import type { components } from "@/lib/openapi-types";

type Template = components["schemas"]["AIContentTemplate"];

export default function AITemplatesPage() {
  const { language } = useLanguage();
  const text = language === "zh" ? { title: "AI 内容模板", description: "管理平台规则、可选输出槽位和不可变的已发布版本。", create: "新建模板", loading: "正在载入模板…", error: "无法载入 AI 模板。", retry: "重试", empty: "尚无 AI 内容模板。", draft: "草稿", published: "已发布", archived: "已归档", slots: "个槽位", platform: "目标平台" } : { title: "AI content templates", description: "Manage platform rules, selectable output slots, and immutable published versions.", create: "New template", loading: "Loading templates…", error: "Could not load AI templates.", retry: "Retry", empty: "No AI content templates yet.", draft: "Draft", published: "Published", archived: "Archived", slots: "slots", platform: "Target platform" };
  const query = useQuery({ queryKey: ["ai-content-templates", "all"], queryFn: () => apiRequest<{ data: Template[] }>("/ai-content-templates?include_all=true"), retry: false });

  return <div className="mx-auto max-w-6xl space-y-6"><header className="flex flex-col gap-4 border-b border-border pb-5 sm:flex-row sm:items-end sm:justify-between"><div><p className="mb-2 flex items-center gap-2 text-xs font-semibold uppercase tracking-[0.14em] text-primary"><FileStack className="h-4 w-4" />CargoFlow · AI</p><h1 className="text-2xl font-semibold tracking-tight">{text.title}</h1><p className="mt-1 max-w-2xl text-sm text-muted-foreground">{text.description}</p></div><Button asChild className="min-h-11"><Link href="/ai-templates/new"><Plus className="h-4 w-4" />{text.create}</Link></Button></header>{query.isLoading ? <p className="flex min-h-40 items-center justify-center gap-2 text-sm text-muted-foreground" role="status"><LoaderCircle className="h-4 w-4 animate-spin motion-reduce:animate-none" />{text.loading}</p> : null}{query.isError ? <div className="rounded-lg border border-danger/30 bg-danger/5 p-5" role="alert"><p className="text-sm text-danger">{text.error}</p><Button className="mt-3 min-h-11" onClick={() => query.refetch()} variant="secondary"><RotateCw className="h-4 w-4" />{text.retry}</Button></div> : null}{query.data?.data.length === 0 ? <Card><CardContent className="py-12 text-center text-sm text-muted-foreground">{text.empty}</CardContent></Card> : null}<div className="grid gap-4">{query.data?.data.map((template) => <Card key={template.public_id}><CardHeader><div className="flex flex-wrap items-start justify-between gap-3"><div><CardTitle>{language === "zh" ? template.name_zh : template.name_en}</CardTitle><p className="mt-1 text-sm text-muted-foreground">{language === "zh" ? template.name_en : template.name_zh}</p></div><span className="rounded-full border border-border bg-muted px-3 py-1 text-xs font-medium">{text.platform}: {template.target_platform}</span></div></CardHeader><CardContent><ol className="grid gap-3 md:grid-cols-2">{[...template.versions].sort((a, b) => b.version_number - a.version_number).map((version) => <li className="rounded-lg border border-border p-4" key={version.public_id}><div className="flex items-center justify-between gap-3"><strong>V{version.version_number}</strong><StatusBadge status={version.status} labels={text} /></div><p className="mt-2 text-sm text-muted-foreground">{version.slots.length} {text.slots}</p></li>)}</ol></CardContent></Card>)}</div></div>;
}

function StatusBadge({ status, labels }: { status: "draft" | "published" | "archived"; labels: { draft: string; published: string; archived: string } }) {
  const label = labels[status];
  const tone = status === "published" ? "border-success/30 bg-success/5 text-success" : status === "draft" ? "border-warning/30 bg-warning/5 text-warning" : "border-border bg-muted text-muted-foreground";
  return <span className={`rounded-full border px-2.5 py-1 text-xs font-semibold ${tone}`}>{label}</span>;
}
