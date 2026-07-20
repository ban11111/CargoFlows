"use client";

import { Boxes, CheckCircle2, Route, Ship } from "lucide-react";
import { Suspense } from "react";
import { LoginForm } from "@/app/(auth)/login/login-form";
import { LanguageToggle } from "@/components/language-toggle";
import { useLanguage } from "@/lib/i18n";

export default function LoginPage() {
  const { language, t } = useLanguage();
  const zh = language === "zh";

  return (
    <main className="grid min-h-dvh bg-card lg:grid-cols-[minmax(420px,0.9fr)_minmax(520px,1.1fr)]">
      <section className="flex min-h-dvh items-center justify-center px-5 py-10 sm:px-10 lg:px-14" aria-labelledby="login-heading">
        <div className="w-full max-w-md">
          <div className="mb-12 flex items-center justify-between gap-4">
            <div className="flex items-center gap-3">
              <span className="flex h-11 w-11 items-center justify-center rounded-xl bg-navy text-white"><Ship className="h-5 w-5" /></span>
              <div><p className="font-bold tracking-tight">CargoFlows</p><p className="text-[10px] font-semibold uppercase tracking-[0.2em] text-muted-foreground">Operations deck</p></div>
            </div>
            <LanguageToggle />
          </div>

          <p className="mb-3 text-xs font-bold uppercase tracking-[0.16em] text-primary">{zh ? "欢迎回来" : "Welcome back"}</p>
          <h1 className="text-[clamp(2rem,5vw,3.25rem)] font-bold leading-[0.98] text-navy" id="login-heading">{t("loginTitle")}</h1>
          <p className="mt-4 max-w-sm text-sm leading-6 text-muted-foreground">{zh ? "登录后继续处理商品资料、拍摄标准、素材审核与 AI 内容任务。" : "Sign in to keep product data, capture standards, asset review, and AI content moving."}</p>
          <div className="mt-9">
            <Suspense fallback={<div className="h-64 animate-pulse rounded-xl bg-muted" />}>
              <LoginForm />
            </Suspense>
          </div>
          <p className="mt-10 text-xs text-muted-foreground">© 2026 CargoFlows · {zh ? "为运营团队而设计" : "Built for operations teams"}</p>
        </div>
      </section>

      <section className="route-grid relative hidden min-h-dvh overflow-hidden bg-navy p-10 text-white lg:flex lg:flex-col lg:justify-between xl:p-16" aria-label={zh ? "产品简介" : "Product overview"}>
        <div aria-hidden className="absolute -right-24 -top-24 h-96 w-96 rounded-full border border-white/10" />
        <div aria-hidden className="absolute -right-2 top-24 h-64 w-64 rounded-full border border-white/10" />
        <div className="relative z-10 flex items-center gap-2 text-xs font-semibold uppercase tracking-[0.18em] text-white/55"><span className="h-2 w-2 rounded-full bg-[#51c58b]" />{zh ? "所有流程运行正常" : "All routes operational"}</div>

        <div className="relative z-10 max-w-2xl">
          <p className="text-sm font-semibold text-[#75d4dd]">{zh ? "从货架到内容发布" : "From shelf to published content"}</p>
          <h2 className="mt-5 max-w-xl font-[Arial_Narrow] text-[clamp(3.25rem,6vw,6.5rem)] font-bold leading-[0.88] tracking-[-0.055em]">{zh ? "让每件商品，都沿着正确的路线前进。" : "Keep every product moving on the right route."}</h2>

          <div className="mt-12 grid max-w-xl gap-3 sm:grid-cols-3">
            {[
              { icon: Boxes, label: zh ? "商品资料" : "Catalog", detail: zh ? "统一管理" : "One source" },
              { icon: Route, label: zh ? "拍摄 SOP" : "Capture SOP", detail: zh ? "标准流转" : "Clear route" },
              { icon: CheckCircle2, label: zh ? "素材审核" : "Asset review", detail: zh ? "有据可查" : "Traceable" },
            ].map((item) => (
              <div className="rounded-xl border border-white/12 bg-white/[0.06] p-4 backdrop-blur-sm" key={item.label}>
                <item.icon className="h-5 w-5 text-signal" />
                <p className="mt-5 text-sm font-bold">{item.label}</p>
                <p className="mt-1 text-xs text-white/48">{item.detail}</p>
              </div>
            ))}
          </div>
        </div>
        <div className="relative z-10 flex items-center gap-4 text-xs text-white/38"><span className="h-px w-16 bg-signal" /> CATALOG / CAPTURE / CONTENT</div>
      </section>
    </main>
  );
}
