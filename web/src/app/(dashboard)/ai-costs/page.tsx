"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, CircleDollarSign, LoaderCircle, RefreshCw, Scale } from "lucide-react";
import { useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { ApiError, apiRequest } from "@/lib/api";
import { isAdministrator, useCurrentUser } from "@/lib/auth";

type Rate = { id: number; version: number; model: string; api_mode: string; service_tier: string; metric: string; unit_size: number; unit_rate_usd: string; effective_at: string };
type Bucket = { public_id: string; line_item: string; actual_amount_usd: string; status: string; project_id: string; api_key_id: string; synced_at: string };
type Day = { date: string; estimated_amount_usd: string; actual_amount_usd: string; difference_amount_usd: string; difference_rate: string | null; unpriced_usage_count: number; status: string; buckets: Bucket[] };
type AnalyticsRow = { dimension_value: string; total_tokens: number; estimated_amount_usd: string; unpriced_count: number };

function isoDate(value: Date) { return value.toISOString().slice(0, 10); }
const now = new Date();
const defaultEnd = isoDate(now);
const defaultStart = isoDate(new Date(now.getTime() - 6 * 86400000));
const usd = (value?: string) => `$${Number(value ?? 0).toLocaleString(undefined, { minimumFractionDigits: 4, maximumFractionDigits: 8 })}`;

export function costErrorMessage(error: unknown) {
  if (error instanceof ApiError) {
    const messages: Record<string, string> = {
      openai_cost_not_configured: "请先到 OpenAI 设置配置 Admin API Key 和 CargoFlows 独占 Project。",
      openai_cost_permission_denied: "Admin API Key 没有该 Project 的组织成本读取权限，请重新验证凭据。",
      openai_cost_request_rejected: "OpenAI 拒绝了成本查询，请检查日期范围后重试。",
      openai_cost_rate_limited: "OpenAI 成本查询暂时受到速率限制，请稍后重试。",
      openai_cost_invalid_response: "OpenAI 返回了无法识别的成本数据，请稍后重试。",
      openai_cost_unavailable: "OpenAI 成本与用量接口暂时不可用，请稍后重试。",
      openai_cost_period_closed: "所选日期包含已关闭账期，请先显式重新开启该月份。",
    };
    return messages[error.code] ?? error.message;
  }
  return error instanceof Error ? error.message : "无法载入 AI 成本数据";
}

export default function AICostsPage() {
  const client = useQueryClient();
  const user = useCurrentUser();
  const admin = isAdministrator(user.data?.role);
  const [startDate, setStartDate] = useState(defaultStart);
  const [endDate, setEndDate] = useState(defaultEnd);
	const [groupBy, setGroupBy] = useState("date");
  const [rate, setRate] = useState({ model: "", api_mode: "responses", service_tier: "default", metric: "input_text", unit_rate_usd: "" });
	const [month, setMonth] = useState(defaultEnd.slice(0, 7));
	const [invoiceReference, setInvoiceReference] = useState("");
  const summary = useQuery({ queryKey: ["ai-cost-reconciliation", startDate, endDate], queryFn: () => apiRequest<{ data: Day[] }>(`/ai-cost/reconciliation?start_date=${startDate}&end_date=${endDate}`) });
	const analytics = useQuery({ queryKey: ["ai-cost-analytics", startDate, endDate, groupBy], queryFn: () => apiRequest<{ data: AnalyticsRow[] }>(`/ai-cost/analytics?start_date=${startDate}&end_date=${endDate}&group_by=${groupBy}`) });
  const rates = useQuery({ queryKey: ["ai-cost-rates"], queryFn: () => apiRequest<{ data: Rate[] }>("/ai-cost/rates") });
  const sync = useMutation({ mutationFn: () => apiRequest<{ synced_buckets: number }>("/ai-cost/reconciliation/sync", { method: "POST", body: JSON.stringify({ start_date: startDate, end_date: endDate }) }), onSuccess: async () => client.invalidateQueries({ queryKey: ["ai-cost-reconciliation"] }) });
  const addRate = useMutation({ mutationFn: () => apiRequest("/ai-cost/rates", { method: "POST", body: JSON.stringify({ effective_at: new Date().toISOString(), rates: [rate] }) }), onSuccess: async () => { setRate({ ...rate, model: "", unit_rate_usd: "" }); await client.invalidateQueries({ queryKey: ["ai-cost-rates"] }); } });
	const period = useMutation({ mutationFn: (action: "close" | "reopen") => apiRequest(`/ai-cost/reconciliation/periods/${month}/${action}`, { method: "POST", body: action === "close" ? JSON.stringify({ invoice_reference: invoiceReference }) : "{}" }) });
  const days = summary.data?.data ?? [];
  const estimated = days.reduce((sum, day) => sum + Number(day.estimated_amount_usd), 0);
  const actual = days.reduce((sum, day) => sum + Number(day.actual_amount_usd), 0);
  const unpriced = days.reduce((sum, day) => sum + day.unpriced_usage_count, 0);
  const error = summary.error ?? sync.error ?? addRate.error;

  return <div className="space-y-6">
    <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-end"><div><p className="mb-2 text-[11px] font-bold uppercase tracking-[0.16em] text-primary">CargoFlows · Dual ledger</p><h1 className="text-3xl font-bold tracking-tight text-navy sm:text-4xl">AI 成本核算</h1><p className="mt-2 text-sm text-muted-foreground">任务级实时估算与 OpenAI 每日聚合实际成本分开记录、自动对账。</p></div>{admin ? <Button className="min-h-11" disabled={sync.isPending} onClick={() => sync.mutate()}>{sync.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}同步实际成本</Button> : null}</div>
    <div className="rounded-xl border border-primary/20 bg-primary/[0.035] p-4 text-sm text-foreground"><strong>账务标签：</strong>“任务估算”来自冻结 token 费率；“实际分摊”来自供应商日成本桶按估算比例分配，并非 OpenAI 逐 Request ID 精确收费。正式 invoice/PDF 仍需从 Billing 控制台下载。</div>
    <div className="grid gap-3 md:grid-cols-3"><Metric icon={<CircleDollarSign className="h-5 w-5" />} label="任务估算" value={usd(String(estimated))} detail="本地冻结费率" /><Metric icon={<Scale className="h-5 w-5" />} label="OpenAI 实际" value={usd(String(actual))} detail={`差额 ${usd(String(actual - estimated))}`} /><Metric icon={<AlertTriangle className="h-5 w-5" />} label="未定价 usage" value={unpriced.toLocaleString()} detail="存在时停止任务级分摊" warning={unpriced > 0} /></div>
    <Card><CardHeader><CardTitle>UTC 日期对账</CardTitle></CardHeader><CardContent>
      <div className="mb-5 grid gap-3 sm:grid-cols-2 lg:max-w-xl"><Field label="开始日期"><Input type="date" value={startDate} onChange={(event) => setStartDate(event.target.value)} /></Field><Field label="结束日期"><Input type="date" value={endDate} onChange={(event) => setEndDate(event.target.value)} /></Field></div>
      {error ? <p className="mb-4 rounded-lg border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert">{costErrorMessage(error)}</p> : null}
      <div className="overflow-x-auto"><table className="w-full text-left text-sm"><thead className="border-b text-xs uppercase tracking-wide text-muted-foreground"><tr><th className="px-3 py-3">UTC 日期</th><th className="px-3 py-3">任务估算</th><th className="px-3 py-3">OpenAI 实际</th><th className="px-3 py-3">差额 / 差异率</th><th className="px-3 py-3">Line item</th><th className="px-3 py-3">状态</th></tr></thead><tbody>{days.map((day) => <tr className="border-b border-border/70 align-top" key={day.date}><td className="px-3 py-4 font-mono">{day.date}</td><td className="px-3 py-4 tabular-nums">{usd(day.estimated_amount_usd)}</td><td className="px-3 py-4 tabular-nums">{usd(day.actual_amount_usd)}</td><td className="px-3 py-4 tabular-nums">{usd(day.difference_amount_usd)}<span className="block text-xs text-muted-foreground">{day.difference_rate == null ? "—" : `${(Number(day.difference_rate) * 100).toFixed(2)}%`}</span></td><td className="px-3 py-4"><div className="space-y-1">{day.buckets.map((bucket) => <p key={bucket.public_id}><span className="font-medium">{bucket.line_item}</span> <span className="tabular-nums text-muted-foreground">{usd(bucket.actual_amount_usd)}</span></p>)}</div></td><td className="px-3 py-4"><Badge variant={day.status === "reconciled" ? "success" : day.status === "needs_attention" ? "warning" : "neutral"}>{day.status}</Badge></td></tr>)}</tbody></table>{summary.isLoading ? <div className="h-28 animate-pulse rounded-lg bg-muted" /> : null}{summary.isSuccess && days.length === 0 ? <p className="py-10 text-center text-sm text-muted-foreground">该日期范围尚无供应商成本桶。</p> : null}</div>
    </CardContent></Card>
    <Card><CardHeader><CardTitle>不可变费率版本</CardTitle></CardHeader><CardContent className="space-y-5">
      {admin ? <div className="grid gap-3 rounded-lg border border-border bg-muted/25 p-4 md:grid-cols-5"><Field label="实际模型"><Input placeholder="gpt-5.4" value={rate.model} onChange={(event) => setRate({ ...rate, model: event.target.value })} /></Field><Field label="API mode"><select className="min-h-11 w-full rounded-lg border border-border bg-card px-3" value={rate.api_mode} onChange={(event) => setRate({ ...rate, api_mode: event.target.value })}><option value="responses">responses</option><option value="images">images</option></select></Field><Field label="Service tier"><Input value={rate.service_tier} onChange={(event) => setRate({ ...rate, service_tier: event.target.value })} /></Field><Field label="Usage 类型"><select className="min-h-11 w-full rounded-lg border border-border bg-card px-3" value={rate.metric} onChange={(event) => setRate({ ...rate, metric: event.target.value })}>{["input_text", "cached_input", "input_image", "output_text", "output_image"].map((metric) => <option key={metric}>{metric}</option>)}</select></Field><Field label="USD / 1M"><Input inputMode="decimal" value={rate.unit_rate_usd} onChange={(event) => setRate({ ...rate, unit_rate_usd: event.target.value })} /></Field><div className="md:col-span-5"><Button className="min-h-11" disabled={addRate.isPending || !rate.model || !rate.unit_rate_usd} onClick={() => addRate.mutate()}>发布新费率版本</Button></div></div> : null}
      <div className="overflow-x-auto"><table className="w-full text-left text-sm"><thead className="border-b text-xs uppercase tracking-wide text-muted-foreground"><tr><th className="px-3 py-3">版本</th><th className="px-3 py-3">模型</th><th className="px-3 py-3">模式 / Tier</th><th className="px-3 py-3">Usage</th><th className="px-3 py-3">USD / 1M</th><th className="px-3 py-3">生效时间</th></tr></thead><tbody>{rates.data?.data.map((item) => <tr className="border-b border-border/70" key={item.id}><td className="px-3 py-3">v{item.version}</td><td className="px-3 py-3 font-mono">{item.model}</td><td className="px-3 py-3">{item.api_mode} · {item.service_tier}</td><td className="px-3 py-3 font-mono">{item.metric}</td><td className="px-3 py-3 tabular-nums">{item.unit_rate_usd}</td><td className="px-3 py-3">{new Date(item.effective_at).toLocaleString()}</td></tr>)}</tbody></table></div>
    </CardContent></Card>
	<Card><CardHeader><div className="flex flex-wrap items-center justify-between gap-3"><CardTitle>本地估算分析</CardTitle><select className="min-h-11 rounded-lg border border-border bg-card px-3 text-sm" value={groupBy} onChange={(event) => setGroupBy(event.target.value)}><option value="date">按日期</option><option value="sku">按 SKU</option><option value="user">按用户</option><option value="model">按模型</option><option value="template">按模板</option><option value="platform">按平台</option></select></div></CardHeader><CardContent><div className="overflow-x-auto"><table className="w-full text-left text-sm"><thead className="border-b text-xs uppercase tracking-wide text-muted-foreground"><tr><th className="px-3 py-3">维度</th><th className="px-3 py-3">Token</th><th className="px-3 py-3">估算 USD</th><th className="px-3 py-3">未定价</th></tr></thead><tbody>{analytics.data?.data.map((row) => <tr className="border-b border-border/70" key={row.dimension_value}><td className="px-3 py-3 font-medium">{row.dimension_value || "—"}</td><td className="px-3 py-3 tabular-nums">{row.total_tokens.toLocaleString()}</td><td className="px-3 py-3 tabular-nums">{usd(row.estimated_amount_usd)}</td><td className="px-3 py-3">{row.unpriced_count ? <Badge variant="warning">{row.unpriced_count}</Badge> : "0"}</td></tr>)}</tbody></table></div></CardContent></Card>
	{admin ? <Card><CardHeader><CardTitle>月结控制</CardTitle></CardHeader><CardContent><div className="grid gap-3 md:grid-cols-[180px_1fr_auto_auto]"><Field label="账期"><Input type="month" value={month} onChange={(event) => setMonth(event.target.value)} /></Field><Field label="Invoice reference（PDF 人工核对）"><Input value={invoiceReference} onChange={(event) => setInvoiceReference(event.target.value)} placeholder="例如 INV-2026-07 / Billing PDF" /></Field><Button className="mt-6 min-h-11" disabled={period.isPending} onClick={() => period.mutate("close")}>关闭月份</Button><Button className="mt-6 min-h-11" disabled={period.isPending} onClick={() => period.mutate("reopen")} variant="outline">显式重新开启</Button></div>{period.isSuccess ? <p className="mt-3 text-sm text-success" role="status">账期状态已更新。</p> : null}{period.isError ? <p className="mt-3 text-sm text-danger" role="alert">{period.error.message}</p> : null}</CardContent></Card> : null}
  </div>;
}

function Field({ label, children }: { label: string; children: React.ReactNode }) { return <label className="space-y-1.5 text-sm"><span className="font-medium">{label}</span>{children}</label>; }
function Metric({ icon, label, value, detail, warning = false }: { icon: React.ReactNode; label: string; value: string; detail: string; warning?: boolean }) { return <Card className={warning ? "border-warning/40" : ""}><CardContent className="flex items-start justify-between"><div><p className="text-xs font-bold uppercase tracking-[0.14em] text-muted-foreground">{label}</p><p className="mt-2 text-2xl font-bold tabular-nums text-navy">{value}</p><p className="mt-1 text-xs text-muted-foreground">{detail}</p></div><span className="rounded-lg bg-primary/10 p-2 text-primary">{icon}</span></CardContent></Card>; }
