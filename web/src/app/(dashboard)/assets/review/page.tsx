"use client";
/* eslint-disable @next/next/no-img-element -- authenticated private media is not compatible with the public image optimizer */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, ChevronRight, ImageUp, Palette, Search, Tags, X } from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { apiRequest, authenticatedMediaURL } from "@/lib/api";
import { categoryLabel } from "@/lib/category-label";
import { useLanguage } from "@/lib/i18n";
import { formatDateTime } from "@/lib/utils";

type ReviewStatus = "pending" | "approved" | "rejected";

interface Asset {
  public_id: string;
  media_url: string;
  review_status: ReviewStatus;
  captured_at: string;
  sop_view_name: { "zh-CN": string; en: string };
  photo_session_code: string;
	origin_type: "captured" | "uploaded" | "ai_generated";
	source_summary: Record<string, string>;
}

interface HierarchySKU {
  public_id: string;
  code: string;
  product_name: string;
  tags: Array<{ name: string }>;
  assets: Asset[];
}

interface HierarchyCategory {
  id: number;
  name: string;
  name_en: string;
  is_system: boolean;
  skus: HierarchySKU[];
}

interface SelectedAsset {
  category: HierarchyCategory;
  sku: HierarchySKU;
  asset: Asset;
}

interface StyleReferenceGrant {
  public_id: string;
  version: number;
  source_sku_id: string;
  description_zh: string;
  description_en: string;
  derivative_sha256: string;
  status: "approved" | "revoked";
}

function ReviewBadge({ status, label }: { status: ReviewStatus; label: string }) {
  const variant = status === "approved" ? "success" : status === "rejected" ? "danger" : "warning";
  return <Badge variant={variant}>{label}</Badge>;
}

