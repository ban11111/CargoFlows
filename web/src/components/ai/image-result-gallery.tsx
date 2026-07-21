"use client";
/* eslint-disable @next/next/no-img-element -- private authenticated media intentionally bypasses the public image optimizer */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Eraser, Image as ImageIcon, Library, Paintbrush, Redo2, RefreshCw, Undo2, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { ErrorNotice } from "@/components/error-notice";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { ApiError, apiRequest, authenticatedMediaURL } from "@/lib/api";
import { formatDateTime } from "@/lib/utils";

type Actor = { public_id: string; name: string; email: string };
type Result = { public_id: string; candidate_index: number; parent_result_id?: string; media_url: string; selected: boolean; created_at: string };
type Turn = { public_id: string; sequence: number; operation: "generate" | "edit" | "restart"; parent_result_id?: string; user_instruction: string; mask_present: boolean; status: string; actor: Actor; safe_error: string; requested_model: string; actual_model: string; api_mode: string; provider_request_id: string; failure_code: string; created_at: string; completed_at?: string; results: Result[] };
type Thread = { public_id: string; job_item_id: string; slot_key: string; selected_result_id?: string; turns: Turn[] };
type EditTarget = { thread: Thread; result: Result };

function statusLabel(status: string, zh: boolean) {
  const values: Record<string, [string, string]> = { queued: ["排队中", "Queued"], running: ["生成中", "Running"], completed: ["已完成", "Completed"], failed: ["失败", "Failed"], needs_attention: ["需要确认", "Needs attention"] };
  return values[status]?.[zh ? 0 : 1] ?? status;
}

