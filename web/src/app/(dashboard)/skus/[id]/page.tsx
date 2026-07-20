"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Pencil, Tag, Trash2 } from "lucide-react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useState } from "react";
import { SKUForm, type SKUFormValue } from "@/components/sku/sku-form";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { apiRequest } from "@/lib/api";
import { categoryLabel } from "@/lib/category-label";
import { useLanguage } from "@/lib/i18n";
import { formatNumber } from "@/lib/utils";

interface SKUDetail {
  public_id: string;
  code: string;
  color: string;
  size: string;
  barcode: string;
  stock: number;
  low_stock_threshold: number;
  platform_title: string;
  selling_points: string;
  status: "active" | "draft" | "disabled";
  tags: Array<{ name: string }>;
  product: {
    category_id: number;
    name: string;
    brand: string;
    category: string;
    category_record?: { name: string; name_en?: string };
  };
}

export default function SkuDetailPage() {
  const { language, t } = useLanguage();
  const queryClient = useQueryClient();
  const params = useParams<{ id: string }>();
  const router = useRouter();
  const [tagDraft, setTagDraft] = useState<string>();
  const [editing, setEditing] = useState(false);
  const skuQuery = useQuery({
    queryKey: ["skus", params.id],
    queryFn: () => apiRequest<SKUDetail>(`/skus/${params.id}`),
  });
  const saveTags = useMutation({
    mutationFn: (tags: string[]) => {
      const sku = skuQuery.data;
      if (!sku) throw new Error("SKU unavailable");
      return apiRequest(`/skus/${sku.public_id}`, {
        method: "PATCH",
        body: JSON.stringify({
          category_id: sku.product.category_id,
          product_name: sku.product.name,
          brand: sku.product.brand,
          category: sku.product.category,
          code: sku.code,
          color: sku.color,
          size: sku.size,
          barcode: sku.barcode,
          stock: sku.stock,
          low_stock_threshold: sku.low_stock_threshold,
          platform_title: sku.platform_title,
          selling_points: sku.selling_points,
          status: sku.status,
          tags,
        }),
      });
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["skus", params.id] }),
        queryClient.invalidateQueries({ queryKey: ["assets", "review", "hierarchy"] }),
      ]);
    },
  });
  const update = useMutation({ mutationFn: (value: SKUFormValue) => apiRequest<SKUDetail>(`/skus/${params.id}`, { method: "PATCH", body: JSON.stringify(value) }), onSuccess: async () => { setEditing(false); setTagDraft(undefined); await queryClient.invalidateQueries({ queryKey: ["skus"] }); } });
  const remove = useMutation({ mutationFn: () => apiRequest<void>(`/skus/${params.id}`, { method: "DELETE" }), onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: ["skus"] }); router.push("/skus"); } });

  const sku = skuQuery.data;
  if (skuQuery.isLoading || !sku) {
    return <p className="py-10 text-sm text-muted-foreground">{t("search")}</p>;
  }

  const tagInput = tagDraft ?? sku.tags.map((tag) => tag.name).join(", ");
  const tags = tagInput
    .split(",")
    .map((tag) => tag.trim())
    .filter(Boolean);

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex items-start gap-3"><Button asChild aria-label={language === "zh" ? "返回 SKU 列表" : "Back to SKUs"} size="icon" variant="secondary"><Link href="/skus"><ArrowLeft className="h-4 w-4" /></Link></Button><div><p className="mb-1 text-[11px] font-bold uppercase tracking-[0.16em] text-primary">CargoFlows · SKU detail</p><h1 className="text-3xl font-bold tracking-tight text-navy sm:text-4xl">{sku.product.name}</h1><p className="mt-1 font-mono text-xs text-muted-foreground">{sku.code}</p></div></div>
        <div className="flex flex-wrap gap-2"><Button onClick={() => setEditing((current) => !current)} variant="secondary"><Pencil className="h-4 w-4" />{language === "zh" ? "编辑资料" : "Edit details"}</Button><Button disabled={remove.isPending} onClick={() => { if (window.confirm(language === "zh" ? "删除这个 SKU？已有业务记录时系统会拒绝删除。" : "Delete this SKU? The system will refuse if it has business history.")) remove.mutate(); }} variant="danger"><Trash2 className="h-4 w-4" />{language === "zh" ? "删除 SKU" : "Delete SKU"}</Button></div>
      </div>

      {remove.isError ? <p className="rounded-md border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert">{language === "zh" ? "无法删除：该 SKU 已有库存、素材、型号组、AI 任务或正式内容记录。请将状态改为停用。" : "Cannot delete: this SKU has inventory, media, model-family, AI-job, or published-content history. Disable it instead."}</p> : null}

      {editing ? <section className="rounded-lg border border-primary/30 bg-card p-5"><h2 className="mb-4 text-sm font-semibold">{language === "zh" ? "编辑 SKU 资料" : "Edit SKU details"}</h2><SKUForm busy={update.isPending} initial={skuFormValue(sku)} mode="edit" onCancel={() => setEditing(false)} onSubmit={(value) => update.mutate(value)} />{update.isError ? <p className="mt-4 text-sm text-danger" role="alert">{language === "zh" ? "保存失败，请检查填写内容。" : "Save failed. Check the entered values."}</p> : null}</section> : null}

      <div className="grid gap-6 lg:grid-cols-[1fr_360px]">
        <section className="rounded-xl border border-border bg-card p-5 shadow-[var(--shadow-sm)]">
          <h2 className="text-sm font-semibold">{t("productInfo")}</h2>
          <dl className="mt-4 grid gap-4 text-sm sm:grid-cols-2">
            <Field label={t("category")} value={categoryLabel(sku.product.category_record ?? { name: sku.product.category }, language)} />
            <Field label={t("brand")} value={sku.product.brand} />
            <Field label={t("color")} value={sku.color} />
            <Field label={t("size")} value={sku.size} />
            <Field label={t("currentStock")} value={formatNumber(sku.stock)} />
            <Field label={t("threshold")} value={formatNumber(sku.low_stock_threshold)} />
          </dl>
        </section>

        <section className="rounded-xl border border-border bg-card p-5 shadow-[var(--shadow-sm)]">
          <div className="flex items-center gap-2">
            <Tag className="h-4 w-4 text-primary" />
            <h2 className="text-sm font-semibold">{t("skuTags")}</h2>
          </div>
          <p className="mt-2 text-xs leading-5 text-muted-foreground">{t("tagHint")}</p>
          <Input
            className="mt-4"
            onChange={(event) => setTagDraft(event.target.value)}
            placeholder={t("tagsPlaceholder")}
            value={tagInput}
          />
          <div className="mt-3 flex flex-wrap gap-1.5">
            {tags.map((tag) => (
              <Badge key={tag} variant="neutral">{tag}</Badge>
            ))}
          </div>
          <Button
            className="mt-4 w-full"
            disabled={saveTags.isPending}
            onClick={() => saveTags.mutate(tags)}
            type="button"
          >
            {t("saveTags")}
          </Button>
        </section>
      </div>
    </div>
  );
}

function skuFormValue(sku: SKUDetail): SKUFormValue { return { category_id: sku.product.category_id, product_name: sku.product.name, brand: sku.product.brand, category: sku.product.category, code: sku.code, color: sku.color, size: sku.size, barcode: sku.barcode, stock: sku.stock, low_stock_threshold: sku.low_stock_threshold, platform_title: sku.platform_title, selling_points: sku.selling_points, status: sku.status, tags: sku.tags.map((tag) => tag.name) }; }

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-xs font-medium text-muted-foreground">{label}</dt>
      <dd className="mt-1 font-medium">{value}</dd>
    </div>
  );
}
