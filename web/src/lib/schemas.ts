import { z } from "zod";

export const loginSchema = z.object({
  email: z.string().email(),
  password: z.string().min(8),
});

export const skuSchema = z.object({
  code: z.string().min(2),
  productName: z.string().min(2),
  brand: z.string().min(1),
  category: z.string().min(1),
  color: z.string().optional(),
  size: z.string().optional(),
  lowStockThreshold: z.coerce.number().int().min(0),
});

export const inventoryAdjustmentSchema = z.object({
  quantityDelta: z.coerce.number().int().refine((value) => value !== 0),
  reason: z.string().min(2),
  note: z.string().optional(),
});

export type LoginInput = z.infer<typeof loginSchema>;
export type SkuInput = z.infer<typeof skuSchema>;
export type InventoryAdjustmentInput = z.infer<typeof inventoryAdjustmentSchema>;
