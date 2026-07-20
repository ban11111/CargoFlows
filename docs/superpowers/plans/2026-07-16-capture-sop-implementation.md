# Capture SOP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the scaffold SOP template model with a versioned, bilingual product-capture SOP whose Views have validated object-space poses, presets, optional details, immutable publication, and exact photo-session references.

**Architecture:** Build pose math and SOP validation as a pure Go package, persist the validated aggregate through a transaction-oriented SOP service, and expose UUID-based DTOs through Gin. The Next.js admin edits draft versions through that API, while the iOS client consumes only published versions and uploads Assets against exact UUID View/session references.

**Tech Stack:** Go 1.25, Gin 1.11, GORM 1.30, MySQL, SQLite test driver, Next.js 16, React 19, TypeScript 5.9, Zod 4, Vitest, Swift 6, SwiftUI, iOS 17, XCTest.

## Global Constraints

- This is greenfield SOP storage: do not implement old-table migration, data preservation, or compatibility endpoints.
- Every version uses `schema_version = "1.0"` and `coordinate_system = "pcs_object_v1"`.
- `pcs_object_v1` is right-handed: `+X = object_top`, `+Y = object_left`, `+Z = object_front`, origin at bounding-box center, normalized target range `[-0.5,0.5]`.
- Every new SOP gets exactly one locked sequence-1 reference View with camera `[0,0,1]`, image-up `[1,0,0]`, target `[0,0,0]`, and `required = true`.
- Published versions are immutable; new changes require copying into the next numbered draft.
- User-facing Web and iOS text must exist in Simplified Chinese and English and switch immediately with the selected language.
- Public SOP, version, View, reference-image, and photo-session identifiers are UUID strings; numeric database keys stay internal.
- `packaging_front` is an optional standard preset with camera `[0,0,1]`, image-up `[1,0,0]`, target `[0,0,0]`, and default `required = false`.
- Preserve the approved side-face image orientation: left/bottom/right/top image-left is object back `-Z`, image-right is object front `+Z`.
- Follow TDD: every behavior change starts with a failing focused test, then minimal implementation, then the full relevant suite.

---

## File Structure

### Backend

- `api/internal/sop/pose.go`: vector types, normalization, orthogonalization, and canonical pose math.
- `api/internal/sop/presets.go`: immutable preset catalog and bilingual defaults.
- `api/internal/sop/validation.go`: aggregate validation and stable bilingual error codes.
- `api/internal/sop/*_test.go`: pure domain tests.
- `api/internal/models/sop.go`: CaptureSOP, SOPVersion, SOPView, reference-image, enums, and composition persistence types.
- `api/internal/models/models.go`: remove legacy SOPTemplate/SOPView and update PhotoSession/Asset references.
- `api/internal/app/sop_service.go`: transactional lifecycle and draft mutation boundary.
- `api/internal/app/sop_dto.go`: UUID API requests/responses and coordinate-system expansion.
- `api/internal/app/sop_handlers.go`: Gin endpoints only; no domain math.
- `api/internal/app/sop_test.go`: SQLite-backed service and handler integration tests.
- `api/internal/app/handlers.go`: remove old SOP handlers; update session and upload contracts.
- `api/internal/app/router.go`: register the new endpoints.
- `api/internal/database/database.go`: migrate/seed only the new SOP aggregate.
- `api/openapi.yaml`: authoritative API paths and structured SOP schemas.

### Web

- `web/src/lib/sop.ts`: generated-contract aliases, presets, localization helpers, and request helpers.
- `web/src/lib/schemas.ts`: Zod schemas for vectors, composition, View, and draft metadata.
- `web/src/lib/schemas.test.ts`: schema and preset validation tests.
- `web/src/components/sop/sop-view-editor.tsx`: one focused View editor.
- `web/src/components/sop/sop-version-editor.tsx`: aggregate ordering, validation, saving, and publication.
- `web/src/components/sop/sop-version-editor.test.tsx`: editor behavior and bilingual tests.
- `web/src/app/(dashboard)/sop-templates/page.tsx`: logical SOP/version list.
- `web/src/app/(dashboard)/sop-templates/new/page.tsx`: create V1 draft.
- `web/src/app/(dashboard)/sop-templates/[sopId]/versions/[versionId]/page.tsx`: edit or inspect one concrete version.
- `web/src/lib/i18n.tsx`: all new bilingual labels and validation copy.
- `web/src/lib/openapi-types.ts`: generated from `api/openapi.yaml`.

### iOS

- `ios/CargoFlows/Models/DTOs.swift`: UUID SOP/version/View/session DTOs and localized helpers.
- `ios/CargoFlows/Networking/APIClient.swift`: published-version and UUID upload requests.
- `ios/CargoFlows/Views/SOPCaptureView.swift`: version selection and exact View checklist.
- `ios/CargoFlows/App/LanguageStore.swift`: bilingual SOP-selection and validation labels.
- `ios/CargoFlowsTests/SOPDTOTests.swift`: decoding and localization tests.
- `ios/project.yml`: add the XCTest target.

---

### Task 1: Canonical Pose Math

**Files:**
- Create: `api/internal/sop/pose.go`
- Create: `api/internal/sop/pose_test.go`

**Interfaces:**
- Produces: `type Vector3 [3]float64`
- Produces: `type CanonicalPose struct { CameraPosition Vector3; ImageUp Vector3 }`
- Produces: `func CanonicalizePose(cameraPosition, imageUp Vector3) (CanonicalPose, error)`
- Produces: sentinel errors `ErrZeroVector`, `ErrNonFiniteVector`, and `ErrParallelVectors`

- [ ] **Step 1: Write failing pose tests**

```go
package sop

import (
    "errors"
    "math"
    "testing"
)

func TestCanonicalizePoseNormalizesAndOrthogonalizes(t *testing.T) {
    got, err := CanonicalizePose(Vector3{0, -1, 1}, Vector3{1, 0.02, 0})
    if err != nil { t.Fatal(err) }
    wantCamera := Vector3{0, -0.707107, 0.707107}
    for i := range wantCamera {
        if math.Abs(got.CameraPosition[i]-wantCamera[i]) > 0.000001 {
            t.Fatalf("camera[%d] = %f, want %f", i, got.CameraPosition[i], wantCamera[i])
        }
    }
    if math.Abs(dot(got.CameraPosition, got.ImageUp)) > 0.000001 {
        t.Fatalf("canonical vectors are not orthogonal: %#v", got)
    }
}

func TestCanonicalizePoseRejectsInvalidVectors(t *testing.T) {
    cases := []struct {
        name string
        camera, up Vector3
        want error
    }{
        {"zero", Vector3{}, Vector3{1, 0, 0}, ErrZeroVector},
        {"non-finite", Vector3{0, 0, math.Inf(1)}, Vector3{1, 0, 0}, ErrNonFiniteVector},
        {"parallel", Vector3{0, 0, 1}, Vector3{0, 0, 2}, ErrParallelVectors},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            _, err := CanonicalizePose(tc.camera, tc.up)
            if !errors.Is(err, tc.want) { t.Fatalf("error = %v, want %v", err, tc.want) }
        })
    }
}
```

