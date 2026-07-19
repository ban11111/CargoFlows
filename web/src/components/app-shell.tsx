"use client";

import {
  Bot,
  ClipboardCheck,
  FileStack,
  FolderTree,
  Images,
  LogOut,
  Menu,
  Package,
  Settings2,
  Ship,
  Users,
  X,
} from "lucide-react";
import type { Route } from "next";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState } from "react";
import { LanguageToggle } from "@/components/language-toggle";
import { Button } from "@/components/ui/button";
import { type MessageKey, useLanguage } from "@/lib/i18n";
import { cn } from "@/lib/utils";

type NavGroup = "navGroupOperations" | "navGroupIntelligence" | "navGroupSystem";

const navItems: Array<{ href: Route; labelKey: MessageKey; icon: typeof Package; group: NavGroup }> = [
  { href: "/skus", labelKey: "navSku", icon: Package, group: "navGroupOperations" },
  { href: "/categories", labelKey: "navCategories", icon: FolderTree, group: "navGroupOperations" },
  { href: "/sop-templates", labelKey: "navSop", icon: ClipboardCheck, group: "navGroupOperations" },
  { href: "/assets/review", labelKey: "navAssets", icon: Images, group: "navGroupOperations" },
  { href: "/ai-templates", labelKey: "navAIContentTemplates", icon: FileStack, group: "navGroupIntelligence" },
  { href: "/ai-jobs", labelKey: "navAi", icon: Bot, group: "navGroupIntelligence" },
  { href: "/settings/openai", labelKey: "navOpenAISettings", icon: Settings2, group: "navGroupSystem" },
  { href: "/users", labelKey: "navUsers", icon: Users, group: "navGroupSystem" },
];

function Navigation({ onNavigate }: { onNavigate?: () => void }) {
  const pathname = usePathname() ?? "";
  const { t } = useLanguage();

  return (
    <nav aria-label={t("primaryNavigation")} className="px-4 py-5">
      {(["navGroupOperations", "navGroupIntelligence", "navGroupSystem"] as const).map((group, groupIndex) => (
        <section className={cn(groupIndex > 0 && "mt-6")} key={group}>
          <p className="mb-2 px-3 text-[10px] font-bold uppercase tracking-[0.2em] text-white/35">{t(group)}</p>
          <div className="space-y-1">
            {navItems.filter((item) => item.group === group).map((item) => {
              const active = pathname === item.href || pathname.startsWith(`${item.href}/`);
              return (
                <Link
                  aria-current={active ? "page" : undefined}
                  data-active={active ? "true" : "false"}
                  className={cn(
                    "sidebar-link group relative flex min-h-11 items-center gap-3 overflow-hidden rounded-lg px-3 text-sm font-medium transition-colors duration-200",
                    active ? "bg-white/10" : "hover:bg-white/[0.055]",
                  )}
                  href={item.href}
                  key={item.href}
                  onClick={onNavigate}
                >
                  {active ? <span aria-hidden className="absolute inset-y-2 left-0 w-[3px] rounded-r-full bg-signal" /> : null}
                  <span className={cn("flex h-8 w-8 shrink-0 items-center justify-center rounded-lg transition-colors", active ? "bg-white/12 text-[#ff9a68]" : "bg-white/[0.055] text-white/52 group-hover:text-white/85")}>
                    <item.icon className="h-4 w-4" />
                  </span>
                  <span className="truncate">{t(item.labelKey)}</span>
                </Link>
              );
            })}
          </div>
        </section>
      ))}
    </nav>
  );
}

function Brand() {
  return (
    <Link className="flex h-[76px] items-center gap-3 border-b border-white/10 px-5" href="/skus">
      <span className="flex h-10 w-10 items-center justify-center rounded-xl bg-signal text-white shadow-[0_8px_20px_rgba(230,107,46,0.24)]">
        <Ship className="h-5 w-5" />
      </span>
      <span>
        <span className="block text-[17px] font-bold tracking-tight text-white">CargoFlow</span>
        <span className="block text-[10px] font-semibold uppercase tracking-[0.22em] text-white/45">Operations deck</span>
      </span>
    </Link>
  );
}

export function AppShell({ children }: { children: React.ReactNode }) {
  const { t } = useLanguage();
  const [mobileOpen, setMobileOpen] = useState(false);

  return (
    <div className="min-h-dvh bg-background">
      <a className="sr-only z-[100] bg-card px-4 py-3 focus:not-sr-only focus:fixed focus:left-3 focus:top-3" href="#main-content">
        {t("skipToContent")}
      </a>

      <aside className="fixed inset-y-0 left-0 z-40 hidden w-64 overflow-y-auto bg-navy shadow-[8px_0_30px_rgba(18,34,53,0.06)] lg:block">
        <Brand />
        <Navigation />
        <div className="mx-4 mb-5 mt-7 rounded-xl border border-white/8 bg-white/[0.035] p-3.5 text-[11px] leading-relaxed">
          <p className="font-semibold uppercase tracking-[0.16em] text-white/32">{t("systemStatus")}</p>
          <p className="mt-2 flex items-center gap-2 text-white/64"><span className="h-2 w-2 rounded-full bg-[#51c58b] shadow-[0_0_0_4px_rgba(81,197,139,0.1)]" /> {t("allRoutesOperational")}</p>
        </div>
      </aside>

      {mobileOpen ? (
        <div className="fixed inset-0 z-50 lg:hidden">
          <button aria-label={t("dismissNavigation")} className="absolute inset-0 bg-[#07131e]/65 backdrop-blur-sm" onClick={() => setMobileOpen(false)} />
          <aside className="relative h-full w-[min(84vw,320px)] overflow-y-auto bg-navy shadow-2xl">
            <div className="relative">
              <Brand />
              <button aria-label={t("closeNavigation")} className="absolute right-3 top-4 flex h-11 w-11 items-center justify-center rounded-lg text-white/70 hover:bg-white/10 hover:text-white" onClick={() => setMobileOpen(false)}>
                <X className="h-5 w-5" />
              </button>
            </div>
            <Navigation onNavigate={() => setMobileOpen(false)} />
          </aside>
        </div>
      ) : null}

      <div className="lg:pl-64">
        <header className="sticky top-0 z-30 flex h-[76px] items-center justify-between gap-3 border-b border-border/80 bg-background/92 px-4 backdrop-blur-xl sm:px-6 lg:px-8">
          <div className="flex min-w-0 items-center gap-3">
            <Button aria-label={t("openNavigation")} className="shrink-0 lg:hidden" onClick={() => setMobileOpen(true)} size="icon" variant="secondary">
              <Menu className="h-5 w-5" />
            </Button>
            <div className="min-w-0">
              <p className="truncate text-sm font-bold tracking-tight text-foreground">{t("adminTitle")}</p>
              <p className="hidden truncate text-xs text-muted-foreground sm:block">{t("adminSubtitle")}</p>
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <LanguageToggle />
            <Button asChild className="hidden sm:inline-flex" variant="secondary" size="sm">
              <Link aria-label={t("navOpenAISettings")} href="/settings/openai">
                <Settings2 className="h-4 w-4" />
                <span className="hidden xl:inline">{t("navOpenAISettings")}</span>
              </Link>
            </Button>
            <Button variant="ghost" size="icon" aria-label={t("logout")}>
              <LogOut className="h-4 w-4" />
            </Button>
          </div>
        </header>
        <main className="page-enter mx-auto max-w-[1480px] px-4 py-6 sm:px-6 lg:px-8 lg:py-8" id="main-content" tabIndex={-1}>{children}</main>
      </div>
    </div>
  );
}
