# OpenAI Content Phase 1 Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the secure, versioned, asynchronous foundation for CargoFlow's Web-only OpenAI content workflow, ending with an auditable dry-run job that never sends product content to OpenAI.

**Architecture:** Extend the existing Go modular monolith with focused `secrets` and `ai` packages, admin-only HTTP handlers, GORM models, and a separate MySQL-leasing worker command. Extend the Next.js admin with credential, template, and dry-run job screens; all endpoints remain behind the existing Web BFF and bearer authorization.

**Tech Stack:** Go 1.24, Gin, GORM, MySQL 8.4, SQLite test databases, AES-256-GCM from the Go standard library, Next.js 16, React 19, TypeScript, TanStack Query, Zod, Vitest, Playwright, OpenAPI 3.0.

**Design:** `docs/superpowers/specs/2026-07-17-openai-product-content-generation-design.md`

## Global Constraints

- The system uses one shared OpenAI Project API key; only `admin` may configure, validate, rotate, or disable it.
- Never return or log API-key plaintext, ciphertext, nonce, bearer authorization, signed MinIO URLs, or storage credentials.
- Encrypt the API key with AES-256-GCM using a 32-byte master key decoded from `CARGOFLOW_SECRETS_MASTER_KEY`; do not provide a production default.
- Templates are bilingual, versioned, and immutable after publication; jobs bind only to published versions.
- A job represents one exact SKU and only user-selected slots.
- Phase 1 performs dry-run execution only: it snapshots and audits inputs but does not upload images or send product content to OpenAI.
- All new Web labels, validation messages, empty states, and navigation items must exist in Simplified Chinese and English and switch immediately.
- Use UUID public identifiers in HTTP APIs; keep GORM integer IDs internal.
- Follow TDD: add a failing focused test, observe the intended failure, implement the minimum behavior, then run the focused and package test suites.
- Do not modify iOS in this phase.

---

## File and Responsibility Map

### Go API

- `api/internal/secrets/aesgcm.go`: authenticated encryption primitive and serialized encrypted value.
- `api/internal/secrets/aesgcm_test.go`: encryption, tamper, wrong-key, and input-validation tests.
- `api/internal/config/config.go`: AI master key, OpenAI base URL, and dry-run worker configuration.
- `api/internal/config/config_test.go`: environment-loading assertions.
- `api/internal/models/ai.go`: credential, template, slot, job, item, execution, audit, and usage models/enums.
- `api/internal/database/database.go`: migration registration only; do not seed a real key.
- `api/internal/database/database_test.go`: AI table migration coverage.
- `api/internal/ai/provider_settings.go`: encrypted setting lifecycle and credential verifier interface.
- `api/internal/ai/provider_settings_test.go`: masking, rotation, disablement, and verifier tests.
- `api/internal/ai/templates.go`: template/version/slot validation and lifecycle.
- `api/internal/ai/templates_test.go`: publication invariants and immutability tests.
- `api/internal/ai/jobs.go`: dry-run input snapshot, selected-slot validation, and job creation.
- `api/internal/ai/jobs_test.go`: same-SKU approved-asset and published-template tests.
- `api/internal/ai/queue.go`: MySQL/SQLite-compatible lease, heartbeat, completion, and failure operations.
- `api/internal/ai/queue_test.go`: concurrent lease and expiry recovery tests.
- `api/internal/ai/worker.go`: provider-neutral worker loop and dry-run executor.
- `api/internal/ai/worker_test.go`: item/job aggregation and idempotent dry-run tests.
- `api/internal/app/ai_dto.go`: stable request/response DTOs with no secret fields.
- `api/internal/app/ai_handlers.go`: credential, template, and dry-run job handlers.
- `api/internal/app/ai_handlers_test.go`: RBAC and HTTP behavior.
- `api/internal/app/router.go`: dependency construction and route registration.
- `api/cmd/worker/main.go`: standalone worker process.
- `api/openapi.yaml`: exact new HTTP contract.
- `api/.env.example`: variable names and safe non-secret examples.
- `docker-compose.yml`: worker service and master-key variable plumbing.

### Web

- `web/src/lib/ai-schemas.ts`: Zod schemas and form types.
- `web/src/lib/ai-schemas.test.ts`: bilingual/template/job form validation tests.
- `web/src/lib/i18n.tsx`: complete Chinese/English AI copy.
- `web/src/components/app-shell.tsx`: AI template and OpenAI settings navigation.
- `web/src/app/(dashboard)/settings/openai/page.tsx`: masked credential administration.
- `web/src/app/(dashboard)/settings/openai/page.test.tsx`: no-echo and RBAC/error behavior.
- `web/src/app/(dashboard)/ai-templates/page.tsx`: versioned template list.
- `web/src/app/(dashboard)/ai-templates/page.test.tsx`: lifecycle rendering tests.
- `web/src/app/(dashboard)/ai-templates/new/page.tsx`: V1 template/slot draft form.
- `web/src/app/(dashboard)/ai-templates/new/page.test.tsx`: validation and submission tests.
- `web/src/app/(dashboard)/ai-jobs/new/page.tsx`: SKU/template/slot dry-run wizard.
- `web/src/app/(dashboard)/ai-jobs/new/page.test.tsx`: optional-slot and confirmation tests.
- `web/src/app/(dashboard)/ai-jobs/page.tsx`: replace mock data with the real API.
- `web/src/app/(dashboard)/ai-jobs/[jobId]/page.tsx`: item status and snapshot summary.
- `web/src/lib/openapi-types.ts`: regenerated output; never hand-edit.

---

### Task 1: Add Authenticated Secret Encryption

**Files:**
- Create: `api/internal/secrets/aesgcm.go`
- Create: `api/internal/secrets/aesgcm_test.go`

