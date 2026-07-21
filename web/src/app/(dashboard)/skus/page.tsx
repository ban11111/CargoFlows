"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { AlertTriangle, Box, LoaderCircle, Plus, Power, PowerOff, Tags } from "lucide-react";
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
  compatible_device_model: string;
  barcode: string;
  stock: number;
	average_unit_cost_sgd: string;
	inventory_value_sgd: string;
	costing_warning: boolean;
  low_stock_threshold: number;
  platform_title: string;
  selling_points: string;
  status: "active" | "draft" | "disabled";
  product: { category_id: number; brand_id: string; name: string; brand: string; category: string; category_record?: { name: string; name_en?: string } };
  tags: Array<{ name: string }>;
}

interface Category {
  id: number;
  name: string;
  name_en: string;
}

export default function SkusPage() {
  const { language, t } = useLanguage();
  const queryClient = useQueryClient();
  const [categoryID, setCategoryID] = useState<number>();
  const skusQuery = useQuery({
    queryKey: ["skus", categoryID],
    queryFn: () => apiRequest<{ data: SKUListItem[] }>(`/skus${categoryID ? `?category_id=${categoryID}` : ""}`),
  });
  const categoriesQuery = useQuery({
    queryKey: ["categories"],
    queryFn: () => apiRequest<{ data: Category[] }>("/categories"),
  });
  const lifecycle = useMutation({
    mutationFn: ({ sku, status }: { sku: SKUListItem; status: "active" | "disabled" }) => apiRequest<SKUListItem>(`/skus/${sku.public_id}`, {
      method: "PATCH",
      body: JSON.stringify({
        category_id: sku.product.category_id,
		brand_id: sku.product.brand_id,
        product_name: sku.product.name,
        brand: sku.product.brand,
        code: sku.code,
        color: sku.color,
        size: sku.size,
        compatible_device_model: sku.compatible_device_model,
        barcode: sku.barcode,
        stock: sku.stock,
        low_stock_threshold: sku.low_stock_threshold,
        platform_title: sku.platform_title,
        selling_points: sku.selling_points,
        status,
        tags: sku.tags.map((tag) => tag.name),
      }),
    }),
    onSuccess: async (updated) => {
      queryClient.setQueriesData({ queryKey: ["skus"] }, (current) => {
        if (!current || typeof current !== "object" || !("data" in current) || !Array.isArray(current.data)) return current;
        return { ...current, data: current.data.map((sku: SKUListItem) => sku.public_id === updated.public_id ? updated : sku) };
      });
      queryClient.setQueryData(["skus", updated.public_id], updated);
      await queryClient.invalidateQueries({ queryKey: ["skus"] });
    },
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
	{ accessorKey: "average_unit_cost_sgd", header: language === "zh" ? "平均成本 (SGD)" : "Avg cost (SGD)", cell: ({ row }) => <span className="tabular-nums">S${Number(row.original.average_unit_cost_sgd ?? 0).toFixed(2)}</span> },
	{ accessorKey: "inventory_value_sgd", header: language === "zh" ? "库存金额" : "Inventory value", cell: ({ row }) => <div><span className="tabular-nums">S${Number(row.original.inventory_value_sgd ?? 0).toFixed(2)}</span>{row.original.costing_warning ? <Badge className="ml-2" variant="warning">{language === "zh" ? "零成本" : "Zero cost"}</Badge> : null}</div> },
    {
      accessorKey: "status",
      header: t("status"),
      cell: ({ row }) => <Badge variant={row.original.status === "active" ? "success" : row.original.status === "draft" ? "warning" : "neutral"}>{t(row.original.status)}</Badge>,
    },
    {
      id: "actions",
      header: language === "zh" ? "操作" : "Actions",
      cell: ({ row }) => {
        const sku = row.original;
        const pending = lifecycle.isPending && lifecycle.variables?.sku.public_id === sku.public_id;
        return sku.status === "active" ? (
          <Button
            aria-label={`${t("skuDisable")} ${sku.code}`}
            className="min-h-11"
            disabled={lifecycle.isPending}
            onClick={() => { if (window.confirm(t("skuDisableConfirm"))) lifecycle.mutate({ sku, status: "disabled" }); }}
            size="sm"
            variant="outline"
          >
            {pending ? <LoaderCircle className="h-4 w-4 animate-spin motion-reduce:animate-none" /> : <PowerOff className="h-4 w-4" />}
            {t("skuDisable")}
          </Button>
        ) : (
          <Button
            aria-label={`${t("skuEnable")} ${sku.code}`}
            className="min-h-11"
            disabled={lifecycle.isPending}
            onClick={() => lifecycle.mutate({ sku, status: "active" })}
            size="sm"
          >
            {pending ? <LoaderCircle className="h-4 w-4 animate-spin motion-reduce:animate-none" /> : <Power className="h-4 w-4" />}
            {t("skuEnable")}
          </Button>
        );
      },
    },
  ];

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div><p className="mb-2 text-[11px] font-bold uppercase tracking-[0.16em] text-primary">CargoFlows · Catalog</p><h1 className="text-3xl font-bold tracking-tight text-navy sm:text-4xl">{t("skuManage")}</h1><p className="mt-2 text-sm text-muted-foreground">{t("skuManageDesc")}</p></div>
        <Button asChild className="min-h-11"><Link href="/skus/new"><Plus className="h-4 w-4" />{t("newSku")}</Link></Button>
      </div>
	  {skus.some((sku) => sku.costing_warning) ? <div className="rounded-lg border border-warning/35 bg-warning/5 p-3 text-sm text-warning" role="status">{language === "zh" ? "部分现有库存以 0 SGD 开账；警告会持续显示，直到通过采购入库形成有效平均成本。" : "Some opening inventory is valued at SGD 0. The warning remains until a priced receipt establishes cost."}</div> : null}
      <div className="grid gap-3 md:grid-cols-4">
        <Stat label={t("skuTotal")} value={formatNumber(skus.length)} detail={t("skuTotalDesc")} />
        <Stat label={t("activeSku")} value={formatNumber(activeCount)} detail={t("activeSkuDesc")} />
        <Stat label={t("lowStock")} value={formatNumber(lowStockCount)} detail={t("lowStockDesc")} />
		<Stat label={language === "zh" ? "库存估值" : "Inventory value"} value={`S$${skus.reduce((sum, sku) => sum + Number(sku.inventory_value_sgd ?? 0), 0).toFixed(2)}`} detail={language === "zh" ? "SGD 移动平均" : "SGD moving average"} />
      </div>
      <section className="rounded-xl border border-border bg-card p-4 shadow-[var(--shadow-sm)] sm:p-5">
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
        {lifecycle.isError ? <p className="mt-4 rounded-md border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert">{t("skuLifecycleError")}</p> : null}
        <div className="mt-4">
          <DataTable columns={columns} data={skus} searchPlaceholder={t("searchSku")} />
        </div>
      </section>
    </div>
  );
}
