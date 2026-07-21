"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Palette, X } from "lucide-react";
import { useState } from "react";
import { createPortal } from "react-dom";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { apiRequest } from "@/lib/api";
import { useLanguage } from "@/lib/i18n";

interface StyleReferenceGrant {
  public_id: string;
  version: number;
  source_sku_id: string;
  description_zh: string;
  description_en: string;
  derivative_sha256: string;
  status: "approved" | "revoked";
}

export function StyleGrantsPanel() {
  const { language } = useLanguage();
  const queryClient = useQueryClient();
  const grants = useQuery({ queryKey: ["style-reference-grants"], queryFn: () => apiRequest<{ data: StyleReferenceGrant[] }>("/style-reference-grants") });
  const revoke = useMutation({
    mutationFn: (id: string) => apiRequest(`/style-reference-grants/${id}`, { method: "PATCH", body: JSON.stringify({ status: "revoked" }) }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["style-reference-grants"] }),
  });
  const zh = language === "zh";

  return (
    <section className="overflow-hidden rounded-xl border border-border bg-card shadow-[var(--shadow-sm)]">
      <div className="border-b border-border p-4 sm:p-5">
        <div className="flex items-center gap-2">
          <Palette className="h-4 w-4 text-primary" />
          <h2 className="text-sm font-semibold">{zh ? "跨 SKU 风格授权" : "Cross-SKU style grants"}</h2>
          <Badge variant="neutral">{grants.data?.data.length ?? 0}</Badge>
        </div>
        <p className="mt-1 max-w-3xl text-xs leading-5 text-muted-foreground">{zh ? "这里只管理去除来源商品主体后的私有派生图。撤销只影响未来任务，历史任务继续使用冻结版本。" : "Only private derivatives with the source product removed are managed here. Revocation affects future jobs; historical snapshots keep their frozen version."}</p>
      </div>
      <div className="divide-y divide-border">
        {(grants.data?.data ?? []).map((grant) => (
          <article className="grid gap-3 p-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center" key={grant.public_id}>
            <div>
              <div className="flex flex-wrap items-center gap-2"><Badge variant={grant.status === "approved" ? "success" : "neutral"}>{grant.status}</Badge><span className="font-mono text-xs">V{grant.version} · {grant.source_sku_id.slice(0, 8)}</span></div>
              <p className="mt-2 text-sm">{zh ? grant.description_zh : grant.description_en}</p>
              <p className="mt-1 font-mono text-[11px] text-muted-foreground">SHA-256 {grant.derivative_sha256.slice(0, 12)}…</p>
            </div>
            {grant.status === "approved" ? <Button aria-label={`${zh ? "撤销风格授权" : "Revoke style grant"}: ${grant.public_id}`} disabled={revoke.isPending} onClick={() => revoke.mutate(grant.public_id)} size="sm" variant="ghost"><X className="h-4 w-4" />{zh ? "撤销" : "Revoke"}</Button> : null}
          </article>
        ))}
        {!grants.isLoading && !(grants.data?.data.length) ? <p className="p-8 text-center text-sm text-muted-foreground">{zh ? "暂无风格授权。请先在已通过素材的详情中创建授权。" : "No style grants yet. Create one from an approved asset."}</p> : null}
      </div>
      {grants.isError || revoke.isError ? <p aria-live="polite" className="m-4 rounded-md border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert">{zh ? "风格授权列表或撤销操作失败，请重试。" : "Style grants could not be loaded or revoked. Try again."}</p> : null}
    </section>
  );
}

export function StyleGrantDialog({ assetID, onClose }: { assetID: string; onClose: () => void }) {
  const { language } = useLanguage();
  const queryClient = useQueryClient();
  const [descriptionZH, setDescriptionZH] = useState("");
  const [descriptionEN, setDescriptionEN] = useState("");
  const [mask, setMask] = useState<File>();
  const zh = language === "zh";
  const create = useMutation({
    mutationFn: async () => {
      if (!mask) throw new Error("missing style grant mask");
      const form = new FormData();
      form.set("asset_id", assetID);
      form.set("description_zh", descriptionZH.trim());
      form.set("description_en", descriptionEN.trim());
      form.set("product_exclusion_mask", mask, "product-mask.png");
      return apiRequest("/style-reference-grants", { method: "POST", body: form });
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["style-reference-grants"] });
      onClose();
    },
  });

  if (typeof document === "undefined") return null;
  return createPortal(
    <div aria-modal="true" className="fixed inset-0 z-[80] grid overflow-y-auto bg-navy/60 p-4" role="dialog">
      <form className="m-auto w-full max-w-lg space-y-4 rounded-xl bg-card p-5 shadow-2xl" onSubmit={(event) => { event.preventDefault(); create.mutate(); }}>
        <div className="flex items-start justify-between gap-3">
          <div><h2 className="font-semibold">{zh ? "创建风格授权" : "Create style grant"}</h2><p className="mt-1 text-xs leading-5 text-muted-foreground">{zh ? "上传与原图同尺寸的 PNG 商品排除蒙版。透明区域必须完整覆盖商品主体；服务端只保存去除主体后的私有派生图。" : "Upload a same-size PNG exclusion mask. Transparent pixels must fully cover the product; only a private product-free derivative is retained."}</p></div>
          <Button aria-label={zh ? "关闭" : "Close"} onClick={onClose} size="icon" type="button" variant="ghost"><X className="h-4 w-4" /></Button>
        </div>
        <label className="block text-sm font-medium">中文风格说明<Textarea className="mt-2" maxLength={2000} onChange={(event) => setDescriptionZH(event.target.value)} value={descriptionZH} /></label>
        <label className="block text-sm font-medium">English style description<Textarea className="mt-2" maxLength={2000} onChange={(event) => setDescriptionEN(event.target.value)} value={descriptionEN} /></label>
        <label className="block text-sm font-medium">{zh ? "商品排除蒙版（PNG）" : "Product exclusion mask (PNG)"}<Input accept="image/png" className="mt-2" onChange={(event) => setMask(event.target.files?.[0])} type="file" /></label>
        {create.isError ? <p aria-live="polite" className="text-sm text-danger" role="alert">{create.error instanceof Error ? create.error.message : (zh ? "创建失败" : "Creation failed")}</p> : null}
        <Button className="w-full" disabled={create.isPending || !mask || !descriptionZH.trim() || !descriptionEN.trim()} type="submit">{create.isPending ? (zh ? "创建中…" : "Creating…") : (zh ? "生成私有派生图并授权" : "Create private derivative and grant")}</Button>
      </form>
    </div>, document.body,
  );
}