- [ ] **Step 2: Run the focused test and confirm the missing-symbol failure**

Run: `cd api && go test ./internal/sop -run TestCanonicalizePose -v`

Expected: FAIL because `Vector3` and `CanonicalizePose` do not exist.

- [ ] **Step 3: Implement canonical pose math**

```go
package sop

import (
    "errors"
    "math"
)

type Vector3 [3]float64

type CanonicalPose struct {
    CameraPosition Vector3
    ImageUp        Vector3
}

var (
    ErrZeroVector     = errors.New("direction vector cannot be zero")
    ErrNonFiniteVector = errors.New("direction vector must contain finite numbers")
    ErrParallelVectors = errors.New("camera and image-up directions cannot be parallel")
)

const parallelThreshold = 0.999

func CanonicalizePose(cameraPosition, imageUp Vector3) (CanonicalPose, error) {
    p, err := normalize(cameraPosition)
    if err != nil { return CanonicalPose{}, err }
    u, err := normalize(imageUp)
    if err != nil { return CanonicalPose{}, err }
    if math.Abs(dot(p, u)) >= parallelThreshold { return CanonicalPose{}, ErrParallelVectors }
    projection := dot(u, p)
    u, err = normalize(Vector3{u[0]-projection*p[0], u[1]-projection*p[1], u[2]-projection*p[2]})
    if err != nil { return CanonicalPose{}, err }
    return CanonicalPose{CameraPosition: round6(p), ImageUp: round6(u)}, nil
}

func normalize(v Vector3) (Vector3, error) {
    for _, n := range v { if math.IsNaN(n) || math.IsInf(n, 0) { return Vector3{}, ErrNonFiniteVector } }
    length := math.Sqrt(dot(v, v))
    if length < 1e-9 { return Vector3{}, ErrZeroVector }
    return Vector3{v[0]/length, v[1]/length, v[2]/length}, nil
}

func dot(a, b Vector3) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
func round6(v Vector3) Vector3 {
    return Vector3{math.Round(v[0]*1e6)/1e6, math.Round(v[1]*1e6)/1e6, math.Round(v[2]*1e6)/1e6}
}
```

- [ ] **Step 4: Run the domain tests**

Run: `cd api && go test ./internal/sop -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add api/internal/sop/pose.go api/internal/sop/pose_test.go
git commit -m "feat(api): add canonical SOP pose math"
```

### Task 2: Persistence Aggregate

**Files:**
- Create: `api/internal/models/sop.go`
- Modify: `api/internal/models/models.go`
- Modify: `api/internal/database/database.go`
- Modify: `api/internal/app/router.go`
- Modify: `api/internal/app/handlers.go`
- Modify: `api/go.mod`
- Modify: `api/go.sum`
- Create: `api/internal/app/test_database_test.go`

**Interfaces:**
- Produces: models `CaptureSOP`, `SOPVersion`, `SOPView`, `SOPViewReferenceImage`, `Composition`
- Produces: enums `SOPVersionStatus`, `SOPViewRole`, `SOPViewKind`
- Changes: `PhotoSession.SOPVersionID` replaces `SOPTemplateID`; `PhotoSession.PublicID` is externally visible
- Consumes later: `newTestDB(t *testing.T) *gorm.DB`

- [ ] **Step 1: Add the SQLite test dependency**

Run: `cd api && go get gorm.io/driver/sqlite@v1.6.0`

Expected: `api/go.mod` lists `gorm.io/driver/sqlite v1.6.0` and `api/go.sum` updates.

- [ ] **Step 2: Write a failing migration-shape test**

```go
package app

import (
    "testing"
    "cargoflows/api/internal/database"
    "cargoflows/api/internal/models"
    "gorm.io/driver/sqlite"
    "gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
    t.Helper()
    db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
    if err != nil { t.Fatal(err) }
    if err := database.Migrate(db); err != nil { t.Fatal(err) }
    return db
}

func TestMigrateCreatesVersionedSOPTables(t *testing.T) {
    db := newTestDB(t)
    for _, model := range []any{&models.CaptureSOP{}, &models.SOPVersion{}, &models.SOPView{}, &models.SOPViewReferenceImage{}} {
        if !db.Migrator().HasTable(model) { t.Fatalf("missing table for %T", model) }
    }
}
```

- [ ] **Step 3: Run and confirm failure because the new models do not exist**

Run: `cd api && go test ./internal/app -run TestMigrateCreatesVersionedSOPTables -v`

Expected: FAIL at compile time for missing model types.

- [ ] **Step 4: Define the aggregate models**

