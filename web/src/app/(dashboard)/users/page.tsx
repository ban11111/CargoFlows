"use client";

import type { ColumnDef } from "@tanstack/react-table";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Crown, Eye, EyeOff, KeyRound, LockKeyhole, ShieldCheck, Trash2, UserCog, UserPlus, Users, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { DataTable } from "@/components/data-table";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { apiRequest } from "@/lib/api";
import { type AppRole, type CurrentUser, useCurrentUser } from "@/lib/auth";
import { useLanguage } from "@/lib/i18n";
import { formatDateTime } from "@/lib/utils";

type ManagedRole = "admin" | "operator";
type ModalState = { kind: "add" } | { kind: "edit"; user: CurrentUser } | { kind: "password"; user: CurrentUser };

function PasswordField({ id, label, value, onChange, show, onToggle, zh }: { id: string; label: string; value: string; onChange: (value: string) => void; show: boolean; onToggle?: () => void; zh: boolean }) {
  return <div className="space-y-2"><Label htmlFor={id}>{label}</Label><div className="relative"><Input autoComplete="new-password" className={onToggle ? "pr-12" : undefined} id={id} maxLength={72} minLength={12} onChange={(event) => onChange(event.target.value)} required type={show ? "text" : "password"} value={value} />{onToggle ? <button aria-label={show ? (zh ? "隐藏密码" : "Hide password") : (zh ? "显示密码" : "Show password")} className="absolute right-1 top-0 flex h-11 w-11 items-center justify-center rounded-lg text-muted-foreground hover:bg-muted" onClick={onToggle} type="button">{show ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}</button> : null}</div></div>;
}

function UserModal({ modal, close }: { modal: ModalState; close: () => void }) {
  const { language } = useLanguage();
  const zh = language === "zh";
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<ManagedRole>(modal.kind === "edit" && modal.user.role === "operator" ? "operator" : "admin");
  const [status, setStatus] = useState<"active" | "disabled">(modal.kind === "edit" ? modal.user.status : "active");
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [show, setShow] = useState(false);
  const [error, setError] = useState<string>();

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") close();
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [close]);

  const mutation = useMutation({
    mutationFn: async () => {
      if (modal.kind === "add") return apiRequest<CurrentUser>("/users", { method: "POST", body: JSON.stringify({ name, email, role, password }) });
      if (modal.kind === "edit") return apiRequest<CurrentUser>(`/users/${modal.user.public_id}`, { method: "PATCH", body: JSON.stringify({ role, status }) });
      return apiRequest<CurrentUser>(`/users/${modal.user.public_id}/password`, { method: "PUT", body: JSON.stringify({ password }) });
    },
    onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: ["users"] }); close(); },
    onError: () => setError(zh ? "操作未完成。请检查输入，或确认邮箱没有被使用。" : "The change was not completed. Check the fields and make sure the email is not already used."),
  });

  function submit(event: React.FormEvent) {
    event.preventDefault();
    setError(undefined);
    if (modal.kind !== "edit" && (password.length < 12 || password !== confirmation)) {
      setError(zh ? "密码至少 12 位，且两次输入必须一致。" : "Passwords must match and contain at least 12 characters.");
      return;
    }
    mutation.mutate();
  }

  const title = modal.kind === "add" ? (zh ? "添加用户" : "Add user") : modal.kind === "edit" ? (zh ? "管理账号" : "Manage account") : (zh ? "重置临时密码" : "Reset temporary password");
  return <div className="fixed inset-0 z-[80] flex items-end justify-center bg-[#07131e]/55 p-0 backdrop-blur-sm sm:items-center sm:p-5" role="presentation"><section aria-labelledby="user-modal-title" aria-modal="true" className="max-h-[92dvh] w-full overflow-y-auto rounded-t-2xl border border-border bg-card p-5 shadow-2xl sm:max-w-lg sm:rounded-2xl sm:p-7" role="dialog"><div className="flex items-start justify-between gap-4"><div><p className="text-xs font-bold uppercase tracking-[0.16em] text-primary">CargoFlows · Access</p><h2 className="mt-2 text-2xl font-bold text-navy" id="user-modal-title">{title}</h2>{modal.kind !== "add" ? <p className="mt-1 text-sm text-muted-foreground">{modal.user.name} · {modal.user.email}</p> : null}</div><Button aria-label={zh ? "关闭" : "Close"} onClick={close} size="icon" type="button" variant="ghost"><X className="h-5 w-5" /></Button></div><form className="mt-7 space-y-4" onSubmit={submit}>{modal.kind === "add" ? <><div className="space-y-2"><Label htmlFor="new-user-name">{zh ? "姓名" : "Name"}</Label><Input autoFocus id="new-user-name" maxLength={120} onChange={(event) => setName(event.target.value)} required value={name} /></div><div className="space-y-2"><Label htmlFor="new-user-email">{zh ? "邮箱" : "Email"}</Label><Input autoComplete="email" id="new-user-email" maxLength={180} onChange={(event) => setEmail(event.target.value)} required type="email" value={email} /></div></> : null}<div className="space-y-2"><Label htmlFor="managed-role">{zh ? "角色" : "Role"}</Label><select className="h-11 w-full rounded-lg border border-border bg-card px-3.5 text-sm outline-none focus:border-primary focus:ring-3 focus:ring-primary/10" id="managed-role" onChange={(event) => setRole(event.target.value as ManagedRole)} value={role}><option value="admin">{zh ? "管理员" : "Admin"}</option><option value="operator">{zh ? "运营" : "Operator"}</option></select></div>{modal.kind === "edit" ? <div className="space-y-2"><Label htmlFor="managed-status">{zh ? "账号状态" : "Account status"}</Label><select className="h-11 w-full rounded-lg border border-border bg-card px-3.5 text-sm outline-none focus:border-primary focus:ring-3 focus:ring-primary/10" id="managed-status" onChange={(event) => setStatus(event.target.value as "active" | "disabled")} value={status}><option value="active">{zh ? "启用" : "Active"}</option><option value="disabled">{zh ? "停用" : "Disabled"}</option></select></div> : null}{modal.kind !== "edit" ? <><PasswordField id="managed-password" label={modal.kind === "add" ? (zh ? "初始密码" : "Initial password") : (zh ? "新临时密码" : "New temporary password")} onChange={setPassword} onToggle={() => setShow((value) => !value)} show={show} value={password} zh={zh} /><PasswordField id="managed-password-confirmation" label={zh ? "确认密码" : "Confirm password"} onChange={setConfirmation} show={show} value={confirmation} zh={zh} /><p className="text-xs leading-5 text-muted-foreground">{zh ? "至少 12 位。用户下次登录时必须设置自己的新密码。" : "At least 12 characters. The user must choose a new password at next sign-in."}</p></> : null}{error ? <p className="rounded-lg border border-danger/25 bg-danger/5 p-3 text-sm text-danger" role="alert">{error}</p> : null}<div className="flex flex-col-reverse gap-2 pt-2 sm:flex-row sm:justify-end"><Button onClick={close} type="button" variant="secondary">{zh ? "取消" : "Cancel"}</Button><Button disabled={mutation.isPending} type="submit">{mutation.isPending ? (zh ? "正在保存…" : "Saving…") : (zh ? "保存" : "Save")}</Button></div></form></section></div>;
}

