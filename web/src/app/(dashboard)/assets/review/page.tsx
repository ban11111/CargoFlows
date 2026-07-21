"use client";
/* eslint-disable @next/next/no-img-element -- authenticated private media bypasses the public image optimizer */

import { useQuery } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight, ClipboardCheck, Filter, Search } from "lucide-react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useMemo, useState } from "react";
import { StyleGrantsPanel } from "@/components/assets/style-grants";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { apiRequest, authenticatedMediaURL } from "@/lib/api";
import { categoryLabel } from "@/lib/category-label";
import { useLanguage } from "@/lib/i18n";
import { formatDateTime } from "@/lib/utils";

type ReviewStatus = "pending" | "approved" | "rejected";
interface Counts { pending: number; approved: number; rejected: number; total: number }
interface Category { id: number; name: string; name_en: string; is_system: boolean }
interface QueueSKU {
  public_id: string;
  code: string;
  product_name: string;
  category: Category;
  tags: Array<{ name: string }>;
  counts: Counts;
  latest_asset_at: string;
  latest_pending_at: string | null;
  cover_asset: { public_id: string; media_url: string; review_status: ReviewStatus; origin_type: string } | null;
}
interface Pagination { page: number; page_size: number; total: number; total_pages: number }

function StatusRail({ counts }: { counts: Counts }) {
  const total = Math.max(counts.total, 1);
  return <div aria-label={`Pending ${counts.pending}, approved ${counts.approved}, rejected ${counts.rejected}`} className="flex h-1.5 w-full min-w-28 overflow-hidden rounded-full bg-muted"><span className="bg-warning" style={{ width: `${counts.pending / total * 100}%` }} /><span className="bg-success" style={{ width: `${counts.approved / total * 100}%` }} /><span className="bg-danger" style={{ width: `${counts.rejected / total * 100}%` }} /></div>;
}