```go
package models

import "time"

type SOPVersionStatus string
const (
    SOPVersionDraft SOPVersionStatus = "draft"
    SOPVersionPublished SOPVersionStatus = "published"
    SOPVersionArchived SOPVersionStatus = "archived"
)

type SOPViewRole string
const (
    SOPViewReferenceFront SOPViewRole = "reference_front"
    SOPViewCapture SOPViewRole = "capture"
)

type SOPViewKind string
const (
    SOPViewStandard SOPViewKind = "standard"
    SOPViewDetail SOPViewKind = "detail"
)

type Composition struct {
    FrameOccupancy          float64 `json:"frame_occupancy"`
    AspectRatio             string  `json:"aspect_ratio"`
    AllowRotationCorrection bool    `json:"allow_rotation_correction"`
    AllowMirror             bool    `json:"allow_mirror"`
}

type CaptureSOP struct {
    ID uint `gorm:"primaryKey" json:"-"`
    PublicID string `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
    CategoryID uint `gorm:"index;not null" json:"category_id"`
    CreatedByID uint `gorm:"index;not null" json:"created_by_id"`
    Versions []SOPVersion `json:"versions,omitempty"`
    Category Category `gorm:"foreignKey:CategoryID" json:"category"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

type SOPVersion struct {
    ID uint `gorm:"primaryKey" json:"-"`
    PublicID string `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
    CaptureSOPID uint `gorm:"uniqueIndex:idx_sop_version;not null" json:"-"`
    VersionNumber int `gorm:"uniqueIndex:idx_sop_version;not null" json:"version_number"`
    SchemaVersion string `gorm:"size:16;not null" json:"schema_version"`
    NameZH string `gorm:"size:160;not null" json:"-"`
    NameEN string `gorm:"size:160;not null" json:"-"`
    DescriptionZH string `gorm:"type:text;not null" json:"-"`
    DescriptionEN string `gorm:"type:text;not null" json:"-"`
    Status SOPVersionStatus `gorm:"size:32;index;not null" json:"status"`
    CoordinateSystem string `gorm:"size:32;not null" json:"-"`
    CopiedFromVersionID *uint `gorm:"index" json:"-"`
    PublishedAt *time.Time `json:"published_at"`
    Views []SOPView `gorm:"foreignKey:SOPVersionID" json:"views"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

type SOPView struct {
    ID uint `gorm:"primaryKey" json:"-"`
    PublicID string `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
    SOPVersionID uint `gorm:"uniqueIndex:idx_version_sequence;not null" json:"-"`
    Sequence int `gorm:"uniqueIndex:idx_version_sequence;not null" json:"sequence"`
    Role SOPViewRole `gorm:"size:32;not null" json:"role"`
    ViewKind SOPViewKind `gorm:"size:32;not null" json:"view_kind"`
    PresetKey string `gorm:"size:64" json:"preset_key"`
    NameZH string `gorm:"size:120;not null" json:"-"`
    NameEN string `gorm:"size:120;not null" json:"-"`
    InstructionZH string `gorm:"type:text;not null" json:"-"`
    InstructionEN string `gorm:"type:text;not null" json:"-"`
    Required bool `gorm:"not null" json:"required"`
    CameraPositionX float64 `gorm:"type:decimal(10,6);not null" json:"-"`
    CameraPositionY float64 `gorm:"type:decimal(10,6);not null" json:"-"`
    CameraPositionZ float64 `gorm:"type:decimal(10,6);not null" json:"-"`
    ImageUpX float64 `gorm:"type:decimal(10,6);not null" json:"-"`
    ImageUpY float64 `gorm:"type:decimal(10,6);not null" json:"-"`
    ImageUpZ float64 `gorm:"type:decimal(10,6);not null" json:"-"`
    TargetX float64 `gorm:"type:decimal(10,6);not null" json:"-"`
    TargetY float64 `gorm:"type:decimal(10,6);not null" json:"-"`
    TargetZ float64 `gorm:"type:decimal(10,6);not null" json:"-"`
    Composition Composition `gorm:"serializer:json;type:json;not null" json:"composition"`
    ReferenceImages []SOPViewReferenceImage `json:"reference_images"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

type SOPViewReferenceImage struct {
    ID uint `gorm:"primaryKey" json:"-"`
    PublicID string `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
    SOPViewID uint `gorm:"uniqueIndex:idx_view_reference_order;not null" json:"-"`
    ObjectKey string `gorm:"size:500;not null" json:"object_key"`
    ThumbnailURL string `gorm:"size:500;not null" json:"thumbnail_url"`
    SortOrder int `gorm:"uniqueIndex:idx_view_reference_order;not null" json:"sort_order"`
    CaptionZH string `gorm:"size:240;not null" json:"-"`
    CaptionEN string `gorm:"size:240;not null" json:"-"`
    CreatedAt time.Time `json:"created_at"`
}
```

- [ ] **Step 5: Replace legacy references and migration entries**

In `models.go`, delete `SOPTemplate` and the legacy `SOPView`. Change `PhotoSession` to:

```go
type PhotoSession struct {
    ID uint `gorm:"primaryKey" json:"-"`
    PublicID string `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
    Code string `gorm:"size:80;uniqueIndex;not null" json:"code"`
    SKUID uint `gorm:"index;not null" json:"sku_id"`
    SOPVersionID uint `gorm:"index;not null" json:"-"`
    PhotographerID uint `gorm:"index;not null" json:"photographer_id"`
    Status string `gorm:"size:32;not null;default:in_progress" json:"status"`
    SOPVersion SOPVersion `json:"sop_version"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}
```

Update `database.Migrate` to register `CaptureSOP`, `SOPVersion`, `SOPView`, and `SOPViewReferenceImage` instead of legacy template models. Remove legacy SOP backfill and seed blocks; later tasks add the new seed. Delete the old SOP request structs/handlers from `handlers.go` and remove the three `/sop-templates` registrations from `router.go`; Task 5 adds the replacement routes after the service exists.

- [ ] **Step 6: Run migration and full API tests**

Run: `cd api && go test ./...`

Expected: PASS; no runtime MySQL is needed for tests.

- [ ] **Step 7: Commit**

```bash
git add api/go.mod api/go.sum api/internal/models api/internal/database/database.go api/internal/app/test_database_test.go api/internal/app/handlers.go api/internal/app/router.go
git commit -m "feat(api): add versioned SOP persistence aggregate"
```

### Task 3: Presets and Aggregate Validation

**Files:**
- Create: `api/internal/sop/presets.go`
- Create: `api/internal/sop/presets_test.go`
- Create: `api/internal/sop/validation.go`
- Create: `api/internal/sop/validation_test.go`

**Interfaces:**
- Consumes: `Vector3`, `CanonicalizePose`, and model enums/types
- Produces: `func PresetByKey(key string) (ViewInput, bool)`
- Produces: `func ValidateVersion(version models.SOPVersion) []ValidationError`
- Produces: stable `ValidationError{Code, Path, Message}`

- [ ] **Step 1: Write failing preset tests including packaging**

```go
func TestPresetCatalog(t *testing.T) {
    cases := []struct{ key string; camera, up Vector3; required bool }{
        {"reference_front", Vector3{0,0,1}, Vector3{1,0,0}, true},
        {"back", Vector3{0,0,-1}, Vector3{1,0,0}, true},
        {"left", Vector3{0,1,0}, Vector3{1,0,0}, true},
        {"bottom", Vector3{-1,0,0}, Vector3{0,1,0}, true},
        {"right", Vector3{0,-1,0}, Vector3{-1,0,0}, true},
        {"top", Vector3{1,0,0}, Vector3{0,-1,0}, true},
        {"detail_label", Vector3{0,0,1}, Vector3{1,0,0}, false},
        {"packaging_front", Vector3{0,0,1}, Vector3{1,0,0}, false},
    }
    for _, tc := range cases {
        got, ok := PresetByKey(tc.key)
        if !ok { t.Fatalf("missing preset %s", tc.key) }
        if got.CameraPosition != tc.camera || got.ImageUp != tc.up || got.Required != tc.required {
            t.Fatalf("preset %s = %#v", tc.key, got)
        }
    }
    packaging, _ := PresetByKey("packaging_front")
    if packaging.NameZH != "包装正面" || packaging.NameEN != "Packaging Front" || packaging.Kind != models.SOPViewStandard {
        t.Fatalf("invalid packaging preset: %#v", packaging)
    }
}
```

- [ ] **Step 2: Write failing aggregate-validation tests**

```go
func TestValidateVersionReportsAllErrors(t *testing.T) {
    version := models.SOPVersion{
        SchemaVersion: "1.0", CoordinateSystem: "pcs_object_v1", Status: models.SOPVersionDraft,
        NameZH: "", NameEN: "Example",
        Views: []models.SOPView{{
            Sequence: 2, Role: models.SOPViewCapture, ViewKind: models.SOPViewStandard,
            NameZH: "", NameEN: "Broken", Required: true,
            CameraPositionZ: 1, ImageUpZ: 1,
            TargetX: 0.2,
            Composition: models.Composition{FrameOccupancy: 1.2, AspectRatio: "1:1"},
        }},
    }
    errors := ValidateVersion(version)
    codes := map[string]bool{}
    for _, item := range errors { codes[item.Code] = true }
    for _, code := range []string{"name_zh_required", "reference_front_count", "sequence_invalid", "pose_vectors_parallel", "standard_target_not_origin", "frame_occupancy_invalid"} {
        if !codes[code] { t.Errorf("missing %s in %#v", code, errors) }
    }
}
```

- [ ] **Step 3: Run focused tests and confirm missing implementations**

Run: `cd api && go test ./internal/sop -run 'TestPresetCatalog|TestValidateVersion' -v`

Expected: FAIL for missing preset and validation symbols.

- [ ] **Step 4: Implement the immutable preset map**

Define `ViewInput` with role, kind, bilingual text, required-state, vectors, target, and composition. Return a value copy from `PresetByKey`; do not expose a mutable global pointer. Use the exact values from the test and these packaging defaults:

```go
"packaging_front": {
    Role: models.SOPViewCapture, Kind: models.SOPViewStandard,
    NameZH: "包装正面", NameEN: "Packaging Front",
    InstructionZH: "完整居中拍摄包装正面，确保品牌与标签清晰可读。",
    InstructionEN: "Center the complete package front and keep branding and labels legible.",
    Required: false,
    CameraPosition: Vector3{0,0,1}, ImageUp: Vector3{1,0,0}, Target: Vector3{0,0,0},
    Composition: models.Composition{FrameOccupancy: 0.85, AspectRatio: "1:1", AllowRotationCorrection: true, AllowMirror: false},
},
```

- [ ] **Step 5: Implement exhaustive validation**

`ValidateVersion` must append errors instead of returning early. Implement helpers for bilingual required fields, exactly one fixed reference, contiguous sequence, pose canonicalizability, standard/detail target rules, `(0,1]` occupancy, positive `width:height`, and `allow_mirror == false`. Use stable field paths such as `views[2].pose.image_up_direction`.

Define:

```go
type LocalizedMessage struct { ZHCN string `json:"zh-CN"`; EN string `json:"en"` }
type ValidationError struct {
    Code string `json:"code"`
    Path string `json:"path"`
    Message LocalizedMessage `json:"message"`
}
```

- [ ] **Step 6: Run all SOP domain tests**

Run: `cd api && go test ./internal/sop -v`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add api/internal/sop
git commit -m "feat(api): validate SOP versions and presets"
```

