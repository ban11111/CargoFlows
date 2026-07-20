# Capture SOP Structured Data Design

**Date:** 2026-07-16

**Status:** Approved design, pending written-spec review

**Reference:** [ChatGPT shared discussion](https://chatgpt.com/share/6a58a12f-ccf4-83ec-8ac6-d09d4b465a86)

## 1. Purpose

CargoFlows needs a versioned, structured definition for product-photography SOPs. An SOP describes only the product images that a photographer must or may capture. It is not a general workflow engine and does not model cleaning, lighting setup, approval, quality review, or AI generation steps.

The structured data must:

- give every SOP the same object-coordinate convention;
- support standard six-sided views and arbitrary oblique views;
- preserve the roll/orientation of the final image, not only the camera side;
- support optional detail views with a local target point;
- provide Simplified Chinese and English user-facing content;
- freeze published definitions so historical photo sessions remain reproducible;
- integrate cleanly with Go, GORM, MySQL, Web, iOS, and object storage.

This is a greenfield model. No migration, legacy-table compatibility, or preservation of existing seed data is required.

## 2. Scope

### Included

- Logical SOP identity and category assignment.
- Immutable, numbered SOP versions.
- Draft editing, validation, publishing, copying, and archiving.
- A mandatory reference-front view.
- Standard and detail capture views.
- Arbitrary three-dimensional camera directions.
- Reference images and bilingual capture instructions.
- Photo-session binding to an exact published version and view.

### Excluded

- Generic workflow steps.
- Review and approval workflow design.
- Physical camera distance, lens, and exposure control.
- Automatic pose estimation from photos.
- Database migration from the current scaffold models.

## 3. Design Approach

Use a hybrid relational model:

- Store identity, lifecycle, ordering, roles, vector components, target components, and required-state in ordinary relational columns.
- Store the bounded, lower-frequency composition object in a JSON column.
- Store reference-image metadata in a related table and image bytes in object storage.
- Expose a nested, self-describing JSON document through APIs and import/export.

This keeps invariants and queries strong without splitting every optional composition property into a separate table.

## 4. Domain Model

```text
CaptureSOP
└── SOPVersion
    ├── SOPView
    │   └── SOPViewReferenceImage
    └── coordinate convention: pcs_object_v1
```

### 4.1 CaptureSOP

`CaptureSOP` is the stable identity shared by all revisions.

| Field | Type | Rule |
|---|---|---|
| `id` | database key | Internal identity. |
| `public_id` | UUID | Stable external identity; unique and immutable. |
| `category_id` | foreign key | Required product category. |
| `created_by_id` | foreign key | Required creator. |
| `created_at` | timestamp | Required. |
| `updated_at` | timestamp | Required. |

The logical SOP does not contain editable names or View definitions. Those belong to a version so a historical version is self-contained.

### 4.2 SOPVersion

`SOPVersion` is the object selected by a photo session.

| Field | Type | Rule |
|---|---|---|
| `id` | database key | Internal identity. |
| `public_id` | UUID | Unique external version identity. |
| `sop_id` | foreign key | Required parent SOP. |
| `version_number` | positive integer | Starts at 1; unique within an SOP. |
| `schema_version` | string | `1.0` for this specification. |
| `name_zh` | string | Required. |
| `name_en` | string | Required. |
| `description_zh` | text | Required; may be an empty string. |
| `description_en` | text | Required; may be an empty string. |
| `status` | enum | `draft`, `published`, or `archived`. |
| `coordinate_system` | enum/string | Fixed to `pcs_object_v1`. |
| `copied_from_version_id` | nullable foreign key | Source version used to create this version. |
| `published_at` | nullable timestamp | Set exactly once when published. |
| `created_at` | timestamp | Required. |
| `updated_at` | timestamp | Required. |

Rules:

- A logical SOP may have many versions.
- Version numbers increase monotonically from 1.
- At most one draft may exist for a logical SOP. MySQL cannot express this as a portable partial unique index, so creation is enforced transactionally in the domain service.
- Only a draft is editable.
- Publishing is atomic: validation runs again inside the publish transaction before status changes.
- A published version is immutable.
- Archiving hides a version from new-session selection but does not remove it.
- A new version copies all Views, composition data, and reference-image relationships from a selected source version.

### 4.3 SOPView

The View separates coordinate role from capture kind.

| Field | Type | Rule |
|---|---|---|
| `id` | database key | Internal identity. |
| `public_id` | UUID | Unique external identity for this version's View row. |
| `sop_version_id` | foreign key | Required parent version. |
| `sequence` | positive integer | Unique and contiguous within a version. |
| `role` | enum | `reference_front` or `capture`. |
| `view_kind` | enum | `standard` or `detail`. |
| `preset_key` | nullable string | Creation shortcut only; never used for pose computation. |
| `name_zh` | string | Required. |
| `name_en` | string | Required. |
| `instruction_zh` | text | Required; may be an empty string. |
| `instruction_en` | text | Required; may be an empty string. |
| `required` | boolean | Whether the View must be captured to finish a session. |
| `camera_position_x/y/z` | decimal | Canonical unit direction components. |
| `image_up_x/y/z` | decimal | Canonical unit direction components, orthogonal to camera position direction. |
| `target_x/y/z` | decimal | Normalized target point components. |
| `composition_json` | JSON | Validated `Composition` object. |
| `created_at` | timestamp | Required. |
| `updated_at` | timestamp | Required. |

`public_id` identifies a View only inside its concrete version. Copied versions receive new View IDs. Cross-version lineage is available at the version level through `copied_from_version_id`; View-level lineage is not required in V1.

### 4.4 SOPViewReferenceImage

| Field | Type | Rule |
|---|---|---|
| `id` | database key | Internal identity. |
| `public_id` | UUID | Unique external identity. |
| `sop_view_id` | foreign key | Required View. |
| `object_key` | string | Required immutable object-storage key. |
| `thumbnail_url` | string | Required display URL or resolved media path. |
| `sort_order` | positive integer | Unique and contiguous within a View. |
| `caption_zh` | string | Required; may be empty. |
| `caption_en` | string | Required; may be empty. |
| `created_at` | timestamp | Required. |

A View may have zero or more reference images. When a new SOP version is copied, new relationship rows reuse the immutable media object keys.

## 5. Object Coordinate Convention

Every version uses `pcs_object_v1`:

```text
handedness = right_handed
origin     = bounding_box_center
unit       = normalized

+X = object_top
-X = object_bottom
+Y = object_left
-Y = object_right
+Z = object_front
-Z = object_back
```

For normalized target coordinates, the object bounding box spans `[-0.5, 0.5]` on each axis.

### 5.1 Pose Semantics

`camera_position_direction` means the direction from the object origin toward the camera. It does not represent the direction in which the camera looks and does not contain physical distance.

The camera forward direction is derived as:

```text
camera_forward = -camera_position_direction
```

`image_up_direction` means the object-space direction that points toward the top of the final image. Together, these two vectors determine the viewing direction and image roll.

The canonical image-right direction is:

```text
image_right = normalize(camera_forward × image_up_direction)
```

This cross-product order is part of `pcs_object_v1`. Renderers and computer-vision integrations must adapt their local camera-axis conventions to it.

### 5.2 Direction Vector Canonicalization

Vector magnitude has no business meaning. `[0,0,1]`, `[0,0,10]`, and `[0,0,0.5]` all describe the same direction.

For submitted camera vector `P` and image-up vector `U`:

```text
P_unit = normalize(P)
U_unit = normalize(U - dot(U, P_unit) × P_unit)
```

Persist components rounded to six decimal places. Reject input when:

- either vector contains a non-finite number;
- either vector is the zero vector within the implementation epsilon;
- `abs(dot(normalize(P), normalize(U))) >= 0.999`.

The length check exists only to reject a directionless zero vector and to produce a unit vector. It does not model camera distance.

## 6. Mandatory Reference-Front View

Creating a new SOP also creates V1 draft and exactly one View with these values:

```json
{
  "sequence": 1,
  "role": "reference_front",
  "view_kind": "standard",
  "preset_key": "reference_front",
  "name": {
    "zh-CN": "正面",
    "en": "Front"
  },
  "instruction": {
    "zh-CN": "将商品正面对准相机，并确保商品顶部朝向画面顶部。",
    "en": "Face the product toward the camera and keep its top aligned with the top of the image."
  },
  "required": true,
  "pose": {
    "space": "object",
    "camera_position_direction": [0, 0, 1],
    "image_up_direction": [1, 0, 0],
    "target": [0, 0, 0]
  },
  "composition": {
    "frame_occupancy": 0.85,
    "aspect_ratio": "1:1",
    "allow_rotation_correction": true,
    "allow_mirror": false
  }
}
```

Every version must have exactly one `reference_front`. It must remain sequence 1, standard, required, and use the fixed pose above. It cannot be deleted, reordered, or changed to another role or pose. While a version is a draft, users may edit its bilingual display name, instruction, composition, and reference images.

The lock is a domain invariant. Do not persist a configurable `locked_fields` list.

## 7. View Rules

- Sequence values are unique, contiguous, and start at 1.
- `space` is always `object` in schema version 1.0.
- A `standard` View always targets `[0,0,0]`.
- A `detail` View may target any point whose three components are each within `[-0.5,0.5]`.
- `frame_occupancy` is greater than 0 and at most 1.
- `allow_mirror` is always `false`; mirrored product images are not permitted.
- `name_zh` and `name_en` are non-empty.
- Instruction and caption fields exist in both languages but may be empty strings.
- Repeating a camera pose is allowed because two Views may have different compositions, targets, or reference examples.
- A published version must contain the reference-front View; all additional Views may be optional.

## 8. Preset Catalog

Presets are creation shortcuts. After adding a preset to a draft, the resulting View is ordinary structured data and may be customized within the invariants.

| Preset | Camera position | Image up | Kind | Default required |
|---|---|---|---|---|
| `reference_front` | `[0,0,1]` | `[1,0,0]` | standard | true, locked |
| `back` | `[0,0,-1]` | `[1,0,0]` | standard | true |
| `left` | `[0,1,0]` | `[1,0,0]` | standard | true |
| `bottom` | `[-1,0,0]` | `[0,1,0]` | standard | true |
| `right` | `[0,-1,0]` | `[-1,0,0]` | standard | true |
| `top` | `[1,0,0]` | `[0,-1,0]` | standard | true |
| `detail_label` | `[0,0,1]` | `[1,0,0]` | detail | false |
| `packaging_front` | `[0,0,1]` | `[1,0,0]` | standard | false |

The `left`, `bottom`, `right`, and `top` mappings ensure the final image's left side corresponds to object back (`-Z`) and its right side corresponds to object front (`+Z`).

`detail_label` is an optional starting point. The user must be able to customize its normalized target, composition, bilingual text, and reference images.

`packaging_front` is an optional standard View for photographing the front of the product packaging. Its default bilingual name is `包装正面` / `Packaging Front`, and its default instruction asks the photographer to center the complete package front with all branding and labels legible. It uses the origin target `[0,0,0]` and may coexist with `reference_front` because identical poses are valid when the subject semantics and composition differ.

## 9. Composition Object

V1 accepts exactly these properties:

```json
{
  "frame_occupancy": 0.85,
  "aspect_ratio": "1:1",
  "allow_rotation_correction": true,
  "allow_mirror": false
}
```

Rules:

- `frame_occupancy`: decimal in `(0,1]`.
- `aspect_ratio`: a normalized positive `width:height` string; V1 UI may offer a controlled list such as `1:1`, `4:5`, `3:4`, and `16:9`.
- `allow_rotation_correction`: boolean.
- `allow_mirror`: must be `false`.
- Unknown properties are rejected in schema version 1.0.

Background, lighting, and styling prose belongs in the bilingual View instruction for V1 rather than in unvalidated JSON fields.

## 10. API Document

Database rows serialize to a self-describing SOP Version document:

```json
{
  "schema_version": "1.0",
  "public_id": "f81fb253-c995-4c99-a453-240d69a5f451",
  "sop_public_id": "4acdab9e-f8b8-4783-8cbf-94e10de4f838",
  "version_number": 1,
  "status": "published",
  "name": {
    "zh-CN": "手机壳六视图",
    "en": "Phone Case Six-View"
  },
  "description": {
    "zh-CN": "手机壳标准电商拍摄规范。",
    "en": "Standard e-commerce capture SOP for phone cases."
  },
  "coordinate_system": {
    "id": "pcs_object_v1",
    "handedness": "right_handed",
    "origin": "bounding_box_center",
    "unit": "normalized",
    "axes": {
      "x_positive": "object_top",
      "y_positive": "object_left",
      "z_positive": "object_front"
    }
  },
  "views": [
    {
      "public_id": "bd616073-10bb-4126-bfcd-531198619231",
      "sequence": 1,
      "role": "reference_front",
      "view_kind": "standard",
      "preset_key": "reference_front",
      "name": {
        "zh-CN": "正面",
        "en": "Front"
      },
      "instruction": {
        "zh-CN": "商品正面对准相机，顶部朝向画面顶部。",
        "en": "Face the product toward the camera with its top aligned upward."
      },
      "required": true,
      "pose": {
        "space": "object",
        "camera_position_direction": [0, 0, 1],
        "image_up_direction": [1, 0, 0],
        "target": [0, 0, 0]
      },
      "composition": {
        "frame_occupancy": 0.85,
        "aspect_ratio": "1:1",
        "allow_rotation_correction": true,
        "allow_mirror": false
      },
      "reference_images": [
        {
          "public_id": "10929a1b-7de0-422c-945b-e83c89565379",
          "object_key": "sop-references/front/example-01.jpg",
          "thumbnail_url": "/media/sop-references/front/example-01-thumb.jpg",
          "sort_order": 1,
          "caption": {
            "zh-CN": "正确构图示例",
            "en": "Correct framing example"
          }
        }
      ]
    }
  ]
}
```

The database stores `coordinate_system = pcs_object_v1`; serializers expand the complete convention in response and export documents.

## 11. Lifecycle and APIs

### 11.1 Endpoints

```text
POST   /capture-sops
GET    /capture-sops
GET    /capture-sops/{sop_id}

GET    /sop-versions/{version_id}
PATCH  /sop-versions/{version_id}
POST   /sop-versions/{version_id}/views
PATCH  /sop-versions/{version_id}/views/{view_id}
DELETE /sop-versions/{version_id}/views/{view_id}
PUT    /sop-versions/{version_id}/view-order

POST   /sop-versions/{version_id}/validate
POST   /sop-versions/{version_id}/publish
POST   /capture-sops/{sop_id}/versions
POST   /sop-versions/{version_id}/archive
```

### 11.2 Flow

1. `POST /capture-sops` creates the logical SOP, V1 draft, and fixed reference-front View in one transaction.
2. The editor adds preset or fully custom Views.
3. All mutation endpoints reject non-draft versions.
4. Reordering uses one batch request containing the complete ordered View-ID list.
5. `validate` returns all detected errors so clients can mark every invalid field in one response.
6. `publish` locks the version only after transactional validation succeeds.
7. `POST /capture-sops/{sop_id}/versions` copies one selected version into the next numbered draft.
8. New photo sessions list and accept only published, non-archived versions.
9. Archiving prevents new use but does not affect historical sessions.

`PhotoSession` references `sop_version_id`, and every captured `Asset` references the concrete `sop_view_id`. A session never resolves Views indirectly through the latest logical SOP version.

## 12. Validation Errors

Validation uses stable machine codes, JSON-style field paths, and bilingual user messages.

```json
{
  "code": "sop_validation_failed",
  "errors": [
    {
      "code": "pose_vectors_parallel",
      "path": "views[2].pose.image_up_direction",
      "message": {
        "zh-CN": "相机方向与图片向上方向不能平行。",
        "en": "Camera direction and image-up direction cannot be parallel."
      }
    }
  ]
}
```

Required error categories include:

- invalid or missing bilingual fields;
- missing, duplicate, or mutated reference-front View;
- duplicate or non-contiguous sequence;
- non-finite, zero, or parallel pose vectors;
- non-origin standard target;
- out-of-bounds detail target;
- invalid composition value or unknown composition field;
- mutation of a published or archived version;
- use of a draft or archived version for a new photo session;
- concurrent draft or version-number conflict.

## 13. Concurrency and Transaction Boundaries

- Creating an SOP and its initial version/reference View is one transaction.
- Allocating a new `version_number` and creating the single draft is one transaction with a parent-SOP lock or equivalent conflict-safe retry.
- Publishing re-reads and validates the version and all Views inside one transaction.
- Reordering updates all View sequences in one transaction and rejects incomplete or duplicate ID lists.
- Optimistic concurrency should use an `updated_at` precondition or explicit revision counter on draft mutations so two editors cannot silently overwrite each other.

## 14. Testing Strategy

### Unit tests

- Normalize arbitrary non-zero direction vectors.
- Orthogonalize image-up input.
- Reject zero, non-finite, and nearly parallel vectors.
- Preserve the approved six-view preset mappings.
- Create `packaging_front` as an optional standard front View with packaging-specific bilingual text.
- Enforce reference-front invariants.
- Validate standard and detail targets.
- Validate composition ranges and reject unknown properties.

### Domain and repository tests

- Create one logical SOP, V1 draft, and one fixed reference View atomically.
- Enforce unique monotonically increasing version numbers.
- Enforce at most one draft under concurrent requests.
- Allow draft edits and reject mutations after publication.
- Copy all Views, composition data, and reference-image relationships into a new draft.
- Reorder to a unique contiguous sequence.

### API contract tests

- Create, retrieve, edit, validate, publish, copy, and archive an SOP version.
- Return all validation errors with stable codes and paths.
- Round-trip bilingual fields and normalized pose data without field loss.
- Expand `pcs_object_v1` in serialized documents.
- Reject a draft or archived version when creating a photo session.
- Bind uploaded assets to the exact View row from the selected version.

### Client tests

- Web editor starts with the locked reference-front View.
- Preset insertion creates the approved vectors and default required-state.
- Packaging-front insertion remains distinct from the locked product reference-front View.
- Arbitrary-angle input shows the canonical saved vectors.
- Detail editor limits target points to the normalized bounding box.
- Language switching updates every SOP, View, validation, and preset label immediately.
- iOS capture displays the selected published version in sequence order and finishes only after every required View has an Asset.

## 15. Acceptance Criteria

- Every new SOP begins with the same fixed front/up coordinate reference.
- A custom View can represent any valid three-dimensional camera direction and image roll.
- Four side-face presets render object back on image-left and object front on image-right.
- Detail Views are optional by default and support an in-bounds normalized target.
- The optional packaging-front preset uses a front pose without replacing or weakening the mandatory product reference-front View.
- Published versions cannot change and historical photo sessions retain exact version/View references.
- API documents are bilingual, self-describing, and losslessly round-trip through the relational model.
- No migration or legacy-data preservation work is included.
