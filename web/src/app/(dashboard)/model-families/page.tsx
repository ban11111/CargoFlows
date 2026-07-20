"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Boxes, ChevronRight, Plus, RotateCw, Search, TriangleAlert, X } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { ModelFamilyForm } from "@/components/model-families/model-family-form";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { isAdministrator, useCurrentUser } from "@/lib/auth";
import { apiRequest } from "@/lib/api";
import { useLanguage } from "@/lib/i18n";
import { dimensionLabel, emptyModelFamilyInput, familyName, type ModelFamily, type ModelFamilyInput } from "@/lib/model-families";

export default function ModelFamiliesPage() {
  const { language } = useLanguage();
  const queryClient = useQueryClient();
  const currentUser = useCurrentUser();
  const [creating, setCreating] = useState(false);
  const [search, setSearch] = useState("");
  const familiesQuery = useQuery({ queryKey: ["model-families"], queryFn: () => apiRequest<{ data: ModelFamily[] }>("/model-families") });
  const create = useMutation({
    mutationFn: (value: ModelFamilyInput) => apiRequest<ModelFamily>("/model-families", { method: "POST", body: JSON.stringify(value) }),
    onSuccess: async () => { setCreating(false); await queryClient.invalidateQueries({ queryKey: ["model-families"] }); },
  });
  const text = language === "zh" ? {
    eyebrow: "CargoFlow · Catalog architecture", title: "型号组", description: "把结构相同、变体属性不同的 SKU 组织在一起，为拍摄和 AI 内容提供可靠边界。",
    create: "新建型号组", createAction: "创建型号组", close: "关闭创建表单", formTitle: "定义一个型号组", formDesc: "先声明不变结构，再选择允许变化的属性。", search: "搜索名称、品牌或系列代码",
    list: "型号组目录", empty: "还没有型号组。创建第一个型号组来组织相关 SKU。", noMatch: "没有符合搜索条件的型号组。", active: "启用", archived: "已归档", open: "查看型号组",
    error: "无法载入型号组，请重试。", retry: "重试载入", createError: "无法创建型号组。请检查系列代码是否重复，以及所有必填项。", loading: "正在载入型号组…",
  } : {
    eyebrow: "CargoFlow · Catalog architecture", title: "Model families", description: "Group structurally related SKU variants to create reliable boundaries for capture and AI content.",
    create: "New model family", createAction: "Create model family", close: "Close create form", formTitle: "Define a model family", formDesc: "Declare shared structure first, then choose the attributes that may vary.", search: "Search name, brand, or family code",
    list: "Model family directory", empty: "No model families yet. Create the first one to organize related SKUs.", noMatch: "No model families match this search.", active: "Active", archived: "Archived", open: "Open model family",
    error: "Model families could not be loaded. Try again.", retry: "Retry model families", createError: "The model family could not be created. Check for a duplicate code and complete every required field.", loading: "Loading model families…",
  };
  const families = familiesQuery.data?.data ?? [];
  const filtered = families.filter((family) => `${family.name_zh} ${family.name_en} ${family.brand} ${family.model_code}`.toLowerCase().includes(search.trim().toLowerCase()));
  const canCreate = isAdministrator(currentUser.data?.role);

  return <div className="space-y-5">
    <header className="flex flex-col gap-4 border-b border-border pb-5 sm:flex-row sm:items-end sm:justify-between">
      <div><p className="mb-2 text-[11px] font-bold uppercase tracking-[0.16em] text-primary">{text.eyebrow}</p><h1 className="text-3xl font-bold tracking-tight text-navy sm:text-4xl">{text.title}</h1><p className="mt-2 max-w-3xl text-sm leading-6 text-muted-foreground">{text.description}</p></div>
      {canCreate ? <Button className="min-h-11" onClick={() => setCreating((value) => !value)}><Plus className="h-4 w-4" />{text.create}</Button> : null}
    </header>

    {creating ? <Card className="border-primary/25"><CardHeader><div className="flex items-start justify-between gap-4"><div><CardTitle>{text.formTitle}</CardTitle><p className="mt-1 text-sm text-muted-foreground">{text.formDesc}</p></div><Button aria-label={text.close} onClick={() => setCreating(false)} size="icon" variant="ghost"><X className="h-4 w-4" /></Button></div></CardHeader><CardContent><ModelFamilyForm busy={create.isPending} initial={emptyModelFamilyInput} onCancel={() => setCreating(false)} onSubmit={(value) => create.mutate(value)} submitLabel={text.createAction} />{create.isError ? <p className="mt-4 rounded-md border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert">{text.createError}</p> : null}</CardContent></Card> : null}

    <Card>
      <CardHeader><div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between"><CardTitle className="flex items-center gap-2"><Boxes className="h-4 w-4 text-primary" />{text.list}</CardTitle><div className="relative sm:w-80"><Search aria-hidden className="pointer-events-none absolute left-3.5 top-3.5 h-4 w-4 text-muted-foreground" /><Input aria-label={text.search} className="pl-10" onChange={(event) => setSearch(event.target.value)} placeholder={text.search} value={search} /></div></div></CardHeader>
      <CardContent className="p-0">
        {familiesQuery.isLoading ? <p className="p-6 text-sm text-muted-foreground" role="status">{text.loading}</p> : null}
        {familiesQuery.isError ? <div className="m-4 flex flex-wrap items-center gap-3 rounded-md border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert"><TriangleAlert className="h-4 w-4" /><span>{text.error}</span><Button aria-label={text.retry} onClick={() => familiesQuery.refetch()} size="sm" variant="secondary"><RotateCw className="h-4 w-4" />{text.retry}</Button></div> : null}
        {familiesQuery.isSuccess && filtered.length === 0 ? <div className="p-10 text-center"><span className="mx-auto grid h-12 w-12 place-items-center rounded-xl bg-muted text-primary"><Boxes className="h-5 w-5" /></span><p className="mt-4 text-sm text-muted-foreground">{families.length ? text.noMatch : text.empty}</p></div> : null}
        {filtered.length ? <div className="divide-y divide-border">{filtered.map((family) => <article className="grid gap-4 p-4 transition-colors hover:bg-muted/30 md:grid-cols-[minmax(0,1fr)_minmax(220px,0.8fr)_auto] md:items-center" key={family.public_id}>
          <div className="flex min-w-0 items-start gap-3"><span className="grid h-11 w-11 shrink-0 place-items-center rounded-xl bg-navy text-white"><Boxes className="h-5 w-5" /></span><div className="min-w-0"><div className="flex flex-wrap items-center gap-2"><h2 className="font-bold text-navy">{familyName(family, language)}</h2><Badge variant={family.status === "active" ? "success" : "neutral"}>{family.status === "active" ? text.active : text.archived}</Badge></div><p className="mt-1 text-sm text-muted-foreground">{family.brand} <span aria-hidden>·</span> <span className="font-mono text-xs">{family.model_code}</span></p><p className="mt-1 text-xs text-muted-foreground">{language === "zh" ? family.name_en : family.name_zh}</p></div></div>
          <div className="flex flex-wrap gap-1.5">{family.variation_dimensions.map((dimension) => <Badge key={dimension} variant="neutral">{dimensionLabel(dimension, language)}</Badge>)}</div>
          <Button asChild aria-label={`${text.open}: ${familyName(family, language)}`} className="min-h-11" variant="secondary"><Link href={`/model-families/${family.public_id}`}>{text.open}<ChevronRight className="h-4 w-4" /></Link></Button>
        </article>)}</div> : null}
      </CardContent>
    </Card>
  </div>;
}
