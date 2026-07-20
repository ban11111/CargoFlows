"use client";

import { useQuery } from "@tanstack/react-query";
import { apiRequest } from "@/lib/api";

export type AppRole = "super_admin" | "admin" | "operator";

export interface CurrentUser {
  public_id: string;
  name: string;
  email: string;
  role: AppRole;
  status: "active" | "disabled";
  must_change_password: boolean;
  last_seen_at: string | null;
  created_at: string;
}

export function useCurrentUser() {
  return useQuery({
    queryKey: ["current-user"],
    queryFn: () => apiRequest<CurrentUser>("/auth/me"),
    staleTime: 60_000,
  });
}

export function isAdministrator(role?: AppRole) {
  return role === "super_admin" || role === "admin";
}