**Interfaces:**
- Produces: `secrets.NewAESGCM(key []byte) (*AESGCM, error)`
- Produces: `(*AESGCM).Seal(plaintext []byte) (EncryptedValue, error)`
- Produces: `(*AESGCM).Open(value EncryptedValue) ([]byte, error)`
- Produces: `EncryptedValue{Ciphertext []byte, Nonce []byte, KeyVersion string}`

- [ ] **Step 1: Write failing round-trip, nonce, tamper, wrong-key, and length tests**

```go
func TestAESGCMRoundTripUsesUniqueNonce(t *testing.T) {
	box, err := NewAESGCM(bytes.Repeat([]byte{0x11}, 32))
	if err != nil { t.Fatal(err) }
	a, err := box.Seal([]byte("sk-proj-secret"))
	if err != nil { t.Fatal(err) }
	b, err := box.Seal([]byte("sk-proj-secret"))
	if err != nil { t.Fatal(err) }
	if bytes.Equal(a.Nonce, b.Nonce) { t.Fatal("nonce was reused") }
	plain, err := box.Open(a)
	if err != nil || string(plain) != "sk-proj-secret" { t.Fatalf("open = %q, %v", plain, err) }
}

func TestAESGCMRejectsTamperingAndWrongLength(t *testing.T) {
	if _, err := NewAESGCM(make([]byte, 31)); err == nil { t.Fatal("accepted 31-byte key") }
	box, _ := NewAESGCM(bytes.Repeat([]byte{0x22}, 32))
	value, _ := box.Seal([]byte("secret"))
	value.Ciphertext[0] ^= 0xff
	if _, err := box.Open(value); err == nil { t.Fatal("accepted tampered ciphertext") }
}
```

- [ ] **Step 2: Run the focused test and verify the missing-symbol failure**

Run: `cd api && go test ./internal/secrets -run TestAESGCM -count=1`

Expected: FAIL because `NewAESGCM` and `EncryptedValue` do not exist.

- [ ] **Step 3: Implement AES-256-GCM with random nonces and defensive copies**

```go
type EncryptedValue struct {
	Ciphertext []byte
	Nonce      []byte
	KeyVersion string
}

type AESGCM struct { aead cipher.AEAD }

func NewAESGCM(key []byte) (*AESGCM, error) {
	if len(key) != 32 { return nil, fmt.Errorf("master key must be 32 bytes") }
	block, err := aes.NewCipher(append([]byte(nil), key...))
	if err != nil { return nil, err }
	aead, err := cipher.NewGCM(block)
	if err != nil { return nil, err }
	return &AESGCM{aead: aead}, nil
}

func (a *AESGCM) Seal(plaintext []byte) (EncryptedValue, error) {
	nonce := make([]byte, a.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil { return EncryptedValue{}, err }
	return EncryptedValue{Ciphertext: a.aead.Seal(nil, nonce, plaintext, nil), Nonce: nonce, KeyVersion: "v1"}, nil
}

func (a *AESGCM) Open(value EncryptedValue) ([]byte, error) {
	if len(value.Nonce) != a.aead.NonceSize() { return nil, fmt.Errorf("invalid nonce length") }
	return a.aead.Open(nil, value.Nonce, value.Ciphertext, nil)
}
```

- [ ] **Step 4: Run tests and static checks**

Run: `cd api && go test ./internal/secrets -count=1 && go vet ./internal/secrets`

Expected: PASS with no vet findings.

- [ ] **Step 5: Commit the encryption primitive**

```bash
git add api/internal/secrets
git commit -m "feat(api): add authenticated secret encryption"
```

### Task 2: Add AI Configuration and Persistence Models

**Files:**
- Modify: `api/internal/config/config.go`
- Modify: `api/internal/config/config_test.go`
- Create: `api/internal/models/ai.go`
- Modify: `api/internal/database/database.go`
- Modify: `api/internal/database/database_test.go`
- Modify: `api/.env.example`

**Interfaces:**
- Produces: `Config.SecretsMasterKey`, `Config.OpenAIBaseURL`, `Config.AIWorkerDryRun`, `Config.AIWorkerPollInterval`
- Produces: model enums and GORM models consumed by all later tasks.

- [ ] **Step 1: Add failing config and migration tests**

```go
func TestLoadReadsAIConfiguration(t *testing.T) {
	t.Setenv("CARGOFLOW_SECRETS_MASTER_KEY", "c2VjcmV0c2VjcmV0c2VjcmV0c2VjcmV0MTIzNDU2Nzg=")
	t.Setenv("OPENAI_BASE_URL", "https://example.test/v1")
	t.Setenv("AI_WORKER_DRY_RUN", "true")
	cfg := Load()
	if cfg.SecretsMasterKey == "" || cfg.OpenAIBaseURL != "https://example.test/v1" || !cfg.AIWorkerDryRun {
		t.Fatalf("unexpected AI config: %#v", cfg)
	}
}

func TestMigrateCreatesAIFoundationTables(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil { t.Fatal(err) }
	for _, model := range []any{&models.OpenAIProviderSetting{}, &models.AIContentTemplate{}, &models.AIContentTemplateVersion{}, &models.AIContentSlot{}, &models.AIJob{}, &models.AIJobItem{}, &models.AIExecution{}, &models.AIAuditEvent{}} {
		if !db.Migrator().HasTable(model) { t.Fatalf("missing table for %T", model) }
	}
}
```

- [ ] **Step 2: Run tests and confirm missing fields/models**

Run: `cd api && go test ./internal/config ./internal/database -run 'TestLoadReadsAI|TestMigrateCreatesAI' -count=1`

Expected: FAIL with undefined configuration fields and model types.

- [ ] **Step 3: Define focused enums and models in `models/ai.go`**

