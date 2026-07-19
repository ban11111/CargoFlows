"use client";

import type { ColumnDef } from "@tanstack/react-table";
import { UserPlus, Users } from "lucide-react";
import { DataTable } from "@/components/data-table";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useLanguage } from "@/lib/i18n";
import { users } from "@/lib/mock-data";
import type { Role, User } from "@/lib/types";
import { formatDateTime } from "@/lib/utils";

export default function UsersPage() {
  const { t } = useLanguage();
  const roleLabel: Record<Role, string> = {
    admin: t("admin"),
    operator: t("operator"),
    photographer: t("photographer"),
    viewer: t("viewer"),
  };
  const columns: ColumnDef<User>[] = [
    { accessorKey: "name", header: t("name") },
    { accessorKey: "email", header: t("email") },
    {
      accessorKey: "role",
      header: t("role"),
      cell: ({ row }) => roleLabel[row.original.role],
    },
    {
      accessorKey: "status",
      header: t("status"),
      cell: ({ row }) => (
        <Badge variant={row.original.status === "active" ? "success" : "neutral"}>
          {row.original.status === "active" ? t("active") : t("disabled")}
        </Badge>
      ),
    },
    {
      accessorKey: "lastSeenAt",
      header: t("lastSeen"),
      cell: ({ row }) => formatDateTime(row.original.lastSeenAt),
    },
  ];

  return (
    <div className="space-y-6">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
        <div>
          <p className="mb-2 text-[11px] font-bold uppercase tracking-[0.16em] text-primary">CargoFlow · Access</p>
          <h1 className="text-3xl font-bold tracking-tight text-navy sm:text-4xl">{t("usersAndRoles")}</h1>
          <p className="mt-2 text-sm text-muted-foreground">{t("usersDesc")}</p>
        </div>
        <Button>
          <UserPlus className="h-4 w-4" />
          {t("inviteUser")}
        </Button>
      </div>
      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <Users className="h-4 w-4 text-primary" />
            <CardTitle>{t("accountList")}</CardTitle>
          </div>
        </CardHeader>
        <CardContent>
          <DataTable columns={columns} data={users} searchPlaceholder={t("searchUsers")} />
        </CardContent>
      </Card>
    </div>
  );
}