### Task 4: Transactional SOP Lifecycle Service

**Files:**
- Create: `api/internal/app/sop_service.go`
- Create: `api/internal/app/sop_service_test.go`

**Interfaces:**
- Consumes: GORM models, `sop.PresetByKey`, `sop.CanonicalizePose`, `sop.ValidateVersion`
- Produces: `SOPService` methods `Create`, `List`, `GetVersion`, `AddView`, `UpdateView`, `DeleteView`, `Reorder`, `Validate`, `Publish`, `CopyVersion`, `Archive`
- Produces: domain errors `ErrVersionImmutable`, `ErrDraftExists`, `ErrReferenceLocked`, `ErrVersionNotFound`

- [ ] **Step 1: Write failing creation and publication tests**

```go
func TestSOPServiceCreateAndPublish(t *testing.T) {
    db := newTestDB(t)
    category, user := seedCategoryAndUser(t, db)
    service := NewSOPService(db)
    created, err := service.Create(context.Background(), CreateSOPInput{
        CategoryID: category.ID, CreatedByID: user.ID,
        NameZH: "手机壳拍摄", NameEN: "Phone Case Capture",
    })
    if err != nil { t.Fatal(err) }
    if created.Version.VersionNumber != 1 || len(created.Version.Views) != 1 { t.Fatalf("unexpected aggregate: %#v", created) }
    ref := created.Version.Views[0]
    if ref.Role != models.SOPViewReferenceFront || ref.Sequence != 1 || !ref.Required { t.Fatalf("invalid ref: %#v", ref) }
    if _, err := service.Publish(context.Background(), created.Version.PublicID); err != nil { t.Fatal(err) }
    if _, err := service.AddView(context.Background(), created.Version.PublicID, AddViewInput{PresetKey:"back"}); !errors.Is(err, ErrVersionImmutable) {
        t.Fatalf("add after publish error = %v", err)
    }
}
```

- [ ] **Step 2: Write failing copy, lock, reorder, and one-draft tests**

Cover these exact assertions:

```go
// DeleteView(ref.PublicID) returns ErrReferenceLocked.
// Reorder with missing/duplicate UUIDs returns a validation error and changes nothing.
// CopyVersion(published.PublicID) creates version 2 draft with new View UUIDs.
// A second CopyVersion while V2 is draft returns ErrDraftExists.
// Archive removes a published version from selectable published versions but preserves rows.
```

- [ ] **Step 3: Run focused lifecycle tests**

Run: `cd api && go test ./internal/app -run TestSOPService -v`

Expected: FAIL because the service does not exist.

- [ ] **Step 4: Implement service inputs and transaction boundaries**

```go
type SOPService struct { db *gorm.DB }
func NewSOPService(db *gorm.DB) *SOPService { return &SOPService{db: db} }

type CreateSOPInput struct { CategoryID, CreatedByID uint; NameZH, NameEN, DescriptionZH, DescriptionEN string }
type UpdateVersionInput struct { NameZH, NameEN, DescriptionZH, DescriptionEN string }
type AddViewInput struct { PresetKey string; Custom *sop.ViewInput }
type UpdateViewInput struct { NameZH, NameEN, InstructionZH, InstructionEN string; Required bool; CameraPosition, ImageUp, Target sop.Vector3; Composition models.Composition }
type ReferenceImageInput struct { ObjectKey, ThumbnailURL, CaptionZH, CaptionEN string; SortOrder int }

func (s *SOPService) Create(ctx context.Context, input CreateSOPInput) (*CreatedSOP, error)
func (s *SOPService) List(ctx context.Context, categoryID uint) ([]models.CaptureSOP, error)
func (s *SOPService) GetVersion(ctx context.Context, publicID string) (*models.SOPVersion, error)
func (s *SOPService) UpdateVersion(ctx context.Context, publicID string, input UpdateVersionInput) (*models.SOPVersion, error)
func (s *SOPService) AddView(ctx context.Context, versionPublicID string, input AddViewInput) (*models.SOPView, error)
func (s *SOPService) UpdateView(ctx context.Context, versionPublicID, viewPublicID string, input UpdateViewInput) (*models.SOPView, error)
func (s *SOPService) DeleteView(ctx context.Context, versionPublicID, viewPublicID string) error
func (s *SOPService) Reorder(ctx context.Context, versionPublicID string, orderedViewIDs []string) error
func (s *SOPService) Validate(ctx context.Context, versionPublicID string) ([]sop.ValidationError, error)
func (s *SOPService) Publish(ctx context.Context, versionPublicID string) (*models.SOPVersion, error)
func (s *SOPService) CopyVersion(ctx context.Context, sopPublicID, sourceVersionPublicID string) (*models.SOPVersion, error)
func (s *SOPService) Archive(ctx context.Context, versionPublicID string) error
func (s *SOPService) AddReferenceImage(ctx context.Context, versionPublicID, viewPublicID string, input ReferenceImageInput) (*models.SOPViewReferenceImage, error)
func (s *SOPService) DeleteReferenceImage(ctx context.Context, versionPublicID, viewPublicID, imagePublicID string) error
func (s *SOPService) ReorderReferenceImages(ctx context.Context, versionPublicID, viewPublicID string, orderedImageIDs []string) error
```