```go
type AITemplateStatus string
const (
	AITemplateDraft AITemplateStatus = "draft"
	AITemplatePublished AITemplateStatus = "published"
	AITemplateArchived AITemplateStatus = "archived"
)

type OpenAIProviderSetting struct {
	ID uint `gorm:"primaryKey" json:"-"`
	Provider string `gorm:"size:32;uniqueIndex;not null" json:"provider"`
	EncryptedAPIKey []byte `gorm:"type:blob;not null" json:"-"`
	EncryptionNonce []byte `gorm:"type:varbinary(32);not null" json:"-"`
	EncryptionKeyVersion string `gorm:"size:16;not null" json:"-"`
	KeyFingerprint string `gorm:"size:16;not null" json:"key_fingerprint"`
	Status string `gorm:"size:32;not null" json:"status"`
	VerifiedAt *time.Time `json:"verified_at"`
	ImageCapabilityVerifiedAt *time.Time `json:"image_capability_verified_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedByID uint `gorm:"index;not null" json:"-"`
	UpdatedByID uint `gorm:"index;not null" json:"-"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
```

Also define `AIContentTemplate`, `AIContentTemplateVersion`, `AIContentSlot`, replacement `AIJob`, `AIJobItem`, `AIExecution`, `AIAuditEvent`, and `AIUsageLedger` exactly with the fields and states from the approved design. Remove the old `AIJob` declaration from `models.go` to avoid duplicate definitions.

- [ ] **Step 4: Load safe defaults and register every model in `Migrate`**

```go
SecretsMasterKey: getEnv("CARGOFLOW_SECRETS_MASTER_KEY", ""),
OpenAIBaseURL: getEnv("OPENAI_BASE_URL", "https://api.openai.com/v1"),
AIWorkerDryRun: getEnv("AI_WORKER_DRY_RUN", "false") == "true",
AIWorkerPollInterval: getDurationEnv("AI_WORKER_POLL_INTERVAL", time.Second),
```

Add only variable names and safe local guidance to `.env.example`; do not include a usable key.

- [ ] **Step 5: Run formatting and package tests**

Run: `cd api && gofmt -w internal/config internal/models internal/database && go test ./internal/config ./internal/database -count=1`

Expected: PASS.

- [ ] **Step 6: Commit the model foundation**

```bash
git add api/internal/config api/internal/models api/internal/database api/.env.example
git commit -m "feat(api): add AI foundation models"
```

### Task 3: Implement Shared Provider Setting Lifecycle

**Files:**
- Create: `api/internal/ai/provider_settings.go`
- Create: `api/internal/ai/provider_settings_test.go`
- Create: `api/internal/ai/openai_verifier.go`
- Create: `api/internal/ai/openai_verifier_test.go`

**Interfaces:**
- Consumes: `secrets.AESGCM`, `models.OpenAIProviderSetting`
- Produces: `ProviderVerifier.Verify(ctx context.Context, apiKey string) (ProviderVerification, error)`
- Produces: `ProviderSettingsService.Get`, `Configure`, `Disable`, and `DecryptActiveKey`
- Produces: `NewHTTPProviderVerifier(baseURL string, client *http.Client) *HTTPProviderVerifier`

- [ ] **Step 1: Write failing service tests for masking, verification, rotation, and disablement**

```go
func TestConfigureStoresCiphertextAndNeverReturnsPlaintext(t *testing.T) {
	db := testDB(t)
	box, _ := secrets.NewAESGCM(bytes.Repeat([]byte{0x31}, 32))
	verifier := &fakeVerifier{result: ProviderVerification{Authenticated: true}}
	service := NewProviderSettingsService(db, box, verifier)
	view, err := service.Configure(t.Context(), 7, "sk-proj-very-secret-ABCD")
	if err != nil { t.Fatal(err) }
	if view.KeyFingerprint != "ABCD" || strings.Contains(fmt.Sprintf("%#v", view), "very-secret") { t.Fatalf("leaked key: %#v", view) }
	var row models.OpenAIProviderSetting
	if err := db.First(&row).Error; err != nil { t.Fatal(err) }
	if bytes.Contains(row.EncryptedAPIKey, []byte("very-secret")) { t.Fatal("stored plaintext") }
}
```

- [ ] **Step 2: Run the focused test and verify missing service symbols**

Run: `cd api && go test ./internal/ai -run TestConfigureStores -count=1`

Expected: FAIL because the service and verifier interfaces do not exist.

- [ ] **Step 3: Implement the transactional lifecycle**

```go
type ProviderSettingView struct {
	Provider string `json:"provider"`
	Status string `json:"status"`
	KeyFingerprint string `json:"key_fingerprint"`
	VerifiedAt *time.Time `json:"verified_at"`
	ImageCapabilityVerifiedAt *time.Time `json:"image_capability_verified_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

