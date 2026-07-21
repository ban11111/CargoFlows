"use client";
/* eslint-disable @next/next/no-img-element -- authenticated private media bypasses the public image optimizer */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Check, ChevronDown, ChevronLeft, ChevronRight, ChevronUp, ImageIcon, Palette, RotateCcw, X } from "lucide-react";
import Link from "next/link";
import { useParams, useSearchParams } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { StyleGrantDialog } from "@/components/assets/style-grants";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { apiRequest, authenticatedMediaURL } from "@/lib/api";
import { categoryLabel } from "@/lib/category-label";
import { useLanguage } from "@/lib/i18n";
import { formatDateTime } from "@/lib/utils";

type ReviewStatus = "pending" | "approved" | "rejected";
interface Counts { pending: number; approved: number; rejected: number; total: number }
interface Category { id: number; name: string; name_en: string; is_system: boolean }
interface SKUDetail { public_id: string; code: string; product_name: string; category: Category; tags: Array<{ name: string }>; counts: Counts }
interface Asset {
  public_id: string;
  sku_id: string;
  media_url: string;
  review_status: ReviewStatus;
  captured_at: string;
  sop_view_key: string;
  sop_view_name: { "zh-CN": string; en: string };
  photo_session_code: string;
  origin_type: "captured" | "uploaded" | "ai_generated";
  source_summary: Record<string, string>;
}
interface Pagination { page: number; page_size: number; total: number; total_pages: number }
interface AssetPage { data: Asset[]; pagination: Pagination }
interface UndoState { assetID: string; previousStatus: ReviewStatus; nextStatus: ReviewStatus }

const statuses: ReviewStatus[] = ["pending", "approved", "rejected"];

function statusVariant(status: ReviewStatus) {
  return status === "approved" ? "success" : status === "rejected" ? "danger" : "warning";
}

