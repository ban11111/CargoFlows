"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, ArrowDownToLine, ArrowUpFromLine, CircleDollarSign, LoaderCircle, Plus, RotateCcw, Warehouse, X } from "lucide-react";
import { useMemo, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { apiRequest } from "@/lib/api";
import type { components } from "@/lib/openapi-types";

type SKU = components["schemas"]["SKU"];
type Transaction = components["schemas"]["InventoryTransaction"];
type Kind = components["schemas"]["InventoryTransactionRequest"]["type"];
type CorrectionKind = components["schemas"]["InventoryCorrectionRequest"]["kind"];
type CorrectionResult = components["schemas"]["InventoryCorrectionResult"];
type DraftLine = { sku_id: string; quantity: number; source_currency: string; source_unit_price: string; fx_rate_to_sgd: string };
type DraftCharge = { type: string; source_currency: string; source_amount: string; fx_rate_to_sgd: string };

const today = () => new Date().toISOString().slice(0, 10);
const blankLine = (): DraftLine => ({ sku_id: "", quantity: 1, source_currency: "CNY", source_unit_price: "0", fx_rate_to_sgd: "0" });
const money = (value?: string) => `S$${Number(value ?? 0).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 4 })}`;

export default function InventoryPage() {
  const client = useQueryClient();
  const [kind, setKind] = useState<Kind>("purchase_receipt");
  const [businessDate, setBusinessDate] = useState(today());
  const [note, setNote] = useState("");
  const [lines, setLines] = useState<DraftLine[]>([blankLine()]);
  const [charge, setCharge] = useState({ type: "freight", source_currency: "CNY", source_amount: "0", fx_rate_to_sgd: "0" });
  const [editingDraft, setEditingDraft] = useState<Transaction | null>(null);
  const [correctionTarget, setCorrectionTarget] = useState<Transaction | null>(null);
  const skus = useQuery({ queryKey: ["skus", "inventory"], queryFn: () => apiRequest<{ data: SKU[] }>("/skus") });
  const transactions = useQuery({ queryKey: ["inventory-transactions"], queryFn: () => apiRequest<{ data: Transaction[] }>("/inventory-transactions") });
  const inventoryValue = useMemo(() => (skus.data?.data ?? []).reduce((sum, sku) => sum + Number(sku.inventory_value_sgd ?? 0), 0), [skus.data]);
  const warningCount = (skus.data?.data ?? []).filter((sku) => sku.costing_warning).length;

  const create = useMutation({
    mutationFn: async ({ post }: { post: boolean }) => {
      const payload = {
        type: kind,
        business_date: businessDate,
        note,
        lines: lines.map((line) => kind === "purchase_receipt" ? line : { ...line, source_currency: "SGD", source_unit_price: "0", fx_rate_to_sgd: "1" }),
        charges: kind === "purchase_receipt" && Number(charge.source_amount) > 0 ? [charge] : [],
      };
      const draft = editingDraft
        ? await apiRequest<Transaction>(`/inventory-transactions/${editingDraft.public_id}`, { method: "PATCH", body: JSON.stringify(payload) })
        : await apiRequest<Transaction>("/inventory-transactions", { method: "POST", headers: { "Idempotency-Key": crypto.randomUUID() }, body: JSON.stringify(payload) });
      return post ? apiRequest<Transaction>(`/inventory-transactions/${draft.public_id}/post`, { method: "POST", body: "{}" }) : draft;
    },
    onSuccess: async () => {
      setLines([blankLine()]); setNote(""); setEditingDraft(null); setCharge({ type: "freight", source_currency: "CNY", source_amount: "0", fx_rate_to_sgd: "0" });
      await Promise.all([client.invalidateQueries({ queryKey: ["inventory-transactions"] }), client.invalidateQueries({ queryKey: ["skus"] })]);
    },
  });

  const post = useMutation({ mutationFn: (id: string) => apiRequest<Transaction>(`/inventory-transactions/${id}/post`, { method: "POST", body: "{}" }), onSuccess: async () => { await Promise.all([client.invalidateQueries({ queryKey: ["inventory-transactions"] }), client.invalidateQueries({ queryKey: ["skus"] })]); } });
  const busy = create.isPending || post.isPending;
  const error = create.error ?? post.error;
  const editDraft = (row: Transaction) => {
    setEditingDraft(row);
    setKind(row.type as Kind);
    setBusinessDate(row.business_date.slice(0, 10));
    setNote(row.note);
    setLines(row.lines.map((line) => ({ sku_id: line.sku.public_id, quantity: row.type === "sale_issue" || row.type === "supplier_return" ? Math.abs(line.quantity_delta) : line.quantity_delta, source_currency: line.source_currency, source_unit_price: line.source_unit_price, fx_rate_to_sgd: line.fx_rate_to_sgd })));
    const firstCharge = row.charges[0];
    setCharge(firstCharge ? { type: firstCharge.type, source_currency: firstCharge.source_currency, source_amount: firstCharge.source_amount, fx_rate_to_sgd: firstCharge.fx_rate_to_sgd } : { type: "freight", source_currency: "CNY", source_amount: "0", fx_rate_to_sgd: "0" });
  };

  return <div className="space-y-6">
    <div><p className="mb-2 text-[11px] font-bold uppercase tracking-[0.16em] text-primary">CargoFlows · Cost ledger</p><h1 className="text-3xl font-bold tracking-tight text-navy sm:text-4xl">库存与成本</h1><p className="mt-2 text-sm text-muted-foreground">SGD 移动加权平均成本、跨币种采购与不可变交易流水。</p></div>
    <div className="grid gap-3 md:grid-cols-3">
      <Metric label="库存估值" value={`S$${inventoryValue.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`} detail="所有 SKU 当前账面金额" />
      <Metric label="在库数量" value={(skus.data?.data ?? []).reduce((sum, sku) => sum + sku.stock, 0).toLocaleString()} detail={`${skus.data?.data.length ?? 0} 个 SKU`} />
      <Metric label="零成本警告" value={warningCount.toLocaleString()} detail="开账为 0 SGD，需后续成本入账" warning={warningCount > 0} />
    </div>

    <Card>
      <CardHeader><CardTitle className="flex items-center gap-2"><Warehouse className="h-4 w-4 text-primary" />{editingDraft ? `编辑库存草稿 · ${editingDraft.public_id.slice(0, 8)}` : "新建库存交易"}</CardTitle></CardHeader>
      <CardContent className="space-y-5">
        <div className="grid gap-3 md:grid-cols-3">
          <Field label="交易类型"><select className="min-h-11 w-full rounded-lg border border-border bg-card px-3 text-sm" value={kind} onChange={(event) => setKind(event.target.value as Kind)}><option value="purchase_receipt">采购入库</option><option value="sale_issue">销售出库</option><option value="customer_return">客户退货</option><option value="supplier_return">供应商退货</option><option value="stock_adjustment">盘点调整</option></select></Field>
          <Field label="业务日期"><Input type="date" value={businessDate} onChange={(event) => setBusinessDate(event.target.value)} /></Field>
          <Field label="备注"><Input value={note} onChange={(event) => setNote(event.target.value)} placeholder="采购单号或调整原因" /></Field>
        </div>
        <div className="space-y-3">
          {lines.map((line, index) => <div className="grid gap-3 rounded-lg border border-border bg-muted/25 p-3 md:grid-cols-[2fr_110px_100px_130px_130px_auto]" key={index}>
            <Field label="SKU"><select className="min-h-11 w-full rounded-lg border border-border bg-card px-3 text-sm" value={line.sku_id} onChange={(event) => setLines((current) => current.map((item, i) => i === index ? { ...item, sku_id: event.target.value } : item))}><option value="">请选择</option>{skus.data?.data.map((sku) => <option key={sku.public_id} value={sku.public_id}>{sku.code} · 库存 {sku.stock}</option>)}</select></Field>
            <Field label="数量"><Input type="number" value={line.quantity} onChange={(event) => setLines((current) => current.map((item, i) => i === index ? { ...item, quantity: Number(event.target.value) } : item))} /></Field>
            <Field label="币种"><Input disabled={kind !== "purchase_receipt"} maxLength={3} value={kind === "purchase_receipt" ? line.source_currency : "SGD"} onChange={(event) => setLines((current) => current.map((item, i) => i === index ? { ...item, source_currency: event.target.value.toUpperCase() } : item))} /></Field>
            <Field label="原币单价"><Input disabled={kind !== "purchase_receipt"} inputMode="decimal" value={line.source_unit_price} onChange={(event) => setLines((current) => current.map((item, i) => i === index ? { ...item, source_unit_price: event.target.value } : item))} /></Field>
            <Field label="汇率 → SGD"><Input disabled={kind !== "purchase_receipt"} inputMode="decimal" value={line.fx_rate_to_sgd} onChange={(event) => setLines((current) => current.map((item, i) => i === index ? { ...item, fx_rate_to_sgd: event.target.value } : item))} /></Field>
            <Button aria-label="移除行" className="mt-6 min-h-11" disabled={lines.length === 1} onClick={() => setLines((current) => current.filter((_, i) => i !== index))} type="button" variant="ghost">×</Button>
          </div>)}
          <Button className="min-h-11" onClick={() => setLines((current) => [...current, blankLine()])} type="button" variant="outline"><Plus className="h-4 w-4" />添加 SKU</Button>
        </div>
        {kind === "purchase_receipt" ? <div className="grid gap-3 rounded-lg border border-dashed border-primary/30 bg-primary/[0.025] p-4 md:grid-cols-4"><Field label="附加费类型"><Input value={charge.type} onChange={(event) => setCharge({ ...charge, type: event.target.value })} /></Field><Field label="币种"><Input maxLength={3} value={charge.source_currency} onChange={(event) => setCharge({ ...charge, source_currency: event.target.value.toUpperCase() })} /></Field><Field label="原币金额"><Input inputMode="decimal" value={charge.source_amount} onChange={(event) => setCharge({ ...charge, source_amount: event.target.value })} /></Field><Field label="汇率（0 = ECB）"><Input inputMode="decimal" value={charge.fx_rate_to_sgd} onChange={(event) => setCharge({ ...charge, fx_rate_to_sgd: event.target.value })} /></Field><p className="text-xs text-muted-foreground md:col-span-4">费用按各 SKU 折算后采购货值比例分摊，最后一行吸收小数尾差。</p></div> : null}
        {error ? <p className="rounded-lg border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert">{error.message}</p> : null}
        <div className="flex flex-wrap justify-end gap-2">{editingDraft ? <Button className="min-h-11" onClick={() => { setEditingDraft(null); setLines([blankLine()]); setNote(""); }} variant="ghost">取消编辑</Button> : null}<Button className="min-h-11" disabled={busy || lines.some((line) => !line.sku_id)} onClick={() => create.mutate({ post: false })} variant="outline">保存草稿</Button><Button className="min-h-11" disabled={busy || lines.some((line) => !line.sku_id)} onClick={() => create.mutate({ post: true })}>{create.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : kind === "purchase_receipt" || kind === "customer_return" ? <ArrowDownToLine className="h-4 w-4" /> : <ArrowUpFromLine className="h-4 w-4" />}{editingDraft ? "保存并过账" : "创建并过账"}</Button></div>
      </CardContent>
    </Card>

    <Card><CardHeader><CardTitle>SKU 成本总览</CardTitle></CardHeader><CardContent><div className="overflow-x-auto"><table className="w-full text-left text-sm"><thead className="border-b text-xs uppercase tracking-wide text-muted-foreground"><tr><th className="px-3 py-3">SKU</th><th className="px-3 py-3">库存</th><th className="px-3 py-3">平均单位成本</th><th className="px-3 py-3">库存金额</th><th className="px-3 py-3">状态</th></tr></thead><tbody>{skus.data?.data.map((sku) => <tr className="border-b border-border/70" key={sku.public_id}><td className="px-3 py-3 font-mono font-medium">{sku.code}</td><td className="px-3 py-3">{sku.stock}</td><td className="px-3 py-3 tabular-nums">{money(sku.average_unit_cost_sgd)}</td><td className="px-3 py-3 tabular-nums">{money(sku.inventory_value_sgd)}</td><td className="px-3 py-3">{sku.costing_warning ? <Badge variant="warning">零成本</Badge> : <Badge variant="success">已计价</Badge>}</td></tr>)}</tbody></table></div></CardContent></Card>

    <Card><CardHeader><CardTitle>不可变交易流水</CardTitle></CardHeader><CardContent><div className="space-y-3">{transactions.data?.data.map((row) => <article className="flex flex-col gap-3 rounded-lg border border-border p-4 lg:flex-row lg:items-center" key={row.public_id}><div className="min-w-0 flex-1"><div className="flex flex-wrap items-center gap-2"><Badge variant={row.status === "posted" ? "success" : "warning"}>{row.status === "posted" ? "已过账" : "草稿"}</Badge><strong>{row.type}</strong><span className="font-mono text-xs text-muted-foreground">{row.public_id.slice(0, 8)}</span>{row.corrections?.length ? <Badge variant="warning">已纠正 v{Math.max(...row.corrections.map((item) => item.version))}</Badge> : null}</div><p className="mt-2 text-sm text-muted-foreground">{row.business_date.slice(0, 10)} · {row.lines.map((line) => `${line.sku.code} ${line.quantity_delta > 0 ? "+" : ""}${line.quantity_delta}`).join(" · ")}</p>{row.lines.some((line) => line.cost_version > 0) ? <p className="mt-1 text-xs text-warning">运营成本已按最新纠正版本重算；原始过账值仍保留。</p> : null}</div><div className="flex gap-2">{row.status === "draft" ? <><Button className="min-h-11" disabled={busy} onClick={() => editDraft(row)} variant="outline">编辑</Button><Button className="min-h-11" disabled={busy} onClick={() => post.mutate(row.public_id)}>过账</Button></> : <Button className="min-h-11" disabled={busy} onClick={() => setCorrectionTarget(row)} variant="outline"><RotateCcw className="h-4 w-4" />纠正交易</Button>}</div></article>)}</div></CardContent></Card>
    {correctionTarget ? <CorrectionDialog onClose={() => setCorrectionTarget(null)} onSaved={async () => { setCorrectionTarget(null); await Promise.all([client.invalidateQueries({ queryKey: ["inventory-transactions"] }), client.invalidateQueries({ queryKey: ["skus"] })]); }} transaction={correctionTarget} /> : null}
  </div>;
}

function CorrectionDialog({ transaction, onClose, onSaved }: { transaction: Transaction; onClose: () => void; onSaved: () => Promise<void> }) {
  const [kind, setKind] = useState<CorrectionKind>(transaction.type === "purchase_receipt" ? "cost" : "void");
  const [reason, setReason] = useState("");
  const [lines, setLines] = useState<DraftLine[]>(() => transaction.lines.map((line) => ({
    sku_id: line.sku.public_id,
    quantity: transaction.type === "sale_issue" || transaction.type === "supplier_return" ? Math.abs(line.quantity_delta) : line.quantity_delta,
    source_currency: line.source_currency,
    source_unit_price: line.source_unit_price,
    fx_rate_to_sgd: line.fx_rate_to_sgd,
  })));
  const [charges, setCharges] = useState<DraftCharge[]>(() => transaction.charges.map((item) => ({ type: item.type, source_currency: item.source_currency, source_amount: item.source_amount, fx_rate_to_sgd: item.fx_rate_to_sgd })));
  const [preview, setPreview] = useState<CorrectionResult | null>(null);

  const payload = () => ({ kind, reason, lines: kind === "void" ? [] : lines, charges: kind === "cost" ? charges : [] });
  const previewMutation = useMutation({
    mutationFn: () => apiRequest<CorrectionResult>(`/inventory-transactions/${transaction.public_id}/corrections/preview`, { method: "POST", body: JSON.stringify(payload()) }),
    onSuccess: setPreview,
  });
  const saveMutation = useMutation({
    mutationFn: () => apiRequest<CorrectionResult>(`/inventory-transactions/${transaction.public_id}/corrections`, { method: "POST", headers: { "Idempotency-Key": crypto.randomUUID() }, body: JSON.stringify(payload()) }),
    onSuccess: onSaved,
  });
  const resetPreview = () => setPreview(null);
  const error = previewMutation.error ?? saveMutation.error;

  return <div className="fixed inset-0 z-[90] flex items-end justify-center bg-[#07131e]/60 p-0 backdrop-blur-sm sm:items-center sm:p-5" role="presentation">
    <section aria-labelledby="inventory-correction-title" aria-modal="true" className="max-h-[94dvh] w-full max-w-4xl overflow-y-auto rounded-t-2xl border border-border bg-card p-5 shadow-2xl sm:rounded-2xl sm:p-7" role="dialog">
      <div className="flex items-start justify-between gap-4"><div><p className="text-xs font-bold uppercase tracking-[0.16em] text-primary">CargoFlows · Correction</p><h2 className="mt-2 text-2xl font-bold text-navy" id="inventory-correction-title">纠正库存交易</h2><p className="mt-1 text-sm text-muted-foreground">{transaction.type} · {transaction.public_id.slice(0, 8)} · 已过账记录不会被覆盖</p></div><Button aria-label="关闭" onClick={onClose} size="icon" type="button" variant="ghost"><X className="h-5 w-5" /></Button></div>

      <div className="mt-6 grid gap-4 md:grid-cols-2">
        <Field label="错误类型"><select className="min-h-11 w-full rounded-lg border border-border bg-card px-3 text-sm" onChange={(event) => { setKind(event.target.value as CorrectionKind); resetPreview(); }} value={kind}><option value="void">整笔业务作废</option>{transaction.type === "purchase_receipt" ? <option value="cost">成本、汇率或附加费填错</option> : null}<option value="quantity">数量填错</option></select></Field>
        <Field label="纠正原因（必填）"><Input onChange={(event) => { setReason(event.target.value); resetPreview(); }} placeholder="说明错误来源和依据" value={reason} /></Field>
      </div>

      {kind === "void" ? <div className="mt-5 flex gap-3 rounded-lg border border-warning/35 bg-warning/5 p-4 text-sm"><AlertTriangle className="mt-0.5 h-5 w-5 shrink-0 text-warning" /><p>只有当这笔交易仍是相关 SKU 的最后一笔库存流水时，系统才允许精确反冲；否则会拒绝，避免负库存或破坏移动平均。</p></div> : null}

      {kind === "cost" ? <div className="mt-5 space-y-4"><div><h3 className="font-semibold">纠正后的完整采购成本</h3><p className="mt-1 text-xs text-muted-foreground">数量和 SKU 不可修改。请填写整张采购单的最终正确值，系统会重新分摊费用并重放后续移动平均。</p></div>{lines.map((line, index) => <div className="grid gap-3 rounded-lg border border-border bg-muted/20 p-3 md:grid-cols-[2fr_100px_110px_150px_150px]" key={line.sku_id}><Field label="SKU"><Input disabled value={transaction.lines.find((item) => item.sku.public_id === line.sku_id)?.sku.code ?? line.sku_id} /></Field><Field label="数量"><Input disabled value={line.quantity} /></Field><Field label="币种"><Input maxLength={3} onChange={(event) => { setLines((current) => current.map((item, i) => i === index ? { ...item, source_currency: event.target.value.toUpperCase() } : item)); resetPreview(); }} value={line.source_currency} /></Field><Field label="正确原币单价"><Input inputMode="decimal" onChange={(event) => { setLines((current) => current.map((item, i) => i === index ? { ...item, source_unit_price: event.target.value } : item)); resetPreview(); }} value={line.source_unit_price} /></Field><Field label="正确汇率 → SGD"><Input inputMode="decimal" onChange={(event) => { setLines((current) => current.map((item, i) => i === index ? { ...item, fx_rate_to_sgd: event.target.value } : item)); resetPreview(); }} value={line.fx_rate_to_sgd} /></Field></div>)}<div className="space-y-3"><h3 className="text-sm font-semibold">纠正后的完整附加费</h3>{charges.map((item, index) => <div className="grid gap-3 rounded-lg border border-dashed border-primary/30 p-3 md:grid-cols-4" key={index}><Field label="类型"><Input onChange={(event) => { setCharges((current) => current.map((value, i) => i === index ? { ...value, type: event.target.value } : value)); resetPreview(); }} value={item.type} /></Field><Field label="币种"><Input maxLength={3} onChange={(event) => { setCharges((current) => current.map((value, i) => i === index ? { ...value, source_currency: event.target.value.toUpperCase() } : value)); resetPreview(); }} value={item.source_currency} /></Field><Field label="正确金额"><Input inputMode="decimal" onChange={(event) => { setCharges((current) => current.map((value, i) => i === index ? { ...value, source_amount: event.target.value } : value)); resetPreview(); }} value={item.source_amount} /></Field><Field label="正确汇率"><div className="flex gap-2"><Input inputMode="decimal" onChange={(event) => { setCharges((current) => current.map((value, i) => i === index ? { ...value, fx_rate_to_sgd: event.target.value } : value)); resetPreview(); }} value={item.fx_rate_to_sgd} /><Button aria-label="删除附加费" onClick={() => { setCharges((current) => current.filter((_, i) => i !== index)); resetPreview(); }} type="button" variant="ghost">×</Button></div></Field></div>)}<Button onClick={() => { setCharges((current) => [...current, { type: "freight", source_currency: "CNY", source_amount: "0", fx_rate_to_sgd: "0" }]); resetPreview(); }} type="button" variant="outline"><Plus className="h-4 w-4" />添加附加费</Button></div></div> : null}

      {kind === "quantity" ? <div className="mt-5 space-y-3"><div><h3 className="font-semibold">原交易的正确总数量</h3><p className="mt-1 text-xs text-muted-foreground">系统将按“正确数量 − 原数量”生成当前日期的关联盘点调整，历史交易本身不变。</p></div>{lines.map((line, index) => <div className="grid gap-3 rounded-lg border border-border bg-muted/20 p-3 md:grid-cols-[2fr_180px]" key={line.sku_id}><Field label="SKU"><Input disabled value={transaction.lines.find((item) => item.sku.public_id === line.sku_id)?.sku.code ?? line.sku_id} /></Field><Field label="正确数量"><Input onChange={(event) => { setLines((current) => current.map((item, i) => i === index ? { ...item, quantity: Number(event.target.value) } : item)); resetPreview(); }} type="number" value={line.quantity} /></Field></div>)}</div> : null}

      {preview ? <div className="mt-6 space-y-3 rounded-xl border border-primary/25 bg-primary/[0.025] p-4"><div className="flex flex-wrap items-center justify-between gap-2"><h3 className="font-semibold">影响预览</h3><Badge variant="warning">运营成本调整</Badge></div><div className="grid gap-3 sm:grid-cols-2"><Metric label="当前库存价值变化" value={money(preview.inventory_value_delta_sgd)} detail="将更新 SKU 当前估值" /><Metric label="历史流出成本差额" value={money(preview.historical_outflow_cost_delta_sgd)} detail="运营分析数据，不是总账凭证" /></div><div className="overflow-x-auto"><table className="w-full text-left text-sm"><thead className="border-b text-xs text-muted-foreground"><tr><th className="py-2">SKU</th><th>平均成本</th><th>库存金额</th><th>受影响流水</th></tr></thead><tbody>{preview.impacts.map((impact) => <tr className="border-b border-border/60" key={impact.sku_id}><td className="py-2 font-mono">{impact.sku_code}</td><td>{money(impact.current_average_cost_sgd)} → {money(impact.effective_average_cost_sgd)}</td><td>{money(impact.current_inventory_value_sgd)} → {money(impact.effective_inventory_value_sgd)}</td><td>{impact.affected_transaction_count}</td></tr>)}</tbody></table></div></div> : null}
      {error ? <p className="mt-5 rounded-lg border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert">{error.message}</p> : null}
      <div className="mt-6 flex flex-col-reverse gap-2 sm:flex-row sm:justify-end"><Button onClick={onClose} type="button" variant="secondary">取消</Button><Button disabled={!reason.trim() || previewMutation.isPending || saveMutation.isPending} onClick={() => previewMutation.mutate()} type="button" variant="outline">{previewMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}预览影响</Button><Button disabled={!preview || saveMutation.isPending} onClick={() => saveMutation.mutate()} type="button">{saveMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}确认纠正</Button></div>
    </section>
  </div>;
}

function Field({ label, children }: { label: string; children: React.ReactNode }) { return <label className="space-y-1.5 text-sm"><span className="font-medium text-foreground">{label}</span>{children}</label>; }
function Metric({ label, value, detail, warning = false }: { label: string; value: string; detail: string; warning?: boolean }) { return <Card className={warning ? "border-warning/40" : ""}><CardContent className="flex items-start justify-between"><div><p className="text-xs font-bold uppercase tracking-[0.14em] text-muted-foreground">{label}</p><p className="mt-2 text-2xl font-bold tabular-nums text-navy">{value}</p><p className="mt-1 text-xs text-muted-foreground">{detail}</p></div><span className="rounded-lg bg-primary/10 p-2 text-primary">{label.includes("估值") ? <CircleDollarSign className="h-5 w-5" /> : <Warehouse className="h-5 w-5" />}</span></CardContent></Card>; }