func (s *ProviderSettingsService) Configure(ctx context.Context, actorID uint, apiKey string) (ProviderSettingView, error) {
	apiKey = strings.TrimSpace(apiKey)
	if len(apiKey) < 20 { return ProviderSettingView{}, ErrInvalidAPIKey }
	verification, err := s.verifier.Verify(ctx, apiKey)
	if err != nil || !verification.Authenticated { return ProviderSettingView{}, ErrCredentialVerification }
	sealed, err := s.box.Seal([]byte(apiKey))
	if err != nil { return ProviderSettingView{}, err }
	// Upsert provider=openai in one transaction; overwrite old ciphertext only after verification.
	return s.saveVerified(ctx, actorID, fingerprint(apiKey), sealed)
}
```

`Get` returns an `unconfigured` view on `gorm.ErrRecordNotFound`. `Disable` changes only status and audit metadata. `DecryptActiveKey` rejects non-active settings and returns a fresh plaintext byte slice for the worker to zero after client construction.

- [ ] **Step 4: Implement and test the bounded credential verifier**

```go
func (v *HTTPProviderVerifier) Verify(ctx context.Context, apiKey string) (ProviderVerification, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.baseURL+"/models", nil)
	if err != nil { return ProviderVerification{}, err }
	req.Header.Set("Authorization", "Bearer "+apiKey)
	response, err := v.client.Do(req)
	if err != nil { return ProviderVerification{}, err }
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return ProviderVerification{}, ErrCredentialVerification
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ProviderVerification{}, fmt.Errorf("verify credential: provider status %d", response.StatusCode)
	}
	return ProviderVerification{Authenticated: true}, nil
}
```

The test server must assert the Bearer header, return `200`, `401`, and `500`, and verify that returned errors do not contain the supplied key.

- [ ] **Step 5: Run the service package tests**

Run: `cd api && gofmt -w internal/ai && go test ./internal/ai -run 'Provider|Configure|Disable|Rotation' -count=1`

Expected: PASS.

- [ ] **Step 6: Commit the provider setting service**

```bash
git add api/internal/ai/provider_settings.go api/internal/ai/provider_settings_test.go api/internal/ai/openai_verifier.go api/internal/ai/openai_verifier_test.go
git commit -m "feat(api): manage encrypted OpenAI setting"
```

### Task 4: Implement Template Validation and Lifecycle

**Files:**
- Create: `api/internal/ai/templates.go`
- Create: `api/internal/ai/templates_test.go`

**Interfaces:**
- Produces: `TemplateService.Create`, `Get`, `List`, `UpdateDraft`, `Publish`, `CopyVersion`, and `Archive`
- Produces: `ValidateTemplateVersion(version, slots) []ValidationIssue`

- [ ] **Step 1: Write failing lifecycle and validation tests**

```go
func TestPublishRejectsMissingEnglishNameAndDuplicateSlotKey(t *testing.T) {
	service := NewTemplateService(testDB(t))
	created, err := service.Create(t.Context(), CreateTemplateInput{NameZH: "Lazada详情", NameEN: "Lazada Detail", TargetPlatform: "lazada", CreatedByID: 1})
	if err != nil { t.Fatal(err) }
	err = service.ReplaceDraft(t.Context(), created.Version.PublicID, UpdateTemplateVersionInput{
		NameZH: "Lazada详情", NameEN: "",
		Slots: []SlotInput{{SlotKey: "hero", Kind: "image"}, {SlotKey: "hero", Kind: "image"}},
	})
	if err != nil { t.Fatal(err) }
	issues, err := service.Publish(t.Context(), created.Version.PublicID, 1)
	if err != nil { t.Fatal(err) }
	assertIssueCodes(t, issues, "name_en_required", "slot_key_duplicate")
}
```

- [ ] **Step 2: Run tests and verify missing lifecycle types**

Run: `cd api && go test ./internal/ai -run 'Template|Publish' -count=1`

Expected: FAIL with undefined template service/input types.

- [ ] **Step 3: Implement normalized inputs and publication validation**

```go
type SlotInput struct {
	SlotKey string
	Kind string
	NameZH string
	NameEN string
	Sequence int
	Optional bool
	DefaultSelected bool
	PromptFragment string
	Constraints json.RawMessage
	GenerationConfig json.RawMessage
	LayoutConfig json.RawMessage
}

type ValidationIssue struct { Code string `json:"code"`; Path string `json:"path"`; Message string `json:"message"` }
```

Validate bilingual names, non-empty/unique slot keys, allowed kinds, contiguous sequence, non-empty prompt, known template variables, JSON object shape, candidate count 1-4, image sizes compatible with the published server allowlist, and normalized safe-area bounds. Publication reloads and validates inside a transaction before changing status. All mutations reject non-draft versions.

- [ ] **Step 4: Run focused and full AI package tests**

Run: `cd api && gofmt -w internal/ai && go test ./internal/ai -count=1`

Expected: PASS.

- [ ] **Step 5: Commit template lifecycle**

```bash
git add api/internal/ai/templates.go api/internal/ai/templates_test.go
git commit -m "feat(api): add versioned AI content templates"
```

### Task 5: Expose Admin Credential and Template APIs

**Files:**
- Create: `api/internal/app/ai_dto.go`
- Create: `api/internal/app/ai_handlers.go`
- Create: `api/internal/app/ai_handlers_test.go`
- Modify: `api/internal/app/router.go`
- Modify: `api/openapi.yaml`

**Interfaces:**
- Produces: `GET/PUT/DELETE /api/v1/settings/openai`
- Produces: template list/create/get/update/validate/publish/copy/archive endpoints under `/api/v1/ai-content-templates`

- [ ] **Step 1: Write failing HTTP tests proving admin-only access and secret non-disclosure**

```go
func TestOpenAISettingIsAdminOnlyAndNeverEchoesKey(t *testing.T) {
	db := newTestDB(t)
	admin := seedUser(t, db, models.RoleAdmin)
	operator := seedUser(t, db, models.RoleOperator)
	server, adminToken := authenticatedAIRouter(t, db, admin, &fakeVerifier{authenticated: true})
	defer server.Close()
	response := aiRequest(t, server, adminToken, http.MethodPut, "/api/v1/settings/openai", `{"api_key":"sk-proj-secret-value-ABCD"}`)
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || bytes.Contains(body, []byte("secret-value")) { t.Fatalf("status/body = %d %s", response.StatusCode, body) }
	_, operatorToken := authenticatedAIRouter(t, db, operator, &fakeVerifier{authenticated: true})
	forbidden := aiRequest(t, server, operatorToken, http.MethodGet, "/api/v1/settings/openai", "")
	defer forbidden.Body.Close()
	if forbidden.StatusCode != http.StatusForbidden { t.Fatalf("status = %d", forbidden.StatusCode) }
}
```

- [ ] **Step 2: Run handler tests and confirm route failures**

Run: `cd api && go test ./internal/app -run 'OpenAISetting|AIContentTemplate' -count=1`

Expected: FAIL because the handlers and routes are absent.

- [ ] **Step 3: Add dependency injection and admin route groups**

```go
type AIDependencies struct {
	ProviderSettings *ai.ProviderSettingsService
	Templates *ai.TemplateService
}

