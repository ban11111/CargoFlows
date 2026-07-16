"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Tag } from "lucide-react";
import { useParams } from "next/navigation";
import { useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { apiRequest } from "@/lib/api";
import { categoryLabel } from "@/lib/category-label";
import { useLanguage } from "@/lib/i18n";
import { formatNumber } from "@/lib/utils";

interface SKUDetail {
  id: number;
  code: string;
  color: string;
  size: string;
  barcode: string;
  stock: number;
  low_stock_threshold: number;
  platform_title: string;
  selling_points: string;
  status: "active" | "draft" | "disabled";
  tags: Array<{ id: number; name: string }>;
  product: {
    id: number;
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
  const [tagDraft, setTagDraft] = useState<string>();
  const skuQuery = useQuery({
    queryKey: ["skus", params.id],
    queryFn: () => apiRequest<SKUDetail>(`/skus/${params.id}`),
  });
  const saveTags = useMutation({
    mutationFn: (tags: string[]) => {
      const sku = skuQuery.data;
      if (!sku) throw new Error("SKU unavailable");
      return apiRequest(`/skus/${sku.id}`, {
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
      <div>
        <h1 className="text-2xl font-semibold">{sku.product.name}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{sku.code}</p>
      </div>

      <div className="grid gap-6 lg:grid-cols-[1fr_360px]">
        <section className="rounded-lg border border-border bg-card p-5">
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

        <section className="rounded-lg border border-border bg-card p-5">
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

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-xs font-medium text-muted-foreground">{label}</dt>
      <dd className="mt-1 font-medium">{value}</dd>
    </div>
  );
}