export default function SKUAssetReviewPage() {
  const { skuId } = useParams<{ skuId: string }>();
  const searchParams = useSearchParams();
  const queryClient = useQueryClient();
  const { language } = useLanguage();
  const zh = language === "zh";
  const requestedReturn = searchParams.get("return") ?? "/assets/review";
  const returnHref = requestedReturn.startsWith("/assets/review") ? requestedReturn : "/assets/review";
  const [expanded, setExpanded] = useState<Set<ReviewStatus>>(() => new Set(["pending"]));
  const [pages, setPages] = useState<Record<ReviewStatus, number>>({ pending: 1, approved: 1, rejected: 1 });
  const [selectedID, setSelectedID] = useState<string>();
  const [mobileDetailOpen, setMobileDetailOpen] = useState(false);
  const [styleDialog, setStyleDialog] = useState(false);
  const [undo, setUndo] = useState<UndoState>();
  const mobileDetailRef = useRef<HTMLDivElement>(null);

  const sku = useQuery({ queryKey: ["assets", "review", "sku", skuId], queryFn: () => apiRequest<SKUDetail>(`/assets/review/skus/${skuId}`) });
  const assetQuery = (status: ReviewStatus) => ({
    queryKey: ["assets", "review", skuId, status, pages[status]],
    queryFn: () => apiRequest<AssetPage>(`/assets/review?sku_id=${skuId}&status=${status}&page=${pages[status]}&page_size=48`),
    enabled: expanded.has(status),
  });
  // The three fixed hooks intentionally keep each status group independently paginated and cached.
  const pendingAssets = useQuery(assetQuery("pending"));
  const approvedAssets = useQuery(assetQuery("approved"));
  const rejectedAssets = useQuery(assetQuery("rejected"));
  const queries: Record<ReviewStatus, typeof pendingAssets> = { pending: pendingAssets, approved: approvedAssets, rejected: rejectedAssets };
  const loadedAssets = useMemo(() => [...(pendingAssets.data?.data ?? []), ...(approvedAssets.data?.data ?? []), ...(rejectedAssets.data?.data ?? [])], [pendingAssets.data, approvedAssets.data, rejectedAssets.data]);
  const effectiveSelectedID = selectedID && loadedAssets.some((asset) => asset.public_id === selectedID) ? selectedID : loadedAssets[0]?.public_id;
  const selected = loadedAssets.find((asset) => asset.public_id === effectiveSelectedID);

  const selectAsset = useCallback((asset: Asset, openMobile = false) => {
    setSelectedID(asset.public_id);
    if (openMobile) setMobileDetailOpen(true);
  }, []);

  useEffect(() => {
    if (!undo) return;
    const timer = window.setTimeout(() => setUndo(undefined), 7000);
    return () => window.clearTimeout(timer);
  }, [undo]);

  useEffect(() => {
    if (!mobileDetailOpen) return;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    if (mobileDetailRef.current) mobileDetailRef.current.scrollTop = 0;
    return () => { document.body.style.overflow = previousOverflow; };
  }, [mobileDetailOpen]);

  const invalidateReview = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["assets", "review", skuId] }),
      queryClient.invalidateQueries({ queryKey: ["assets", "review", "sku", skuId] }),
      queryClient.invalidateQueries({ queryKey: ["assets", "review", "skus"] }),
    ]);
  };

  const review = useMutation({
    mutationFn: ({ asset, status, reason }: { asset: Asset; status: ReviewStatus; reason?: string }) => apiRequest(`/assets/${asset.public_id}/review`, { method: "PATCH", body: JSON.stringify({ status, reason: reason ?? "" }) }),
    onSuccess: async (_, variables) => {
      const wasPending = variables.asset.review_status === "pending";
      const pendingList = pendingAssets.data?.data ?? [];
      const currentIndex = pendingList.findIndex((asset) => asset.public_id === variables.asset.public_id);
      const nextPending = wasPending ? (pendingList[currentIndex + 1] ?? pendingList[currentIndex - 1]) : pendingList[0];
      setUndo({ assetID: variables.asset.public_id, previousStatus: variables.asset.review_status, nextStatus: variables.status });
      if (nextPending) {
        setSelectedID(nextPending.public_id);
      } else if (wasPending && (pendingAssets.data?.pagination.total_pages ?? 0) > pages.pending) {
        setPages((value) => ({ ...value, pending: value.pending + 1 }));
        setSelectedID(undefined);
      } else if (wasPending && pages.pending > 1) {
        setPages((value) => ({ ...value, pending: value.pending - 1 }));
        setSelectedID(undefined);
      }
      await invalidateReview();
    },
  });

  const undoReview = useMutation({
    mutationFn: async (value: UndoState) => {
      const asset = loadedAssets.find((item) => item.public_id === value.assetID) ?? { public_id: value.assetID } as Asset;
      return apiRequest(`/assets/${value.assetID}/review`, { method: "PATCH", body: JSON.stringify({ status: value.previousStatus, reason: "undo" }) }).then(() => asset);
    },
    onSuccess: async (asset, value) => {
      setExpanded((current) => new Set(current).add(value.previousStatus));
      setSelectedID(asset.public_id);
      setUndo(undefined);
      await invalidateReview();
    },
  });

  const navigateSelection = useCallback((direction: -1 | 1) => {
    if (!selected) return;
    const group = selected.review_status === "pending" ? (pendingAssets.data?.data ?? []) : selected.review_status === "approved" ? (approvedAssets.data?.data ?? []) : (rejectedAssets.data?.data ?? []);
    const index = group.findIndex((asset) => asset.public_id === selected.public_id);
    const next = group[index + direction];
    if (next) setSelectedID(next.public_id);
  }, [approvedAssets.data, pendingAssets.data, rejectedAssets.data, selected]);

  useEffect(() => {
    const handleKey = (event: KeyboardEvent) => {
      if (event.target instanceof HTMLInputElement || event.target instanceof HTMLTextAreaElement || event.target instanceof HTMLSelectElement) return;
      if (event.key === "ArrowLeft") { event.preventDefault(); navigateSelection(-1); }
      if (event.key === "ArrowRight") { event.preventDefault(); navigateSelection(1); }
      if (event.key === "Escape") setMobileDetailOpen(false);
    };
    window.addEventListener("keydown", handleKey);
    return () => window.removeEventListener("keydown", handleKey);
  }, [navigateSelection]);

  const statusLabel = (status: ReviewStatus) => status === "pending" ? (zh ? "待审核" : "Pending") : status === "approved" ? (zh ? "已通过" : "Approved") : (zh ? "已拒绝" : "Rejected");
  const viewName = (asset: Asset) => asset.origin_type === "ai_generated" ? (zh ? "AI 生成成品" : "AI-generated output") : asset.sop_view_name[zh ? "zh-CN" : "en"];

  const detailPanel = selected ? (
    <div className="overflow-hidden rounded-xl border border-border bg-card shadow-[var(--shadow-md)]">
      <div className="flex items-start justify-between gap-3 border-b border-border p-4">
        <div><div className="flex flex-wrap items-center gap-2"><Badge variant={statusVariant(selected.review_status)}>{statusLabel(selected.review_status)}</Badge>{selected.origin_type === "ai_generated" ? <Badge variant="warning">AI</Badge> : null}</div><p className="mt-2 font-semibold">{viewName(selected)}</p><p className="mt-1 text-xs text-muted-foreground">{selected.photo_session_code || selected.source_summary.job_id || "—"}</p></div>
        <Button aria-label={zh ? "关闭详情" : "Close details"} className="xl:hidden" onClick={() => setMobileDetailOpen(false)} size="icon" variant="ghost"><X className="h-4 w-4" /></Button>
      </div>
      <div className="aspect-[4/5] bg-muted"><img alt={`${sku.data?.code ?? "SKU"} ${viewName(selected)}`} className="h-full w-full object-contain" src={authenticatedMediaURL(selected.media_url)} /></div>
      <div className="space-y-4 p-4">
        <div className="flex items-center justify-between gap-3 text-xs text-muted-foreground"><span>{selected.origin_type === "captured" ? (zh ? "现场拍摄" : "Captured") : selected.origin_type === "uploaded" ? (zh ? "上传素材" : "Uploaded") : (zh ? "AI 生成" : "AI generated")}</span><span>{formatDateTime(selected.captured_at)}</span></div>
        {selected.origin_type === "ai_generated" ? <><p className="rounded-lg bg-warning/10 p-3 text-xs leading-5 text-warning">{zh ? "审核通过仅表示可作为渠道成品使用，不能作为商品身份或事实依据。" : "Approval permits channel use only; it never establishes product identity or facts."}</p><dl className="grid gap-2 rounded-lg bg-muted/50 p-3 text-xs"><div className="flex justify-between gap-3"><dt>{zh ? "模型" : "Model"}</dt><dd className="font-mono">{selected.source_summary.model || "—"}</dd></div><div className="flex justify-between gap-3"><dt>{zh ? "API 路径" : "API path"}</dt><dd className="font-mono">{selected.source_summary.api_mode || "—"}</dd></div></dl><section><h3 className="text-xs font-semibold">{zh ? "真实身份素材对比" : "Real identity comparison"}</h3><div className="mt-2 grid grid-cols-3 gap-2">{loadedAssets.filter((asset) => asset.origin_type !== "ai_generated").slice(0, 3).map((asset) => <img alt={viewName(asset)} className="aspect-square rounded-md bg-muted object-cover" key={asset.public_id} src={authenticatedMediaURL(asset.media_url)} />)}</div></section></> : null}
        <div className="grid grid-cols-2 gap-2"><Button disabled={review.isPending || selected.review_status === "approved"} onClick={() => review.mutate({ asset: selected, status: "approved" })} variant="secondary"><Check className="h-4 w-4" />{zh ? "批准" : "Approve"}</Button><Button disabled={review.isPending || selected.review_status === "rejected"} onClick={() => review.mutate({ asset: selected, status: "rejected" })} variant="danger"><X className="h-4 w-4" />{zh ? "拒绝" : "Reject"}</Button></div>
        {selected.review_status === "approved" ? <Button className="w-full" onClick={() => setStyleDialog(true)} variant="outline"><Palette className="h-4 w-4" />{zh ? "授权为跨 SKU 风格参考" : "Authorize as cross-SKU style"}</Button> : null}
        {review.isError ? <p className="text-sm text-danger" role="alert">{zh ? "审核操作失败，素材状态未改变。" : "Review failed. The asset status was not changed."}</p> : null}
      </div>
    </div>
  ) : <div className="grid min-h-64 place-items-center rounded-xl border border-dashed border-border p-8 text-center text-sm text-muted-foreground"><div><ImageIcon className="mx-auto mb-3 h-7 w-7" />{zh ? "选择一张素材开始审核" : "Select an asset to start reviewing"}</div></div>;

  return (
    <div className="space-y-6">
      <header>
        <Button asChild size="sm" variant="ghost"><Link href={returnHref}><ArrowLeft className="h-4 w-4" />{zh ? "返回审核队列" : "Back to review queue"}</Link></Button>
        <div className="mt-4 flex flex-col justify-between gap-4 lg:flex-row lg:items-end">
          <div><p className="text-xs font-bold uppercase tracking-[0.14em] text-primary">{sku.data ? categoryLabel(sku.data.category, language) : "CargoFlows"}</p><h1 className="mt-1 text-3xl font-bold text-navy">{sku.data?.code ?? (zh ? "素材审核" : "Asset review")}</h1><p className="mt-1 text-sm text-muted-foreground">{sku.data?.product_name}</p><div className="mt-2 flex flex-wrap gap-1">{sku.data?.tags.map((tag) => <Badge key={tag.name} variant="neutral">{tag.name}</Badge>)}</div></div>
          <div className="grid grid-cols-3 overflow-hidden rounded-xl border border-border bg-card shadow-[var(--shadow-sm)]"><div className="px-4 py-3"><p className="text-[11px] text-muted-foreground">{statusLabel("pending")}</p><strong className="text-xl text-warning">{sku.data?.counts.pending ?? 0}</strong></div><div className="border-x border-border px-4 py-3"><p className="text-[11px] text-muted-foreground">{statusLabel("approved")}</p><strong className="text-xl text-success">{sku.data?.counts.approved ?? 0}</strong></div><div className="px-4 py-3"><p className="text-[11px] text-muted-foreground">{statusLabel("rejected")}</p><strong className="text-xl text-danger">{sku.data?.counts.rejected ?? 0}</strong></div></div>
        </div>
      </header>

      <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_390px]">
        <main className="min-w-0 space-y-3">
          {statuses.map((status) => {
            const count = sku.data?.counts[status] ?? 0;
            const open = expanded.has(status);
            const query = queries[status];
            const page = pages[status];
            return <section className="overflow-hidden rounded-xl border border-border bg-card shadow-[var(--shadow-sm)]" key={status}><button aria-expanded={open} className="flex w-full items-center justify-between gap-3 p-4 text-left hover:bg-muted/40" onClick={() => setExpanded((current) => { const next = new Set(current); if (next.has(status)) next.delete(status); else next.add(status); return next; })} type="button"><span className="flex items-center gap-2"><Badge variant={statusVariant(status)}>{count}</Badge><strong className="text-sm">{statusLabel(status)}</strong>{status === "pending" && count > 0 ? <span className="text-xs text-muted-foreground">{zh ? "优先处理" : "Priority queue"}</span> : null}</span>{open ? <ChevronUp className="h-4 w-4 text-muted-foreground" /> : <ChevronDown className="h-4 w-4 text-muted-foreground" />}</button>{open ? <div className="border-t border-border p-3 sm:p-4">{query.isLoading ? <div className="grid min-h-32 place-items-center text-sm text-muted-foreground">{zh ? "正在加载素材…" : "Loading assets…"}</div> : null}{query.isError ? <p className="rounded-lg border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert">{zh ? "素材加载失败，请重试。" : "Assets could not be loaded. Try again."}</p> : null}<div className="grid grid-cols-3 gap-2 sm:grid-cols-4 lg:grid-cols-6">{(query.data?.data ?? []).map((asset) => <button aria-label={`${viewName(asset)} · ${statusLabel(asset.review_status)}`} aria-pressed={effectiveSelectedID === asset.public_id} className={`group relative aspect-square overflow-hidden rounded-lg border-2 bg-muted transition ${effectiveSelectedID === asset.public_id ? "border-primary ring-2 ring-primary/20" : "border-transparent hover:border-primary/40"}`} key={asset.public_id} onClick={() => selectAsset(asset, true)} type="button"><img alt="" className="h-full w-full object-cover transition duration-200 group-hover:scale-[1.03]" src={authenticatedMediaURL(asset.media_url)} /><span className="absolute inset-x-0 bottom-0 truncate bg-navy/75 px-2 py-1 text-left text-[10px] text-white">{viewName(asset)}</span>{asset.origin_type === "ai_generated" ? <span className="absolute right-1 top-1 rounded bg-warning px-1.5 py-0.5 text-[9px] font-bold text-white">AI</span> : null}</button>)}</div>{!query.isLoading && !query.data?.data.length ? <p className="py-8 text-center text-sm text-muted-foreground">{zh ? "此状态下暂无素材。" : "No assets in this status."}</p> : null}{(query.data?.pagination.total_pages ?? 0) > 1 ? <div className="mt-4 flex items-center justify-between border-t border-border pt-3"><span className="text-xs text-muted-foreground">{page} / {query.data?.pagination.total_pages}</span><div className="flex gap-2"><Button aria-label={zh ? "上一页" : "Previous page"} disabled={page <= 1} onClick={() => setPages((value) => ({ ...value, [status]: page - 1 }))} size="icon" variant="secondary"><ChevronLeft className="h-4 w-4" /></Button><Button aria-label={zh ? "下一页" : "Next page"} disabled={page >= (query.data?.pagination.total_pages ?? 1)} onClick={() => setPages((value) => ({ ...value, [status]: page + 1 }))} size="icon" variant="secondary"><ChevronRight className="h-4 w-4" /></Button></div></div> : null}</div> : null}</section>;
          })}
        </main>
        <aside className="hidden xl:sticky xl:top-20 xl:block xl:self-start">{detailPanel}</aside>
      </div>

      {mobileDetailOpen && typeof document !== "undefined" ? createPortal(<div className="fixed inset-0 z-[60] overflow-y-auto bg-background p-3 xl:hidden" ref={mobileDetailRef}><div className="mx-auto max-w-xl">{detailPanel}</div></div>, document.body) : null}
      {undo && typeof document !== "undefined" ? createPortal(<div aria-live="polite" className="fixed bottom-4 left-1/2 z-[90] flex w-[calc(100%-2rem)] max-w-md -translate-x-1/2 items-center justify-between gap-3 rounded-xl bg-navy px-4 py-3 text-sm text-white shadow-2xl"><span>{zh ? `已${undo.nextStatus === "approved" ? "批准" : "拒绝"}素材` : `Asset ${undo.nextStatus === "approved" ? "approved" : "rejected"}`}</span><Button disabled={undoReview.isPending} onClick={() => undoReview.mutate(undo)} size="sm" variant="secondary"><RotateCcw className="h-4 w-4" />{zh ? "撤销" : "Undo"}</Button></div>, document.body) : null}
      {styleDialog && selected ? <StyleGrantDialog assetID={selected.public_id} onClose={() => setStyleDialog(false)} /> : null}
      {sku.isError ? <div className="rounded-lg border border-danger/30 bg-danger/5 p-4 text-sm text-danger" role="alert">{zh ? "SKU 审核信息加载失败。" : "SKU review details could not be loaded."}</div> : null}
    </div>
  );
}