func NewRouter(cfg config.Config, db *gorm.DB) *gin.Engine {
	deps, err := newAIDependencies(cfg, db)
	if err != nil { panic("configure AI services: " + err.Error()) }
	return newRouter(cfg, db, deps)
}

func NewRouterWithAIDependencies(cfg config.Config, db *gorm.DB, deps AIDependencies) *gin.Engine {
	return newRouter(cfg, db, deps)
}

aiAdmin := protected.Group("")
aiAdmin.Use(requireRoles(models.RoleAdmin))
aiAdmin.GET("/settings/openai", server.getOpenAISetting)
aiAdmin.PUT("/settings/openai", server.putOpenAISetting)
aiAdmin.DELETE("/settings/openai", server.disableOpenAISetting)
aiAdmin.POST("/ai-content-templates", server.createAIContentTemplate)
aiAdmin.PATCH("/ai-content-template-versions/:version_id", server.updateAIContentTemplateVersion)
aiAdmin.POST("/ai-content-template-versions/:version_id/publish", server.publishAIContentTemplateVersion)
```

Move the current route-registration body unchanged into `registerExistingRoutes(router, server)`, then append the new AI route group shown above. Keep DTOs explicit; never JSON-serialize GORM secret-bearing models.

- [ ] **Step 4: Add exact OpenAPI schemas and responses, then regenerate Web types**

Run: `cd web && pnpm generate:api`

Expected: `src/lib/openapi-types.ts` updates without generator errors.

- [ ] **Step 5: Run API handler and OpenAPI contract tests**

Run: `cd api && gofmt -w internal/app && go test ./internal/app -run 'OpenAI|AIContent|OpenAPI' -count=1`

Expected: PASS.

- [ ] **Step 6: Commit the admin APIs**

```bash
git add api/internal/app api/openapi.yaml web/src/lib/openapi-types.ts
git commit -m "feat(api): expose AI settings and template APIs"
```

### Task 6: Create Reproducible Dry-run Jobs

**Files:**
- Create: `api/internal/ai/jobs.go`
- Create: `api/internal/ai/jobs_test.go`
- Modify: `api/internal/app/ai_dto.go`
- Modify: `api/internal/app/ai_handlers.go`
- Modify: `api/internal/app/ai_handlers_test.go`
- Modify: `api/internal/app/router.go`
- Modify: `api/openapi.yaml`

**Interfaces:**
- Produces: `JobService.Create(ctx, CreateJobInput) (JobDocument, error)`
- Produces: `GET/POST /api/v1/ai-jobs` and `GET /api/v1/ai-jobs/{job_id}`

- [ ] **Step 1: Write failing service tests for published template, selected slots, exact SKU, and approved assets**

```go
func TestCreateJobSnapshotsOnlySelectedSlotsAndApprovedSameSKUAssets(t *testing.T) {
	db, fixture := seedAIJobFixture(t)
	service := NewJobService(db)
	job, err := service.Create(t.Context(), CreateJobInput{
		SKUID: fixture.SKU.ID,
		TemplateVersionPublicID: fixture.PublishedVersion.PublicID,
		SelectedSlotKeys: []string{"title", "hero"},
		SelectedAssetIDs: []uint{fixture.ApprovedAsset.ID},
		Locale: "zh-CN", CreatedByID: fixture.Operator.ID,
	})
	if err != nil { t.Fatal(err) }
	if len(job.Items) != 2 || strings.Contains(string(job.InputSnapshot), "low_stock_threshold") { t.Fatalf("unsafe snapshot: %s", job.InputSnapshot) }
	fixture.ApprovedAsset.SKUID = fixture.OtherSKU.ID
	db.Save(&fixture.ApprovedAsset)
	if _, err := service.Create(t.Context(), CreateJobInput{SKUID: fixture.SKU.ID, TemplateVersionPublicID: fixture.PublishedVersion.PublicID, SelectedSlotKeys: []string{"hero"}, SelectedAssetIDs: []uint{fixture.ApprovedAsset.ID}}); !errors.Is(err, ErrAssetNotEligible) {
		t.Fatalf("cross-SKU asset error = %v", err)
	}
}
```

- [ ] **Step 2: Run the focused tests and verify missing job service**

Run: `cd api && go test ./internal/ai -run TestCreateJob -count=1`

Expected: FAIL because `JobService` and normalized snapshot types do not exist.

- [ ] **Step 3: Implement whitelisted snapshot structs and atomic job/item creation**

```go
type ProductSnapshotV1 struct {
	Schema string `json:"schema"`
	Locale string `json:"locale"`
	TargetPlatform string `json:"target_platform"`
	Product ProductFacts `json:"product"`
	SKU SKUFacts `json:"sku"`
	SOP SOPFacts `json:"sop"`
	SelectedAssets []AssetFacts `json:"selected_assets"`
}
```

Inside one `db.WithContext(ctx).Transaction`, query `SKU` with `Product`, `Product.CatalogCategory`, and tags; query the template version by public UUID and `status=published`; query its ordered slots; reduce those slots against a de-duplicated `SelectedSlotKeys` set; and query every requested `Asset` with `sku_id=input.SKUID AND review_status=approved`. Return `ErrAssetNotEligible` when the eligible-asset count differs from the de-duplicated requested count. Marshal only `ProductSnapshotV1`, insert one UUID-backed job, and insert one queued item per selected slot in template sequence order. Require at least one selected slot, reject unknown/duplicate slot keys, reject archived/draft versions, and never serialize whole GORM objects into the snapshot.

- [ ] **Step 4: Replace the old untyped comma-separated AI job handlers**

Delete `aiJobRequest` and the current `createAIJob`/`listAIJobs` implementations from `handlers.go`. Route the typed endpoints through `ai_handlers.go`; use UUIDs and JSON arrays throughout.

- [ ] **Step 5: Update OpenAPI, regenerate types, and run tests**

Run: `cd web && pnpm generate:api`

Run: `cd api && gofmt -w internal/ai internal/app && go test ./internal/ai ./internal/app -run 'Job|AIJob|OpenAPI' -count=1`

Expected: PASS.

- [ ] **Step 6: Commit dry-run job creation**

```bash
git add api/internal/ai/jobs* api/internal/app api/openapi.yaml web/src/lib/openapi-types.ts
git commit -m "feat(api): create reproducible AI jobs"
```

### Task 7: Add Leased Queue and Standalone Dry-run Worker

**Files:**
- Create: `api/internal/ai/queue.go`
- Create: `api/internal/ai/queue_test.go`
- Create: `api/internal/ai/worker.go`
- Create: `api/internal/ai/worker_test.go`
- Create: `api/cmd/worker/main.go`
- Modify: `docker-compose.yml`

**Interfaces:**
- Produces: `Queue.LeaseNext(ctx, workerID, now, ttl) (*LeasedItem, error)`
- Produces: `Queue.Heartbeat`, `Complete`, `Fail`
- Produces: `Worker.RunOnce(ctx) (bool, error)` and `DryRunExecutor.Execute`

- [ ] **Step 1: Write failing lease exclusivity and expiry tests**

```go
func TestLeaseNextIsExclusiveAndExpiredLeaseCanRecover(t *testing.T) {
	db, item := seedQueuedItem(t)
	queue := NewQueue(db)
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	first, err := queue.LeaseNext(t.Context(), "worker-a", now, time.Minute)
	if err != nil || first.PublicID != item.PublicID { t.Fatalf("first lease = %#v, %v", first, err) }
	second, err := queue.LeaseNext(t.Context(), "worker-b", now, time.Minute)
	if err != nil || second != nil { t.Fatalf("duplicate lease = %#v, %v", second, err) }
	recovered, err := queue.LeaseNext(t.Context(), "worker-b", now.Add(2*time.Minute), time.Minute)
	if err != nil || recovered.PublicID != item.PublicID { t.Fatalf("recovery = %#v, %v", recovered, err) }
}
```

- [ ] **Step 2: Run queue tests and verify missing queue symbols**

Run: `cd api && go test ./internal/ai -run 'Lease|Worker|DryRun' -count=1`

Expected: FAIL because queue and worker types do not exist.

- [ ] **Step 3: Implement transactional leasing and idempotent completion**

```go
type LeasedItem struct { PublicID string; JobPublicID string; SlotKey string; LeaseOwner string }