Use `github.com/google/uuid`. Canonicalize pose input before persistence. For reorder, temporarily offset all sequences within one transaction, then assign final `1..N` to avoid the unique index. For publication, preload all Views/reference images, validate inside the transaction, then set status and `published_at`.

- [ ] **Step 5: Run lifecycle and full API tests**

Run: `cd api && go test ./internal/app -run TestSOPService -v`

Expected: PASS.

Run: `cd api && go test ./...`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add api/internal/app/sop_service.go api/internal/app/sop_service_test.go
git commit -m "feat(api): add transactional SOP lifecycle"
```

### Task 5: UUID DTOs, HTTP Endpoints, and OpenAPI

**Files:**
- Create: `api/internal/app/sop_dto.go`
- Create: `api/internal/app/sop_handlers.go`
- Create: `api/internal/app/sop_handlers_test.go`
- Modify: `api/internal/app/router.go`
- Modify: `api/internal/app/handlers.go`
- Create: `api/openapi.yaml`

**Interfaces:**
- Consumes: all `SOPService` methods
- Produces: endpoints from specification section 11
- Produces: nested localized DTOs, vector arrays, expanded `pcs_object_v1`, and structured validation responses

- [ ] **Step 1: Write failing handler tests**

Use an authenticated test router and assert:

```go
func TestCreateCaptureSOPReturnsReferenceVersionDocument(t *testing.T) {
    // POST /api/v1/capture-sops with category_id, name.zh-CN, name.en.
    // Assert 201, UUID public_id, version_number 1, status draft,
    // coordinate_system.id pcs_object_v1, and one fixed reference_front View.
}

func TestPublishReturnsAllValidationErrors(t *testing.T) {
    // Corrupt a draft through the DB fixture, POST /sop-versions/{uuid}/publish,
    // assert 422, code sop_validation_failed, and multiple errors with code/path/message.
}

func TestPublishedVersionRejectsPatch(t *testing.T) {
    // PATCH a published version and assert 409 with code version_immutable.
}
```

- [ ] **Step 2: Run focused handler tests**

Run: `cd api && go test ./internal/app -run 'TestCreateCaptureSOP|TestPublishReturns|TestPublishedVersion' -v`

Expected: FAIL because routes and DTOs are missing.

- [ ] **Step 3: Implement DTO conversion**

Define reusable DTOs:

```go
type localizedTextDTO struct { ZHCN string `json:"zh-CN"`; EN string `json:"en"` }
type poseDTO struct {
    Space string `json:"space"`
    CameraPositionDirection sop.Vector3 `json:"camera_position_direction"`
    ImageUpDirection sop.Vector3 `json:"image_up_direction"`
    Target sop.Vector3 `json:"target"`
}
type coordinateSystemDTO struct {
    ID string `json:"id"`; Handedness string `json:"handedness"`; Origin string `json:"origin"`; Unit string `json:"unit"`
    Axes map[string]string `json:"axes"`
}
```

`versionDTOFromModel` must sort Views/reference images and expand the coordinate system exactly as the spec document.

- [ ] **Step 4: Register and implement the routes**

```go
protected.POST("/capture-sops", server.createCaptureSOP)
protected.GET("/capture-sops", server.listCaptureSOPs)
protected.GET("/capture-sops/:sop_id", server.getCaptureSOP)
protected.GET("/sop-versions/:version_id", server.getSOPVersion)
protected.PATCH("/sop-versions/:version_id", server.updateSOPVersion)
protected.POST("/sop-versions/:version_id/views", server.addSOPView)
protected.PATCH("/sop-versions/:version_id/views/:view_id", server.updateSOPView)
protected.DELETE("/sop-versions/:version_id/views/:view_id", server.deleteSOPView)
protected.PUT("/sop-versions/:version_id/view-order", server.reorderSOPViews)
protected.POST("/sop-versions/:version_id/validate", server.validateSOPVersion)
protected.POST("/sop-versions/:version_id/publish", server.publishSOPVersion)
protected.POST("/capture-sops/:sop_id/versions", server.copySOPVersion)
protected.POST("/sop-versions/:version_id/archive", server.archiveSOPVersion)
protected.POST("/sop-versions/:version_id/views/:view_id/reference-images/upload-url", server.createSOPReferenceUploadURL)
protected.POST("/sop-versions/:version_id/views/:view_id/reference-images", server.addSOPReferenceImage)
protected.DELETE("/sop-versions/:version_id/views/:view_id/reference-images/:image_id", server.deleteSOPReferenceImage)
protected.PUT("/sop-versions/:version_id/views/:view_id/reference-image-order", server.reorderSOPReferenceImages)
```

The reference-image presign endpoint accepts `file_name` and `content_type`, writes only under `sop-references/{version_uuid}/{view_uuid}/`, and returns the same method/upload URL/object-key envelope used by Asset uploads. The metadata endpoint accepts the resulting object key, thumbnail URL, bilingual caption, and sort order. Add matching transactional methods to `SOPService`; all four endpoints reject non-draft versions.

Use strict JSON decoding for all new mutation endpoints so unknown composition fields are rejected:

```go
func decodeJSONStrict(c *gin.Context, target any) error {
    decoder := json.NewDecoder(c.Request.Body)
    decoder.DisallowUnknownFields()
    return decoder.Decode(target)
}
```

Map invalid input to 400, not found to 404, immutable/draft conflict to 409, aggregate validation to 422, and unexpected DB errors to 500.

- [ ] **Step 5: Write the OpenAPI document and generate Web types**

Define every route above plus components for `LocalizedText`, `Vector3`, `Composition`, `ReferenceImage`, `SOPView`, `CoordinateSystem`, `SOPVersion`, `CaptureSOPSummary`, and `ValidationResponse`. Require array length 3 for vectors and disallow extra composition properties. Use these exact component constraints:

```yaml
Vector3:
  type: array
  minItems: 3
  maxItems: 3
  items: { type: number, format: double }
