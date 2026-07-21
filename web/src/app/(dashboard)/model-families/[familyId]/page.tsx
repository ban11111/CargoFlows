"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Archive, ArrowLeft, Boxes, CheckCircle2, GitBranch, ImageIcon, Pencil, Plus, RotateCw, ShieldCheck, Trash2, TriangleAlert, Users, XCircle } from "lucide-react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { useState } from "react";
import { ModelFamilyForm } from "@/components/model-families/model-family-form";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { isAdministrator, useCurrentUser } from "@/lib/auth";
import { ApiError, apiRequest } from "@/lib/api";
import { useLanguage } from "@/lib/i18n";
import { dimensionLabel, familyName, type ModelFamily, type ModelFamilyInput, type SKU, type VariantIdentity } from "@/lib/model-families";

type StructureRole = "geometry_only" | "viewpoint_only" | "detail_geometry";
interface ReferenceAsset { public_id: string; sku_id: string; media_url: string; origin_type: "captured" | "uploaded" | "ai_generated" }
interface FamilyReference { public_id: string; version: number; source_sku_id: string; role: StructureRole; derivative_sha256: string; status: "approved" | "revoked" }

export default function ModelFamilyDetailPage() {
  const { language } = useLanguage();
  const params = useParams<{ familyId: string }>();
  const queryClient = useQueryClient();
  const currentUser = useCurrentUser();
  const [editing, setEditing] = useState(false);
  const [selectedSKU, setSelectedSKU] = useState("");
  const [referenceAssetID, setReferenceAssetID] = useState("");
  const [referenceRole, setReferenceRole] = useState<StructureRole>("geometry_only");
  const [referenceMask, setReferenceMask] = useState<File>();
  const familyQuery = useQuery({ queryKey: ["model-families", params.familyId], queryFn: () => apiRequest<ModelFamily>(`/model-families/${params.familyId}`) });
  const skusQuery = useQuery({ queryKey: ["skus"], queryFn: () => apiRequest<{ data: SKU[] }>("/skus") });
  const approvedAssetsQuery = useQuery({ queryKey: ["assets", "approved"], queryFn: () => apiRequest<{ data: ReferenceAsset[] }>("/assets/review?status=approved") });
  const referencesQuery = useQuery({ queryKey: ["model-families", params.familyId, "reference-assets"], queryFn: () => apiRequest<{ data: FamilyReference[] }>(`/model-families/${params.familyId}/reference-assets`) });
  const refreshFamily = () => queryClient.invalidateQueries({ queryKey: ["model-families"] });
  const update = useMutation({
    mutationFn: (value: ModelFamilyInput) => apiRequest<ModelFamily>(`/model-families/${params.familyId}`, { method: "PATCH", body: JSON.stringify(value) }),
    onSuccess: async () => { setEditing(false); await refreshFamily(); },
  });
  const archive = useMutation({
    mutationFn: () => apiRequest<ModelFamily>(`/model-families/${params.familyId}`, { method: "PATCH", body: JSON.stringify({ status: "archived" }) }),
    onSuccess: refreshFamily,
  });
  const addMember = useMutation({
    mutationFn: (skuID: string) => apiRequest(`/model-families/${params.familyId}/members`, { method: "POST", body: JSON.stringify({ sku_id: skuID }) }),
    onSuccess: async () => { setSelectedSKU(""); await refreshFamily(); },
  });
  const removeMember = useMutation({
    mutationFn: (memberID: string) => apiRequest(`/model-families/${params.familyId}/members/${memberID}`, { method: "DELETE" }),
    onSuccess: refreshFamily,
  });
  const createReference = useMutation({
    mutationFn: async () => {
      if (!referenceAssetID || !referenceMask) throw new Error("missing structure reference fields");
      const form = new FormData();
      form.set("asset_id", referenceAssetID);
      form.set("role", referenceRole);
      form.set("forbidden_identity_mask", referenceMask, "forbidden-identity-mask.png");
      return apiRequest(`/model-families/${params.familyId}/reference-assets`, { method: "POST", body: form });
    },
    onSuccess: async () => {
      setReferenceAssetID("");
      setReferenceMask(undefined);
      await queryClient.invalidateQueries({ queryKey: ["model-families", params.familyId, "reference-assets"] });
    },
  });
  const revokeReference = useMutation({
    mutationFn: (referenceID: string) => apiRequest(`/model-families/${params.familyId}/reference-assets/${referenceID}`, { method: "PATCH", body: JSON.stringify({ status: "revoked" }) }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["model-families", params.familyId, "reference-assets"] }),
  });
  const text = language === "zh" ? {
    back: "返回型号组", loading: "正在载入型号组…", loadError: "无法载入这个型号组。", retry: "重试载入", active: "启用", archived: "已归档", edit: "编辑资料", archive: "归档型号组",
    archiveConfirm: "归档后不能再编辑资料或成员。继续归档？", archiveError: "归档失败，请稍后重试。", save: "保存型号组", editError: "保存失败。请检查系列代码是否重复及必填项。",
    structure: "系列结构边界", invariants: "共同结构", dimensions: "允许变化", members: "SKU 成员", membersDesc: "成员共享上方结构，但必须分别维护自己的变体身份。", add: "添加 SKU", choose: "选择尚未加入的 SKU",
    noOptions: "没有可添加的 SKU", noMembers: "还没有 SKU 成员。", removed: "已移除", joined: "加入时间", remove: "移除", removeConfirm: "从型号组移除这个 SKU？历史记录会保留。",
    memberError: "成员变更失败。该 SKU 可能已属于其他型号组。", identity: "变体身份", published: "已发布", missing: "未发布", identityError: "状态不可用", skuLoadError: "SKU 列表载入失败，暂时无法添加成员。",
    references: "型号组结构参考", referencesDesc: "只允许同型号组的真实审核素材。服务端生成灰度派生图，并遮盖颜色、标签、接口、控制件、配件与包装。", referenceAsset: "选择同型号组素材", referenceRole: "结构用途", referenceMask: "禁止继承区域蒙版（PNG）", authorize: "生成派生图并授权", noReferences: "暂无结构参考授权。", revoke: "撤销", referenceError: "结构参考授权失败。请检查素材、蒙版尺寸和型号组成员关系。", maskHelp: "蒙版透明区域覆盖禁止继承的身份属性，必须与原图同尺寸。",
  } : {
    back: "Back to model families", loading: "Loading model family…", loadError: "This model family could not be loaded.", retry: "Retry", active: "Active", archived: "Archived", edit: "Edit details", archive: "Archive family",
    archiveConfirm: "Archiving prevents future edits and membership changes. Continue?", archiveError: "The family could not be archived. Try again.", save: "Save model family", editError: "Changes could not be saved. Check for a duplicate code and complete required fields.",
    structure: "Family structure boundary", invariants: "Shared structure", dimensions: "Allowed variation", members: "SKU members", membersDesc: "Members share the structure above but retain their own variant identity.", add: "Add SKU", choose: "Choose an unassigned SKU",
    noOptions: "No SKUs available to add", noMembers: "No SKU members yet.", removed: "Removed", joined: "Joined", remove: "Remove", removeConfirm: "Remove this SKU from the family? Membership history will be retained.",
    memberError: "Membership could not be changed. This SKU may already belong to another family.", identity: "Variant identity", published: "Published", missing: "Not published", identityError: "Status unavailable", skuLoadError: "The SKU list could not be loaded, so members cannot be added right now.",
    references: "Model-family structure references", referencesDesc: "Only approved real assets from this family are eligible. The server creates a grayscale derivative and masks color, labels, ports, controls, accessories, and packaging.", referenceAsset: "Choose a same-family asset", referenceRole: "Structure use", referenceMask: "Forbidden-inheritance mask (PNG)", authorize: "Create derivative and authorize", noReferences: "No structural reference grants yet.", revoke: "Revoke", referenceError: "The structural reference grant failed. Check the asset, mask dimensions, and family membership.", maskHelp: "Transparent mask pixels cover identity attributes that must not transfer. The mask must match the source dimensions.",
  };

  if (familyQuery.isLoading) return <p className="py-10 text-sm text-muted-foreground" role="status">{text.loading}</p>;
  if (familyQuery.isError || !familyQuery.data) return <LoadError action={text.retry} message={text.loadError} onRetry={() => familyQuery.refetch()} />;
  const family = familyQuery.data;
  const activeMembers = (family.members ?? []).filter((member) => !member.removed_at);
  const skuByID = new Map((skusQuery.data?.data ?? []).map((sku) => [sku.public_id, sku]));
  const memberIDs = new Set(activeMembers.map((member) => member.sku_id));
  const availableSKUs = (skusQuery.data?.data ?? []).filter((sku) => !memberIDs.has(sku.public_id) && sku.status !== "disabled");
  const eligibleReferenceAssets = (approvedAssetsQuery.data?.data ?? []).filter((asset) => memberIDs.has(asset.sku_id) && asset.origin_type !== "ai_generated");
  const references = referencesQuery.data?.data ?? [];
  const canEdit = isAdministrator(currentUser.data?.role) && family.status === "active";
  const canManageMembers = ["super_admin", "admin", "operator"].includes(currentUser.data?.role ?? "") && family.status === "active";
  const initial: ModelFamilyInput = { brand: family.brand, name_zh: family.name_zh, name_en: family.name_en, model_code: family.model_code, common_structure: family.common_structure, variation_dimensions: family.variation_dimensions };

  return <div className="space-y-5">
    <header className="flex flex-col gap-4 border-b border-border pb-5 sm:flex-row sm:items-start sm:justify-between">
      <div className="flex items-start gap-3"><Button asChild aria-label={text.back} size="icon" variant="secondary"><Link href="/model-families"><ArrowLeft className="h-4 w-4" /></Link></Button><div><p className="mb-1 text-[11px] font-bold uppercase tracking-[0.16em] text-primary">CargoFlow · Model family</p><div className="flex flex-wrap items-center gap-2"><h1 className="text-3xl font-bold tracking-tight text-navy sm:text-4xl">{familyName(family, language)}</h1><Badge variant={family.status === "active" ? "success" : "neutral"}>{family.status === "active" ? text.active : text.archived}</Badge></div><p className="mt-2 text-sm text-muted-foreground">{family.brand} <span aria-hidden>·</span> <span className="font-mono text-xs">{family.model_code}</span> <span aria-hidden>·</span> {language === "zh" ? family.name_en : family.name_zh}</p></div></div>
      {canEdit ? <div className="flex flex-wrap gap-2"><Button onClick={() => setEditing((value) => !value)} variant="secondary"><Pencil className="h-4 w-4" />{text.edit}</Button><Button disabled={archive.isPending} onClick={() => { if (window.confirm(text.archiveConfirm)) archive.mutate(); }} variant="danger"><Archive className="h-4 w-4" />{text.archive}</Button></div> : null}
    </header>

    {archive.isError ? <p className="rounded-md border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert">{text.archiveError}</p> : null}
    {editing ? <Card className="border-primary/25"><CardHeader><CardTitle>{text.edit}</CardTitle></CardHeader><CardContent><ModelFamilyForm busy={update.isPending} initial={initial} onCancel={() => setEditing(false)} onSubmit={(value) => update.mutate(value)} submitLabel={text.save} />{update.isError ? <p className="mt-4 rounded-md border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert">{text.editError}</p> : null}</CardContent></Card> : null}

    <Card className="overflow-hidden">
      <CardHeader><CardTitle className="flex items-center gap-2"><GitBranch className="h-4 w-4 text-primary" />{text.structure}</CardTitle></CardHeader>
      <CardContent className="grid gap-0 p-0 md:grid-cols-2">
        <section className="border-b border-border p-5 md:border-b-0 md:border-r"><h2 className="text-xs font-bold uppercase tracking-[0.12em] text-muted-foreground">{text.invariants}</h2><ol className="mt-4 space-y-3">{family.common_structure.invariants.map((item, index) => <li className="flex gap-3 text-sm leading-6" key={`${item}-${index}`}><span className="grid h-6 w-6 shrink-0 place-items-center rounded-md bg-navy font-mono text-[10px] font-bold text-white">{String(index + 1).padStart(2, "0")}</span><span>{item}</span></li>)}</ol></section>
        <section className="p-5"><h2 className="text-xs font-bold uppercase tracking-[0.12em] text-muted-foreground">{text.dimensions}</h2><div className="mt-4 flex flex-wrap gap-2">{family.variation_dimensions.map((dimension) => <Badge className="px-3 py-1.5" key={dimension} variant="warning">{dimensionLabel(dimension, language)}</Badge>)}</div></section>
      </CardContent>
    </Card>

    <Card>
      <CardHeader><CardTitle className="flex items-center gap-2"><ShieldCheck className="h-4 w-4 text-primary" />{text.references}<Badge variant="neutral">{references.filter((reference) => reference.status === "approved").length}</Badge></CardTitle><p className="mt-1 text-sm leading-6 text-muted-foreground">{text.referencesDesc}</p></CardHeader>
      <CardContent className="space-y-4">
        {canManageMembers ? <form className="grid gap-3 rounded-xl border border-border bg-muted/35 p-4 lg:grid-cols-[minmax(0,1fr)_220px_minmax(0,1fr)_auto] lg:items-end" onSubmit={(event) => { event.preventDefault(); createReference.mutate(); }}>
          <label className="grid gap-1.5 text-sm font-medium">{text.referenceAsset}<select className="h-11 min-w-0 rounded-lg border border-border bg-card px-3 text-sm" onChange={(event) => setReferenceAssetID(event.target.value)} value={referenceAssetID}><option value="">{text.referenceAsset}</option>{eligibleReferenceAssets.map((asset) => <option key={asset.public_id} value={asset.public_id}>{skuByID.get(asset.sku_id)?.code ?? asset.sku_id} · {asset.public_id.slice(0, 8)}</option>)}</select></label>
          <label className="grid gap-1.5 text-sm font-medium">{text.referenceRole}<select className="h-11 rounded-lg border border-border bg-card px-3 text-sm" onChange={(event) => setReferenceRole(event.target.value as StructureRole)} value={referenceRole}><option value="geometry_only">geometry_only</option><option value="viewpoint_only">viewpoint_only</option><option value="detail_geometry">detail_geometry</option></select></label>
          <label className="grid gap-1.5 text-sm font-medium">{text.referenceMask}<Input accept="image/png" onChange={(event) => setReferenceMask(event.target.files?.[0])} type="file" /><span className="text-xs font-normal text-muted-foreground">{text.maskHelp}</span></label>
          <Button className="min-h-11" disabled={!referenceAssetID || !referenceMask || createReference.isPending} type="submit"><ImageIcon className="h-4 w-4" />{text.authorize}</Button>
        </form> : null}
        {createReference.isError || referencesQuery.isError || approvedAssetsQuery.isError ? <p aria-live="polite" className="rounded-md border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert">{text.referenceError}</p> : null}
        {!references.length ? <p className="rounded-lg border border-dashed border-border p-6 text-center text-sm text-muted-foreground">{text.noReferences}</p> : <div className="divide-y divide-border rounded-xl border border-border">{references.map((reference) => <article className="grid gap-3 p-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center" key={reference.public_id}><div><div className="flex flex-wrap items-center gap-2"><Badge variant={reference.status === "approved" ? "success" : "neutral"}>{reference.status}</Badge><span className="font-mono text-xs">{reference.role} · V{reference.version}</span></div><p className="mt-2 text-xs text-muted-foreground">{skuByID.get(reference.source_sku_id)?.code ?? reference.source_sku_id} · SHA-256 {reference.derivative_sha256.slice(0, 12)}…</p></div>{canManageMembers && reference.status === "approved" ? <Button aria-label={`${text.revoke}: ${reference.public_id}`} disabled={revokeReference.isPending} onClick={() => revokeReference.mutate(reference.public_id)} size="sm" variant="ghost"><XCircle className="h-4 w-4" />{text.revoke}</Button> : null}</article>)}</div>}
      </CardContent>
    </Card>

    <Card>
      <CardHeader><div className="flex flex-col gap-1 sm:flex-row sm:items-end sm:justify-between"><div><CardTitle className="flex items-center gap-2"><Users className="h-4 w-4 text-primary" />{text.members}<Badge variant="neutral">{activeMembers.length}</Badge></CardTitle><p className="mt-1 text-sm text-muted-foreground">{text.membersDesc}</p></div></div></CardHeader>
      <CardContent className="space-y-4">
        {canManageMembers ? <div className="flex flex-col gap-2 rounded-xl border border-border bg-muted/35 p-3 sm:flex-row"><select aria-label={text.choose} className="h-11 min-w-0 flex-1 rounded-lg border border-border bg-card px-3 text-sm outline-none focus:border-primary focus:ring-3 focus:ring-primary/10" disabled={skusQuery.isError || !availableSKUs.length || addMember.isPending} onChange={(event) => setSelectedSKU(event.target.value)} value={selectedSKU}><option value="">{availableSKUs.length ? text.choose : text.noOptions}</option>{availableSKUs.map((sku) => <option key={sku.public_id} value={sku.public_id}>{sku.code} · {sku.product.name} · {sku.color || sku.size}</option>)}</select><Button className="min-h-11" disabled={!selectedSKU || addMember.isPending} onClick={() => addMember.mutate(selectedSKU)}><Plus className="h-4 w-4" />{text.add}</Button></div> : null}
        {skusQuery.isError ? <p className="text-sm text-danger" role="alert">{text.skuLoadError}</p> : null}
        {addMember.isError || removeMember.isError ? <p className="text-sm text-danger" role="alert">{text.memberError}</p> : null}
        {!activeMembers.length ? <div className="py-8 text-center text-sm text-muted-foreground">{text.noMembers}</div> : <div className="divide-y divide-border rounded-xl border border-border">{activeMembers.map((member) => {
          const sku = skuByID.get(member.sku_id);
          return <article className="grid gap-3 p-4 md:grid-cols-[minmax(0,1fr)_auto_auto] md:items-center" key={member.public_id}>
            <div className="flex min-w-0 items-start gap-3"><span className="grid h-10 w-10 shrink-0 place-items-center rounded-lg bg-muted text-primary"><Boxes className="h-4 w-4" /></span><div className="min-w-0"><h3 className="font-semibold">{sku?.product.name ?? member.sku_id}</h3><p className="mt-0.5 font-mono text-xs text-muted-foreground">{sku?.code ?? member.sku_id}</p>{sku ? <p className="mt-1 text-xs text-muted-foreground">{[sku.color, sku.size].filter(Boolean).join(" · ")}</p> : null}</div></div>
            <MemberIdentityStatus skuID={member.sku_id} labels={text} />
            {canManageMembers ? <Button aria-label={`${text.remove}: ${sku?.code ?? member.sku_id}`} disabled={removeMember.isPending} onClick={() => { if (window.confirm(text.removeConfirm)) removeMember.mutate(member.public_id); }} size="sm" variant="ghost"><Trash2 className="h-4 w-4" />{text.remove}</Button> : null}
          </article>;
        })}</div>}
      </CardContent>
    </Card>
  </div>;
}