function AssetReviewQueue() {
  const { language } = useLanguage();
  const zh = language === "zh";
  const router = useRouter();
  const searchParams = useSearchParams();
  const tab = searchParams.get("tab") === "styles" ? "styles" : "review";
  const page = Math.max(1, Number(searchParams.get("page")) || 1);
  const status = searchParams.get("status") ?? "";
  const categoryID = searchParams.get("category_id") ?? "";
  const query = searchParams.get("q") ?? "";
  const [searchEdit, setSearchEdit] = useState({ base: query, value: query });
  const search = searchEdit.base === query ? searchEdit.value : query;

  useEffect(() => {
    if (search === query) return;
    const timer = window.setTimeout(() => {
      const next = new URLSearchParams(searchParams.toString());
      if (search.trim()) next.set("q", search.trim()); else next.delete("q");
      next.delete("page");
      router.replace(`/assets/review?${next.toString()}`);
    }, 300);
    return () => window.clearTimeout(timer);
  }, [query, router, search, searchParams]);

  const categories = useQuery({ queryKey: ["categories"], queryFn: () => apiRequest<{ data: Category[] }>("/categories") });
  const queueParams = useMemo(() => {
    const params = new URLSearchParams({ page: String(page), page_size: "40" });
    if (query) params.set("q", query);
    if (status) params.set("status", status);
    if (categoryID) params.set("category_id", categoryID);
    return params;
  }, [categoryID, page, query, status]);
  const queue = useQuery({
    queryKey: ["assets", "review", "skus", queueParams.toString()],
    queryFn: () => apiRequest<{ data: QueueSKU[]; pagination: Pagination }>(`/assets/review/skus?${queueParams}`),
    enabled: tab === "review",
  });

  const setParam = (name: string, value: string) => {
    const next = new URLSearchParams(searchParams.toString());
    if (value) next.set(name, value); else next.delete(name);
    if (name !== "page") next.delete("page");
    router.replace(`/assets/review?${next.toString()}`);
  };
  const currentReturn = `/assets/review?${searchParams.toString()}`;
  const statusLabel = (value: string) => value === "pending" ? (zh ? "待审核" : "Pending") : value === "approved" ? (zh ? "已通过" : "Approved") : value === "rejected" ? (zh ? "已拒绝" : "Rejected") : (zh ? "全部状态" : "All statuses");

  return (
    <div className="space-y-6">
      <header>
        <p className="mb-2 text-[11px] font-bold uppercase tracking-[0.16em] text-primary">CargoFlows · Quality gate</p>
        <h1 className="text-3xl font-bold tracking-tight text-navy sm:text-4xl">{zh ? "素材审核" : "Asset review"}</h1>
        <p className="mt-2 text-sm text-muted-foreground">{zh ? "先定位需要处理的 SKU，再进入专注的逐张审核工作台。" : "Find the SKU that needs attention, then review its assets in a focused workspace."}</p>
      </header>

      <nav aria-label={zh ? "素材审核页面" : "Asset review sections"} className="inline-flex rounded-xl border border-border bg-card p-1 shadow-[var(--shadow-sm)]">
        <button className={`rounded-lg px-4 py-2 text-sm font-semibold ${tab === "review" ? "bg-navy text-white" : "text-muted-foreground hover:bg-muted"}`} onClick={() => setParam("tab", "")} type="button">{zh ? "素材审核" : "Review queue"}</button>
        <button className={`rounded-lg px-4 py-2 text-sm font-semibold ${tab === "styles" ? "bg-navy text-white" : "text-muted-foreground hover:bg-muted"}`} onClick={() => setParam("tab", "styles")} type="button">{zh ? "风格授权" : "Style grants"}</button>
      </nav>

      {tab === "styles" ? <StyleGrantsPanel /> : (
        <section className="overflow-hidden rounded-xl border border-border bg-card shadow-[var(--shadow-sm)]">
          <div className="grid gap-3 border-b border-border p-4 lg:grid-cols-[minmax(240px,1fr)_220px_190px_auto] lg:items-center">
            <div className="relative"><Search className="pointer-events-none absolute left-3.5 top-3.5 h-4 w-4 text-muted-foreground" /><Input className="pl-9" onChange={(event) => setSearchEdit({ base: query, value: event.target.value })} placeholder={zh ? "搜索 SKU、商品、分类或标签" : "Search SKU, product, category, or tag"} value={search} /></div>
            <label className="sr-only" htmlFor="review-category">{zh ? "分类" : "Category"}</label>
            <select className="h-11 rounded-lg border border-border bg-card px-3 text-sm" id="review-category" onChange={(event) => setParam("category_id", event.target.value)} value={categoryID}><option value="">{zh ? "全部分类" : "All categories"}</option>{(categories.data?.data ?? []).map((category) => <option key={category.id} value={category.id}>{categoryLabel(category, language)}</option>)}</select>
            <label className="sr-only" htmlFor="review-status">{zh ? "状态" : "Status"}</label>
            <select className="h-11 rounded-lg border border-border bg-card px-3 text-sm" id="review-status" onChange={(event) => setParam("status", event.target.value)} value={status}><option value="">{statusLabel("")}</option><option value="pending">{statusLabel("pending")}</option><option value="approved">{statusLabel("approved")}</option><option value="rejected">{statusLabel("rejected")}</option></select>
            <div className="flex items-center gap-2 text-xs text-muted-foreground"><Filter className="h-4 w-4" />{queue.data?.pagination.total ?? 0} {zh ? "个 SKU" : "SKUs"}</div>
          </div>

          <div className="hidden overflow-x-auto md:block">
            <table className="w-full min-w-[900px] text-sm">
              <thead className="bg-muted/55 text-left text-[11px] font-bold uppercase tracking-[0.08em] text-muted-foreground"><tr><th className="px-4 py-3">{zh ? "SKU / 商品" : "SKU / product"}</th><th className="px-4 py-3">{zh ? "分类与标签" : "Category & tags"}</th><th className="px-4 py-3">{zh ? "审核状态" : "Review status"}</th><th className="px-4 py-3">{zh ? "最近更新" : "Latest"}</th><th className="w-12 px-4 py-3"><span className="sr-only">{zh ? "打开" : "Open"}</span></th></tr></thead>
              <tbody className="divide-y divide-border">
                {(queue.data?.data ?? []).map((sku) => (
                  <tr className="group hover:bg-primary/[0.035]" key={sku.public_id}>
                    <td className="px-4 py-3"><Link className="flex items-center gap-3" href={`/assets/review/${sku.public_id}?return=${encodeURIComponent(currentReturn)}`}><span className="h-12 w-12 shrink-0 overflow-hidden rounded-lg border border-border bg-muted">{sku.cover_asset ? <img alt="" className="h-full w-full object-cover" src={authenticatedMediaURL(sku.cover_asset.media_url)} /> : null}</span><span className="min-w-0"><span className="block font-semibold text-primary group-hover:underline">{sku.code}</span><span className="block max-w-72 truncate text-xs text-muted-foreground">{sku.product_name}</span></span></Link></td>
                    <td className="px-4 py-3"><span className="block text-xs font-medium">{categoryLabel(sku.category, language)}</span><span className="mt-1 flex max-w-64 gap-1 overflow-hidden">{sku.tags.slice(0, 3).map((tag) => <Badge key={tag.name} variant="neutral">{tag.name}</Badge>)}</span></td>
                    <td className="px-4 py-3"><div className="mb-2 flex gap-3 text-xs"><strong className="text-warning">{sku.counts.pending} {zh ? "待审" : "pending"}</strong><span className="text-success">{sku.counts.approved}</span><span className="text-danger">{sku.counts.rejected}</span></div><StatusRail counts={sku.counts} /></td>
                    <td className="whitespace-nowrap px-4 py-3 text-xs text-muted-foreground">{formatDateTime(sku.latest_pending_at ?? sku.latest_asset_at)}</td>
                    <td className="px-4 py-3"><Button asChild aria-label={`${zh ? "审核" : "Review"} ${sku.code}`} size="icon" variant="ghost"><Link href={`/assets/review/${sku.public_id}?return=${encodeURIComponent(currentReturn)}`}><ChevronRight className="h-4 w-4" /></Link></Button></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div className="divide-y divide-border md:hidden">{(queue.data?.data ?? []).map((sku) => <Link className="block p-4 active:bg-muted" href={`/assets/review/${sku.public_id}?return=${encodeURIComponent(currentReturn)}`} key={sku.public_id}><div className="flex gap-3"><span className="h-14 w-14 shrink-0 overflow-hidden rounded-lg border border-border bg-muted">{sku.cover_asset ? <img alt="" className="h-full w-full object-cover" src={authenticatedMediaURL(sku.cover_asset.media_url)} /> : null}</span><span className="min-w-0 flex-1"><span className="flex items-center justify-between gap-2"><strong className="text-primary">{sku.code}</strong><ChevronRight className="h-4 w-4 text-muted-foreground" /></span><span className="block truncate text-xs text-muted-foreground">{sku.product_name} · {categoryLabel(sku.category, language)}</span><span className="mt-2 flex gap-3 text-xs"><strong className="text-warning">{sku.counts.pending} {zh ? "待审" : "pending"}</strong><span className="text-success">{sku.counts.approved} {zh ? "通过" : "approved"}</span><span className="text-danger">{sku.counts.rejected} {zh ? "拒绝" : "rejected"}</span></span><span className="mt-2 block"><StatusRail counts={sku.counts} /></span></span></div></Link>)}</div>

          {queue.isLoading ? <div className="grid min-h-48 place-items-center text-sm text-muted-foreground">{zh ? "正在加载审核队列…" : "Loading review queue…"}</div> : null}
          {queue.isError ? <div className="m-4 rounded-lg border border-danger/30 bg-danger/5 p-4 text-sm text-danger" role="alert">{zh ? "审核队列加载失败，请重试。" : "The review queue could not be loaded. Try again."}</div> : null}
          {!queue.isLoading && !queue.isError && !queue.data?.data.length ? <div className="grid min-h-52 place-items-center p-8 text-center"><div><ClipboardCheck className="mx-auto h-8 w-8 text-primary" /><p className="mt-3 font-medium">{zh ? "没有符合条件的 SKU" : "No matching SKUs"}</p><p className="mt-1 text-sm text-muted-foreground">{zh ? "调整搜索或筛选条件后再试。" : "Try changing the search or filters."}</p></div></div> : null}

          {(queue.data?.pagination.total_pages ?? 0) > 1 ? <footer className="flex items-center justify-between gap-3 border-t border-border p-4"><span className="text-xs text-muted-foreground">{zh ? `第 ${page} / ${queue.data?.pagination.total_pages} 页` : `Page ${page} of ${queue.data?.pagination.total_pages}`}</span><div className="flex gap-2"><Button disabled={page <= 1} onClick={() => setParam("page", String(page - 1))} size="sm" variant="secondary"><ChevronLeft className="h-4 w-4" />{zh ? "上一页" : "Previous"}</Button><Button disabled={page >= (queue.data?.pagination.total_pages ?? 1)} onClick={() => setParam("page", String(page + 1))} size="sm" variant="secondary">{zh ? "下一页" : "Next"}<ChevronRight className="h-4 w-4" /></Button></div></footer> : null}
        </section>
      )}
    </div>
  );
}

export default function AssetReviewPage() {
  return <Suspense fallback={<div className="min-h-64 rounded-xl border border-border bg-card" />}><AssetReviewQueue /></Suspense>;
}
