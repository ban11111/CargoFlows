"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, PackagePlus } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";

import { SKUForm, type SKUFormValue } from "@/components/sku/sku-form";
import { Button } from "@/components/ui/button";
import { apiRequest } from "@/lib/api";
import { useLanguage } from "@/lib/i18n";

export default function NewSKUPage() {
  const router = useRouter();
  const queryClient = useQueryClient();
  const { language } = useLanguage();
  const zh = language === "zh";
  const create = useMutation({
    mutationFn: (value: SKUFormValue) => apiRequest<{ public_id: string }>("/skus", { method: "POST", body: JSON.stringify(value) }),
    onSuccess: async (sku) => { await queryClient.invalidateQueries({ queryKey: ["skus"] }); router.push(`/skus/${sku.public_id}`); },
  });
  return <div className="mx-auto max-w-4xl space-y-6"><header><Button asChild className="mb-3" variant="ghost"><Link href="/skus"><ArrowLeft className="h-4 w-4" />{zh ? "返回 SKU 列表" : "Back to SKUs"}</Link></Button><div className="flex items-center gap-2 text-primary"><PackagePlus className="h-5 w-5" /><p className="text-xs font-semibold uppercase tracking-[0.14em]">CargoFlow · Catalog</p></div><h1 className="mt-2 text-2xl font-semibold">{zh ? "新建 SKU" : "New SKU"}</h1><p className="mt-1 text-sm text-muted-foreground">{zh ? "建立商品、规格、库存预警和平台内容的基础资料。" : "Create the product, variant, stock-alert, and marketplace-content foundation."}</p></header><section className="rounded-lg border border-border bg-card p-5"><SKUForm busy={create.isPending} mode="create" onSubmit={(value) => create.mutate(value)} />{create.isError ? <p className="mt-4 rounded-md border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert">{zh ? "创建失败，请检查 SKU 编码是否重复。" : "Creation failed. Check whether the SKU code already exists."}</p> : null}</section></div>;
}