function MemberIdentityStatus({ skuID, labels }: { skuID: string; labels: { identity: string; published: string; missing: string; identityError: string } }) {
  const query = useQuery({ queryKey: ["variant-identity", skuID], queryFn: async () => {
    try { return await apiRequest<VariantIdentity>(`/skus/${skuID}/variant-identity`); }
    catch (error) { if (error instanceof ApiError && error.status === 404) return null; throw error; }
  }, retry: false });
  if (query.isLoading) return <span className="text-xs text-muted-foreground">{labels.identity}…</span>;
  if (query.isError) return <Badge variant="danger"><XCircle className="h-3 w-3" />{labels.identityError}</Badge>;
  return query.data ? <Badge variant="success"><CheckCircle2 className="h-3 w-3" />{labels.identity}: {labels.published} · V{query.data.version_number}</Badge> : <Badge variant="warning"><TriangleAlert className="h-3 w-3" />{labels.identity}: {labels.missing}</Badge>;
}

function LoadError({ message, action, onRetry }: { message: string; action: string; onRetry: () => unknown }) {
  return <div className="flex flex-wrap items-center gap-3 rounded-md border border-danger/30 bg-danger/5 p-4 text-sm text-danger" role="alert"><TriangleAlert className="h-4 w-4" /><span>{message}</span><Button aria-label={action} onClick={onRetry} size="sm" variant="secondary"><RotateCw className="h-4 w-4" />{action}</Button></div>;
}
