import type { components } from "@/lib/openapi-types";

export type Role = "super_admin" | "admin" | "operator";

export type SkuStatus = "active" | "draft" | "disabled";

export type ReviewStatus = "pending" | "approved" | "rejected";

export type AiJobStatus = components["schemas"]["AIJob"]["status"];

export interface Sku {
  id: string;
  code: string;
  productName: string;
  brand: string;
  category: string;
  color: string;
  size: string;
  stock: number;
  lowStockThreshold: number;
  status: SkuStatus;
  updatedAt: string;
}

export interface InventoryAdjustment {
  id: string;
  skuId: string;
  quantityDelta: number;
  reason: string;
  operator: string;
  createdAt: string;
}

export interface SopTemplate {
  id: string;
  name: string;
  category: string;
  requiredViews: number;
  optionalViews: number;
  status: "active" | "draft";
  updatedAt: string;
}

export interface AssetReviewItem {
  id: string;
  skuCode: string;
  productName: string;
  viewName: string;
  sessionCode: string;
  reviewer?: string;
  status: ReviewStatus;
  capturedAt: string;
}

export type AiJob = components["schemas"]["AIJob"];

export interface User {
  id: string;
  name: string;
  email: string;
  role: Role;
  status: "active" | "disabled";
  lastSeenAt: string;
}