function RevisionDialog({ target, jobID, zh, onClose, onSaved }: { target: EditTarget; jobID: string; zh: boolean; onClose: () => void; onSaved: () => void }) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const drawing = useRef(false);
  const undo = useRef<ImageData[]>([]);
  const redo = useRef<ImageData[]>([]);
  const [instruction, setInstruction] = useState("");
  const [wholeImage, setWholeImage] = useState(true);
  const [tool, setTool] = useState<"brush" | "eraser">("brush");
  const [brushSize, setBrushSize] = useState(36);
  const [error, setError] = useState<Error>();
  const [pending, setPending] = useState(false);

  function snapshot() {
    const canvas = canvasRef.current;
    const ctx = canvas?.getContext("2d");
    if (canvas && ctx) {
      undo.current.push(ctx.getImageData(0, 0, canvas.width, canvas.height));
      if (undo.current.length > 20) undo.current.shift();
      redo.current = [];
    }
  }
  function restore(from: React.MutableRefObject<ImageData[]>, to: React.MutableRefObject<ImageData[]>) {
    const canvas = canvasRef.current;
    const ctx = canvas?.getContext("2d");
    const next = from.current.pop();
    if (!canvas || !ctx || !next) return;
    to.current.push(ctx.getImageData(0, 0, canvas.width, canvas.height));
    ctx.putImageData(next, 0, 0);
  }
  function point(event: React.PointerEvent<HTMLCanvasElement>) {
    const canvas = event.currentTarget;
    const rect = canvas.getBoundingClientRect();
    return { x: (event.clientX - rect.left) * canvas.width / rect.width, y: (event.clientY - rect.top) * canvas.height / rect.height };
  }
  function start(event: React.PointerEvent<HTMLCanvasElement>) {
    snapshot(); drawing.current = true; event.currentTarget.setPointerCapture(event.pointerId);
    const p = point(event); const ctx = event.currentTarget.getContext("2d"); if (!ctx) return;
    ctx.beginPath(); ctx.moveTo(p.x, p.y); ctx.lineTo(p.x + 0.01, p.y + 0.01); stroke(ctx); ctx.stroke();
  }
  function move(event: React.PointerEvent<HTMLCanvasElement>) {
    if (!drawing.current) return; const ctx = event.currentTarget.getContext("2d"); if (!ctx) return;
    const p = point(event); ctx.lineTo(p.x, p.y); stroke(ctx); ctx.stroke();
  }
  function stroke(ctx: CanvasRenderingContext2D) {
    ctx.lineCap = "round"; ctx.lineJoin = "round"; ctx.lineWidth = brushSize;
    ctx.globalCompositeOperation = tool === "brush" ? "source-over" : "destination-out";
    ctx.strokeStyle = "rgba(239,68,68,.62)";
  }
  function clear() {
    const canvas = canvasRef.current; const ctx = canvas?.getContext("2d"); if (!canvas || !ctx) return;
    snapshot(); ctx.clearRect(0, 0, canvas.width, canvas.height);
  }
  async function maskBlob() {
    const overlay = canvasRef.current;
    if (!overlay) return undefined;
    const output = document.createElement("canvas"); output.width = overlay.width; output.height = overlay.height;
    const ctx = output.getContext("2d"); if (!ctx) return undefined;
    ctx.fillStyle = "black"; ctx.fillRect(0, 0, output.width, output.height);
    ctx.globalCompositeOperation = "destination-out"; ctx.drawImage(overlay, 0, 0);
    return new Promise<Blob | undefined>((resolve) => output.toBlob((blob) => resolve(blob ?? undefined), "image/png"));
  }
  async function submit() {
    if (!instruction.trim()) return;
    setPending(true); setError(undefined);
    try {
      if (wholeImage) {
        await apiRequest(`/ai-jobs/${jobID}/items/${target.thread.job_item_id}/image-turns`, { method: "POST", body: JSON.stringify({ operation: "edit", parent_result_id: target.result.public_id, user_instruction: instruction.trim() }) });
      } else {
        const blob = await maskBlob();
        if (!blob) throw new Error(zh ? "无法创建蒙版" : "Could not create mask");
        const form = new FormData(); form.set("operation", "edit"); form.set("parent_result_id", target.result.public_id); form.set("user_instruction", instruction.trim()); form.set("mask", blob, "mask.png");
        await apiRequest(`/ai-jobs/${jobID}/items/${target.thread.job_item_id}/image-turns`, { method: "POST", body: form });
      }
      onSaved(); onClose();
    } catch (value) { setError(value instanceof Error ? value : new Error("Request failed")); } finally { setPending(false); }
  }

  return <div aria-modal="true" className="fixed inset-0 z-50 grid overflow-y-auto bg-navy/55 p-4" role="dialog">
    <div className="m-auto w-full max-w-4xl overflow-hidden rounded-xl bg-card shadow-2xl">
      <header className="flex items-center justify-between border-b border-border p-4"><div><h3 className="font-semibold">{zh ? "修改此图" : "Edit this image"}</h3><p className="mt-1 text-xs text-muted-foreground">{zh ? "红色标记表示允许 AI 修改的区域" : "Red marks define where AI may make changes"}</p></div><Button aria-label={zh ? "关闭" : "Close"} onClick={onClose} size="icon" variant="ghost"><X className="h-4 w-4" /></Button></header>
      <div className="grid gap-5 p-4 lg:grid-cols-[minmax(0,1fr)_320px]">
        <div className="relative overflow-hidden rounded-lg bg-muted">
          <img alt="" className="block h-auto w-full" onLoad={(event) => { const canvas = canvasRef.current; if (canvas) { canvas.width = event.currentTarget.naturalWidth; canvas.height = event.currentTarget.naturalHeight; } }} src={authenticatedMediaURL(target.result.media_url)} />
          {!wholeImage ? <canvas aria-label={zh ? "区域修改蒙版" : "Area edit mask"} className="absolute inset-0 h-full w-full touch-none cursor-crosshair" onPointerDown={start} onPointerMove={move} onPointerUp={() => { drawing.current = false; }} ref={canvasRef} /> : <canvas className="hidden" ref={canvasRef} />}
        </div>
        <div className="space-y-4">
          <label className="flex min-h-11 items-center gap-3 rounded-lg border border-border p-3 text-sm"><input checked={wholeImage} className="h-5 w-5" onChange={(event) => setWholeImage(event.target.checked)} type="checkbox" />{zh ? "整图修改（不使用蒙版）" : "Edit the whole image (no mask)"}</label>
          {!wholeImage ? <div className="space-y-3 rounded-lg border border-border p-3"><div className="grid grid-cols-2 gap-2"><Button onClick={() => setTool("brush")} variant={tool === "brush" ? "default" : "outline"}><Paintbrush className="h-4 w-4" />{zh ? "画笔" : "Brush"}</Button><Button onClick={() => setTool("eraser")} variant={tool === "eraser" ? "default" : "outline"}><Eraser className="h-4 w-4" />{zh ? "橡皮擦" : "Eraser"}</Button></div><label className="block text-xs">{zh ? "笔刷大小" : "Brush size"}<input className="mt-2 w-full" max="120" min="8" onChange={(e) => setBrushSize(Number(e.target.value))} type="range" value={brushSize} /></label><div className="grid grid-cols-3 gap-2"><Button aria-label={zh ? "撤销" : "Undo"} onClick={() => restore(undo, redo)} size="icon" variant="outline"><Undo2 className="h-4 w-4" /></Button><Button aria-label={zh ? "重做" : "Redo"} onClick={() => restore(redo, undo)} size="icon" variant="outline"><Redo2 className="h-4 w-4" /></Button><Button onClick={clear} variant="outline">{zh ? "清空" : "Clear"}</Button></div></div> : null}
          <label className="block text-sm font-medium">{zh ? "告诉 AI 要修改什么" : "Describe the change"}<Textarea className="mt-2" maxLength={1000} onChange={(event) => setInstruction(event.target.value)} placeholder={zh ? "例如：保持商品完全不变，把背景改成柔和的米白色。" : "For example: keep the product unchanged and make the background soft ivory."} value={instruction} /></label>
          {error ? <ErrorNotice message={error.message} recovery={zh ? "调整修改说明或蒙版后重试。" : "Adjust the instruction or mask and retry."} requestId={error instanceof ApiError ? error.requestId : ""} title={zh ? "无法创建修改轮次" : "Could not create revision"} /> : null}
          <Button className="w-full" disabled={pending || !instruction.trim()} onClick={submit}>{pending ? (zh ? "提交中…" : "Submitting…") : (zh ? "生成 1 张修改版本" : "Generate 1 revised image")}</Button>
        </div>
      </div>
    </div>
  </div>;
}

