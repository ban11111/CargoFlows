"use client";

import { useMutation, useQuery } from "@tanstack/react-query";
import { ArrowLeft, LoaderCircle, Plus, RotateCw } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { type FormEvent, useState } from "react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { apiRequest } from "@/lib/api";
import { categoryLabel } from "@/lib/category-label";
import { useLanguage } from "@/lib/i18n";
import type { SOPVersion } from "@/lib/sop";

interface Category { id: number; name: string; name_en: string }

export default function NewSOPPage() {
  const { language } = useLanguage();
  const router = useRouter();
  const [categoryID, setCategoryID] = useState("");
  const [nameZH, setNameZH] = useState("");
  const [nameEN, setNameEN] = useState("");
  const [descriptionZH, setDescriptionZH] = useState("");
  const [descriptionEN, setDescriptionEN] = useState("");
  const categories = useQuery({ queryKey: ["categories"], queryFn: () => apiRequest<{ data: Category[] }>("/categories") });
  const create = useMutation({
    mutationFn: () => apiRequest<SOPVersion>("/capture-sops", { method: "POST", body: JSON.stringify({ category_id: Number(categoryID), name: { "zh-CN": nameZH.trim(), en: nameEN.trim() }, description: { "zh-CN": descriptionZH.trim(), en: descriptionEN.trim() } }) }),
    onSuccess: (version) => router.push(`/sop-templates/${version.sop_public_id}/versions/${version.public_id}`),
  });
  const text = language === "zh" ? {
    title: "新建商品拍摄 SOP",
    description: "创建后会自动生成 V1 草稿和锁定的基准正面。",
    back: "返回 SOP 列表",
    category: "商品分类",
    choose: "选择分类",
    categoryLoading: "正在载入分类…",
    categoryError: "无法载入商品分类，请重试。",
    categoryRetry: "重试载入商品分类",
    categoryEmpty: "暂无可用分类，请先创建商品分类。",
    nameZH: "中文名称",
    nameEN: "English name",
    descriptionZH: "中文说明",
    descriptionEN: "English description",
    create: "创建并编辑",
    error: "创建失败，请检查输入后重试。",
  } : {
    title: "Create product capture SOP",
    description: "V1 draft and its locked reference front are created automatically.",
    back: "Back to SOP list",
    category: "Product category",
    choose: "Choose a category",
    categoryLoading: "Loading categories…",
    categoryError: "Could not load product categories. Try again.",
    categoryRetry: "Retry loading product categories",
    categoryEmpty: "No categories are available. Create a product category first.",
    nameZH: "Chinese name",
    nameEN: "English name",
    descriptionZH: "Chinese description",
    descriptionEN: "English description",
    create: "Create and edit",
    error: "Creation failed. Check the input and try again.",
  };
  function submit(event: FormEvent) { event.preventDefault(); if (categoryID && nameZH.trim() && nameEN.trim()) create.mutate(); }
  const noCategories = categories.isSuccess && categories.data.data.length === 0;

  return <div className="mx-auto max-w-3xl space-y-5"><header><Button asChild className="mb-3" variant="ghost"><Link href="/sop-templates"><ArrowLeft className="h-4 w-4" />{text.back}</Link></Button><h1 className="text-2xl font-semibold tracking-tight">{text.title}</h1><p className="mt-1 text-sm text-muted-foreground">{text.description}</p></header><Card><CardHeader><CardTitle>{text.title}</CardTitle></CardHeader><CardContent><form className="grid gap-4 md:grid-cols-2" onSubmit={submit}><div className="space-y-1.5 md:col-span-2"><Label htmlFor="category">{text.category}</Label><select className="h-11 w-full rounded-md border border-border bg-card px-3 text-sm outline-none focus:border-primary focus:ring-2 focus:ring-primary/20" disabled={categories.isLoading || categories.isError || noCategories} id="category" onChange={(e) => setCategoryID(e.target.value)} required value={categoryID}><option value="">{text.choose}</option>{categories.data?.data.map((category) => <option key={category.id} value={category.id}>{categoryLabel(category, language)}</option>)}</select>{categories.isLoading ? <p className="text-sm text-muted-foreground" role="status">{text.categoryLoading}</p> : null}{categories.isError ? <div className="flex flex-wrap items-center gap-2" role="alert"><p className="text-sm text-danger">{text.categoryError}</p><Button aria-label={text.categoryRetry} className="min-h-11" onClick={() => categories.refetch()} type="button" variant="outline"><RotateCw className="h-4 w-4" />{text.categoryRetry}</Button></div> : null}{noCategories ? <p className="text-sm text-muted-foreground" role="status">{text.categoryEmpty}</p> : null}</div><Field id="name-zh" label={text.nameZH}><Input className="h-11" id="name-zh" onChange={(e) => setNameZH(e.target.value)} required value={nameZH} /></Field><Field id="name-en" label={text.nameEN}><Input className="h-11" id="name-en" onChange={(e) => setNameEN(e.target.value)} required value={nameEN} /></Field><Field id="description-zh" label={text.descriptionZH}><Textarea id="description-zh" onChange={(e) => setDescriptionZH(e.target.value)} value={descriptionZH} /></Field><Field id="description-en" label={text.descriptionEN}><Textarea id="description-en" onChange={(e) => setDescriptionEN(e.target.value)} value={descriptionEN} /></Field>{create.isError ? <p className="text-sm text-danger md:col-span-2" role="alert">{text.error}</p> : null}<div className="flex justify-end md:col-span-2"><Button className="min-h-11" disabled={!categoryID || !nameZH.trim() || !nameEN.trim() || create.isPending} type="submit"><Plus className="h-4 w-4" />{create.isPending ? <LoaderCircle className="h-4 w-4 animate-spin motion-reduce:animate-none" /> : null}{text.create}</Button></div></form></CardContent></Card></div>;
}

function Field({ id, label, children }: { id: string; label: string; children: React.ReactNode }) { return <div className="space-y-1.5"><Label htmlFor={id}>{label}</Label>{children}</div>; }
