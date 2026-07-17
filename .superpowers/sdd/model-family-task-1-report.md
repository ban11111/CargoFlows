# Task 1 Report — Greenfield Model-family and Variant Identity Schema

Commit: `d0206af feat(catalog): add model family identity schema`

Post-review remediation: included in the follow-up public-identity contract commit that contains this report update.

## Implementation summary

- Added model-family, membership, per-SKU identity-manifest, immutable manifest-version, difference-region, and unique evidence-asset mapping models with lifecycle enums, UUID public IDs, safe JSON defaults, and named uniqueness/check constraints.
- Registered all greenfield tables in normal schema migration without any legacy-data backfill path.
- Added UUID public IDs for SKU and Asset. Internal primary IDs are hidden by the model JSON serializers.
- Added immutable-on-update Asset MIME type, dimensions, byte count, and SHA-256 fields. Upload completion now reads the stored object, validates it with the shared image validator, binds the normalized declared content type into the signed completion ticket, saves detected metadata, and returns a generic rejection for invalid or mismatched images.
- Added serialization, migration/constraint, metadata immutability, successful upload metadata, and declared-content-type mismatch tests.

## Files changed

- `api/internal/models/model_family.go`
- `api/internal/models/model_family_test.go`
- `api/internal/models/models.go`
- `api/internal/database/database.go`
- `api/internal/database/model_family_test.go`
- `api/internal/app/handlers.go`
- `api/internal/app/object_store.go`
- `api/internal/app/photo_session_test.go`

`object_store.go` was additionally changed because the upload-completion handler needs the existing object-store `ReadSource` boundary to invoke the shared validator.

## RED evidence

Command:

```sh
cd api && GOCACHE=/private/tmp/cargoflow-go-cache go test ./internal/models ./internal/database -run 'ModelFamily|VariantIdentity|DifferenceRegion' -count=1
```

Result: failed as intended before implementation with undefined `VariantIdentityManifestVersion`, `VariantManifestDraft`, `ModelFamily`, `ModelFamilyMember`, `VariantIdentityManifest`, `VariantDifferenceRegion`, `VariantDifferenceRegionEvidenceAsset`, and SKU/Asset `PublicID`/`BeforeCreate` members.

Upload metadata RED command:

```sh
cd api && GOCACHE=/private/tmp/cargoflow-go-cache go test ./internal/app -run 'AssetMetadata|MismatchedDeclaredContentType' -count=1
```

Result: failed as intended before handler changes: completed assets had empty metadata and a PNG uploaded under an `image/jpeg` ticket was accepted with HTTP 201.

Metadata immutability RED command:

```sh
cd api && GOCACHE=/private/tmp/cargoflow-go-cache go test ./internal/database -run 'AssetMetadataIsImmutable' -count=1
```

Result: failed as intended before `gorm:"<-:create"` protections: a `Save` changed MIME type, dimensions, byte count, and SHA-256.

## GREEN and full verification

Focused GREEN:

```sh
cd api && GOCACHE=/private/tmp/cargoflow-go-cache go test ./internal/models ./internal/database ./internal/app -run 'ModelFamily|VariantIdentity|DifferenceRegion|AssetMetadata|Migrate' -count=1
```

Result: PASS for models, database, and app packages.

Focused declared-type regression:

```sh
cd api && GOCACHE=/private/tmp/cargoflow-go-cache go test ./internal/app -run 'MismatchedDeclaredContentType' -count=1
```

Result: PASS.

Full verification (run outside the sandbox because existing `httptest` suites need loopback socket binding):

```sh
cd api && GOCACHE=/private/tmp/cargoflow-go-cache go test ./... -count=1
cd api && GOCACHE=/private/tmp/cargoflow-go-cache go vet ./...
cd api && git diff --check
```

Result: PASS. The in-sandbox full-suite attempt only failed where existing `httptest` cases were denied loopback binding; the elevated rerun passed every package.

## Self-review

