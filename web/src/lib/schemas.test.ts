import { describe, expect, it } from "vitest";

import { compositionSchema, sopVersionSchema, sopViewSchema } from "./schemas";
import { localizedText, sopPresetKeys } from "./sop";

const id = "f81fb253-c995-4c99-a453-240d69a5f451";

const baseView = {
  public_id: id,
  sequence: 2,
  role: "capture",
  view_kind: "standard",
  preset_key: "packaging_front",
  name: { "zh-CN": "包装正面", en: "Packaging Front" },
  instruction: { "zh-CN": "完整拍摄包装正面", en: "Capture the complete package front" },
  required: false,
  pose: {
    space: "object",
    camera_position_direction: [0, 0, 1],
    image_up_direction: [1, 0, 0],
    target: [0, 0, 0],
  },
  composition: {
    frame_occupancy: 0.85,
    aspect_ratio: "1:1",
    allow_rotation_correction: true,
    allow_mirror: false,
  },
  reference_images: [],
};

const referenceFront = {
  ...baseView,
  sequence: 1,
  role: "reference_front",
  preset_key: "reference_front",
  required: true,
};

const baseVersion = {
  schema_version: "1.0",
  public_id: id,
  sop_public_id: "4acdab9e-f8b8-4783-8cbf-94e10de4f838",
  version_number: 1,
  status: "draft",
  updated_at: "2026-07-16T10:00:00.000Z",
  name: { "zh-CN": "手机壳", en: "Phone Case" },
  description: { "zh-CN": "", en: "" },
  coordinate_system: {
    id: "pcs_object_v1",
    handedness: "right_handed",
    origin: "bounding_box_center",
    unit: "normalized",
    axes: {
      x_positive: "object_top",
      y_positive: "object_left",
      z_positive: "object_front",
    },
  },
  views: [referenceFront, baseView],
};

describe("compositionSchema", () => {
  it("accepts only the V1 composition shape", () => {
    expect(compositionSchema.parse(baseView.composition)).toEqual(baseView.composition);
    expect(() => compositionSchema.parse({ ...baseView.composition, background: "white" })).toThrow();
  });

  it("rejects invalid occupancy, ratio, and mirror values", () => {
    expect(() => compositionSchema.parse({ ...baseView.composition, frame_occupancy: 0 })).toThrow();
    expect(() => compositionSchema.parse({ ...baseView.composition, frame_occupancy: 1.01 })).toThrow();
    expect(() => compositionSchema.parse({ ...baseView.composition, aspect_ratio: "01:1" })).toThrow();
    expect(() => compositionSchema.parse({ ...baseView.composition, allow_mirror: true })).toThrow();
  });
});