func (q *Queue) LeaseNext(ctx context.Context, workerID string, now time.Time, ttl time.Duration) (*LeasedItem, error) {
	// In a transaction select one queued item or expired running lease ordered by created_at.
	// Conditionally update status, lease_owner, lease_expires_at, and started_at.
	// Return nil when no work exists; a lost conditional update retries selection.
}
```

Use MySQL `FOR UPDATE SKIP LOCKED` in production and a conditional-update fallback supported by SQLite tests. `Complete` and `Fail` require matching lease owner. Aggregate the parent job to `queued`, `running`, `partial`, `completed`, or `failed` after every item transition.

- [ ] **Step 4: Implement provider-neutral worker and dry-run executor**

```go
type ItemExecutor interface { Execute(context.Context, LeasedItem) error }

func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	item, err := w.queue.LeaseNext(ctx, w.id, w.clock.Now(), w.leaseTTL)
	if err != nil || item == nil { return false, err }
	if err := w.executor.Execute(ctx, *item); err != nil { return true, w.queue.Fail(ctx, *item, safeError(err)) }
	return true, w.queue.Complete(ctx, *item)
}
```

`DryRunExecutor` writes an `AIExecution` with operation inferred from item kind, `status=completed`, zero usage, and an audit event. It never decrypts the OpenAI key and never performs network I/O.

- [ ] **Step 5: Add worker command and Compose service**

`api/cmd/worker/main.go` loads config/database, requires `AI_WORKER_DRY_RUN=true` in Phase 1, constructs the queue/worker, handles SIGINT/SIGTERM, and polls using `AI_WORKER_POLL_INTERVAL`. Add a `worker` service using the API image and the same database dependency/environment.

- [ ] **Step 6: Run race-enabled worker tests**

Run: `cd api && gofmt -w internal/ai cmd/worker && go test -race ./internal/ai -run 'Lease|Worker|DryRun' -count=1`

Expected: PASS without duplicate completion or race reports.

- [ ] **Step 7: Commit the worker foundation**

```bash
git add api/internal/ai/queue* api/internal/ai/worker* api/cmd/worker docker-compose.yml
git commit -m "feat(api): add leased AI dry-run worker"
```

### Task 8: Build the Admin OpenAI Settings Page

**Files:**
- Create: `web/src/lib/ai-schemas.ts`
- Create: `web/src/lib/ai-schemas.test.ts`
- Modify: `web/src/lib/i18n.tsx`
- Modify: `web/src/components/app-shell.tsx`
- Create: `web/src/app/(dashboard)/settings/openai/page.tsx`
- Create: `web/src/app/(dashboard)/settings/openai/page.test.tsx`

**Interfaces:**
- Consumes: typed settings endpoints from Task 5.
- Produces: admin credential form with masked status and no client persistence.

- [ ] **Step 1: Write failing schema and page tests**

```tsx
it("clears the secret field and never renders the submitted key", async () => {
  vi.spyOn(globalThis, "fetch").mockImplementation((input, init) => {
    if (init?.method === "PUT") return jsonResponse({ provider: "openai", status: "active", key_fingerprint: "ABCD", verified_at: "2026-07-17T10:00:00Z" });
    return jsonResponse({ provider: "openai", status: "unconfigured", key_fingerprint: "" });
  });
  render(<OpenAISettingsPage />, { wrapper: Providers });
  const input = await screen.findByLabelText("OpenAI Project API Key");
  fireEvent.change(input, { target: { value: "sk-proj-secret-value-ABCD" } });
  fireEvent.click(screen.getByRole("button", { name: "保存并验证" }));
  await waitFor(() => expect(input).toHaveValue(""));
  expect(screen.queryByText(/secret-value/)).not.toBeInTheDocument();
  expect(localStorage.length).toBe(0);
});
```

- [ ] **Step 2: Run focused Web tests and confirm missing page/schema**

Run: `cd web && pnpm test -- 'src/lib/ai-schemas.test.ts' 'src/app/(dashboard)/settings/openai/page.test.tsx'`

Expected: FAIL because the files and translations do not exist.

- [ ] **Step 3: Add Zod input validation and complete bilingual copy**

```ts
export const openAIKeySchema = z.object({
  api_key: z.string().trim().min(20, "openAIKeyTooShort").max(512, "openAIKeyTooLong"),
});
export type OpenAIKeyInput = z.infer<typeof openAIKeySchema>;
```

Add paired Chinese/English keys for navigation, configured states, fingerprint, validation, replacement confirmation, disabled state, permissions, safe errors, and success messages.

- [ ] **Step 4: Implement the settings page with React Query mutations**

Use a controlled password input held only in component state, `autocomplete="new-password"`, no URL parameters, and reset state in `onSuccess` and unmount cleanup. Render only server-returned fingerprint and status. Treat `403` as an explicit permission state.

- [ ] **Step 5: Run tests, typecheck, and lint**

Run: `cd web && pnpm test -- 'src/lib/ai-schemas.test.ts' 'src/app/(dashboard)/settings/openai/page.test.tsx' && pnpm typecheck && pnpm lint`

Expected: PASS.

- [ ] **Step 6: Commit the credential UI**

```bash
git add web/src/lib/ai-schemas* web/src/lib/i18n.tsx web/src/components/app-shell.tsx 'web/src/app/(dashboard)/settings/openai'
git commit -m "feat(web): add OpenAI credential settings"
```

### Task 9: Build Versioned Template Management

**Files:**
- Modify: `web/src/lib/ai-schemas.ts`
- Modify: `web/src/lib/ai-schemas.test.ts`
- Create: `web/src/app/(dashboard)/ai-templates/page.tsx`
- Create: `web/src/app/(dashboard)/ai-templates/page.test.tsx`
- Create: `web/src/app/(dashboard)/ai-templates/new/page.tsx`
- Create: `web/src/app/(dashboard)/ai-templates/new/page.test.tsx`
- Modify: `web/src/lib/i18n.tsx`

**Interfaces:**
- Consumes: template endpoints and generated types from Task 5.
- Produces: administrator list, draft creation, validation summary, and publication flow.

- [ ] **Step 1: Write failing form tests for bilingual names and selectable slots**

```tsx
it("blocks publication until bilingual names and a valid slot exist", async () => {
  render(<NewAITemplatePage />, { wrapper: Providers });
  fireEvent.change(screen.getByLabelText("中文名称"), { target: { value: "Lazada 商品详情" } });
  fireEvent.click(screen.getByRole("button", { name: "创建草稿" }));
  expect(await screen.findByText("请输入英文名称")).toBeInTheDocument();
  expect(globalThis.fetch).not.toHaveBeenCalled();
});
```

- [ ] **Step 2: Run focused tests and verify missing pages**

Run: `cd web && pnpm test -- 'src/app/(dashboard)/ai-templates/**/*.test.tsx' 'src/lib/ai-schemas.test.ts'`

Expected: FAIL because the template pages/schema do not exist.

- [ ] **Step 3: Extend the Zod schema with discriminated slot validation**

```ts
const baseSlot = z.object({ slot_key: z.string().regex(/^[a-z][a-z0-9_]*$/), name_zh: z.string().min(1), name_en: z.string().min(1), prompt_fragment: z.string().min(1) });
const imageSlot = baseSlot.extend({ kind: z.literal("image"), size: z.enum(["1024x1024", "1536x1024", "1024x1536"]), quality: z.enum(["low", "medium", "high"]), candidate_count: z.number().int().min(1).max(4) });
const titleSlot = baseSlot.extend({ kind: z.literal("title"), min_length: z.number().int().min(1), max_length: z.number().int().max(500) });
const seoSlot = baseSlot.extend({ kind: z.literal("seo_description"), max_length: z.number().int().max(10000) });
export const aiTemplateDraftSchema = z.object({ name_zh: z.string().min(1), name_en: z.string().min(1), target_platform: z.string().min(1), slots: z.array(z.discriminatedUnion("kind", [imageSlot, titleSlot, seoSlot])).min(1) });
```

- [ ] **Step 4: Implement list and V1 draft editor**

The list groups logical templates with ordered versions and clear lifecycle badges. The editor supports adding/removing/reordering title, SEO, and image slots; advanced raw JSON is not exposed. Show server validation issues by path before allowing publish.

- [ ] **Step 5: Run Web tests and static checks**

Run: `cd web && pnpm test -- 'src/app/(dashboard)/ai-templates/**/*.test.tsx' 'src/lib/ai-schemas.test.ts' && pnpm typecheck && pnpm lint`

Expected: PASS.

- [ ] **Step 6: Commit template management**

```bash
git add 'web/src/app/(dashboard)/ai-templates' web/src/lib/ai-schemas* web/src/lib/i18n.tsx
git commit -m "feat(web): manage AI content templates"
```

### Task 10: Replace Mock AI Jobs with the Dry-run Wizard and Detail

**Files:**
- Create: `web/src/app/(dashboard)/ai-jobs/new/page.tsx`
- Create: `web/src/app/(dashboard)/ai-jobs/new/page.test.tsx`
- Modify: `web/src/app/(dashboard)/ai-jobs/page.tsx`
- Create: `web/src/app/(dashboard)/ai-jobs/[jobId]/page.tsx`
- Create: `web/src/app/(dashboard)/ai-jobs/[jobId]/page.test.tsx`
- Modify: `web/src/lib/i18n.tsx`
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/lib/mock-data.ts`

