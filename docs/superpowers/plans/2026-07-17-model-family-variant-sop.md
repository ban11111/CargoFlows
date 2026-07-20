# Model-family and Variant-aware SOP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reuse SOP views and approved structural image references across products in one explicitly managed model family without allowing another SKU's appearance or variant details to leak into target-SKU image generation.

**Architecture:** Add versioned model-family, identity-manifest, difference-region, family-SOP, and reference-grant domains. Resolve category SOP + family overrides + target-variant evidence requirements into a frozen `ResolvedSOPV1`, build a deterministic evidence matrix before any billable image call, and pass only target identity images plus restricted cross-SKU derivatives into the existing multi-turn image pipeline. The target SKU remains authoritative and all provider inputs are role-labelled, hashed, private, and auditable.

**Tech Stack:** Go 1.25, GORM/MySQL, Gin/OpenAPI, MinIO private objects, OpenAI Responses image-generation tool, Next.js 16, React Query, Vitest, Playwright, Docker Compose.

## Global Constraints

- Web only; do not change iOS.
- Old development SOP, AI, and image data are outside the compatibility contract and may be re-uploaded after release.
- A SKU may have only one active model-family membership in the first release.
- Image generation requires at least one approved target-SKU identity anchor.
- Cross-SKU assets are opt-in, family-scoped, reviewer-approved, and `geometry_only`, `viewpoint_only`, or `detail_geometry`; cross-SKU appearance reuse is forbidden.
- Every `exact` difference region requires approved target-SKU evidence before any OpenAI image call.
- Target evidence always wins conflicts. References may not establish target color, finish, texture, logos, labels, ports, controls, accessories, package contents, or packaging.
- Title and SEO generation use target structured data and explicitly published family invariants only; they never use sibling-SKU image appearance as evidence.
- Published family SOP and identity-manifest versions are immutable. Jobs freeze exact versions and content hashes.
- Cross-SKU provider inputs use private grayscale/masked derivatives by default, never unrestricted originals.
- No API key, image bytes, internal object key, endpoint, credential, or signed URL may enter prompts, logs, public DTOs, or audit metadata.
- Missing evidence and invalid grants fail preflight before billing. Ambiguous provider outcomes enter `needs_attention` and are not retried blindly.
- This plan runs after private generated-image storage primitives from `2026-07-17-openai-image-multiturn-gallery.md` Task 4 and before its executor/API/Web Tasks 5-8.

---

### Task 1: Add Greenfield Model-family and Variant Identity Schema

**Files:**
- Create: `api/internal/models/model_family.go`
- Create: `api/internal/models/model_family_test.go`
- Modify: `api/internal/models/models.go`
- Modify: `api/internal/database/database.go`
- Create: `api/internal/database/model_family_test.go`
- Modify: `api/internal/app/handlers.go`
- Modify: `api/internal/app/photo_session_test.go`

**Interfaces:**
- Produces `ModelFamily`, `ModelFamilyMember`, `VariantIdentityManifest`, `VariantIdentityManifestVersion`, `VariantDifferenceRegion`, `VariantDifferenceRegionEvidenceAsset`, and lifecycle enums.
- `VariantIdentityManifestVersion.IdentityJSON` contains the typed identity document; regions remain relational for evidence coverage and normalized geometry queries.

- [ ] **Step 1: Write failing model/default/serialization tests**

Add tests proving one active family membership per SKU, one draft manifest version per manifest, immutable published-state metadata, positive version numbers, JSON defaults, normalized region geometry, exact-region required view keys, and non-serialization of internal IDs.

```go
func TestVariantIdentityDefaults(t *testing.T) {
    version := models.VariantIdentityManifestVersion{}
    if err := version.BeforeCreate(nil); err != nil { t.Fatal(err) }
    if string(version.IdentityJSON) != `{}` { t.Fatalf("identity = %s", version.IdentityJSON) }
    if version.Status != models.VariantManifestDraft { t.Fatalf("status = %s", version.Status) }
}
```

- [ ] **Step 2: Run RED**

Run: `cd api && GOCACHE=/private/tmp/cargoflows-go-cache go test ./internal/models ./internal/database -run 'ModelFamily|VariantIdentity|DifferenceRegion' -count=1`
Expected: FAIL because the model-family types do not exist.

- [ ] **Step 3: Implement schema and database constraints**

Use these core shapes and named unique/check constraints:

