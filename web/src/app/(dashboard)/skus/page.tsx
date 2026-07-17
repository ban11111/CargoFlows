"use client";

import { useQuery } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { AlertTriangle, Box, Tags } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { DataTable } from "@/components/data-table";
import { Stat } from "@/components/stat";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { apiRequest } from "@/lib/api";
import { categoryLabel } from "@/lib/category-label";
import { useLanguage } from "@/lib/i18n";
import { formatNumber } from "@/lib/utils";

interface SKUListItem {
  public_id: string;
  code: string;
  color: string;
  size: string;
  stock: number;
  low_stock_threshold: number;
  status: "active" | "draft" | "disabled";
  product: { name: string; category: string; category_record?: { name: string; name_en?: string } };
  tags: Array<{ name: string }>;
}

interface Category {
  id: number;
  name: string;
  name_en: string;
}

export default function SkusPage() {
  const { language, t } = useLanguage();
  const [categoryID, setCategoryID] = useState<number>();
  const skusQuery = useQuery({
    queryKey: ["skus", categoryID],
    queryFn: () => apiRequest<{ data: SKUListItem[] }>(`/skus${categoryID ? `?category_id=${categoryID}` : ""}`),
  });
  const categoriesQuery = useQuery({
    queryKey: ["categories"],
    queryFn: () => apiRequest<{ data: Category[] }>("/categories"),
  });
  const skus = skusQuery.data?.data ?? [];
  const lowStockCount = skus.filter((sku) => sku.stock <= sku.low_stock_threshold).length;
  const activeCount = skus.filter((sku) => sku.status === "active").length;
  const columns: ColumnDef<SKUListItem>[] = [
    {
      accessorKey: "code",
      header: t("sku"),
      cell: ({ row }) => (
        <Link className="font-medium text-primary" href={`/skus/${row.original.public_id}`}>
          {row.original.code}
        </Link>
      ),
    },
    { accessorFn: (row) => row.product.name, id: "product", header: t("product") },
    {
      accessorFn: (row) => categoryLabel(row.product.category_record ?? { name: row.product.category }, language),
      id: "category",
      header: t("category"),
    },
    { header: t("spec"), cell: ({ row }) => `${row.original.color} / ${row.original.size}` },
    {
      id: "tags",
      header: t("skuTags"),
      cell: ({ row }) => (
        <div className="flex flex-wrap gap-1">
          {row.original.tags.map((tag) => <Badge key={tag.name} variant="neutral">{tag.name}</Badge>)}
        </div>
      ),
    },
    {
      accessorKey: "stock",
      header: t("stock"),
      cell: ({ row }) => {
        const low = row.original.stock <= row.original.low_stock_threshold;
        return <span className={low ? "font-semibold text-danger" : ""}>{formatNumber(row.original.stock)}</span>;
      },
    },
  ];

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">{t("skuManage")}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{t("skuManageDesc")}</p>
      </div>
      <div className="grid gap-3 md:grid-cols-3">
        <Stat label={t("skuTotal")} value={formatNumber(skus.length)} detail={t("skuTotalDesc")} />
        <Stat label={t("activeSku")} value={formatNumber(activeCount)} detail={t("activeSkuDesc")} />
        <Stat label={t("lowStock")} value={formatNumber(lowStockCount)} detail={t("lowStockDesc")} />
      </div>
      <section className="rounded-lg border border-border bg-card p-4">
        <div className="flex flex-col gap-3 border-b border-border pb-4 lg:flex-row lg:items-center lg:justify-between">
          <div className="flex items-center gap-2">
            <Box className="h-4 w-4 text-primary" />
            <h2 className="text-sm font-semibold">{t("skuList")}</h2>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button onClick={() => setCategoryID(undefined)} size="sm" variant={categoryID ? "ghost" : "secondary"}>{t("all")}</Button>
            {categoriesQuery.data?.data.map((category) => (
              <Button
                key={category.id}
                onClick={() => setCategoryID(category.id)}
                size="sm"
                variant={categoryID === category.id ? "secondary" : "ghost"}
              >
                {categoryLabel(category, language)}
              </Button>
            ))}
          </div>
        </div>
        <div className="mt-4 flex items-center gap-2 text-xs text-muted-foreground">
          <AlertTriangle className="h-4 w-4 text-warning" />
          {t("lowStockHint")}
          <Tags className="ml-2 h-4 w-4" />
          {t("tagHint")}
        </div>
        <div className="mt-4">
          <DataTable columns={columns} data={skus} searchPlaceholder={t("searchSku")} />
        </div>
      </section>
    </div>
  );
}
