"use client";

import { Eye, EyeOff, KeyRound, Ship } from "lucide-react";
import { useSearchParams } from "next/navigation";
import { Suspense, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { LanguageToggle } from "@/components/language-toggle";
import { useLanguage } from "@/lib/i18n";

function ChangePasswordForm() {
  const { language } = useLanguage();
  const zh = language === "zh";
  const searchParams = useSearchParams();
  const next = searchParams.get("next") ?? "/skus";
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [show, setShow] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string>();

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setError(undefined);
    if (newPassword.length < 12 || newPassword !== confirmation) {
      setError(zh ? "新密码至少 12 位，且两次输入必须一致。" : "The new password must be at least 12 characters and both entries must match.");
      return;
    }
    setPending(true);
    const response = await fetch("/api/auth/change-password", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }) });
    setPending(false);
    if (!response.ok) {
      setError(response.status === 422 ? (zh ? "当前密码不正确，或新密码不符合要求。" : "The current password is incorrect or the new password is invalid.") : (zh ? "暂时无法修改密码，请重试。" : "The password could not be changed. Try again."));
      return;
    }
    window.location.assign(next.startsWith("/") ? next : "/skus");
  }

  return <form className="mt-8 space-y-4" onSubmit={submit}>
    <div className="space-y-2"><Label htmlFor="current-password">{zh ? "当前临时密码" : "Current temporary password"}</Label><Input autoComplete="current-password" id="current-password" onChange={(event) => setCurrentPassword(event.target.value)} required type="password" value={currentPassword} /></div>
    <div className="space-y-2"><Label htmlFor="new-password">{zh ? "新密码" : "New password"}</Label><div className="relative"><Input autoComplete="new-password" className="pr-12" id="new-password" maxLength={72} minLength={12} onChange={(event) => setNewPassword(event.target.value)} required type={show ? "text" : "password"} value={newPassword} /><button aria-label={show ? (zh ? "隐藏密码" : "Hide password") : (zh ? "显示密码" : "Show password")} className="absolute right-1 top-0 flex h-11 w-11 items-center justify-center text-muted-foreground" onClick={() => setShow((value) => !value)} type="button">{show ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}</button></div><p className="text-xs text-muted-foreground">{zh ? "至少 12 位，最多 72 字节。" : "12 characters minimum, 72 bytes maximum."}</p></div>
    <div className="space-y-2"><Label htmlFor="confirm-password">{zh ? "确认新密码" : "Confirm new password"}</Label><Input autoComplete="new-password" id="confirm-password" maxLength={72} minLength={12} onChange={(event) => setConfirmation(event.target.value)} required type={show ? "text" : "password"} value={confirmation} /></div>
    {error ? <p className="rounded-lg border border-danger/25 bg-danger/5 p-3 text-sm text-danger" role="alert">{error}</p> : null}
    <Button className="w-full" disabled={pending} type="submit"><KeyRound className="h-4 w-4" />{pending ? (zh ? "正在保存…" : "Saving…") : (zh ? "设置新密码" : "Set new password")}</Button>
  </form>;
}

export default function ChangePasswordPage() {
  const { language } = useLanguage();
  const zh = language === "zh";
  return <main className="flex min-h-dvh items-center justify-center bg-background px-5 py-10"><section className="w-full max-w-md rounded-2xl border border-border bg-card p-6 shadow-[0_20px_60px_rgba(18,34,53,0.08)] sm:p-9" aria-labelledby="change-password-heading"><div className="flex items-center justify-between"><div className="flex items-center gap-3"><span className="flex h-11 w-11 items-center justify-center rounded-xl bg-navy text-white"><Ship className="h-5 w-5" /></span><div><p className="font-bold">CargoFlows</p><p className="text-[10px] uppercase tracking-[0.2em] text-muted-foreground">Secure access</p></div></div><LanguageToggle /></div><p className="mt-10 text-xs font-bold uppercase tracking-[0.16em] text-primary">{zh ? "首次登录" : "First sign-in"}</p><h1 className="mt-2 text-3xl font-bold text-navy" id="change-password-heading">{zh ? "设置你的新密码" : "Set your new password"}</h1><p className="mt-3 text-sm leading-6 text-muted-foreground">{zh ? "管理员提供的是临时密码。完成修改后才能进入 CargoFlows。" : "Your administrator provided a temporary password. Change it before entering CargoFlows."}</p><Suspense fallback={<div className="mt-8 h-72 animate-pulse rounded-xl bg-muted" />}><ChangePasswordForm /></Suspense></section></main>;
}