describe("sopViewSchema", () => {
  it("accepts optional packaging front", () => {
    expect(sopViewSchema.parse(baseView).required).toBe(false);
  });

  it("rejects vectors with wrong cardinality, non-finite values, zero length, or parallel orientation", () => {
    for (const camera of [[0, 1], [0, 0, Number.POSITIVE_INFINITY], [0, 0, 0]]) {
      expect(() => sopViewSchema.parse({ ...baseView, pose: { ...baseView.pose, camera_position_direction: camera } })).toThrow();
    }
    expect(() => sopViewSchema.parse({
      ...baseView,
      pose: { ...baseView.pose, image_up_direction: [0, 0, -2] },
    })).toThrow();
    expect(() => sopViewSchema.parse({
      ...baseView,
      pose: {
        ...baseView.pose,
        camera_position_direction: [Number.MAX_VALUE, Number.MAX_VALUE, 0],
        image_up_direction: [Number.MAX_VALUE, Number.MAX_VALUE, 0],
      },
    })).toThrow();
    expect(() => sopViewSchema.parse({
      ...baseView,
      pose: {
        ...baseView.pose,
        camera_position_direction: [Number.MAX_VALUE, Number.MAX_VALUE, 0],
        image_up_direction: [-Number.MAX_VALUE, -Number.MAX_VALUE, 0],
      },
    })).toThrow();
    expect(sopViewSchema.parse({
      ...baseView,
      pose: {
        ...baseView.pose,
        camera_position_direction: [Number.MAX_VALUE, Number.MAX_VALUE, 0],
        image_up_direction: [Number.MAX_VALUE, -Number.MAX_VALUE, 0],
      },
    }).pose.image_up_direction).toEqual([Number.MAX_VALUE, -Number.MAX_VALUE, 0]);
  });

  it("enforces standard and detail target rules", () => {
    expect(() => sopViewSchema.parse({ ...baseView, pose: { ...baseView.pose, target: [0.1, 0, 0] } })).toThrow();
    expect(sopViewSchema.parse({
      ...baseView,
      view_kind: "detail",
      preset_key: "detail_label",
      pose: { ...baseView.pose, target: [0.5, -0.5, 0.25] },
    }).view_kind).toBe("detail");
    expect(() => sopViewSchema.parse({
      ...baseView,
      view_kind: "detail",
      preset_key: "detail_label",
      pose: { ...baseView.pose, target: [0.500001, 0, 0] },
    })).toThrow();
  });

  it("enforces every fixed reference-front invariant", () => {
    expect(sopViewSchema.parse(referenceFront).role).toBe("reference_front");
    for (const mutation of [
      { sequence: 2 },
      { view_kind: "detail" },
      { preset_key: "packaging_front" },
      { required: false },
      { pose: { ...referenceFront.pose, camera_position_direction: [0, 0, -1] } },
      { pose: { ...referenceFront.pose, image_up_direction: [-1, 0, 0] } },
    ]) {
      expect(() => sopViewSchema.parse({ ...referenceFront, ...mutation })).toThrow();
    }
  });

  it("rejects unknown presets and incomplete localized names", () => {
    expect(() => sopViewSchema.parse({ ...baseView, preset_key: "frontish" })).toThrow();
    expect(() => sopViewSchema.parse({ ...baseView, name: { "zh-CN": "", en: "Packaging" } })).toThrow();
  });
});

describe("sopVersionSchema", () => {
  it("accepts a valid, ordered pcs_object_v1 aggregate", () => {
    expect(sopVersionSchema.parse(baseVersion).views).toHaveLength(2);
  });

  it("rejects invalid schema or coordinate metadata", () => {
    expect(() => sopVersionSchema.parse({ ...baseVersion, schema_version: "2.0" })).toThrow();
    expect(() => sopVersionSchema.parse({
      ...baseVersion,
      coordinate_system: { ...baseVersion.coordinate_system, id: "camera_space" },
    })).toThrow();
  });

  it("requires exactly one reference front and contiguous sorted sequences", () => {
    expect(() => sopVersionSchema.parse({ ...baseVersion, views: [baseView] })).toThrow();
    expect(() => sopVersionSchema.parse({ ...baseVersion, views: [referenceFront, referenceFront] })).toThrow();
    expect(() => sopVersionSchema.parse({ ...baseVersion, views: [baseView, referenceFront] })).toThrow();
    expect(() => sopVersionSchema.parse({ ...baseVersion, views: [referenceFront, { ...baseView, sequence: 3 }] })).toThrow();
  });
});

describe("SOP contract helpers", () => {
  it("exposes the complete preset catalog and resolves both UI language forms", () => {
    expect(sopPresetKeys).toEqual([
      "reference_front", "back", "left", "bottom", "right", "top", "detail_label", "packaging_front",
    ]);
    expect(localizedText("zh", baseView.name)).toBe("包装正面");
    expect(localizedText("zh-CN", baseView.name)).toBe("包装正面");
    expect(localizedText("en", baseView.name)).toBe("Packaging Front");
  });

  it("falls back to the other locale and then to empty text", () => {
    expect(localizedText("zh", { "zh-CN": "", en: "English fallback" })).toBe("English fallback");
    expect(localizedText("en", { "zh-CN": "中文回退", en: "" })).toBe("中文回退");
    expect(localizedText("zh-CN", { "zh-CN": "", en: "" })).toBe("");
  });
});
