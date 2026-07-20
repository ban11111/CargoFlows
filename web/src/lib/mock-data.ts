import type { AssetReviewItem, InventoryAdjustment, Sku, SopTemplate, User } from "@/lib/types";

export const skus: Sku[] = [
  {
    id: "sku_001",
    code: "CF-BAG-BLK-M",
    productName: "Urban Carry Tote",
    brand: "CargoFlow",
    category: "Bags",
    color: "Black",
    size: "M",
    stock: 18,
    lowStockThreshold: 20,
    status: "active",
    updatedAt: "2026-07-12T09:30:00.000Z",
  },
  {
    id: "sku_002",
    code: "CF-CAP-CRM-F",
    productName: "Logo Cotton Cap",
    brand: "CargoFlow",
    category: "Accessories",
    color: "Cream",
    size: "Free",
    stock: 64,
    lowStockThreshold: 25,
    status: "active",
    updatedAt: "2026-07-12T11:10:00.000Z",
  },
  {
    id: "sku_003",
    code: "CF-SHOE-WHT-42",
    productName: "Daily Runner",
    brand: "CargoFlow",
    category: "Shoes",
    color: "White",
    size: "42",
    stock: 0,
    lowStockThreshold: 10,
    status: "draft",
    updatedAt: "2026-07-13T03:20:00.000Z",
  },
];

export const inventoryAdjustments: InventoryAdjustment[] = [
  {
    id: "ia_001",
    skuId: "sku_001",
    quantityDelta: -7,
    reason: "Sample handoff",
    operator: "Ivy Chen",
    createdAt: "2026-07-12T14:12:00.000Z",
  },
  {
    id: "ia_002",
    skuId: "sku_001",
    quantityDelta: 25,
    reason: "Inbound count",
    operator: "Bo Lin",
    createdAt: "2026-07-11T08:05:00.000Z",
  },
];

export const sopTemplates: SopTemplate[] = [
  {
    id: "sop_001",
    name: "Apparel Standard Pack",
    category: "Apparel",
    requiredViews: 8,
    optionalViews: 2,
    status: "active",
    updatedAt: "2026-07-08T07:15:00.000Z",
  },
  {
    id: "sop_002",
    name: "Bag Marketplace Pack",
    category: "Bags",
    requiredViews: 10,
    optionalViews: 3,
    status: "active",
    updatedAt: "2026-07-09T12:40:00.000Z",
  },
];

export const assetReviews: AssetReviewItem[] = [
  {
    id: "asset_001",
    skuCode: "CF-BAG-BLK-M",
    productName: "Urban Carry Tote",
    viewName: "Front",
    sessionCode: "PS-20260712-001",
    reviewer: "Mia Wong",
    status: "pending",
    capturedAt: "2026-07-12T06:20:00.000Z",
  },
  {
    id: "asset_002",
    skuCode: "CF-BAG-BLK-M",
    productName: "Urban Carry Tote",
    viewName: "Label",
    sessionCode: "PS-20260712-001",
    status: "rejected",
    capturedAt: "2026-07-12T06:27:00.000Z",
  },
  {
    id: "asset_003",
    skuCode: "CF-CAP-CRM-F",
    productName: "Logo Cotton Cap",
    viewName: "Top",
    sessionCode: "PS-20260712-002",
    reviewer: "Mia Wong",
    status: "approved",
    capturedAt: "2026-07-12T08:04:00.000Z",
  },
];

export const users: User[] = [
  {
    id: "user_001",
    name: "Zheng Baiyi",
    email: "admin@cargoflow.local",
    role: "admin",
    status: "active",
    lastSeenAt: "2026-07-13T04:30:00.000Z",
  },
  {
    id: "user_002",
    name: "Ivy Chen",
    email: "ivy@cargoflow.local",
    role: "operator",
    status: "active",
    lastSeenAt: "2026-07-12T12:11:00.000Z",
  },
  {
    id: "user_003",
    name: "Bo Lin",
    email: "bo@cargoflow.local",
    role: "operator",
    status: "active",
    lastSeenAt: "2026-07-12T10:45:00.000Z",
  },
];