Composition:
  type: object
  additionalProperties: false
  required: [frame_occupancy, aspect_ratio, allow_rotation_correction, allow_mirror]
  properties:
    frame_occupancy: { type: number, exclusiveMinimum: 0, maximum: 1 }
    aspect_ratio: { type: string, pattern: '^[1-9][0-9]*:[1-9][0-9]*$' }
    allow_rotation_correction: { type: boolean }
    allow_mirror: { type: boolean, enum: [false] }
```

Run: `cd web && pnpm generate:api`

Expected: `web/src/lib/openapi-types.ts` contains concrete `/capture-sops` and `/sop-versions/{version_id}` paths instead of `Record<string, never>`.

- [ ] **Step 6: Run backend and generated-contract verification**

Run: `cd api && go test ./...`

Expected: PASS.

Run: `cd web && pnpm typecheck`

Expected: PASS with generated types.

- [ ] **Step 7: Commit**

```bash
git add api/internal/app api/openapi.yaml web/src/lib/openapi-types.ts
git commit -m "feat(api): expose versioned SOP endpoints"
```

### Task 6: Bind Photo Sessions and Assets to Published Versions

**Files:**
- Modify: `api/internal/app/handlers.go`
- Modify: `api/internal/app/catalog.go`
- Create: `api/internal/app/photo_session_test.go`
- Modify: `api/internal/database/database.go`

**Interfaces:**
- Changes request: `createPhotoSessionRequest{SKUID uint, SOPVersionID string}`
- Changes upload request: View and session identifiers are UUID strings
- Guarantees: session creation accepts only `published`, non-archived version; Asset references exact internal SOPView row

- [ ] **Step 1: Write failing photo-session and upload tests**

```go
func TestCreatePhotoSessionRequiresPublishedVersion(t *testing.T) {
    // Create draft and SKU fixtures; POST with draft UUID => 409 version_not_published.
    // Publish; POST again => 201 and response contains session UUID + version UUID.
}

func TestCompleteAssetRejectsViewFromAnotherVersion(t *testing.T) {
    // Create session for V1, send a V2 View UUID to complete upload, expect 409 view_version_mismatch.
}
```

- [ ] **Step 2: Run focused tests and confirm current numeric-template contract fails**

Run: `cd api && go test ./internal/app -run 'TestCreatePhotoSessionRequires|TestCompleteAssetRejects' -v`

Expected: FAIL.

- [ ] **Step 3: Resolve UUIDs and enforce version membership**

Use a transaction to load the public version ID, verify status `published`, create the session with a new UUID, and return its public ID. Upload URL creation resolves the View UUID; completion resolves both session and View UUID and rejects when `view.SOPVersionID != session.SOPVersionID`.

Update asset-review responses to use localized View fields:

```go
type localizedViewName struct { ZHCN string `json:"zh-CN"`; EN string `json:"en"` }
```

- [ ] **Step 4: Add a clean new-model seed**

Seed one phone-case SOP with V1 published and these Views: reference front, back, left, bottom, right, top, optional label detail, and optional packaging front. Generate UUIDs and use `sop.PresetByKey` so seed vectors cannot drift from the catalog.

- [ ] **Step 5: Run all backend tests**

Run: `cd api && go test ./...`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add api/internal/app/handlers.go api/internal/app/catalog.go api/internal/app/photo_session_test.go api/internal/database/database.go
git commit -m "feat(api): bind captures to published SOP versions"
```

### Task 7: Web SOP Contract and Validation

**Files:**
- Create: `web/src/lib/sop.ts`
- Modify: `web/src/lib/schemas.ts`
- Create: `web/src/lib/schemas.test.ts`
- Modify: `web/src/lib/i18n.tsx`

**Interfaces:**
- Consumes: generated OpenAPI `components["schemas"]`
- Produces: `sopVersionSchema`, `sopViewSchema`, `compositionSchema`, `localizedText(language, value)`
- Produces: preset keys including `packaging_front`

- [ ] **Step 1: Write failing Zod tests**

```ts
import { describe, expect, it } from "vitest";
import { sopViewSchema } from "./schemas";

const baseView = {
  public_id: crypto.randomUUID(), sequence: 2, role: "capture", view_kind: "standard",
  preset_key: "packaging_front", name: { "zh-CN": "包装正面", en: "Packaging Front" },
  instruction: { "zh-CN": "完整拍摄包装正面", en: "Capture the complete package front" },
  required: false,
  pose: { space: "object", camera_position_direction: [0,0,1], image_up_direction: [1,0,0], target: [0,0,0] },
  composition: { frame_occupancy: 0.85, aspect_ratio: "1:1", allow_rotation_correction: true, allow_mirror: false },
  reference_images: [],
};

describe("sopViewSchema", () => {
  it("accepts optional packaging front", () => expect(sopViewSchema.parse(baseView).required).toBe(false));
  it("rejects mirror", () => expect(() => sopViewSchema.parse({ ...baseView, composition: { ...baseView.composition, allow_mirror: true } })).toThrow());
  it("rejects detail targets outside the normalized box", () => expect(() => sopViewSchema.parse({ ...baseView, view_kind: "detail", pose: { ...baseView.pose, target: [0.6,0,0] } })).toThrow());
});
```

- [ ] **Step 2: Run and confirm schema failure**

Run: `cd web && pnpm test -- src/lib/schemas.test.ts`

Expected: FAIL because `sopViewSchema` is missing.

- [ ] **Step 3: Implement typed contract aliases and schemas**

```ts
export const vector3Schema = z.tuple([z.number().finite(), z.number().finite(), z.number().finite()]);
export const compositionSchema = z.object({
  frame_occupancy: z.number().gt(0).lte(1),
  aspect_ratio: z.string().regex(/^[1-9]\d*:[1-9]\d*$/),
  allow_rotation_correction: z.boolean(),
  allow_mirror: z.literal(false),
}).strict();
```

Build `sopViewSchema` with `superRefine` for standard target origin, detail target bounds, fixed reference values, and non-parallel vector check. Export `SOPVersion`, `SOPView`, and `ValidationResponse` from the generated OpenAPI component types where possible; use `z.infer` only for form inputs.

- [ ] **Step 4: Add all Web translation keys**

Add matching keys in `zh` and `en` for create/copy/publish/archive, version status, View role/kind, vector labels, target, composition, all presets including Packaging Front, validation summary, immutable notice, and reference lock notice.

- [ ] **Step 5: Run Web unit, type, and lint checks**

Run: `cd web && pnpm test -- src/lib/schemas.test.ts`

Expected: PASS.