```go
type ModelFamily struct {
    ID uint `gorm:"primaryKey" json:"-"`
    PublicID string `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
    Brand string `gorm:"size:120;index;not null" json:"brand"`
    NameZH string `gorm:"size:180;not null" json:"name_zh"`
    NameEN string `gorm:"size:180;not null" json:"name_en"`
    ModelCode string `gorm:"size:120;uniqueIndex;not null" json:"model_code"`
    CommonStructureJSON []byte `gorm:"type:json;not null" json:"common_structure"`
    VariationDimensionsJSON []byte `gorm:"type:json;not null" json:"variation_dimensions"`
    Status ModelFamilyStatus `gorm:"size:32;index;not null;default:active" json:"status"`
    CreatedByID uint `gorm:"index;not null" json:"-"`
    CreatedAt time.Time
    UpdatedAt time.Time
}

type ModelFamilyMember struct {
    ID uint `gorm:"primaryKey" json:"-"`
    PublicID string `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
    ModelFamilyID uint `gorm:"index;not null" json:"-"`
    SKUID uint `gorm:"uniqueIndex:idx_active_family_member,priority:1;index;not null" json:"-"`
    ActiveGuard *string `gorm:"size:16;uniqueIndex:idx_active_family_member,priority:2" json:"-"`
    AddedByID uint `gorm:"index;not null" json:"-"`
    RemovedByID *uint `gorm:"index" json:"-"`
    RemovedAt *time.Time `json:"removed_at"`
    CreatedAt time.Time `json:"created_at"`
}

type VariantIdentityManifestVersion struct {
    ID uint `gorm:"primaryKey" json:"-"`
    PublicID string `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
    VariantIdentityManifestID uint `gorm:"uniqueIndex:idx_variant_manifest_version,priority:1;uniqueIndex:idx_variant_manifest_draft,priority:1;not null" json:"-"`
    VersionNumber int `gorm:"uniqueIndex:idx_variant_manifest_version,priority:2;check:chk_variant_manifest_version,version_number > 0;not null" json:"version_number"`
    Status VariantManifestStatus `gorm:"size:32;index;not null;default:draft" json:"status"`
    DraftGuard *string `gorm:"size:16;uniqueIndex:idx_variant_manifest_draft,priority:2" json:"-"`
    IdentityJSON []byte `gorm:"type:json;not null" json:"identity"`
    CreatedByID uint `gorm:"index;not null" json:"-"`
    PublishedByID *uint `gorm:"index" json:"-"`
    PublishedAt *time.Time `json:"published_at"`
    CreatedAt time.Time `json:"created_at"`
}
```

Define `VariantDifferenceRegion` with `DifferenceKind`, `Strictness`, normalized shape JSON, forbidden-inheritance JSON, required-view-keys JSON, and the manifest-version foreign key. Define `VariantDifferenceRegionEvidenceAsset` as a unique region/asset mapping; service validation requires every mapped asset to belong to the manifest SKU and have approved immutable metadata. Use `BeforeCreate` hooks only for safe defaults; service validation owns cross-row semantics.

Add opaque `PublicID` UUID columns to `SKU` and `Asset`, generated in `BeforeCreate` when absent. Add immutable upload metadata to `Asset`: `MIMEType`, `Width`, `Height`, `ByteCount`, and `SHA256`. Update upload completion to stream the object through the shared image validator before inserting `Asset`; persist detected MIME, decoded dimensions, byte count, and SHA-256, and reject mismatched declared content types, invalid images, excessive bytes/pixels, and unsupported formats. New uploads must populate all fields; no legacy rows are backfilled.

- [ ] **Step 4: Register greenfield migrations and run GREEN**

Add all new models to `migrateSchema`. Do not add backfill or legacy compatibility branches.

Run: `cd api && GOCACHE=/private/tmp/cargoflows-go-cache go test ./internal/models ./internal/database ./internal/app -run 'ModelFamily|VariantIdentity|DifferenceRegion|AssetMetadata|Migrate' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add api/internal/models api/internal/database api/internal/app/handlers.go api/internal/app/photo_session_test.go
git commit -m "feat(catalog): add model family identity schema"
```

### Task 2: Implement Model-family Membership Lifecycle and Admin API

**Files:**
- Create: `api/internal/sop/model_families.go`
- Create: `api/internal/sop/model_families_test.go`
- Modify: `api/internal/app/router.go`
- Modify: `api/internal/app/handlers.go`
- Create: `api/internal/app/model_family_handlers.go`
- Create: `api/internal/app/model_family_handlers_test.go`
- Modify: `api/openapi.yaml`
- Regenerate: `web/src/lib/openapi-types.ts`

**Interfaces:**
- Produces `ModelFamilyService.Create`, `Update`, `AddMember`, `RemoveMember`, `Get`, and `List`.
- Public IDs are UUIDs; internal SKU IDs are resolved server-side.

- [ ] **Step 1: Write failing service tests**

Cover cross-Product membership, duplicate active membership rejection, removal/re-add auditability, archived-family mutation denial, model-code uniqueness, strict common-structure schema, strict variation dimensions, transactions, and concurrent add attempts.

```go
_, err := service.AddMember(ctx, familyA.PublicID, sku.PublicID, operator.ID)
if err != nil { t.Fatal(err) }
_, err = service.AddMember(ctx, familyB.PublicID, sku.PublicID, operator.ID)
if !errors.Is(err, sop.ErrSKUAlreadyInModelFamily) { t.Fatalf("error = %v", err) }
```

- [ ] **Step 2: Run RED**

Run: `cd api && GOCACHE=/private/tmp/cargoflows-go-cache go test -race ./internal/sop -run 'ModelFamily' -count=1`
Expected: FAIL because `ModelFamilyService` is undefined.

- [ ] **Step 3: Implement lifecycle service**

Use transactions and `clause.Locking{Strength:"UPDATE"}` around family and membership rows. Normalize identity dimensions to this allow-list in the first release:

```go
var allowedVariationDimensions = map[string]struct{}{
    "color": {}, "material": {}, "finish": {}, "texture": {},
    "trim": {}, "ports": {}, "controls": {}, "labels": {},
    "accessories": {}, "packaging": {}, "other": {},
}
```

Every add/remove/archive mutation writes `AIAuditEvent` metadata containing public IDs and action only, never internal IDs.

- [ ] **Step 4: Add admin/operator HTTP contracts and RBAC tests**

Endpoints:

- `POST /api/v1/model-families` — admin;
- `GET /api/v1/model-families` — authenticated read;
- `GET /api/v1/model-families/{family_id}` — authenticated read;
- `PATCH /api/v1/model-families/{family_id}` — admin;
- `POST /api/v1/model-families/{family_id}/members` — admin/operator;
- `DELETE /api/v1/model-families/{family_id}/members/{member_id}` — admin/operator.

Assert viewer/photographer mutation denial, malformed UUID rejection, safe conflicts, and no internal relationship IDs in JSON.

- [ ] **Step 5: Run GREEN, regenerate types, and commit**

Run: `cd api && GOCACHE=/private/tmp/cargoflows-go-cache go test -race ./internal/sop ./internal/app -run 'ModelFamily' -count=1 && cd ../web && pnpm generate:api && pnpm typecheck`
Expected: PASS.

```bash
git add api/internal/sop api/internal/app api/openapi.yaml web/src/lib/openapi-types.ts
git commit -m "feat(api): manage model family membership"
```

### Task 3: Implement Versioned Identity Manifests and Difference Regions

**Files:**
- Create: `api/internal/sop/variant_manifests.go`
- Create: `api/internal/sop/variant_manifests_test.go`
- Modify: `api/internal/app/model_family_handlers.go`
- Modify: `api/internal/app/model_family_handlers_test.go`
- Modify: `api/openapi.yaml`
- Regenerate: `web/src/lib/openapi-types.ts`

**Interfaces:**
- Produces `VariantManifestService.CreateDraft`, `UpdateDraft`, `CopyVersion`, `Validate`, `Publish`, and `GetForSKU`.
- `Validate` returns stable issue codes and blocks publication when evidence requirements are malformed.

- [ ] **Step 1: Write failing validation and lifecycle tests**

Test exact typed identity fields, one draft per manifest, copy-on-write versions, immutable published versions, normalized rectangle/polygon bounds, strictness enum, non-empty exact-region required view keys, allowed variation dimensions, actor metadata, and cross-family/SKU rejection.

```go
issues := service.Validate(ctx, version.PublicID)
if !slices.Contains(issueCodes(issues), "exact_region_view_required") { t.Fatalf("issues = %#v", issues) }
```

- [ ] **Step 2: Run RED**

Run: `cd api && GOCACHE=/private/tmp/cargoflows-go-cache go test ./internal/sop -run 'VariantManifest|DifferenceRegion' -count=1`
Expected: FAIL because manifest lifecycle methods do not exist.

- [ ] **Step 3: Implement strict identity document validation**

Decode with `DisallowUnknownFields` into:

```go
type VariantIdentityDocumentV1 struct {
    Schema string `json:"schema"`
    Colors []VariantColorRegion `json:"colors"`
    Material string `json:"material"`
    Finish string `json:"finish"`
    Texture string `json:"texture"`
    Labels []VariantLabel `json:"labels"`
    Ports []VariantFeature `json:"ports"`
    Controls []VariantFeature `json:"controls"`
    Accessories []string `json:"accessories"`
    Packaging []VariantFeature `json:"packaging"`
    Other []VariantFeature `json:"other"`
    MustProveWithTargetAssets []string `json:"must_prove_with_target_assets"`
}
```

Require `schema == "variant_identity_v1"`; trim and bound all strings; reject URLs, credentials, object keys, unknown fields, duplicate region keys, invalid color values, and dimensions not published by the family.

- [ ] **Step 4: Add DTOs/endpoints and authorization tests**

Endpoints:

- `GET /api/v1/skus/{sku_id}/variant-identity`;
- `POST /api/v1/skus/{sku_id}/variant-identity/versions`;
- `PATCH /api/v1/variant-identity-versions/{version_id}`;
- `POST /api/v1/variant-identity-versions/{version_id}/validate`;
- `POST /api/v1/variant-identity-versions/{version_id}/publish`.

Only admin/operator may mutate or publish. Photographers may read the resolved capture requirements; viewers receive read-only manifest facts without internal IDs or private locators.

- [ ] **Step 5: Run GREEN and commit**

Run: `cd api && GOCACHE=/private/tmp/cargoflows-go-cache go test ./internal/sop ./internal/app -run 'VariantManifest|DifferenceRegion' -count=1 && cd ../web && pnpm generate:api && pnpm typecheck`
Expected: PASS.

```bash
git add api/internal/sop api/internal/app api/openapi.yaml web/src/lib/openapi-types.ts
git commit -m "feat(sop): add versioned SKU identity manifests"
```

### Task 4: Resolve Category, Family, and Variant SOP Layers

**Files:**
- Modify: `api/internal/models/model_family.go`
- Create: `api/internal/sop/family_sops.go`
- Create: `api/internal/sop/family_sops_test.go`
- Create: `api/internal/sop/resolver.go`
- Create: `api/internal/sop/resolver_test.go`
- Modify: `api/internal/models/models.go`
- Modify: `api/internal/database/database.go`
- Modify: `api/internal/app/model_family_handlers.go`
- Modify: `api/openapi.yaml`
- Regenerate: `web/src/lib/openapi-types.ts`

**Interfaces:**
- Produces `ModelFamilySOP`, immutable versions, full-spec `ModelFamilySOPViewOverride`, and `ResolveSOP(base, family, manifest) (ResolvedSOPV1, error)`.
- Extends `PhotoSession` with frozen family/manifest version public provenance, `ResolvedSOPJSON`, and `ResolvedSOPSHA256`.

- [ ] **Step 1: Write failing resolver golden tests**

Cover inherit/replace/add/disable by stable `view_key`, deterministic ordering, locked base SOP version, variant-derived mandatory views, inability to disable exact-region views, coordinate validation, duplicate keys, canonical JSON/hash stability, and per-view provenance.

```go
resolved, err := ResolveSOP(base, family, manifest)
if err != nil { t.Fatal(err) }
if got := resolved.ViewKeys(); !reflect.DeepEqual(got, []string{"front", "right_ports", "texture_detail"}) { t.Fatalf("keys = %v", got) }
if got := resolved.View("right_ports").Provenance.Source; got != "variant_requirement" { t.Fatalf("source = %s", got) }
```

- [ ] **Step 2: Run RED**

Run: `cd api && GOCACHE=/private/tmp/cargoflows-go-cache go test ./internal/sop -run 'FamilySOP|ResolveSOP' -count=1`
Expected: FAIL because family SOP models and resolver are missing.

- [ ] **Step 3: Implement family SOP lifecycle and pure resolver**

Use full replacement specs rather than nullable per-field patches:

```go
type ModelFamilySOPViewOverride struct {
    ViewKey string `json:"view_key"`
    Operation string `json:"operation"` // replace, add, disable
    Spec *ResolvedSOPViewV1 `json:"spec,omitempty"`
}
```

`replace` and `add` require a complete valid spec; `disable` forbids a spec and cannot remove a view required by an exact region. Hash canonical JSON with SHA-256.

- [ ] **Step 4: Freeze resolved SOP on photo-session creation**

Update photo-session service tests so creating a session for a family SKU locks the current published category SOP, published family SOP, and published identity manifest. Persist complete resolved JSON and hash; do not resolve dynamically when reading the session.

- [ ] **Step 5: Add family SOP version endpoints, run GREEN, and commit**

Add create/copy/update/validate/publish/read endpoints under `/api/v1/model-families/{family_id}/capture-sop` and `/api/v1/model-family-sop-versions/{version_id}` with the existing manager-role pattern.

Run: `cd api && GOCACHE=/private/tmp/cargoflows-go-cache go test ./internal/sop ./internal/app ./internal/database -run 'FamilySOP|ResolveSOP|PhotoSession' -count=1 && cd ../web && pnpm generate:api && pnpm typecheck`
Expected: PASS.

```bash
git add api/internal/models api/internal/database api/internal/sop api/internal/app api/openapi.yaml web/src/lib/openapi-types.ts
git commit -m "feat(sop): resolve family and variant capture layers"
```

### Task 5: Add Reference Grants and Deterministic Evidence Preflight

**Files:**
- Modify: `api/internal/models/model_family.go`
- Create: `api/internal/sop/reference_grants.go`
- Create: `api/internal/sop/reference_grants_test.go`
- Create: `api/internal/ai/variant_evidence.go`
- Create: `api/internal/ai/variant_evidence_test.go`
- Modify: `api/internal/database/database.go`
- Modify: `api/internal/app/model_family_handlers.go`
- Modify: `api/openapi.yaml`
- Regenerate: `web/src/lib/openapi-types.ts`

**Interfaces:**
- Produces `ModelFamilyReferenceAsset` grants and `BuildVariantEvidence(ctx, VariantEvidenceInput) (VariantEvidenceMatrixV1, error)`.
- Stable errors map to the nine public preflight codes from the design spec.

- [ ] **Step 1: Write failing grant/evidence tests**

Cover source asset approval, same-family membership, source/target distinction, role allow-list, forbidden appearance role, allowed region validation, grant revocation, exact-region coverage by target assets, missing identity anchor, target conflict priority, no-billing error classification, and deterministic matrix hashing.

```go
matrix, err := builder.Build(ctx, input)
if err != nil { t.Fatal(err) }
if got := matrix.Property("color").Authority; got != "target_identity" { t.Fatalf("authority = %s", got) }
if slices.Contains(matrix.Reference("family-side").AllowedAttributes, "color") { t.Fatal("family reference inherited color") }
```

- [ ] **Step 2: Run RED**

Run: `cd api && GOCACHE=/private/tmp/cargoflows-go-cache go test ./internal/sop ./internal/ai -run 'ReferenceGrant|VariantEvidence' -count=1`
Expected: FAIL because grants and evidence builder are undefined.

- [ ] **Step 3: Implement reference grants and evidence matrix**

Reference roles are exact constants:

```go
const (
    ReferenceGeometryOnly ReferenceRole = "geometry_only"
    ReferenceViewpointOnly ReferenceRole = "viewpoint_only"
    ReferenceDetailGeometry ReferenceRole = "detail_geometry"
)
```

The matrix records public references, content hashes, view keys, allowed/forbidden attributes, covered regions, and authority. It keeps internal object locators in a separate worker-only load plan that is never serialized into execution/audit metadata. Reference grants require the immutable verified asset metadata created in Task 1 rather than trusting URLs or caller declarations.

- [ ] **Step 4: Add grant endpoints and safe preflight DTOs**

Endpoints:

- `POST /api/v1/model-families/{family_id}/reference-assets`;
- `GET /api/v1/model-families/{family_id}/reference-assets`;
- `DELETE /api/v1/model-families/{family_id}/reference-assets/{grant_id}`;
- `POST /api/v1/skus/{sku_id}/ai-image-preflight`.

Admin/operator may grant/revoke; photographer/viewer may read only. Preflight returns issue codes, affected public region/view keys, and remediation text without object keys.

- [ ] **Step 5: Run GREEN and commit**

Run: `cd api && GOCACHE=/private/tmp/cargoflows-go-cache go test -race ./internal/sop ./internal/ai ./internal/app -run 'ReferenceGrant|VariantEvidence|ImagePreflight' -count=1 && cd ../web && pnpm generate:api && pnpm typecheck`
Expected: PASS.

```bash
git add api/internal/models api/internal/database api/internal/sop api/internal/ai api/internal/app api/openapi.yaml web/src/lib/openapi-types.ts
git commit -m "feat(ai): build variant-safe image evidence"
```

### Task 6: Freeze Variant-aware AI Job Snapshot V2

**Files:**
- Modify: `api/internal/ai/jobs.go`
- Modify: `api/internal/ai/jobs_test.go`
- Modify: `api/internal/ai/queue_test.go`
- Modify: `api/internal/ai/prompt_compiler.go`
- Modify: `api/internal/ai/prompt_compiler_test.go`
- Modify: `api/internal/ai/text_executor.go`
- Modify: `api/internal/ai/text_executor_test.go`
- Modify: `api/internal/ai/image_prompt_compiler.go`
- Modify: `api/internal/ai/image_prompt_compiler_test.go`

**Interfaces:**
- Produces `ProductSnapshotV2` with `ModelFamilyFacts`, `VariantIdentityFacts`, `ResolvedSOPV1`, and `VariantEvidenceMatrixV1`.
- Removes runtime dependence on mutable current family/manifests for a queued or historical task.

- [ ] **Step 1: Write failing snapshot and mixed-job tests**

Prove image jobs require and freeze V2 family evidence, text-only jobs can proceed without image evidence, selected image slots only trigger image preflight, later manifest/SOP/grant edits do not change snapshot bytes, and concurrent publication cannot mix versions.

- [ ] **Step 2: Run RED**

Run: `cd api && GOCACHE=/private/tmp/cargoflows-go-cache go test -race ./internal/ai -run 'ProductSnapshotV2|VariantSnapshot|MixedVariantJob' -count=1`
Expected: FAIL because V2 snapshot creation is missing.

- [ ] **Step 3: Implement V2 snapshot creation**

Use `ProductSnapshotSchemaV2 = "cargoflows_product_generation_v2"`. Within one transaction, lock target SKU, active membership, family, published category/family SOP versions, published manifest version, exact regions, selected target assets, and active reference grants. Build the evidence matrix before inserting the job.

Text-only jobs include published family invariants and target identity facts but leave image evidence empty. Update the text prompt compiler and executor to consume V2, use target identity plus explicitly published invariants only, and exclude sibling image/reference appearance. Because legacy AI jobs are outside the contract, workers reject V1 jobs with a safe unsupported-snapshot error instead of maintaining dual compiler paths. Image preflight failures return stable user-correctable errors and insert no job/provider execution.

- [ ] **Step 4: Update image compiler input facts**

Replace generic `source_1` descriptors with evidence descriptors containing public role labels, source SKU public references, allowed/forbidden attributes, region/view keys, derivative hash, and authority. Keep worker-only object locators outside `CompiledImagePrompt`.

- [ ] **Step 5: Run GREEN and commit**

Run: `cd api && GOCACHE=/private/tmp/cargoflows-go-cache go test -race ./internal/ai -run 'ProductSnapshotV2|VariantSnapshot|MixedVariantJob|CompileImagePrompt' -count=3`
Expected: PASS.

```bash
git add api/internal/ai
git commit -m "feat(ai): freeze variant-aware job evidence"
```

### Task 7: Create Private Restricted Reference Derivatives

**Files:**
- Create: `api/internal/ai/reference_derivative.go`
- Create: `api/internal/ai/reference_derivative_test.go`
- Modify: `api/internal/ai/image_storage.go`
- Modify: `api/internal/config/config.go`
- Modify: `api/cmd/worker/main.go`
- Modify: `docker-compose.yml`
- Modify: `api/go.mod`
- Modify: `api/go.sum`

**Interfaces:**
- Produces `ReferenceDerivativeService.Create(ctx, DerivativeRequest) (DerivativeDescriptor, error)`.
- Consumes the private `ImageObjectStore` and validated source bytes from the multi-turn image storage task.

- [ ] **Step 1: Write failing deterministic image-transform tests**

Use fixture PNG/JPEG/WebP images to prove geometry-only grayscale conversion, normalized rectangle/polygon masking, unchanged dimensions, no EXIF carry-over, safe output PNG, stable transform-version hash, idempotent same-input storage, and rejection of regions outside bounds or beyond pixel limits.

```go
descriptor, err := service.Create(ctx, DerivativeRequest{
    Role: ReferenceGeometryOnly,
    ForbiddenRegions: []NormalizedPolygon{portRegion},
})
if err != nil { t.Fatal(err) }
if descriptor.TransformVersion != "reference-derivative-v1" { t.Fatalf("version = %s", descriptor.TransformVersion) }
if descriptor.SourceSHA256 == descriptor.OutputSHA256 { t.Fatal("derivative hash equals source hash") }
```

- [ ] **Step 2: Run RED**

Run: `cd api && GOCACHE=/private/tmp/cargoflows-go-cache go test ./internal/ai -run 'ReferenceDerivative' -count=1`
Expected: FAIL because the derivative service is missing.

- [ ] **Step 3: Implement deterministic transforms**

Decode only validated PNG/JPEG/WebP inputs, using `golang.org/x/image/webp` for WebP decode support. Convert geometry-only references using luminance while preserving alpha. Fill forbidden regions with a neutral checker-free gray that conveys no appearance detail. Strip metadata by decoding and re-encoding. Store under:

`derivatives/{family_public_id}/{grant_public_id}/{transform_version}-{source_sha256}-{policy_sha256}.png`

Never accept caller-provided output keys.

- [ ] **Step 4: Prove private storage policy**

Use disposable MinIO to assert worker-authenticated read/write succeeds and anonymous GET fails for derivative objects. Verify source public bucket policy does not propagate to the AI-private bucket.

- [ ] **Step 5: Run GREEN and commit**

Run: `cd api && GOCACHE=/private/tmp/cargoflows-go-cache go test ./internal/ai -run 'ReferenceDerivative|ImageStorage' -count=1 && cd .. && docker compose config --quiet && git diff --check`
Expected: PASS.

```bash
git add api/internal/ai api/internal/config api/cmd/worker docker-compose.yml
git commit -m "feat(ai): restrict cross-SKU reference images"
```

### Task 8: Enforce Role-labelled Provider Inputs in Multi-turn Execution

**Files:**
- Modify: `api/internal/ai/openai_image_responses_client.go`
- Modify: `api/internal/ai/openai_image_responses_client_test.go`
- Modify: `api/internal/ai/image_executor.go`
- Modify: `api/internal/ai/image_executor_test.go`
- Modify: `api/internal/ai/image_turn_queue_test.go`

**Interfaces:**
- `ImageInput` gains server-generated `RoleLabel` and evidence public reference.
- Executor ordering is parent, target anchors, target regions, restricted family derivatives, then SOP references.

- [ ] **Step 1: Write failing provider/executor ordering tests**

Assert label/image interleaving, target authority wording, no sibling original bytes, parent-first edit, restart without parent, frozen-evidence reuse on edits, exact-region coverage recheck, zero calls on preflight failure, per-candidate billing, and no internal locator leakage.

- [ ] **Step 2: Run RED**

Run: `cd api && GOCACHE=/private/tmp/cargoflows-go-cache go test -race ./internal/ai -run 'VariantImageExecutor|RoleLabelledImage|ImageTurnQueue' -count=1`
Expected: FAIL because role-labelled inputs and derivative loading are not wired.

- [ ] **Step 3: Interleave trusted labels and input images**

Emit content blocks in pairs:

```go
content = append(content,
    map[string]any{"type":"input_text", "text": input.RoleLabel},
    map[string]any{"type":"input_image", "image_url": dataURL},
)
```

Role labels are compiled by CargoFlows from frozen evidence, never copied from user text. Reject empty, duplicate, reordered, or mismatched descriptors before sending.

- [ ] **Step 4: Wire executor and audit metadata**

Load target originals from private/internal storage, create or reuse reference derivatives, validate all bytes, and send the exact frozen order. Persist public evidence references, input hashes, derivative transform versions, prompt hash, provider IDs, usage, and actor. Clear credential and image buffers after each call.

- [ ] **Step 5: Run race/recovery GREEN and commit**

Run: `cd api && GOCACHE=/private/tmp/cargoflows-go-cache go test -race ./internal/ai -run 'VariantImageExecutor|RoleLabelledImage|ImageTurnQueue|Lease|Recovery' -count=5`
Expected: PASS.

```bash
git add api/internal/ai
git commit -m "feat(ai): execute variant-safe image turns"
```

### Task 9: Build Web Model-family, Identity, SOP, and Evidence Workflows

**Files:**
- Create: `web/src/app/(dashboard)/model-families/page.tsx`
- Create: `web/src/app/(dashboard)/model-families/page.test.tsx`
- Create: `web/src/app/(dashboard)/model-families/[familyId]/page.tsx`
- Create: `web/src/app/(dashboard)/model-families/[familyId]/page.test.tsx`
- Create: `web/src/components/model-families/identity-manifest-editor.tsx`
- Create: `web/src/components/model-families/identity-manifest-editor.test.tsx`
- Create: `web/src/components/model-families/difference-region-editor.tsx`
- Create: `web/src/components/model-families/reference-grant-panel.tsx`
- Modify: `web/src/app/(dashboard)/ai-jobs/new/page.tsx`
- Modify: `web/src/app/(dashboard)/ai-jobs/[jobId]/page.tsx`
- Modify: `web/src/components/app-shell.tsx`
- Modify: `web/src/lib/i18n.tsx`

**Interfaces:**
- Consumes generated OpenAPI types and exposes family/member management, versioned manifest/SOP editing, region evidence linking, reference grants, preflight issues, and variant-aware image review.

- [ ] **Step 1: Write failing bilingual UI tests**

Cover cross-Product member search, one-family conflict, draft/published badges, typed identity fields, exact-region view/evidence requirements, normalized region editor bounds, structure-only grant warnings, source SKU labelling, preflight issue remediation, role-based disabled actions, mobile layout, keyboard operation, and absence of object keys/URLs.

- [ ] **Step 2: Run RED**

Run: `cd web && pnpm test -- 'src/app/(dashboard)/model-families' src/components/model-families`
Expected: FAIL because components do not exist.

- [ ] **Step 3: Implement family and manifest workbench**

Use separate tabs for Overview, Members, Identity manifests, Family SOP, and Structural references. Keep version publication explicit. Difference-region drawing supports keyboard numeric coordinates as an accessible alternative to pointer drawing.

- [ ] **Step 4: Integrate AI preflight and result review**

Before image-job submission, show target identity coverage and reference roles. Block only image slots with issue codes while allowing selected title/SEO slots. In result review, render target evidence, generated result, and structure-only references with the checklist for color, texture, logo, label, ports, controls, packaging, and mirroring.

- [ ] **Step 5: Run Web verification and commit**

Run: `cd web && pnpm test && pnpm typecheck && pnpm lint && pnpm exec next build --webpack`
Expected: all checks pass.

```bash
git add web/src/app web/src/components/model-families web/src/components/app-shell.tsx web/src/lib/i18n.tsx
git commit -m "feat(web): manage variant-aware model families"
```

### Task 10: Verify Variant-safe Full-stack Workflow and Security

**Files:**
- Modify: `api/cmd/fake-openai/main.go`
- Create: `api/internal/ai/variant_image_integration_test.go`
- Create: `web/tests/e2e/model-family-image-generation.spec.ts`
- Create: `scripts/run-model-family-ai-e2e.sh`
- Modify: `README.md`
- Modify: `docker-compose.yml`

**Interfaces:**
- Produces a repeatable loopback-only, disposable full-stack acceptance path with MySQL, private MinIO, API, worker, Web, and fake Responses provider.

- [ ] **Step 1: Extend fake provider with sanitized input inspection**

Record role labels, MIME types, image hashes, action, and input count only. Never record bearer credentials, base64 bodies, prompt bodies, or image bytes. Return deterministic test PNGs for generate/edit.

- [ ] **Step 2: Add the full Playwright workflow**

Create two products/SKUs in one family: black reference and blue target with a changed port. Publish family SOP and blue identity manifest, upload blue full view and port detail, approve a black geometry-only grant, run one selected image slot, edit candidate 1, restart, and verify all candidates remain visible. Assert title/SEO can run when image evidence is intentionally incomplete.

- [ ] **Step 3: Assert provider and storage boundaries**

Through test-only sanitized facts, prove target images precede the restricted grayscale/masked derivative, sibling original hash was never sent, changed-port region was masked, edit parent is first, restart has no generated parent, unselected slots caused no calls, and anonymous derivative/result GET fails.

- [ ] **Step 4: Run complete verification**

Run:

```bash
cd api
GOCACHE=/private/tmp/cargoflows-go-cache go test -race ./... -count=1
GOCACHE=/private/tmp/cargoflows-go-cache go vet ./...
cd ../web
pnpm test
pnpm typecheck
pnpm lint
pnpm exec next build --webpack
cd ..
docker compose config --quiet
./scripts/run-model-family-ai-e2e.sh
git diff --check
```

Expected: all commands pass; Playwright reports the model-family image scenario passed; the isolated Compose project and volumes are removed.

- [ ] **Step 5: Security review and commit**

Review key/image leakage, cross-family access, unauthorized grants, stale versions, incorrect evidence authority, derivative privacy, prompt injection, duplicate billing, ambiguous outcomes, and silent history replacement. Resolve every P1/P2 before committing.

```bash
git add api/cmd/fake-openai api/internal/ai/variant_image_integration_test.go web/tests/e2e/model-family-image-generation.spec.ts scripts/run-model-family-ai-e2e.sh README.md docker-compose.yml
git commit -m "test: verify variant-safe model family workflow"
```

## Completion Gate

- Cross-Product SKUs can join one explicit family while each SKU has only one active membership.
- A published target identity manifest and approved identity anchor are required for image generation.
- Exact difference regions require target evidence and cannot be waived by templates or users.
- Family SOP views resolve deterministically over a locked category SOP version.
- Cross-SKU references require approved narrow grants and are sent only as private restricted derivatives.
- Target evidence outranks family references in snapshots, prompts, provider order, review UI, and audit.
- Missing image evidence incurs zero OpenAI calls while eligible title/SEO work remains available.
- Edits use frozen evidence; restarts exclude generated parents; every historical candidate remains visible.
- All provider calls use the shared encrypted administrator key and unified usage/audit ledger.
- Go race/vet, Web tests/type/lint/build, private storage checks, isolated E2E, and security review pass.