- Cross-SKU identity constraints: active family membership is unique per SKU via `idx_active_family_member`; a historical removed membership clears the guard. Manifest versions are unique per manifest/version and limited to one draft guard. The manifest has one row per SKU, with family/membership cross-row checks intentionally left to the next lifecycle service.
- Asset metadata integrity: the only new completion path derives metadata from the uploaded bytes, verifies the signed declared MIME type, and maps invalid input to a generic safe error. ORM create-only tags prevent ordinary GORM updates from modifying the persisted metadata.
- Public IDs: all new model identifiers and SKU/Asset public contracts use opaque UUID fields. The post-review remediation extended that boundary through the existing catalog, review, photo-session, AI-job, platform-content, OpenAPI, and Web flows so the migration is not partial.
- Validation safety: validator errors are not exposed to clients; object-storage read failures remain a generic availability response.

## Concerns

- GORM create-only metadata protections cannot prevent a privileged raw SQL writer from mutating rows. The application has no raw-SQL update path for Asset metadata; database-level immutable-column triggers were not added because the task specifies normal GORM schema migrations and service ownership of cross-row semantics.
- Asset source bytes remain in private object storage and are exposed to the Web UI only through the authenticated `/assets/{asset_id}/media` endpoint. The signed PUT URL necessarily addresses the upload destination, but responses, completion tickets, stored AI snapshots, job payloads, review DTOs, and platform-content DTOs no longer expose a separate object key or original/thumbnail storage URL.

## Post-review public identity remediation

The Important review findings were addressed atomically across the API, persisted AI contracts, OpenAPI, generated Web types, and all current Web callers:

- SKU, Asset, photo-session, asset-review, AI-job, selected-asset, and platform-content public contracts now use canonical UUID strings. Internal numeric primary/foreign keys remain database-only.
- AI job snapshots carry only SKU/Asset public UUIDs plus safe immutable asset metadata. They no longer contain numeric asset IDs, object keys, original URLs, or thumbnail URLs.
- SKU and asset response DTOs are explicit allowlists. Asset upload completion and review responses use `public_id` plus an authenticated same-origin `media_url`; tag DTOs no longer expose internal IDs.
- Signed capture upload tickets bind the actor through an HMAC, the public session/view UUIDs, a public upload nonce, normalized content type, and expiry. They no longer serialize an internal actor ID or object key. HEIC/HEIF is rejected before a signed URL is issued.
- Every Task 1 `BeforeCreate` public-ID hook rejects invalid caller-supplied values and canonicalizes valid UUIDs.

Post-review RED evidence included failures showing that numeric SKU routes were accepted, AI requests still required numeric IDs, upload responses exposed object locators, media responses lacked safe headers, and UUID platform-content routes failed. The following focused GREEN command now passes:

```sh
cd api && GOCACHE=/private/tmp/cargoflow-go-cache go test ./internal/ai ./internal/app ./internal/models ./internal/database -run 'TestCompileImagePromptGolden|TestCreateJobSnapshotsOnlyWhitelistedFactsAndSelectedSlots|TestCreateJobRejectsInvalidTemplateSlotsAndAssetsWithoutWriting|TestAIJobEndpointsUseTypedArraysUUIDsAndSafeDTOs|TestSKURoutesUsePublicUUIDAndSafeDTOs|TestAssetMediaRequiresAuthenticationAndSetsSafeHeaders|TestOpenAPI|TestEveryNewPublicIDHookRejectsCallerSuppliedNonUUIDs' -count=1
```

Final post-review verification:

```sh
cd api && GOCACHE=/private/tmp/cargoflow-go-cache go test ./... -count=1
cd api && GOCACHE=/private/tmp/cargoflow-go-cache go vet ./...
cd web && pnpm test
cd web && pnpm run lint
cd web && pnpm run typecheck
git diff --check
```

Result: all Go packages passed; Web passed 13 files / 77 tests; lint, TypeScript, vet, and whitespace verification passed.
