import { z } from "zod";

import { sopPresetKeys } from "./sop";

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

export const localizedTextSchema = z.object({
  "zh-CN": z.string(),
  en: z.string(),
}).strict();

const requiredLocalizedTextSchema = z.object({
  "zh-CN": z.string().trim().min(1),
  en: z.string().trim().min(1),
}).strict();

export const vector3Schema = z.tuple([
  z.number().finite(),
  z.number().finite(),
  z.number().finite(),
]);

export const compositionSchema = z.object({
  frame_occupancy: z.number().finite().gt(0).lte(1),
  aspect_ratio: z.string().regex(/^[1-9]\d*:[1-9]\d*$/),
  allow_rotation_correction: z.boolean(),
  allow_mirror: z.literal(false),
}).strict();

const poseSchema = z.object({
  space: z.literal("object"),
  camera_position_direction: vector3Schema,
  image_up_direction: vector3Schema,
  target: vector3Schema,
}).strict().superRefine((pose, context) => {
  const scaledUnit = (vector: readonly [number, number, number]) => {
    const scale = Math.max(...vector.map(Math.abs));
    if (scale === 0) return undefined;
    const scaled = vector.map((component) => component / scale) as [number, number, number];
    const scaledLength = Math.hypot(...scaled);
    if (scale < 1e-9 / scaledLength) return undefined;
    return scaled.map((component) => component / scaledLength) as [number, number, number];
  };
  const cameraUnit = scaledUnit(pose.camera_position_direction);
  const upUnit = scaledUnit(pose.image_up_direction);
  if (!cameraUnit) {
    context.addIssue({ code: "custom", path: ["camera_position_direction"], message: "Camera direction must be non-zero" });
  }
  if (!upUnit) {
    context.addIssue({ code: "custom", path: ["image_up_direction"], message: "Image-up direction must be non-zero" });
  }
  if (cameraUnit && upUnit) {
    const dot = cameraUnit.reduce((sum, value, index) => sum + value * upUnit[index], 0);
    if (Math.abs(dot) >= 0.999) {
      context.addIssue({ code: "custom", path: ["image_up_direction"], message: "Pose directions must not be parallel" });
    }
  }
});

const referenceImageSchema = z.object({
  public_id: z.string().uuid(),
  object_key: z.string().min(1),
  thumbnail_url: z.string().min(1),
  sort_order: z.number().int().positive(),
  caption: localizedTextSchema,
  created_at: z.string().datetime({ offset: true }).optional(),
}).strict();

const isExactVector = (value: readonly number[], expected: readonly number[]) =>
  value.every((component, index) => component === expected[index]);

export const sopViewSchema = z.object({
  public_id: z.string().uuid(),
  sequence: z.number().int().positive(),
  role: z.enum(["reference_front", "capture"]),
  view_kind: z.enum(["standard", "detail"]),
  preset_key: z.enum(sopPresetKeys).optional(),
  name: requiredLocalizedTextSchema,
  instruction: localizedTextSchema,
  required: z.boolean(),
  pose: poseSchema,
  composition: compositionSchema,
  reference_images: z.array(referenceImageSchema),
}).strict().superRefine((view, context) => {
  if (view.view_kind === "standard" && !isExactVector(view.pose.target, [0, 0, 0])) {
    context.addIssue({ code: "custom", path: ["pose", "target"], message: "Standard views must target the origin" });
  }
  if (view.view_kind === "detail" && view.pose.target.some((component) => component < -0.5 || component > 0.5)) {
    context.addIssue({ code: "custom", path: ["pose", "target"], message: "Detail target must be inside the normalized box" });
  }
  if (view.role === "reference_front") {
    const invariantChecks: Array<[boolean, PropertyKey[], string]> = [
      [view.sequence === 1, ["sequence"], "Reference front must be first"],
      [view.view_kind === "standard", ["view_kind"], "Reference front must be standard"],
      [view.preset_key === "reference_front", ["preset_key"], "Reference front preset is fixed"],
      [view.required, ["required"], "Reference front must be required"],
      [isExactVector(view.pose.camera_position_direction, [0, 0, 1]), ["pose", "camera_position_direction"], "Reference camera direction is fixed"],
      [isExactVector(view.pose.image_up_direction, [1, 0, 0]), ["pose", "image_up_direction"], "Reference image-up direction is fixed"],
      [isExactVector(view.pose.target, [0, 0, 0]), ["pose", "target"], "Reference target is fixed"],
    ];
    for (const [valid, path, message] of invariantChecks) {
      if (!valid) context.addIssue({ code: "custom", path, message });
    }
  }

  view.reference_images.forEach((image, index) => {
    if (image.sort_order !== index + 1) {
      context.addIssue({ code: "custom", path: ["reference_images", index, "sort_order"], message: "Reference images must have contiguous sort order" });
    }
  });
});

const coordinateSystemSchema = z.object({
  id: z.literal("pcs_object_v1"),
  handedness: z.literal("right_handed"),
  origin: z.literal("bounding_box_center"),
  unit: z.literal("normalized"),
  axes: z.object({
    x_positive: z.literal("object_top"),
    y_positive: z.literal("object_left"),
    z_positive: z.literal("object_front"),
  }).strict(),
}).strict();

export const sopVersionSchema = z.object({
  schema_version: z.literal("1.0"),
  public_id: z.string().uuid(),
  sop_public_id: z.string().uuid(),
  version_number: z.number().int().positive(),
  status: z.enum(["draft", "published", "archived"]),
  name: requiredLocalizedTextSchema,
  description: localizedTextSchema,
  coordinate_system: coordinateSystemSchema,
  published_at: z.string().datetime({ offset: true }).nullable().optional(),
  created_at: z.string().datetime({ offset: true }).optional(),
  updated_at: z.string().datetime({ offset: true }),
  views: z.array(sopViewSchema).min(1),
}).strict().superRefine((version, context) => {
  const referenceCount = version.views.filter((view) => view.role === "reference_front").length;
  if (referenceCount !== 1) {
    context.addIssue({ code: "custom", path: ["views"], message: "A version must have exactly one reference front" });
  }
  version.views.forEach((view, index) => {
    if (view.sequence !== index + 1) {
      context.addIssue({ code: "custom", path: ["views", index, "sequence"], message: "Views must be sorted with contiguous sequence values" });
    }
  });
});

export type LoginInput = z.infer<typeof loginSchema>;
export type SkuInput = z.infer<typeof skuSchema>;
export type InventoryAdjustmentInput = z.infer<typeof inventoryAdjustmentSchema>;
export type SOPViewInput = z.infer<typeof sopViewSchema>;
export type SOPVersionInput = z.infer<typeof sopVersionSchema>;
