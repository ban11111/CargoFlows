export type Role = "admin" | "operator" | "photographer" | "viewer";

export type SkuStatus = "active" | "draft" | "disabled";

export type ReviewStatus = "pending" | "approved" | "rejected";

export type AiJobStatus = "pending" | "queued" | "running" | "completed" | "failed";

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

export interface AiJob {
  id: string;
  skuCode: string;
  targetPlatform: string;
  status: AiJobStatus;
  inputAssets: number;
  createdAt: string;
}

export interface User {
  id: string;
  name: string;
  email: string;
  role: Role;
  status: "active" | "disabled";
  lastSeenAt: string;
}