Run: `cd web && pnpm typecheck && pnpm lint`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/sop.ts web/src/lib/schemas.ts web/src/lib/schemas.test.ts web/src/lib/i18n.tsx
git commit -m "feat(web): add SOP contracts and validation"
```

### Task 8: Web SOP List, Creation, and Version Editor

**Files:**
- Modify: `web/src/app/(dashboard)/sop-templates/page.tsx`
- Create: `web/src/app/(dashboard)/sop-templates/new/page.tsx`
- Create: `web/src/app/(dashboard)/sop-templates/[sopId]/versions/[versionId]/page.tsx`
- Create: `web/src/components/sop/sop-view-editor.tsx`
- Create: `web/src/components/sop/sop-version-editor.tsx`
- Create: `web/src/components/sop/sop-version-editor.test.tsx`

**Interfaces:**
- Consumes: `/capture-sops`, `/sop-versions/*`, generated DTOs and Zod schemas
- Produces: draft-only editor, preset insertion, exact ordering, validation display, publication/copy/archive controls

- [ ] **Step 1: Write failing editor tests**

```tsx
it("renders the reference front as locked", async () => {
  render(<SOPVersionEditor initialVersion={draftFixture} />);
  expect(screen.getByText("正面")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "删除正面" })).toBeDisabled();
});

it("adds packaging front as optional", async () => {
  render(<SOPVersionEditor initialVersion={draftFixture} />);
  fireEvent.click(screen.getByRole("button", { name: "添加包装正面" }));
  expect(await screen.findByDisplayValue("Packaging Front")).toBeInTheDocument();
  expect(screen.getByLabelText("Packaging Front 必拍")).not.toBeChecked();
});

it("disables all mutations for a published version", () => {
  render(<SOPVersionEditor initialVersion={{ ...draftFixture, status: "published" }} />);
  expect(screen.getByText("已发布版本不可修改")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "添加视图" })).toBeDisabled();
});
```

- [ ] **Step 2: Run and confirm missing editor failure**

Run: `cd web && pnpm test -- src/components/sop/sop-version-editor.test.tsx`

Expected: FAIL because the components do not exist.

- [ ] **Step 3: Implement focused View editor**

`SOPViewEditor` receives `{view, language, locked, onChange, onDelete}`. Render bilingual name/instruction inputs, required toggle, camera/up/target numeric triples, kind, composition, and reference-image list. Disable role, sequence, pose, required, and deletion for `reference_front`; disable all controls when version status is not draft. Reference-image add/remove/reorder calls the dedicated endpoints from Task 5 after presigned object-store upload.

Use native accessible inputs and existing Card/Button/Input/Label components. Do not add a 3D editor in this implementation; numeric fields plus preset buttons satisfy V1.

- [ ] **Step 4: Implement aggregate editor behavior**

`SOPVersionEditor` owns the ordered View form state, calls Zod before requests, displays server `errors[]` by path, sends one complete UUID list for reorder, and invalidates React Query after mutation. Publish must call `/validate` first, show all issues, then call `/publish` only when validation returns an empty list.

- [ ] **Step 5: Implement list and route pages**

The list shows logical SOP, bilingual active name, category, latest published version, draft badge, required/optional count, and actions. New page posts category plus bilingual name and navigates to the returned V1 draft. Version page fetches the concrete UUID version and renders `SOPVersionEditor`.

- [ ] **Step 6: Run Web verification**

Run: `cd web && pnpm test`

Expected: PASS.

Run: `cd web && pnpm typecheck && pnpm lint && pnpm build`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/app/\(dashboard\)/sop-templates web/src/components/sop
git commit -m "feat(web): add versioned SOP editor"
```

### Task 9: iOS UUID DTOs and API Client

**Files:**
- Modify: `ios/CargoFlows/Models/DTOs.swift`
- Modify: `ios/CargoFlows/Networking/APIClient.swift`
- Modify: `ios/project.yml`
- Create: `ios/CargoFlowsTests/SOPDTOTests.swift`

**Interfaces:**
- Produces: `CaptureSOPSummary`, `SOPVersion`, `SOPView`, `LocalizedText`, `Vector3DTO`, `CompositionDTO`
- Changes: `PhotoSession.id`, `SOPVersion.id`, and `SOPView.id` are `String` UUIDs
- Produces: `listPublishedSOPs(categoryID:)`, `getSOPVersion(id:)`, and UUID upload/session methods

- [ ] **Step 1: Add XCTest target and write failing decoding test**

Add this target to `project.yml`:

```yaml
  CargoFlowsTests:
    type: bundle.unit-test
    platform: iOS
    sources:
      - CargoFlowsTests
    dependencies:
      - target: CargoFlows

schemes:
  CargoFlows:
    build:
      targets:
        CargoFlows: all
    test:
      targets:
        - CargoFlowsTests
```

Write:

```swift
import XCTest
@testable import CargoFlows

final class SOPDTOTests: XCTestCase {
    func testDecodesPackagingFrontAndLocalizesName() throws {
        let data = Data(#"{"public_id":"11111111-1111-1111-1111-111111111111","sequence":2,"role":"capture","view_kind":"standard","preset_key":"packaging_front","name":{"zh-CN":"包装正面","en":"Packaging Front"},"instruction":{"zh-CN":"拍摄包装","en":"Capture package"},"required":false,"pose":{"space":"object","camera_position_direction":[0,0,1],"image_up_direction":[1,0,0],"target":[0,0,0]},"composition":{"frame_occupancy":0.85,"aspect_ratio":"1:1","allow_rotation_correction":true,"allow_mirror":false},"reference_images":[]}"#.utf8)
        let view = try JSONDecoder().decode(SOPView.self, from: data)
        XCTAssertEqual(view.displayName(for: .zh), "包装正面")
        XCTAssertEqual(view.displayName(for: .en), "Packaging Front")
        XCTAssertFalse(view.required)
    }
}
```

- [ ] **Step 2: Generate the test project and confirm failure**

Run: `cd ios && xcodegen generate && xcodebuild -project CargoFlows.xcodeproj -scheme CargoFlows -sdk iphonesimulator -destination 'platform=iOS Simulator,name=iPhone 16' test`

Expected: FAIL because the new DTO shape does not exist.

- [ ] **Step 3: Implement Swift DTOs**

```swift
struct LocalizedText: Decodable {
    let zhCN: String
    let en: String
    enum CodingKeys: String, CodingKey { case zhCN = "zh-CN"; case en }
    func value(for language: AppLanguage) -> String { language == .en ? en : zhCN }
}

struct SOPView: Identifiable, Decodable {
    let publicID: String
    var id: String { publicID }
    let sequence: Int
    let role: String
    let viewKind: String
    let presetKey: String?
    let name: LocalizedText
    let instruction: LocalizedText
    let required: Bool
    let pose: SOPPose
    let composition: CompositionDTO
    func displayName(for language: AppLanguage) -> String { name.value(for: language) }
    enum CodingKeys: String, CodingKey {
        case publicID = "public_id"
        case sequence, role, required, name, instruction, pose, composition
        case viewKind = "view_kind"
        case presetKey = "preset_key"
    }
}
```

Define `SOPPose` with three `[Double]` arrays and explicit coding keys for `camera_position_direction`, `image_up_direction`, and `target`. Use explicit `public_id` coding keys for every UUID DTO so acronym casing never depends on `.convertFromSnakeCase`.

- [ ] **Step 4: Update API methods to UUID contracts**

```swift
func listPublishedSOPs(categoryID: Int) async throws -> ListResponse<CaptureSOPSummary>
func getSOPVersion(id: String) async throws -> SOPVersion
func createPhotoSession(skuID: Int, sopVersionID: String) async throws -> PhotoSession
func uploadImage(_ imageData: Data, skuID: Int, sopViewID: String, photoSessionID: String, fileName: String) async throws -> AssetReceipt
```

Percent-encode path UUIDs and send `sop_version_id`, `sop_view_id`, and `photo_session_id` as strings.

- [ ] **Step 5: Run iOS tests**

Run: `cd ios && xcodegen generate && xcodebuild -project CargoFlows.xcodeproj -scheme CargoFlows -sdk iphonesimulator -destination 'platform=iOS Simulator,name=iPhone 16' test`

Expected: PASS. If the installed simulator has a different model, run `xcrun simctl list devices available` and use one available iOS 17+ simulator; record the exact chosen destination in the task log.

- [ ] **Step 6: Commit**

```bash
git add ios/CargoFlows/Models/DTOs.swift ios/CargoFlows/Networking/APIClient.swift ios/CargoFlowsTests/SOPDTOTests.swift ios/project.yml ios/CargoFlows.xcodeproj/project.pbxproj
git commit -m "feat(ios): adopt versioned SOP contracts"
```

### Task 10: iOS Published-Version Capture Flow

**Files:**
- Modify: `ios/CargoFlows/Views/SOPCaptureView.swift`
- Modify: `ios/CargoFlows/App/LanguageStore.swift`
- Create: `ios/CargoFlowsTests/SOPCaptureLogicTests.swift`

**Interfaces:**
- Consumes: published summaries/version details and UUID session/upload methods
- Produces: explicit published-version selection, localized View checklist, required completion logic

- [ ] **Step 1: Extract and test required-completion logic**

```swift
func requiredViewsComplete(views: [SOPView], capturedViewIDs: Set<String>) -> Bool {
    views.filter(\.required).allSatisfy { capturedViewIDs.contains($0.id) }
}

final class SOPCaptureLogicTests: XCTestCase {
    func testOptionalPackagingDoesNotBlockFinish() throws {
        let views = [referenceFixture(required: true), packagingFixture(required: false)]
        XCTAssertTrue(requiredViewsComplete(views: views, capturedViewIDs: [views[0].id]))
    }
}
```

- [ ] **Step 2: Run and confirm helper failure**

Run the same `xcodebuild ... -scheme CargoFlowsTests ... test` command from Task 9.

Expected: FAIL because `requiredViewsComplete` is missing.

- [ ] **Step 3: Implement version selection and capture**

Load published SOP summaries for `sku.product.categoryRecord.id`, present a Picker when more than one published version exists, fetch the selected concrete version, and render Views by `sequence`. Use localized View name/instruction. Store captured images by UUID View ID and create one session for the selected version before the first upload.

When no published SOP exists, show a bilingual empty state and disable capture. When a version becomes archived after loading, an already-created session remains usable; creating a new session surfaces the server rejection.

- [ ] **Step 4: Add bilingual iOS strings**

Add matching Chinese/English keys for SOP selection, version number, no published SOP, standard/detail, optional, reference View, and version-load/session-create failures.

- [ ] **Step 5: Run iOS tests and build**

Run: `cd ios && xcodegen generate && xcodebuild -project CargoFlows.xcodeproj -scheme CargoFlowsTests -sdk iphonesimulator -destination 'platform=iOS Simulator,name=iPhone 16' test`

Expected: PASS.

Run: `cd ios && xcodebuild -project CargoFlows.xcodeproj -scheme CargoFlows -sdk iphonesimulator -destination 'generic/platform=iOS Simulator' build`

Expected: `BUILD SUCCEEDED`.

- [ ] **Step 6: Commit**

```bash
git add ios/CargoFlows/Views/SOPCaptureView.swift ios/CargoFlows/App/LanguageStore.swift ios/CargoFlowsTests/SOPCaptureLogicTests.swift ios/CargoFlows.xcodeproj/project.pbxproj
git commit -m "feat(ios): capture against published SOP versions"
```

### Task 11: Cross-Stack Contract and Final Verification

**Files:**
- Modify: `README.md`
- Modify: `api/openapi.yaml` only if verification exposes a contract mismatch
- Modify: generated/client files only if regeneration changes them

**Interfaces:**
- Verifies all earlier interfaces; introduces no new product behavior

- [ ] **Step 1: Regenerate every generated artifact**

Run: `cd web && pnpm generate:api`

Run: `cd ios && xcodegen generate`

Expected: generated files are current; a second run produces no diff.

- [ ] **Step 2: Run backend verification**

Run: `cd api && gofmt -w internal/sop internal/models internal/app internal/database`

Run: `cd api && go test ./...`

Expected: PASS with zero failures.

- [ ] **Step 3: Run Web verification**

Run: `cd web && pnpm test`

Run: `cd web && pnpm typecheck`

Run: `cd web && pnpm lint`

Run: `cd web && pnpm build`

Expected: every command exits 0.

- [ ] **Step 4: Run iOS verification**

Run the XCTest and generic simulator build commands from Tasks 9–10.

Expected: all tests pass and app build reports `BUILD SUCCEEDED`.

- [ ] **Step 5: Perform an API smoke test against clean infrastructure**

Run: `docker compose up --build -d mysql minio api`

Use the seeded admin token to create a draft SOP, add `packaging_front`, validate, publish, create V2, and create a photo session against V1. Verify:

```text
V1 has exactly one reference_front.
packaging_front is standard and optional.
Published V1 rejects mutations.
V2 is draft with new View UUIDs.
PhotoSession stores V1's exact version UUID.
```

- [ ] **Step 6: Update README**

Document the new SOP terminology, coordinate convention, version lifecycle, API generation command, Web editor route, and iOS published-version behavior. State explicitly that this release assumes a clean database and includes no legacy migration.

- [ ] **Step 7: Inspect the final diff and commit**

Run: `git diff --check`

Run: `git status --short`

Expected: no whitespace errors; only planned files are changed.

```bash
git add README.md api/openapi.yaml web/src/lib/openapi-types.ts ios/CargoFlows.xcodeproj/project.pbxproj
git commit -m "docs: document versioned capture SOP workflow"
```

## Execution Completion Criteria

- All eleven tasks are committed independently in order.
- Backend, Web, and iOS verification commands have fresh passing output.
- A clean-database smoke test proves `packaging_front`, immutable publication, copied versions, and exact photo-session version binding.
- No legacy SOP migration or compatibility code is present.
- The final implementation matches `docs/superpowers/specs/2026-07-16-capture-sop-structured-data-design.md` without unresolved placeholders or undocumented contract deviations.
