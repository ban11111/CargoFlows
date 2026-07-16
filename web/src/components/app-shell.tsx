"use client";

import {
  Bot,
  ClipboardCheck,
  FolderTree,
  Images,
  LayoutDashboard,
  LogOut,
  Package,
  Settings2,
  Users,
} from "lucide-react";
import type { Route } from "next";
import Link from "next/link";
import { LanguageToggle } from "@/components/language-toggle";
import { Button } from "@/components/ui/button";
import { type MessageKey, useLanguage } from "@/lib/i18n";

const navItems: Array<{ href: Route; labelKey: MessageKey; icon: typeof Package }> = [
  { href: "/skus", labelKey: "navSku", icon: Package },
  { href: "/categories", labelKey: "navCategories", icon: FolderTree },
  { href: "/sop-templates", labelKey: "navSop", icon: ClipboardCheck },
  { href: "/assets/review", labelKey: "navAssets", icon: Images },
  { href: "/ai-jobs", labelKey: "navAi", icon: Bot },
  { href: "/users", labelKey: "navUsers", icon: Users },
];

export function AppShell({ children }: { children: React.ReactNode }) {
  const { t } = useLanguage();

  return (
    <div className="min-h-screen bg-background">
      <aside className="fixed inset-y-0 left-0 hidden w-60 border-r border-border bg-card lg:block">
        <div className="flex h-14 items-center gap-2 border-b border-border px-4">
          <LayoutDashboard className="h-5 w-5 text-primary" />
          <span className="font-semibold">CargoFlow</span>
        </div>
        <nav className="space-y-1 p-3">
          {navItems.map((item) => (
            <Link
              className="flex h-9 items-center gap-2 rounded-md px-3 text-sm text-muted-foreground hover:bg-muted hover:text-foreground"
              href={item.href}
              key={item.href}
            >
              <item.icon className="h-4 w-4" />
              {t(item.labelKey)}
            </Link>
          ))}
        </nav>
      </aside>
      <div className="lg:pl-60">
        <header className="sticky top-0 z-20 flex h-14 items-center justify-between border-b border-border bg-card px-4">
          <div>
            <p className="text-sm font-semibold">{t("adminTitle")}</p>
            <p className="text-xs text-muted-foreground">{t("adminSubtitle")}</p>
          </div>
          <div className="flex items-center gap-2">
            <LanguageToggle />
            <Button variant="secondary" size="sm">
              <Settings2 className="h-4 w-4" />
              {t("settings")}
            </Button>
            <Button variant="ghost" size="icon" aria-label={t("logout")}>
              <LogOut className="h-4 w-4" />
            </Button>
          </div>
        </header>
        <main className="mx-auto max-w-7xl p-4 lg:p-6">{children}</main>
      </div>
    </div>
  );
}