**Interfaces:**
- Consumes: job endpoints from Task 6 and existing SKU/category endpoints.
- Produces: optional-slot wizard, real job list, and dry-run item status detail.

- [ ] **Step 1: Write failing wizard and detail tests**

```tsx
it("submits only checked slots and shows the data disclosure", async () => {
  const fetchMock = mockAIWizardAPIs();
  render(<NewAIJobPage />, { wrapper: Providers });
  await selectSKUAndTemplate("CF-CASE-CLR-IP17", "Lazada 商品详情");
  fireEvent.click(screen.getByRole("checkbox", { name: "白底主图" }));
  fireEvent.click(screen.getByRole("button", { name: "下一步" }));
  expect(screen.getByText("商品数据和所选图片将发送给 OpenAI")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "创建任务" }));
  await waitFor(() => expect(fetchMock).toHavePosted("/api/proxy/ai-jobs", expect.objectContaining({ selected_slot_keys: ["hero"] })));
});
```

- [ ] **Step 2: Run focused tests and confirm missing wizard/detail**

Run: `cd web && pnpm test -- 'src/app/(dashboard)/ai-jobs/**/*.test.tsx'`

Expected: FAIL because the new pages are absent and the list still uses mock data.

- [ ] **Step 3: Implement a five-step dry-run wizard**

