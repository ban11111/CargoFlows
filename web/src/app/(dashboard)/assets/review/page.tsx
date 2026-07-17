"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, ChevronRight, ImageUp, Search, Tags, X } from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { apiRequest } from "@/lib/api";
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

function ReviewBadge({ status, label }: { status: ReviewStatus; label: string }) {
  const variant = status === "approved" ? "success" : status === "rejected" ? "danger" : "warning";
  return <Badge variant={variant}>{label}</Badge>;
}

export default function AssetReviewPage() {
  const { language, t } = useLanguage();
  const queryClient = useQueryClient();
  const [query, setQuery] = useState("");
  const [selectedAssetID, setSelectedAssetID] = useState<string>();
  const hierarchyQuery = useQuery({
    queryKey: ["assets", "review", "hierarchy"],
    queryFn: () => apiRequest<{ data: HierarchyCategory[] }>("/assets/review/hierarchy"),
  });
  const reviewMutation = useMutation({
    mutationFn: ({ id, status }: { id: string; status: "approved" | "rejected" }) =>
      apiRequest(`/assets/${id}/review`, { method: "PATCH", body: JSON.stringify({ status }) }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["assets", "review"] });
    },
  });

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
  const viewName = (asset: Asset) => asset.sop_view_name[language === "zh" ? "zh-CN" : "en"];

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">{t("assetReview")}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{t("categoryHierarchyDesc")}</p>
      </div>

      <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_380px]">
        <section className="min-w-0 overflow-hidden rounded-lg border border-border bg-card">
          <div className="flex flex-col gap-3 border-b border-border p-4 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex items-center gap-2">
              <ImageUp className="h-4 w-4 text-primary" />
              <h2 className="text-sm font-semibold">{t("categoryHierarchy")}</h2>
            </div>
            <div className="relative w-full sm:max-w-xs">
              <Search className="pointer-events-none absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" />
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
                            <span className="min-w-0 truncate">{viewName(asset)}</span>
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
            <div className="overflow-hidden rounded-lg border border-border bg-card">
              <div className="border-b border-border p-4">
                <p className="text-xs text-muted-foreground">{categoryLabel(selected.category, language)}</p>
                <p className="mt-1 font-semibold">{selected.sku.code} · {viewName(selected.asset)}</p>
              </div>
              <div className="aspect-[4/5] bg-muted">
                {/* MinIO image URLs are local development assets and bypass Next Image optimization. */}
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img alt={`${selected.sku.code} ${viewName(selected.asset)}`} className="h-full w-full object-contain" src={`/api/proxy${selected.asset.media_url}`} />
              </div>
              <div className="space-y-4 p-4">
                <div className="flex items-center justify-between gap-3">
                  <ReviewBadge label={statusLabel(selected.asset.review_status)} status={selected.asset.review_status} />
                  <span className="text-xs text-muted-foreground">{formatDateTime(selected.asset.captured_at)}</span>
                </div>
                <p className="text-sm text-muted-foreground">{selected.asset.photo_session_code}</p>
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
              </div>
            </div>
          ) : (
            <div className="rounded-lg border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
              {t("selectAsset")}
            </div>
          )}
        </aside>
      </div>
    </div>
  );
}
