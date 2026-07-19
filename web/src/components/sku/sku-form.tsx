"use client";

import { useQuery } from "@tanstack/react-query";
import { FormEvent, useState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { apiRequest } from "@/lib/api";
import { categoryLabel } from "@/lib/category-label";
import { useLanguage } from "@/lib/i18n";

export interface SKUFormValue {
  category_id: number;
  product_name: string;
  brand: string;
  category: string;
  code: string;
  color: string;
  size: string;
  barcode: string;
  stock: number;
  low_stock_threshold: number;
  platform_title: string;
  selling_points: string;
  status: "active" | "draft" | "disabled";
  tags: string[];
}

interface Category { id: number; name: string; name_en: string }

const emptyValue: SKUFormValue = { category_id: 0, product_name: "", brand: "", category: "", code: "", color: "", size: "", barcode: "", stock: 0, low_stock_threshold: 0, platform_title: "", selling_points: "", status: "draft", tags: [] };

export function SKUForm({ initial, mode, busy, onCancel, onSubmit }: { initial?: SKUFormValue; mode: "create" | "edit"; busy?: boolean; onCancel?: () => void; onSubmit: (value: SKUFormValue) => void }) {
  const { language } = useLanguage();
  const zh = language === "zh";
  const [value, setValue] = useState<SKUFormValue>(initial ?? emptyValue);
  const [tags, setTags] = useState((initial?.tags ?? []).join(", "));
  const [errors, setErrors] = useState<Record<string, string>>({});
  const categories = useQuery({ queryKey: ["categories"], queryFn: () => apiRequest<{ data: Category[] }>("/categories") });
  const text = zh ? { productName: "商品名称", brand: "品牌", category: "分类", code: "SKU 编码", color: "颜色", size: "规格 / 型号", barcode: "条码", stock: "初始库存", threshold: "低库存阈值", platformTitle: "平台标题", sellingPoints: "卖点", status: "状态", tags: "标签（逗号分隔）", active: "启用", draft: "草稿", disabled: "停用", save: mode === "create" ? "创建 SKU" : "保存修改", cancel: "取消", required: "请填写此字段", chooseCategory: "请选择分类" } : { productName: "Product name", brand: "Brand", category: "Category", code: "SKU code", color: "Color", size: "Specification / model", barcode: "Barcode", stock: "Initial stock", threshold: "Low-stock threshold", platformTitle: "Platform title", sellingPoints: "Selling points", status: "Status", tags: "Tags (comma-separated)", active: "Active", draft: "Draft", disabled: "Disabled", save: mode === "create" ? "Create SKU" : "Save changes", cancel: "Cancel", required: "This field is required", chooseCategory: "Choose a category" };

  function set<K extends keyof SKUFormValue>(key: K, next: SKUFormValue[K]) { setValue((current) => ({ ...current, [key]: next })); }
  function submit(event: FormEvent) {
    event.preventDefault();
    const nextErrors: Record<string, string> = {};
    if (!value.product_name.trim()) nextErrors.product_name = text.required;
    if (!value.code.trim()) nextErrors.code = text.required;
    if (!value.category_id) nextErrors.category_id = text.chooseCategory;
    setErrors(nextErrors);
    if (Object.keys(nextErrors).length) return;
    const selected = categories.data?.data.find((category) => category.id === value.category_id);
    onSubmit({ ...value, product_name: value.product_name.trim(), code: value.code.trim(), category: selected?.name ?? value.category, tags: tags.split(",").map((tag) => tag.trim()).filter(Boolean) });
  }

  return <form className="space-y-5" onSubmit={submit}>
    <div className="grid gap-4 sm:grid-cols-2">
      <FormField error={errors.product_name} id="sku-product-name" label={text.productName}><Input id="sku-product-name" maxLength={180} onChange={(event) => set("product_name", event.target.value)} value={value.product_name} /></FormField>
      <FormField id="sku-brand" label={text.brand}><Input id="sku-brand" maxLength={120} onChange={(event) => set("brand", event.target.value)} value={value.brand} /></FormField>
      <FormField error={errors.category_id} id="sku-category" label={text.category}><select className="h-9 w-full rounded-md border border-border bg-card px-3 text-sm" id="sku-category" onChange={(event) => set("category_id", Number(event.target.value))} value={value.category_id}><option value={0}>{text.chooseCategory}</option>{categories.data?.data.map((category) => <option key={category.id} value={category.id}>{categoryLabel(category, language)}</option>)}</select></FormField>
      <FormField error={errors.code} id="sku-code" label={text.code}><Input id="sku-code" maxLength={80} onChange={(event) => set("code", event.target.value)} value={value.code} /></FormField>
      <FormField id="sku-color" label={text.color}><Input id="sku-color" maxLength={80} onChange={(event) => set("color", event.target.value)} value={value.color} /></FormField>
      <FormField id="sku-size" label={text.size}><Input id="sku-size" maxLength={80} onChange={(event) => set("size", event.target.value)} value={value.size} /></FormField>
      <FormField id="sku-barcode" label={text.barcode}><Input id="sku-barcode" maxLength={120} onChange={(event) => set("barcode", event.target.value)} value={value.barcode} /></FormField>
      {mode === "create" ? <FormField id="sku-stock" label={text.stock}><Input id="sku-stock" min={0} onChange={(event) => set("stock", Number(event.target.value))} type="number" value={value.stock} /></FormField> : null}
      <FormField id="sku-threshold" label={text.threshold}><Input id="sku-threshold" min={0} onChange={(event) => set("low_stock_threshold", Number(event.target.value))} type="number" value={value.low_stock_threshold} /></FormField>
      <FormField id="sku-status" label={text.status}><select className="h-9 w-full rounded-md border border-border bg-card px-3 text-sm" id="sku-status" onChange={(event) => set("status", event.target.value as SKUFormValue["status"])} value={value.status}><option value="active">{text.active}</option><option value="draft">{text.draft}</option><option value="disabled">{text.disabled}</option></select></FormField>
      <FormField id="sku-tags" label={text.tags}><Input id="sku-tags" onChange={(event) => setTags(event.target.value)} value={tags} /></FormField>
      <div className="sm:col-span-2"><FormField id="sku-platform-title" label={text.platformTitle}><Input id="sku-platform-title" maxLength={240} onChange={(event) => set("platform_title", event.target.value)} value={value.platform_title} /></FormField></div>
      <div className="sm:col-span-2"><FormField id="sku-selling-points" label={text.sellingPoints}><Textarea id="sku-selling-points" onChange={(event) => set("selling_points", event.target.value)} value={value.selling_points} /></FormField></div>
    </div>
    <div className="flex justify-end gap-2">{onCancel ? <Button disabled={busy} onClick={onCancel} type="button" variant="ghost">{text.cancel}</Button> : null}<Button disabled={busy} type="submit">{text.save}</Button></div>
  </form>;
}

function FormField({ id, label, error, children }: { id: string; label: string; error?: string; children: React.ReactNode }) { return <div className="space-y-1.5"><Label htmlFor={id}>{label}</Label>{children}{error ? <p className="text-xs text-danger" role="alert">{error}</p> : null}</div>; }