Implement SKU/template/locale selection, optional slot checkboxes, approved-asset summary, allowed parameters, and confirmation. In Phase 1 the final page explicitly labels execution as dry-run and states that no product content or image is sent to OpenAI. Submit typed arrays, not comma-separated strings.

- [ ] **Step 4: Replace mock list data and add job detail polling**

The list queries `/ai-jobs`. Detail queries `/ai-jobs/{uuid}` every two seconds while any item is `queued` or `running`, then stops. Render selected slot, item status, safe error, creation time, and a redacted snapshot summary. Remove `aiJobs` from `mock-data.ts` and update legacy `AiJob` types to generated/normalized types.

- [ ] **Step 5: Run Web tests and full static checks**

Run: `cd web && pnpm test -- 'src/app/(dashboard)/ai-jobs/**/*.test.tsx' && pnpm typecheck && pnpm lint`

Expected: PASS.

- [ ] **Step 6: Commit dry-run Web workflow**

```bash
git add 'web/src/app/(dashboard)/ai-jobs' web/src/lib/i18n.tsx web/src/lib/types.ts web/src/lib/mock-data.ts
git commit -m "feat(web): add AI dry-run job workflow"
```

### Task 11: Verify the Integrated Phase 1 Deliverable

**Files:**
- Modify: `README.md`
- Modify: `web/tests/e2e/login.spec.ts`
- Create: `web/tests/e2e/ai-foundation.spec.ts`

**Interfaces:**
- Consumes: every Phase 1 API and Web interface.
- Produces: documented local startup and one end-to-end template-to-dry-run acceptance path; credential configuration remains covered by Task 5's isolated API tests so E2E never needs a real OpenAI key.

- [ ] **Step 1: Add an E2E test for template publication and dry-run completion**

```ts
test("admin can publish a template and complete a dry-run job", async ({ page }) => {
  await loginAsAdmin(page);
  await page.goto("/ai-templates/new");
  await createMinimalBilingualTemplate(page, { slotKey: "title", kind: "title" });
  await page.getByRole("button", { name: "发布" }).click();
  await page.goto("/ai-jobs/new");
  await createDryRunForSeedSKU(page, ["title"]);
  await expect(page.getByText("已完成")).toBeVisible();
});
```

- [ ] **Step 2: Document safe local configuration and worker startup**

Add commands to generate a local 32-byte base64 master key without printing any OpenAI key, set `AI_WORKER_DRY_RUN=true`, start MySQL/MinIO/API/worker, and explain that Phase 1 performs no product-content provider calls.

- [ ] **Step 3: Run the complete API verification**

Run: `cd api && go test -race ./... && go vet ./...`

Expected: all packages PASS with no race or vet findings.

- [ ] **Step 4: Run the complete Web verification**

Run: `cd web && pnpm generate:api && pnpm test && pnpm typecheck && pnpm lint && pnpm build`

Expected: generator, Vitest, TypeScript, ESLint, and Next production build all PASS.

- [ ] **Step 5: Run the E2E acceptance path with the dry-run stack**

Run: `docker compose up --build -d mysql minio api worker`

Run: `cd web && pnpm e2e -- ai-foundation.spec.ts`

Expected: the admin/template/job dry-run test PASSes and the job reaches `completed` without an OpenAI product-content request.

- [ ] **Step 6: Inspect the final diff and commit Phase 1 verification**

Run: `git diff --check && git status --short`

Expected: no whitespace errors; only README and E2E changes remain after earlier task commits.

```bash
git add README.md web/tests/e2e
git commit -m "test: verify AI foundation workflow"
```

## Phase 1 Completion Gate

Do not start the text-generation plan until all of these are true:

- The shared key is encrypted at rest, never returned, and admin-only.
- Published template versions are immutable and bilingual.
- Dry-run jobs snapshot only whitelisted one-SKU data and selected slots.
- Only approved same-SKU assets pass eligibility checks.
- Multiple workers cannot complete the same item.
- API, Web, build, race, contract, and E2E verification pass.
- The dry-run executor has no OpenAI product-content network path.