export default function UsersPage() {
  const { language } = useLanguage();
  const zh = language === "zh";
  const currentUser = useCurrentUser();
  const [modal, setModal] = useState<ModalState>();
  const [deleteError, setDeleteError] = useState(false);
  const usersQuery = useQuery({ queryKey: ["users"], queryFn: () => apiRequest<{ data: CurrentUser[] }>("/users") });
  const queryClient = useQueryClient();
  const removeUser = useMutation({
    mutationFn: (user: CurrentUser) => apiRequest<void>(`/users/${user.public_id}`, { method: "DELETE" }),
    onSuccess: async () => { setDeleteError(false); await queryClient.invalidateQueries({ queryKey: ["users"] }); },
    onError: () => setDeleteError(true),
  });
  const roleLabel = useMemo<Record<AppRole, string>>(() => ({ super_admin: zh ? "超级管理员" : "Super admin", admin: zh ? "管理员" : "Admin", operator: zh ? "运营" : "Operator" }), [zh]);

  const columns = useMemo<ColumnDef<CurrentUser>[]>(() => [
    { accessorKey: "name", header: zh ? "姓名" : "Name", cell: ({ row }) => <div><p className="font-medium">{row.original.name}</p>{row.original.role === "super_admin" ? <p className="mt-1 flex items-center gap-1 text-xs text-primary"><Crown className="h-3 w-3" />{zh ? "系统主账号" : "System owner"}</p> : null}</div> },
    { accessorKey: "email", header: zh ? "邮箱" : "Email" },
    { accessorKey: "role", header: zh ? "角色" : "Role", cell: ({ row }) => roleLabel[row.original.role] },
    { accessorKey: "status", header: zh ? "状态" : "Status", cell: ({ row }) => <div className="flex flex-wrap gap-1.5"><Badge variant={row.original.status === "active" ? "success" : "neutral"}>{row.original.status === "active" ? (zh ? "启用" : "Active") : (zh ? "停用" : "Disabled")}</Badge>{row.original.must_change_password ? <Badge variant="neutral">{zh ? "待首次改密" : "Password change due"}</Badge> : null}</div> },
    { accessorKey: "last_seen_at", header: zh ? "最近活跃" : "Last seen", cell: ({ row }) => !row.original.last_seen_at || row.original.last_seen_at.startsWith("0001-") ? "—" : formatDateTime(row.original.last_seen_at) },
    { id: "actions", header: zh ? "操作" : "Actions", cell: ({ row }) => { const locked = row.original.role === "super_admin"; const self = row.original.public_id === currentUser.data?.public_id; return locked ? <span className="flex items-center gap-1.5 text-xs text-muted-foreground"><LockKeyhole className="h-3.5 w-3.5" />{zh ? "固定账号" : "Protected"}</span> : <div className="flex flex-wrap gap-2"><Button disabled={self} onClick={() => setModal({ kind: "edit", user: row.original })} size="sm" title={self ? (zh ? "不能修改自己的角色或状态" : "You cannot change your own role or status") : undefined} variant="secondary"><UserCog className="h-3.5 w-3.5" />{zh ? "管理" : "Manage"}</Button><Button disabled={self} onClick={() => setModal({ kind: "password", user: row.original })} size="sm" variant="ghost"><KeyRound className="h-3.5 w-3.5" />{zh ? "重置密码" : "Reset password"}</Button>{row.original.status === "disabled" ? <Button disabled={self || removeUser.isPending} onClick={() => { setDeleteError(false); if (window.confirm(zh ? `永久移除账号 ${row.original.email}？历史业务记录仍会保留。` : `Remove ${row.original.email}? Historical business records will be preserved.`)) removeUser.mutate(row.original); }} size="sm" variant="danger"><Trash2 className="h-3.5 w-3.5" />{zh ? "删除" : "Delete"}</Button> : null}</div>; } },
  ], [currentUser.data?.public_id, removeUser, roleLabel, zh]);

  const roles = [
    { icon: Crown, title: zh ? "超级管理员" : "Super admin", text: zh ? "完整权限；唯一可管理 OpenAI 设置。" : "Full access; the only role that manages OpenAI settings." },
    { icon: ShieldCheck, title: zh ? "管理员" : "Admin", text: zh ? "管理用户、素材审核与管理员级配置。" : "Manages users, asset review, and admin configuration." },
    { icon: Users, title: zh ? "运营" : "Operator", text: zh ? "执行日常运营、SOP 与 AI 任务，不管理用户或审核素材。" : "Runs daily operations, SOPs, and AI jobs without user management or asset review." },
  ];

  return <div className="space-y-6"><div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-center"><div><p className="mb-2 text-[11px] font-bold uppercase tracking-[0.16em] text-primary">CargoFlows · Access</p><h1 className="text-3xl font-bold tracking-tight text-navy sm:text-4xl">{zh ? "用户与角色" : "Users & roles"}</h1><p className="mt-2 text-sm text-muted-foreground">{zh ? "添加内部账号，并清楚控制后台权限。" : "Add internal accounts and control console access clearly."}</p></div><Button onClick={() => setModal({ kind: "add" })}><UserPlus className="h-4 w-4" />{zh ? "添加用户" : "Add user"}</Button></div><section aria-label={zh ? "角色权限摘要" : "Role permission summary"} className="grid gap-3 md:grid-cols-3">{roles.map((item) => <div className="rounded-xl border border-border bg-card p-4" key={item.title}><div className="flex items-center gap-2 font-semibold text-navy"><span className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary/8 text-primary"><item.icon className="h-4 w-4" /></span>{item.title}</div><p className="mt-3 text-sm leading-6 text-muted-foreground">{item.text}</p></div>)}</section><Card><CardHeader><div className="flex items-center gap-2"><Users className="h-4 w-4 text-primary" /><CardTitle>{zh ? "账号列表" : "Account list"}</CardTitle></div></CardHeader><CardContent>{deleteError ? <p className="mb-4 rounded-lg border border-danger/25 bg-danger/5 p-3 text-sm text-danger" role="alert">{zh ? "删除失败。请确认账号已停用后重试。" : "Delete failed. Disable the account and try again."}</p> : null}{usersQuery.isError ? <div className="rounded-xl border border-danger/25 bg-danger/5 p-5 text-sm text-danger" role="alert">{zh ? "无法载入用户列表，请重试。" : "Users could not be loaded. Try again."}</div> : <DataTable columns={columns} data={usersQuery.data?.data ?? []} searchPlaceholder={zh ? "搜索姓名、邮箱、角色" : "Search name, email, or role"} />}</CardContent></Card>{modal ? <UserModal close={() => setModal(undefined)} modal={modal} /> : null}</div>;
}
