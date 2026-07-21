"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowDownToLine, ArrowUpFromLine, CircleDollarSign, LoaderCircle, Plus, RotateCcw, Warehouse } from "lucide-react";
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
type DraftLine = { sku_id: string; quantity: number; source_currency: string; source_unit_price: string; fx_rate_to_sgd: string };

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
      const draft = await apiRequest<Transaction>("/inventory-transactions", { method: "POST", headers: { "Idempotency-Key": crypto.randomUUID() }, body: JSON.stringify(payload) });
      return post ? apiRequest<Transaction>(`/inventory-transactions/${draft.public_id}/post`, { method: "POST", body: "{}" }) : draft;
    },
    onSuccess: async () => {
      setLines([blankLine()]); setNote(""); setCharge({ type: "freight", source_currency: "CNY", source_amount: "0", fx_rate_to_sgd: "0" });
      await Promise.all([client.invalidateQueries({ queryKey: ["inventory-transactions"] }), client.invalidateQueries({ queryKey: ["skus"] })]);
    },
  });

  const post = useMutation({ mutationFn: (id: string) => apiRequest<Transaction>(`/inventory-transactions/${id}/post`, { method: "POST", body: "{}" }), onSuccess: async () => { await Promise.all([client.invalidateQueries({ queryKey: ["inventory-transactions"] }), client.invalidateQueries({ queryKey: ["skus"] })]); } });
  const reverse = useMutation({ mutationFn: (id: string) => apiRequest<Transaction>(`/inventory-transactions/${id}/reverse`, { method: "POST", body: "{}" }), onSuccess: async () => { await Promise.all([client.invalidateQueries({ queryKey: ["inventory-transactions"] }), client.invalidateQueries({ queryKey: ["skus"] })]); } });
  const busy = create.isPending || post.isPending || reverse.isPending;
  const error = create.error ?? post.error ?? reverse.error;

  return <div className="space-y-6">
    <div><p className="mb-2 text-[11px] font-bold uppercase tracking-[0.16em] text-primary">CargoFlows · Cost ledger</p><h1 className="text-3xl font-bold tracking-tight text-navy sm:text-4xl">库存与成本</h1><p className="mt-2 text-sm text-muted-foreground">SGD 移动加权平均成本、跨币种采购与不可变交易流水。</p></div>
    <div className="grid gap-3 md:grid-cols-3">
      <Metric label="库存估值" value={`S$${inventoryValue.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`} detail="所有 SKU 当前账面金额" />
      <Metric label="在库数量" value={(skus.data?.data ?? []).reduce((sum, sku) => sum + sku.stock, 0).toLocaleString()} detail={`${skus.data?.data.length ?? 0} 个 SKU`} />
      <Metric label="零成本警告" value={warningCount.toLocaleString()} detail="开账为 0 SGD，需后续成本入账" warning={warningCount > 0} />
    </div>

    <Card>
      <CardHeader><CardTitle className="flex items-center gap-2"><Warehouse className="h-4 w-4 text-primary" />新建库存交易</CardTitle></CardHeader>
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
        <div className="flex flex-wrap justify-end gap-2"><Button className="min-h-11" disabled={busy || lines.some((line) => !line.sku_id)} onClick={() => create.mutate({ post: false })} variant="outline">保存草稿</Button><Button className="min-h-11" disabled={busy || lines.some((line) => !line.sku_id)} onClick={() => create.mutate({ post: true })}>{create.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : kind === "purchase_receipt" || kind === "customer_return" ? <ArrowDownToLine className="h-4 w-4" /> : <ArrowUpFromLine className="h-4 w-4" />}创建并过账</Button></div>
      </CardContent>
    </Card>

    <Card><CardHeader><CardTitle>SKU 成本总览</CardTitle></CardHeader><CardContent><div className="overflow-x-auto"><table className="w-full text-left text-sm"><thead className="border-b text-xs uppercase tracking-wide text-muted-foreground"><tr><th className="px-3 py-3">SKU</th><th className="px-3 py-3">库存</th><th className="px-3 py-3">平均单位成本</th><th className="px-3 py-3">库存金额</th><th className="px-3 py-3">状态</th></tr></thead><tbody>{skus.data?.data.map((sku) => <tr className="border-b border-border/70" key={sku.public_id}><td className="px-3 py-3 font-mono font-medium">{sku.code}</td><td className="px-3 py-3">{sku.stock}</td><td className="px-3 py-3 tabular-nums">{money(sku.average_unit_cost_sgd)}</td><td className="px-3 py-3 tabular-nums">{money(sku.inventory_value_sgd)}</td><td className="px-3 py-3">{sku.costing_warning ? <Badge variant="warning">零成本</Badge> : <Badge variant="success">已计价</Badge>}</td></tr>)}</tbody></table></div></CardContent></Card>

    <Card><CardHeader><CardTitle>不可变交易流水</CardTitle></CardHeader><CardContent><div className="space-y-3">{transactions.data?.data.map((row) => <article className="flex flex-col gap-3 rounded-lg border border-border p-4 lg:flex-row lg:items-center" key={row.public_id}><div className="min-w-0 flex-1"><div className="flex flex-wrap items-center gap-2"><Badge variant={row.status === "posted" ? "success" : "warning"}>{row.status === "posted" ? "已过账" : "草稿"}</Badge><strong>{row.type}</strong><span className="font-mono text-xs text-muted-foreground">{row.public_id.slice(0, 8)}</span></div><p className="mt-2 text-sm text-muted-foreground">{row.business_date.slice(0, 10)} · {row.lines.map((line) => `${line.sku.code} ${line.quantity_delta > 0 ? "+" : ""}${line.quantity_delta}`).join(" · ")}</p></div><div className="flex gap-2">{row.status === "draft" ? <Button className="min-h-11" disabled={busy} onClick={() => post.mutate(row.public_id)}>过账</Button> : <Button className="min-h-11" disabled={busy} onClick={() => reverse.mutate(row.public_id)} variant="outline"><RotateCcw className="h-4 w-4" />反冲</Button>}</div></article>)}</div></CardContent></Card>
  </div>;
}

function Field({ label, children }: { label: string; children: React.ReactNode }) { return <label className="space-y-1.5 text-sm"><span className="font-medium text-foreground">{label}</span>{children}</label>; }
function Metric({ label, value, detail, warning = false }: { label: string; value: string; detail: string; warning?: boolean }) { return <Card className={warning ? "border-warning/40" : ""}><CardContent className="flex items-start justify-between"><div><p className="text-xs font-bold uppercase tracking-[0.14em] text-muted-foreground">{label}</p><p className="mt-2 text-2xl font-bold tabular-nums text-navy">{value}</p><p className="mt-1 text-xs text-muted-foreground">{detail}</p></div><span className="rounded-lg bg-primary/10 p-2 text-primary">{label.includes("估值") ? <CircleDollarSign className="h-5 w-5" /> : <Warehouse className="h-5 w-5" />}</span></CardContent></Card>; }
