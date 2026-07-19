"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FolderPlus, LockKeyhole, Trash2 } from "lucide-react";
import { useState } from "react";
import type { FormEvent } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { apiRequest } from "@/lib/api";
import { categoryLabel } from "@/lib/category-label";
import { useLanguage } from "@/lib/i18n";

interface Category {
  id: number;
  name: string;
  name_en: string;
  is_system: boolean;
  sku_count: number;
  capture_sop_count: number;
}

export default function CategoriesPage() {
  const { language, t } = useLanguage();
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [nameEN, setNameEN] = useState("");
  const [error, setError] = useState<string>();
  const categoriesQuery = useQuery({
    queryKey: ["categories"],
    queryFn: () => apiRequest<{ data: Category[] }>("/categories"),
  });
  const addCategory = useMutation({
    mutationFn: ({ categoryName, categoryNameEN }: { categoryName: string; categoryNameEN: string }) =>
      apiRequest("/categories", { method: "POST", body: JSON.stringify({ name: categoryName, name_en: categoryNameEN }) }),
    onSuccess: async () => {
      setName("");
      setNameEN("");
      setError(undefined);
      await queryClient.invalidateQueries({ queryKey: ["categories"] });
    },
    onError: () => setError(t("categoryName")),
  });
  const deleteCategory = useMutation({
    mutationFn: (id: number) => apiRequest(`/categories/${id}`, { method: "DELETE" }),
    onSuccess: async () => {
      setError(undefined);
      await queryClient.invalidateQueries({ queryKey: ["categories"] });
    },
    onError: () => setError(t("categoryInUse")),
  });

  function submitCategory(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmedName = name.trim();
    const trimmedNameEN = nameEN.trim();
    if (trimmedName && trimmedNameEN) {
      addCategory.mutate({ categoryName: trimmedName, categoryNameEN: trimmedNameEN });
    }
  }

  return (
    <div className="space-y-6">
      <div>
        <p className="mb-2 text-[11px] font-bold uppercase tracking-[0.16em] text-primary">CargoFlow · Taxonomy</p>
        <h1 className="text-3xl font-bold tracking-tight text-navy sm:text-4xl">{t("categoryManage")}</h1>
        <p className="mt-2 text-sm text-muted-foreground">{t("categoryManageDesc")}</p>
      </div>

      <form className="max-w-2xl space-y-2 rounded-xl border border-border bg-card p-4 shadow-[var(--shadow-sm)]" onSubmit={submitCategory}>
        <div className="grid gap-2 sm:grid-cols-[1fr_1fr_auto]">
        <Input
          aria-label={t("categoryNameZh")}
          onChange={(event) => setName(event.target.value)}
          placeholder={t("categoryNameZh")}
          value={name}
        />
        <Input
          aria-label={t("categoryNameEn")}
          onChange={(event) => setNameEN(event.target.value)}
          placeholder={t("categoryNameEn")}
          value={nameEN}
        />
        <Button disabled={!name.trim() || !nameEN.trim() || addCategory.isPending} type="submit">
          <FolderPlus className="h-4 w-4" />
          {t("addCategory")}
        </Button>
        </div>
        <p className="text-xs text-muted-foreground">{t("categoryLanguageHint")}</p>
      </form>
      {error ? <p className="text-sm text-danger">{error}</p> : null}

      <div className="overflow-x-auto rounded-xl border border-border bg-card shadow-[var(--shadow-sm)]">
        <div className="min-w-[620px]">
        <div className="grid grid-cols-[minmax(0,1fr)_90px_90px_104px] gap-3 border-b border-border px-4 py-3 text-xs font-medium text-muted-foreground">
          <span>{t("category")}</span>
          <span>{t("skuCount")}</span>
          <span>{t("sopCount")}</span>
          <span />
        </div>
        {categoriesQuery.data?.data.map((category) => (
          <div
            className="grid grid-cols-[minmax(0,1fr)_90px_90px_104px] items-center gap-3 border-b border-border px-4 py-3 last:border-b-0"
            key={category.id}
          >
            <div className="flex min-w-0 items-center gap-2">
              <div className="min-w-0">
                <span className="block truncate font-medium">{categoryLabel(category, language)}</span>
                <span className="block truncate text-xs text-muted-foreground">{language === "en" ? category.name : category.name_en}</span>
              </div>
              {category.is_system ? (
                <Badge variant="neutral">
                  <LockKeyhole className="h-3 w-3" />
                  {t("systemCategory")}
                </Badge>
              ) : (
                <Badge variant="warning">{t("customCategory")}</Badge>
              )}
            </div>
            <span className="text-sm text-muted-foreground">{category.sku_count}</span>
            <span className="text-sm text-muted-foreground">{category.capture_sop_count}</span>
            <div className="flex justify-end">
              {category.is_system ? null : (
                <Button
                  aria-label={t("deleteCategory")}
                  disabled={deleteCategory.isPending}
                  onClick={() => {
                    if (window.confirm(`${t("deleteCategory")}: ${categoryLabel(category, language)}?`)) {
                      deleteCategory.mutate(category.id);
                    }
                  }}
                  size="icon"
                  title={t("deleteCategory")}
                  variant="ghost"
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              )}
            </div>
          </div>
        ))}
        </div>
      </div>
    </div>
  );
}
