"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, KeyRound, LoaderCircle, Search } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { apiRequest } from "@/lib/api";

type Setting = { status: string; admin_key_fingerprint: string; project_id: string; api_key_id?: string; scope: "project"; last_synced_at: string | null };
type Scope = { id: string; name: string };

export function OpenAICostSettings() {
  const client = useQueryClient();
  const [adminKey, setAdminKey] = useState("");
  const [projectID, setProjectID] = useState("");
  const [scopes, setScopes] = useState<{ projects: Scope[]; api_keys: Scope[] }>({ projects: [], api_keys: [] });
  const setting = useQuery({ queryKey: ["openai-cost-setting"], queryFn: () => apiRequest<Setting>("/settings/openai/costs"), retry: false, enabled: false });
  const discover = useMutation({ mutationFn: () => apiRequest<{ projects: Scope[]; api_keys: Scope[] }>("/settings/openai/costs/scopes", { method: "POST", body: JSON.stringify({ admin_api_key: adminKey }) }), onSuccess: setScopes });
  const save = useMutation({ mutationFn: () => apiRequest<Setting>("/settings/openai/costs", { method: "PUT", body: JSON.stringify({ admin_api_key: adminKey, project_id: projectID }) }), onSuccess: async () => { setAdminKey(""); await client.invalidateQueries({ queryKey: ["openai-cost-setting"] }); } });
  const error = setting.error ?? discover.error ?? save.error;

  return <Card className="border-primary/25">
    <CardHeader><CardTitle className="flex items-center gap-2"><KeyRound className="h-4 w-4 text-primary" />组织成本查询凭据（Admin API Key）</CardTitle></CardHeader>
    <CardContent className="space-y-5">
      <p className="text-sm leading-6 text-muted-foreground">与执行模型请求的 Project API Key 分离保存。Admin Key 只用于组织 Usage/Costs 查询，经过加密且不会回显。成本按整个 CargoFlows 独占 Project 同步，更换 Project API Key 后历史费用仍会保留。</p>
      {setting.data?.status === "active" ? <div className="flex flex-wrap items-center gap-2 rounded-lg bg-success/5 p-3 text-sm text-success"><CheckCircle2 className="h-4 w-4" />已配置 · 指纹 {setting.data.admin_key_fingerprint} · Project {setting.data.project_id} · 整个 Project（支持 Key 轮换）</div> : null}
      <div className="grid gap-4 lg:grid-cols-2">
        <div className="space-y-2"><Label htmlFor="cost-admin-key">Admin API Key</Label><Input autoComplete="new-password" id="cost-admin-key" placeholder="sk-admin-…" type="password" value={adminKey} onChange={(event) => setAdminKey(event.target.value)} /></div>
        <div className="space-y-2"><Label htmlFor="cost-project">CargoFlows 独占 Project</Label><select className="min-h-11 w-full rounded-lg border border-border bg-card px-3 font-mono text-sm" id="cost-project" value={projectID} onChange={(event) => setProjectID(event.target.value)}><option value="">选择 project_id</option>{scopes.projects.map((scope) => <option key={scope.id} value={scope.id}>{scope.name} · {scope.id}</option>)}</select></div>
      </div>
      {error ? <p className="rounded-lg border border-danger/30 bg-danger/5 p-3 text-sm text-danger" role="alert">{error.message}</p> : null}
      <div className="flex flex-wrap justify-end gap-2"><Button className="min-h-11" disabled={setting.isFetching} onClick={() => setting.refetch()} variant="ghost">读取当前绑定</Button><Button className="min-h-11" disabled={discover.isPending || adminKey.length < 20} onClick={() => discover.mutate()} variant="outline">{discover.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Search className="h-4 w-4" />}读取 Project 列表</Button><Button className="min-h-11" disabled={save.isPending || !adminKey || !projectID} onClick={() => save.mutate()}>{save.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <KeyRound className="h-4 w-4" />}验证并保存</Button></div>
    </CardContent>
  </Card>;
}