export function ImageResultGallery({ jobID, language, active, refreshKey, itemLabels = {} }: { jobID: string; language: "zh" | "en"; active: boolean; refreshKey: string; itemLabels?: Record<string, string> }) {
  const zh = language === "zh";
  const queryClient = useQueryClient();
  const [editTarget, setEditTarget] = useState<EditTarget>();
  const [notice, setNotice] = useState<string>();
  const query = useQuery({ queryKey: ["ai-jobs", jobID, "image-threads"], queryFn: () => apiRequest<{ data: Thread[] }>(`/ai-jobs/${encodeURIComponent(jobID)}/image-threads`), refetchInterval: active ? 2000 : false });
  const refresh = async () => { await queryClient.invalidateQueries({ queryKey: ["ai-jobs", jobID] }); };
  const action = useMutation({ mutationFn: ({ path, body }: { path: string; body?: unknown }) => apiRequest(path, { method: "POST", ...(body ? { body: JSON.stringify(body) } : {}) }), onSuccess: async () => { setNotice(undefined); await refresh(); }, onError: (error) => setNotice(error instanceof Error ? error.message : "Request failed") });
  const { refetch } = query;
  useEffect(() => { void refetch(); }, [refetch, refreshKey]);
  if (query.isLoading) return <section className="animate-pulse rounded-lg border border-border bg-card p-4"><div className="h-5 w-28 rounded bg-muted" /><div className="mt-4 aspect-square max-w-64 rounded bg-muted" /></section>;
  if (query.isError) return <ErrorNotice actionLabel={zh ? "重试" : "Retry"} message={query.error instanceof Error ? query.error.message : "Request failed"} onAction={() => query.refetch()} title={zh ? "无法加载图片版本" : "Could not load image versions"} />;
  if (!query.data?.data.length) return null;

  return <section aria-labelledby="image-results-title" className="space-y-4">
    <div className="flex items-center gap-2"><ImageIcon className="h-4 w-4 text-primary" /><h2 className="font-semibold" id="image-results-title">{zh ? "图片版本工作台" : "Image version workspace"}</h2></div>
    {notice ? <ErrorNotice message={notice} recovery={zh ? "检查当前版本状态后重试。" : "Check the current version state and retry."} title={zh ? "操作未完成" : "Action not completed"} /> : null}
    {query.data.data.map((thread) => {
      const resultByID = new Map(thread.turns.flatMap((turn) => turn.results).map((result) => [result.public_id, result]));
      const threadActive = thread.turns.some((turn) => ["queued", "running"].includes(turn.status));
      return <article className="overflow-hidden rounded-xl border border-border bg-card" key={thread.public_id}>
      <header className="flex flex-wrap items-center justify-between gap-3 border-b border-border bg-muted/30 p-4"><div><h3 className="font-semibold">{itemLabels[thread.job_item_id] || thread.slot_key}</h3><p className="mt-1 text-xs text-muted-foreground">{zh ? `${thread.turns.length} 个永久保留的轮次` : `${thread.turns.length} permanently retained turns`}</p></div><Button disabled={action.isPending || threadActive} onClick={() => action.mutate({ path: `/ai-jobs/${jobID}/items/${thread.job_item_id}/image-turns`, body: { operation: "restart", user_instruction: "" } })} variant="outline"><RefreshCw className="h-4 w-4" />{zh ? "重新生成" : "Regenerate"}</Button></header>
      <div className="space-y-5 p-4">{[...thread.turns].reverse().map((turn) => <section className="space-y-3" key={turn.public_id}>
        <div className="flex flex-wrap items-center gap-2 text-xs"><Badge variant={turn.status === "completed" ? "success" : turn.status === "failed" ? "danger" : "neutral"}>#{turn.sequence} · {statusLabel(turn.status, zh)}</Badge><span>{turn.operation === "edit" ? (zh ? "修改" : "Edit") : turn.operation === "restart" ? (zh ? "重做" : "Restart") : (zh ? "初次生成" : "Initial generation")}</span>{turn.mask_present ? <Badge variant="neutral">{zh ? "区域蒙版" : "Area mask"}</Badge> : null}<span className="text-muted-foreground">{turn.actor.name || turn.actor.email || (zh ? "旧记录未记录" : "Not recorded")} · {formatDateTime(turn.created_at)}</span>{turn.requested_model ? <span className="font-mono text-muted-foreground">{turn.actual_model || turn.requested_model} · {turn.api_mode}</span> : null}{turn.provider_request_id ? <span className="font-mono text-muted-foreground">{turn.provider_request_id}</span> : null}</div>
        {turn.user_instruction ? <p className="rounded-md bg-muted/50 px-3 py-2 text-sm">{turn.user_instruction}</p> : null}
        {turn.safe_error ? <ErrorNotice message={turn.safe_error} recovery={turn.status === "needs_attention" ? (zh ? "结果可能已产生。请先联系管理员确认，再决定是否重试，避免重复付费。" : "A result may exist. Ask an administrator to verify before retrying to avoid duplicate charges.") : (zh ? "调整反馈后从任意历史版本再次修改。" : "Adjust the feedback and revise from any historical version.")} title={zh ? "失败原因" : "Failure reason"} /> : null}
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">{turn.results.map((result) => <div className={`overflow-hidden rounded-lg border ${result.selected ? "border-primary ring-2 ring-primary/15" : "border-border"}`} key={result.public_id}>
          {result.parent_result_id && resultByID.get(result.parent_result_id) ? <div className="grid grid-cols-2 gap-px bg-border"><figure className="bg-muted"><img alt={zh ? "父版本" : "Parent version"} className="aspect-square w-full object-cover" src={authenticatedMediaURL(resultByID.get(result.parent_result_id)!.media_url)} /><figcaption className="bg-card px-2 py-1 text-center text-[11px] text-muted-foreground">{zh ? "修改前" : "Before"}</figcaption></figure><a className="bg-muted" href={authenticatedMediaURL(result.media_url)} rel="noreferrer" target="_blank"><img alt={zh ? `图片版本 ${turn.sequence}` : `Image version ${turn.sequence}`} className="aspect-square w-full object-cover" src={authenticatedMediaURL(result.media_url)} /><span className="block bg-card px-2 py-1 text-center text-[11px] text-muted-foreground">{zh ? "修改后" : "After"}</span></a></div> : <a className="block bg-muted" href={authenticatedMediaURL(result.media_url)} rel="noreferrer" target="_blank"><img alt={zh ? `图片版本 ${turn.sequence}` : `Image version ${turn.sequence}`} className="aspect-square w-full object-cover" src={authenticatedMediaURL(result.media_url)} /></a>}
          <div className="space-y-3 p-3">{result.selected ? <Badge variant="success"><Check className="h-3 w-3" />{zh ? "采用版本" : "Selected version"}</Badge> : null}<div className="grid grid-cols-2 gap-2"><Button disabled={threadActive} onClick={() => setEditTarget({ thread, result })} variant="outline"><Paintbrush className="h-4 w-4" />{zh ? "修改此图" : "Edit"}</Button><Button disabled={result.selected || action.isPending} onClick={() => action.mutate({ path: `/ai-jobs/${jobID}/items/${thread.job_item_id}/image-results/${result.public_id}/select` })} variant="secondary"><Check className="h-4 w-4" />{zh ? "采用" : "Select"}</Button></div>{result.selected ? <Button className="w-full" disabled={action.isPending} onClick={() => action.mutate({ path: `/ai-jobs/${jobID}/items/${thread.job_item_id}/image-results/${result.public_id}/submit-to-assets` })} variant="outline"><Library className="h-4 w-4" />{zh ? "提交到素材库审核" : "Submit for asset review"}</Button> : null}</div>
        </div>)}</div>
      </section>)}</div>
    </article>;
    })}
    {editTarget ? <RevisionDialog jobID={jobID} onClose={() => setEditTarget(undefined)} onSaved={refresh} target={editTarget} zh={zh} /> : null}
  </section>;
}
