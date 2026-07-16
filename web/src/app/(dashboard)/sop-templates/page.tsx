"use client";

import { useQuery } from "@tanstack/react-query";
import type { ColumnDef } from "@tanstack/react-table";
import { ClipboardCheck } from "lucide-react";
import { useState } from "react";
import { DataTable } from "@/components/data-table";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { apiRequest } from "@/lib/api";
import { categoryLabel } from "@/lib/category-label";
import { useLanguage } from "@/lib/i18n";
import { formatDateTime } from "@/lib/utils";

interface SOPTemplate {
  id: number;
  name: string;
  category: string;
  category_record?: { name: string; name_en?: string };
  category_id: number;
  status: "active" | "draft";
  updated_at: string;
  views: Array<{ id: number; required: boolean }>;
}

interface Category {
  id: number;
  name: string;
  name_en: string;
}

export default function SopTemplatesPage() {
  const { language, t } = useLanguage();
  const [categoryID, setCategoryID] = useState("");
  const templatesQuery = useQuery({
    queryKey: ["sop-templates", categoryID],
    queryFn: () => apiRequest<{ data: SOPTemplate[] }>(`/sop-templates${categoryID ? `?category_id=${categoryID}` : ""}`),
  });
  const categoriesQuery = useQuery({
    queryKey: ["categories"],
    queryFn: () => apiRequest<{ data: Category[] }>("/categories"),
  });
  const templates = templatesQuery.data?.data ?? [];
  const columns: ColumnDef<SOPTemplate>[] = [
    { accessorKey: "name", header: t("templateName") },
    { id: "category", header: t("applicableCategory"), cell: ({ row }) => categoryLabel(row.original.category_record ?? { name: row.original.category }, language) },
    { id: "required", header: t("requiredViews"), cell: ({ row }) => row.original.views.filter((view) => view.required).length },
    { id: "optional", header: t("optionalViews"), cell: ({ row }) => row.original.views.filter((view) => !view.required).length },
    {
      accessorKey: "status",
      header: t("status"),
      cell: ({ row }) => <Badge variant={row.original.status === "active" ? "success" : "warning"}>{row.original.status === "active" ? t("active") : t("draft")}</Badge>,
    },
    { accessorKey: "updated_at", header: t("updated"), cell: ({ row }) => formatDateTime(row.original.updated_at) },
  ];

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">{t("sopTemplates")}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{t("sopDesc")}</p>
      </div>
      <Card>
        <CardHeader>
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex items-center gap-2">
              <ClipboardCheck className="h-4 w-4 text-primary" />
              <CardTitle>{t("templateList")}</CardTitle>
            </div>
            <select
              aria-label={t("applicableCategory")}
              className="h-9 rounded-md border border-border bg-card px-3 text-sm outline-none focus:border-primary"
              onChange={(event) => setCategoryID(event.target.value)}
              value={categoryID}
            >
              <option value="">{t("all")}</option>
              {categoriesQuery.data?.data.map((category) => <option key={category.id} value={category.id}>{categoryLabel(category, language)}</option>)}
            </select>
          </div>
        </CardHeader>
        <CardContent>
          <DataTable columns={columns} data={templates} searchPlaceholder={t("searchTemplate")} />
        </CardContent>
      </Card>
    </div>
  );
}