export default function AssetReviewPage() {
  const { language, t } = useLanguage();
  const queryClient = useQueryClient();
  const [query, setQuery] = useState("");
  const [selectedAssetID, setSelectedAssetID] = useState<string>();
	const [styleDialog, setStyleDialog] = useState(false);
	const [styleZH, setStyleZH] = useState("");
	const [styleEN, setStyleEN] = useState("");
	const [styleMask, setStyleMask] = useState<File>();
  const hierarchyQuery = useQuery({
    queryKey: ["assets", "review", "hierarchy"],
    queryFn: () => apiRequest<{ data: HierarchyCategory[] }>("/assets/review/hierarchy"),
  });
  const styleReferencesQuery = useQuery({ queryKey: ["style-reference-grants"], queryFn: () => apiRequest<{ data: StyleReferenceGrant[] }>("/style-reference-grants") });
  const reviewMutation = useMutation({
    mutationFn: ({ id, status }: { id: string; status: "approved" | "rejected" }) =>
      apiRequest(`/assets/${id}/review`, { method: "PATCH", body: JSON.stringify({ status }) }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["assets", "review"] });
    },
  });
	const styleMutation = useMutation({ mutationFn: async () => { if (!selected || !styleMask) throw new Error("missing style grant fields"); const form = new FormData(); form.set("asset_id", selected.asset.public_id); form.set("description_zh", styleZH.trim()); form.set("description_en", styleEN.trim()); form.set("product_exclusion_mask", styleMask, "product-mask.png"); return apiRequest("/style-reference-grants", { method: "POST", body: form }); }, onSuccess: async () => { setStyleDialog(false); setStyleZH(""); setStyleEN(""); setStyleMask(undefined); await queryClient.invalidateQueries({ queryKey: ["style-reference-grants"] }); } });
  const revokeStyleReference = useMutation({ mutationFn: (id: string) => apiRequest(`/style-reference-grants/${id}`, { method: "PATCH", body: JSON.stringify({ status: "revoked" }) }), onSuccess: () => queryClient.invalidateQueries({ queryKey: ["style-reference-grants"] }) });

  const selectedAssets = useMemo<SelectedAsset[]>(() => {
    const normalizedQuery = query.trim().toLocaleLowerCase();
    return (hierarchyQuery.data?.data ?? []).flatMap((category) =>
      category.skus.flatMap((sku) =>
        sku.assets
          .filter(() => {
            if (!normalizedQuery) return true;
            const searchable = [category.name, category.name_en, sku.code, sku.product_name, ...sku.tags.map((tag) => tag.name)].join(" ").toLocaleLowerCase();
            return searchable.includes(normalizedQuery);
          })
          .map((asset) => ({ category, sku, asset })),
      ),
    );
  }, [hierarchyQuery.data, query]);
  const selected = selectedAssets.find(({ asset }) => asset.public_id === selectedAssetID) ?? selectedAssets[0];

  const grouped = useMemo(() => {
    const assetsBySKU = new Map<string, Asset[]>();
    for (const item of selectedAssets) {
      assetsBySKU.set(item.sku.public_id, [...(assetsBySKU.get(item.sku.public_id) ?? []), item.asset]);
    }
    return (hierarchyQuery.data?.data ?? [])
      .map((category) => ({
        ...category,
        skus: category.skus
          .map((sku) => ({ ...sku, assets: assetsBySKU.get(sku.public_id) ?? [] }))
          .filter((sku) => sku.assets.length > 0),
      }))
      .filter((category) => category.skus.length > 0);
  }, [hierarchyQuery.data, selectedAssets]);

  const statusLabel = (status: ReviewStatus) =>
    status === "approved" ? t("approved") : status === "rejected" ? t("rejected") : t("pendingReview");
	const viewName = (asset: Asset) => asset.origin_type === "ai_generated"
		? (language === "zh" ? "AI 生成成品" : "AI-generated output")
		: asset.sop_view_name[language === "zh" ? "zh-CN" : "en"];

  return (
    <div className="space-y-6">
      <div>
        <p className="mb-2 text-[11px] font-bold uppercase tracking-[0.16em] text-primary">CargoFlows · Quality gate</p>
        <h1 className="text-3xl font-bold tracking-tight text-navy sm:text-4xl">{t("assetReview")}</h1>
        <p className="mt-2 text-sm text-muted-foreground">{t("categoryHierarchyDesc")}</p>
      </div>

      <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_380px]">
        <section className="min-w-0 overflow-hidden rounded-xl border border-border bg-card shadow-[var(--shadow-sm)]">
          <div className="flex flex-col gap-3 border-b border-border p-4 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex items-center gap-2">
              <ImageUp className="h-4 w-4 text-primary" />
              <h2 className="text-sm font-semibold">{t("categoryHierarchy")}</h2>
            </div>
            <div className="relative w-full sm:max-w-xs">
              <Search className="pointer-events-none absolute left-3.5 top-3.5 h-4 w-4 text-muted-foreground" />
              <Input className="pl-9" onChange={(event) => setQuery(event.target.value)} placeholder={t("searchAssets")} value={query} />
            </div>
          </div>

          <div className="divide-y divide-border">
            {grouped.map((category) => (
              <section key={category.id || category.name}>
                <div className="flex items-center gap-2 bg-muted/40 px-4 py-2.5">
                  <span className="text-sm font-semibold">{categoryLabel(category, language)}</span>
                  {category.is_system ? <Badge variant="neutral">{t("systemCategory")}</Badge> : null}
                </div>
                <div className="divide-y divide-border">
                  {category.skus.map((sku) => (
                    <div className="px-4 py-3" key={sku.public_id}>
                      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
                        <Link className="font-medium text-primary" href={`/skus/${sku.public_id}`}>
                          {sku.code}
                        </Link>
                        <span className="text-sm text-muted-foreground">{sku.product_name}</span>
                        <span className="text-xs text-muted-foreground">
                          {sku.assets.length} {t("assetCount")}
                        </span>
                        {sku.tags.map((tag) => (
                          <Badge key={tag.name} variant="neutral">
                            <Tags className="h-3 w-3" />
                            {tag.name}
                          </Badge>
                        ))}
                      </div>
                      <div className="mt-2 grid gap-1">
                        {sku.assets.map((asset) => (
                          <button
                            className={`grid grid-cols-[minmax(0,1fr)_auto_auto] items-center gap-3 rounded-md px-2 py-2 text-left text-sm transition-colors hover:bg-muted ${selected?.asset.public_id === asset.public_id ? "bg-muted" : ""}`}
                            key={asset.public_id}
                            onClick={() => setSelectedAssetID(asset.public_id)}
                            type="button"
                          >
							<span className="min-w-0 truncate">{viewName(asset)} {asset.origin_type === "ai_generated" ? <Badge variant="warning">AI</Badge> : null}</span>
                            <ReviewBadge label={statusLabel(asset.review_status)} status={asset.review_status} />
                            <ChevronRight className="h-4 w-4 text-muted-foreground" />
                          </button>
                        ))}
                      </div>
                    </div>
                  ))}
                </div>
              </section>
            ))}
            {!hierarchyQuery.isLoading && grouped.length === 0 ? (
              <p className="px-4 py-10 text-center text-sm text-muted-foreground">{t("noData")}</p>
            ) : null}
          </div>
        </section>

        <aside className="xl:sticky xl:top-20 xl:self-start">
          {selected ? (
            <div className="overflow-hidden rounded-xl border border-border bg-card shadow-[var(--shadow-md)]">
              <div className="border-b border-border p-4">
                <p className="text-xs text-muted-foreground">{categoryLabel(selected.category, language)}</p>
                <p className="mt-1 font-semibold">{selected.sku.code} · {viewName(selected.asset)}</p>
				{selected.asset.origin_type === "ai_generated" ? <p className="mt-2 text-xs leading-5 text-amber-700">{language === "zh" ? "AI 来源：审核通过仅表示可作为渠道成品使用，不能作为商品身份或事实依据。" : "AI source: approval permits channel use only; it never establishes product identity or facts."}</p> : null}
              </div>
              <div className="aspect-[4/5] bg-muted">
                {/* MinIO image URLs are private authenticated assets and bypass Next Image optimization. */}
                <img alt={`${selected.sku.code} ${viewName(selected.asset)}`} className="h-full w-full object-contain" src={authenticatedMediaURL(selected.asset.media_url)} />
              </div>
              <div className="space-y-4 p-4">
                <div className="flex items-center justify-between gap-3">
                  <ReviewBadge label={statusLabel(selected.asset.review_status)} status={selected.asset.review_status} />
                  <span className="text-xs text-muted-foreground">{formatDateTime(selected.asset.captured_at)}</span>
                </div>
                <p className="text-sm text-muted-foreground">{selected.asset.photo_session_code}</p>
                {selected.asset.origin_type === "ai_generated" ? <dl className="grid gap-2 rounded-lg bg-muted/50 p-3 text-xs"><div className="flex justify-between gap-3"><dt>{language === "zh" ? "模型" : "Model"}</dt><dd className="font-mono">{selected.asset.source_summary.model || "—"}</dd></div><div className="flex justify-between gap-3"><dt>{language === "zh" ? "API 路径" : "API path"}</dt><dd className="font-mono">{selected.asset.source_summary.api_mode || "—"}</dd></div><div className="flex justify-between gap-3"><dt>{language === "zh" ? "生成任务" : "Source job"}</dt><dd className="max-w-48 truncate font-mono">{selected.asset.source_summary.job_id || "—"}</dd></div></dl> : null}
				{selected.asset.origin_type === "ai_generated" ? <section><h3 className="text-xs font-semibold">{language === "zh" ? "目标 SKU 真实身份素材对比" : "Target SKU real identity comparison"}</h3><div className="mt-2 grid grid-cols-3 gap-2">{selectedAssets.filter((item) => item.sku.public_id === selected.sku.public_id && item.asset.origin_type !== "ai_generated").slice(0, 3).map((item) => <img alt={viewName(item.asset)} className="aspect-square rounded-md bg-muted object-cover" key={item.asset.public_id} src={authenticatedMediaURL(item.asset.media_url)} />)}</div></section> : null}
                <div className="flex gap-2">
                  <Button
                    className="flex-1"
                    disabled={reviewMutation.isPending}
                    onClick={() => reviewMutation.mutate({ id: selected.asset.public_id, status: "approved" })}
                    variant="secondary"
                  >
                    <Check className="h-4 w-4" />
                    {t("approve")}
                  </Button>
                  <Button
                    className="flex-1"
                    disabled={reviewMutation.isPending}
                    onClick={() => reviewMutation.mutate({ id: selected.asset.public_id, status: "rejected" })}
                    variant="danger"
                  >
                    <X className="h-4 w-4" />
                    {t("reject")}
                  </Button>
                </div>
				{selected.asset.review_status === "approved" ? <Button className="w-full" onClick={() => setStyleDialog(true)} variant="outline"><Palette className="h-4 w-4" />{language === "zh" ? "授权为跨 SKU 风格参考" : "Authorize as cross-SKU style"}</Button> : null}
              </div>
            </div>
          ) : (
            <div className="rounded-lg border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
              {t("selectAsset")}
            </div>
          )}
        </aside>
      </div>

      <section className="overflow-hidden rounded-xl border border-border bg-card shadow-[var(--shadow-sm)]">
        <div className="border-b border-border p-4"><div className="flex items-center gap-2"><Palette className="h-4 w-4 text-primary" /><h2 className="text-sm font-semibold">{language === "zh" ? "跨 SKU 风格授权" : "Cross-SKU style grants"}<Badge className="ml-2" variant="neutral">{styleReferencesQuery.data?.data.length ?? 0}</Badge></h2></div><p className="mt-1 text-xs leading-5 text-muted-foreground">{language === "zh" ? "这里只管理去除来源商品主体后的私有派生图。撤销只影响未来任务，历史任务继续使用冻结版本。" : "Only private derivatives with the source product removed are managed here. Revocation affects future jobs; historical snapshots keep their frozen version."}</p></div>
        <div className="divide-y divide-border">{(styleReferencesQuery.data?.data ?? []).map((grant) => <article className="grid gap-3 p-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center" key={grant.public_id}><div><div className="flex flex-wrap items-center gap-2"><Badge variant="success">{grant.status}</Badge><span className="font-mono text-xs">V{grant.version} · {grant.source_sku_id.slice(0, 8)}</span></div><p className="mt-2 text-sm">{language === "zh" ? grant.description_zh : grant.description_en}</p><p className="mt-1 font-mono text-[11px] text-muted-foreground">SHA-256 {grant.derivative_sha256.slice(0, 12)}…</p></div><Button aria-label={`${language === "zh" ? "撤销风格授权" : "Revoke style grant"}: ${grant.public_id}`} disabled={revokeStyleReference.isPending} onClick={() => revokeStyleReference.mutate(grant.public_id)} size="sm" variant="ghost"><X className="h-4 w-4" />{language === "zh" ? "撤销" : "Revoke"}</Button></article>)}{!styleReferencesQuery.isLoading && !(styleReferencesQuery.data?.data.length) ? <p className="p-6 text-center text-sm text-muted-foreground">{language === "zh" ? "暂无风格授权。" : "No style grants yet."}</p> : null}</div>
        {styleReferencesQuery.isError || revokeStyleReference.isError ? <p aria-live="polite" className="m-4 rounded-md border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert">{language === "zh" ? "风格授权列表或撤销操作失败，请重试。" : "Style grants could not be loaded or revoked. Try again."}</p> : null}
      </section>
		{styleDialog && selected ? <div aria-modal="true" className="fixed inset-0 z-50 grid overflow-y-auto bg-navy/55 p-4" role="dialog"><form className="m-auto w-full max-w-lg space-y-4 rounded-xl bg-card p-5 shadow-2xl" onSubmit={(event) => { event.preventDefault(); styleMutation.mutate(); }}><div className="flex items-start justify-between gap-3"><div><h2 className="font-semibold">{language === "zh" ? "创建风格授权" : "Create style grant"}</h2><p className="mt-1 text-xs leading-5 text-muted-foreground">{language === "zh" ? "上传与原图同尺寸的 PNG 商品排除蒙版：透明区域必须完整覆盖商品主体。服务端只保存去除主体后的私有派生图。" : "Upload a same-size PNG exclusion mask. Transparent pixels must fully cover the product. Only a private product-free derivative is retained for reuse."}</p></div><Button aria-label={language === "zh" ? "关闭" : "Close"} onClick={() => setStyleDialog(false)} size="icon" type="button" variant="ghost"><X className="h-4 w-4" /></Button></div><label className="block text-sm font-medium">中文风格说明<Textarea className="mt-2" maxLength={2000} onChange={(event) => setStyleZH(event.target.value)} value={styleZH} /></label><label className="block text-sm font-medium">English style description<Textarea className="mt-2" maxLength={2000} onChange={(event) => setStyleEN(event.target.value)} value={styleEN} /></label><label className="block text-sm font-medium">{language === "zh" ? "商品排除蒙版（PNG）" : "Product exclusion mask (PNG)"}<Input accept="image/png" className="mt-2" onChange={(event) => setStyleMask(event.target.files?.[0])} type="file" /></label>{styleMutation.isError ? <p aria-live="polite" className="text-sm text-danger" role="alert">{styleMutation.error instanceof Error ? styleMutation.error.message : (language === "zh" ? "创建失败" : "Creation failed")}</p> : null}<Button className="w-full" disabled={styleMutation.isPending || !styleMask || !styleZH.trim() || !styleEN.trim()} type="submit">{styleMutation.isPending ? (language === "zh" ? "创建中…" : "Creating…") : (language === "zh" ? "生成私有派生图并授权" : "Create private derivative and grant")}</Button></form></div> : null}
    </div>
  );
}
