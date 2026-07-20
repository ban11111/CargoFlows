# OpenAI Product Text Generation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Upgrade CargoFlows from the Phase 1 dry-run foundation to safe, auditable OpenAI Responses API generation of title and SEO candidates that users can edit, approve, and explicitly apply.

**Architecture:** A one-shot migrator exclusively owns schema changes before API and worker startup. The worker compiles immutable L0-L4 prompts from the existing job snapshot, decrypts the shared key only immediately before a provider call, invokes a provider-neutral Responses client, and atomically stores executions, candidates, usage, and audit data. Generated candidates never overwrite SKU content; authenticated Web actions edit, approve, and apply a candidate into versioned platform content.

**Tech Stack:** Go 1.25, GORM/MySQL, Gin, OpenAI Responses API over a provider adapter, Next.js 16, React Query, Zod, Vitest, Playwright, Docker Compose.

## Global Constraints

- Web only; do not change iOS files.
- Never use, persist, log, or test with the API key disclosed in chat. Real-provider smoke tests require a newly rotated key supplied through the admin UI or a local ignored environment variable.
- One shared Project API key remains administrator-only and encrypted with `CARGOFLOWS_SECRETS_MASTER_KEY`.
- Default text model is configurable and starts at `gpt-5.6-terra`; reasoning effort is explicit `low`; Responses requests use `store=false`.
- Model-generated facts are untrusted. The server enforces structured schemas, length limits, forbidden terms, and candidate counts independently.
- No candidate automatically updates SKU or platform content. Application is an explicit audited action.
- Image slots remain unsupported by the real executor in this phase and fail safely without an OpenAI call.
- Pre-release legacy SOP data is not upgraded; the user will re-upload it after the new version is ready.
- Every behavior change follows RED-GREEN-REFACTOR and receives focused plus full verification.

---

### Task 1: Make Database Migrations Single-Owner

**Files:**
- Create: `api/cmd/migrate/main.go`
- Modify: `api/cmd/server/main.go`
- Modify: `api/cmd/worker/main.go`
- Modify: `api/internal/database/database.go`
- Modify: `api/internal/database/database_test.go`
- Modify: `docker-compose.yml`
- Modify: `README.md`

**Interfaces:**
- Produces: `database.Migrate(ctx context.Context, db *gorm.DB) error` and a one-shot `cargoflows-migrate` command.
- Guarantees: API and worker never run migrations; Compose does not start either until the migrator exits successfully.

- [ ] **Step 1: Write failing identity and ownership tests**

Add tests that blank existing-schema `public_id` values, call `Migrate` twice, and assert every public identifier is a non-zero unique UUID. Add a source/config test asserting server and worker do not call `database.Migrate`, while Compose has a `migrate` service and `service_completed_successfully` dependencies. Old pre-release SOP schemas are intentionally outside the upgrade contract.

- [ ] **Step 2: Run RED**

Run: `cd api && go test ./internal/database ./cmd/server ./cmd/worker -run 'LegacyPublicID|MigrationOwnership' -count=1`

Expected: FAIL because legacy SOP rows cannot receive the unique index and both processes currently migrate.

- [ ] **Step 3: Implement staged backfill and migrator ownership**

Before `AutoMigrate`, inspect existing public-identity tables (`capture_sops`, `sop_versions`, `sop_views`, `sop_view_reference_images`, `photo_sessions`) and backfill null/empty IDs row-by-row with `uuid.NewString()`. On MySQL, hold `GET_LOCK('cargoflows_schema_migrate', 60)` through migration and always release it. Move migration calls into `cmd/migrate`; update Compose to run that command once and gate API/worker startup. Document `go run ./cmd/migrate` before local API/worker startup.

- [ ] **Step 4: Run GREEN and existing migration tests**

Run: `cd api && go test ./internal/database ./cmd/server ./cmd/worker -count=1 && go test ./... -count=1`

Expected: PASS; repeated migration is idempotent for the current schema.

- [ ] **Step 5: Commit**

```bash
git add api/cmd api/internal/database docker-compose.yml README.md
git commit -m "fix(database): make schema upgrades legacy-safe"
```

### Task 2: Add Immutable Text Candidates and Platform Content Revisions

**Files:**
- Modify: `api/internal/models/ai.go`
- Create: `api/internal/models/ai_text_test.go`
- Modify: `api/internal/database/database.go`
- Modify: `api/internal/database/database_test.go`

**Interfaces:**
- Produces: `AITextResult`, `SKUPlatformContent`, and `SKUPlatformContentRevision` models.
- Candidate states: `candidate`, `approved`, `rejected`; human edits are stored separately from raw provider JSON.

- [ ] **Step 1: Write failing model tests**

Test unique candidate sequence per execution, one platform-content row per `(sku_id, platform, locale)`, monotonically increasing revisions, nullable approver/application metadata, and non-empty JSON defaults.

- [ ] **Step 2: Run RED**

Run: `cd api && go test ./internal/models ./internal/database -run 'AIText|PlatformContent' -count=1`

Expected: FAIL because the models and tables do not exist.

- [ ] **Step 3: Implement persistence models**

`AITextResult` stores execution ID, candidate index, kind, raw structured JSON, validation JSON, optional edited JSON, state, approver, approval time, and timestamps. `SKUPlatformContent` stores title, short/long descriptions, selling points JSON, search keywords JSON, source result ID, revision, updater, and timestamps. Revisions store before/after JSON and actor.

- [ ] **Step 4: Run GREEN**

Run: `cd api && go test ./internal/models ./internal/database -run 'AIText|PlatformContent|Migrate' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add api/internal/models api/internal/database
git commit -m "feat(api): add AI text result persistence"
```

### Task 3: Compile Versioned L0-L4 Text Prompts and Schemas

**Files:**
- Create: `api/internal/ai/prompt_compiler.go`
- Create: `api/internal/ai/prompt_compiler_test.go`
- Create: `api/internal/ai/testdata/title_prompt.golden.json`
- Create: `api/internal/ai/testdata/seo_prompt.golden.json`

**Interfaces:**
- Produces: `CompileTextPrompt(snapshot ProductSnapshotV1, slot SlotFacts) (CompiledTextPrompt, error)`.
- `CompiledTextPrompt` includes developer instructions, user input JSON, strict JSON Schema, layer versions, SHA-256, requested candidate count, and safe request metadata.

- [ ] **Step 1: Write failing golden and security tests**

Assert deterministic field ordering, exact L0/L1 versions, L0→L1→L2→L3→L4 precedence, locale/platform preservation, candidate-count constraints, user-preference delimiting, no asset URLs for text slots, rejection of unknown variables, and no secret-looking values.

- [ ] **Step 2: Run RED**

Run: `cd api && go test ./internal/ai -run 'CompileTextPrompt' -count=1`

Expected: FAIL because the compiler does not exist.

- [ ] **Step 3: Implement the compiler**

Keep fixed policies in code as `l0-product-safety-v1` and `l1-product-context-v1`. Emit title schema `{candidates:[{title,keywords,source_fields}]}` or SEO schema `{candidates:[{short_description,selling_points,long_description,search_keywords,source_fields}]}` with `additionalProperties:false`. Put application rules in Responses `instructions`; put the normalized snapshot and delimited optional preference in `input`.

- [ ] **Step 4: Run GREEN and golden review**

Run: `cd api && go test ./internal/ai -run 'CompileTextPrompt' -count=1`

Expected: PASS with stable golden files.

- [ ] **Step 5: Commit**

```bash
git add api/internal/ai/prompt_compiler* api/internal/ai/testdata
git commit -m "feat(ai): compile layered product text prompts"
```

### Task 4: Implement a Provider-Neutral Responses Text Client

**Files:**
- Create: `api/internal/ai/text_provider.go`
- Create: `api/internal/ai/openai_responses_client.go`
- Create: `api/internal/ai/openai_responses_client_test.go`
- Modify: `api/internal/config/config.go`
- Modify: `api/internal/config/config_test.go`
- Modify: `api/.env.example`

**Interfaces:**
- Produces: `TextProvider.Generate(context.Context, []byte, TextRequest) (TextResponse, error)`.
- Provider errors classify authentication, rate limit, retryable server error, invalid response, refusal, and ambiguous timeout without exposing response bodies to users.

- [ ] **Step 1: Write failing fake-server contract tests**

Assert `POST /v1/responses`, bearer auth, `store:false`, configured model, `reasoning.effort=low`, strict `text.format` schema, metadata using internal public IDs only, request-ID capture, aggregated output text parsing, token usage, refusal handling, bounded `429/5xx` retry with `Retry-After`, and no retry after ambiguous timeout.

- [ ] **Step 2: Run RED**

Run: `cd api && go test ./internal/ai -run 'ResponsesTextClient' -count=1`

Expected: FAIL because the provider does not exist.

- [ ] **Step 3: Implement the minimal HTTP adapter**

Use a dedicated `http.Client` with configurable timeout. Never log authorization, request body, full provider error body, or output text. Decode all message/output-text items rather than assuming `output[0]`. Validate status `completed`, strict JSON, candidate count, and usage. Default `OPENAI_TEXT_MODEL=gpt-5.6-terra`, `OPENAI_REASONING_EFFORT=low`, and `OPENAI_REQUEST_TIMEOUT=120s`.

- [ ] **Step 4: Run GREEN**

Run: `cd api && go test ./internal/ai ./internal/config -run 'ResponsesTextClient|OpenAIText' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add api/internal/ai/text_provider.go api/internal/ai/openai_responses_client* api/internal/config api/.env.example
git commit -m "feat(ai): add OpenAI Responses text client"
```

### Task 5: Execute Real Text Jobs with Encrypted Credentials and Usage Audit

**Files:**
- Create: `api/internal/ai/text_executor.go`
- Create: `api/internal/ai/text_executor_test.go`
- Modify: `api/internal/ai/worker.go`
- Modify: `api/internal/ai/worker_test.go`
- Modify: `api/internal/ai/provider_settings.go`
- Modify: `api/cmd/worker/main.go`
- Modify: `docker-compose.yml`

**Interfaces:**
- Produces: `NewTextExecutor(db, providerSettings, provider, config) ItemExecutor` and a kind-routing executor that retains dry-run behavior when configured.
- Guarantees: one provider call result per execution, zero plaintext key persistence, usage-ledger uniqueness, and safe unsupported-image handling.

- [ ] **Step 1: Write failing executor tests**

Cover key decryption only immediately before call, byte-slice clearing, execution `preparing→calling_openai→completed`, provider IDs and usage persistence, multiple candidates, audit event, `last_used_at`, duplicate lease/idempotent recovery, authentication failure, refusal, malformed structured output, timeout→`needs_attention`, and image slot rejection without provider invocation.

- [ ] **Step 2: Run RED**

Run: `cd api && go test -race ./internal/ai -run 'TextExecutor|RealWorker' -count=1`

Expected: FAIL because only `DryRunExecutor` exists.

- [ ] **Step 3: Implement transaction boundaries and routing**

Create the execution and compiled prompt before the network call, call OpenAI outside a DB transaction while heartbeat remains active, then re-check the lease and atomically persist text results, usage, audit, provider timestamp, and execution completion. Preserve enough provider response metadata to recover storage without another OpenAI call. Do not allow real mode unless the master key and active credential are available.

- [ ] **Step 4: Run GREEN and race tests**

Run: `cd api && go test -race ./internal/ai -run 'TextExecutor|Worker|Lease' -count=5`

Expected: PASS with one provider invocation and no races.

- [ ] **Step 5: Commit**

```bash
git add api/internal/ai api/cmd/worker docker-compose.yml
git commit -m "feat(ai): execute auditable product text jobs"
```

### Task 6: Add Text Candidate Review, Approval, and Explicit Application APIs

**Files:**
- Create: `api/internal/ai/text_results.go`
- Create: `api/internal/ai/text_results_test.go`
- Modify: `api/internal/app/ai_dto.go`
- Modify: `api/internal/app/ai_handlers.go`
- Modify: `api/internal/app/ai_handlers_test.go`
- Modify: `api/internal/app/router.go`
- Modify: `api/openapi.yaml`
- Regenerate: `web/src/lib/openapi-types.ts`

**Interfaces:**
- Produces: safe result DTOs and endpoints to edit, approve/reject, preview application diff, apply, and read platform-content history.
- Authorization: admin/operator mutate; viewer behavior remains outside V1; compiled prompts and internal errors are never exposed to operators.

- [ ] **Step 1: Write failing lifecycle and RBAC tests**

Test cross-job/result rejection, immutable raw output, editable candidate JSON validation, one effective approval per item, approval required before application, idempotent application, transactional revision increment, before/after history, operator-safe DTOs, and explicit audit actors.

- [ ] **Step 2: Run RED**

Run: `cd api && go test ./internal/ai ./internal/app -run 'TextResult|PlatformContent' -count=1`

Expected: FAIL because lifecycle services/routes do not exist.

- [ ] **Step 3: Implement services and OpenAPI contracts**

Add nested job/item/result routes using UUIDs. Revalidate edited or raw candidate content on every transition. Applying title modifies only formal platform title; applying SEO modifies only formal SEO fields. Existing `SKU.PlatformTitle` and `SKU.SellingPoints` remain unchanged.

- [ ] **Step 4: Run GREEN and regenerate Web types**

Run: `cd api && go test ./internal/ai ./internal/app -run 'TextResult|PlatformContent' -count=1 && cd ../web && pnpm generate:api && pnpm typecheck`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add api/internal/ai/text_results* api/internal/app api/openapi.yaml web/src/lib/openapi-types.ts
git commit -m "feat(api): add AI text review and application"
```

### Task 7: Build the Web Text Review Experience

**Files:**
- Modify: `web/src/app/(dashboard)/ai-jobs/[jobId]/page.tsx`
- Modify: `web/src/app/(dashboard)/ai-jobs/[jobId]/page.test.tsx`
- Modify: `web/src/lib/i18n.tsx`
- Modify: `web/src/lib/ai-schemas.ts`
- Modify: `web/src/lib/ai-schemas.test.ts`

**Interfaces:**
- Consumes: Task 6 typed text-result and platform-content endpoints.
- Produces: candidate cards, validation warnings, edit form, approval/rejection, application diff, and revision confirmation.

- [ ] **Step 1: Write failing component tests**

Cover bilingual title/SEO candidates, character counts, structured selling points/keywords, validation warnings, edit cancellation, approval confirmation, disabled apply before approval, before/after diff, successful apply, sanitized errors, keyboard operation, and no compiled-prompt/secret rendering.

- [ ] **Step 2: Run RED**

Run: `cd web && pnpm test -- 'src/app/(dashboard)/ai-jobs/[jobId]/page.test.tsx' src/lib/ai-schemas.test.ts`

Expected: FAIL because results are not rendered.

- [ ] **Step 3: Implement the review UI**

Keep job progress polling unchanged. Render title and SEO candidates under their slot, use controlled forms with Zod validation, and invalidate only the job/platform-content queries affected by mutations. Require explicit confirmation before apply and announce mutation results through live regions.

- [ ] **Step 4: Run GREEN and Web verification**

Run: `cd web && pnpm test && pnpm typecheck && pnpm lint && pnpm exec next build --webpack`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add 'web/src/app/(dashboard)/ai-jobs/[jobId]' web/src/lib
git commit -m "feat(web): review and apply AI product text"
```

### Task 8: Verify Fake-Provider E2E and Add an Opt-In Real Smoke Harness

**Files:**
- Create: `api/internal/ai/openai_text_integration_test.go`
- Create: `api/cmd/openai-smoke/main.go`
- Create: `web/tests/e2e/ai-text-generation.spec.ts`
- Modify: `README.md`
- Modify: `docker-compose.yml`

**Interfaces:**
- Produces: deterministic fake-provider E2E by default and an explicit local smoke command that never runs in CI.

- [ ] **Step 1: Add failing acceptance tests**

The fake server must receive a sanitized Responses request and return two title/SEO candidates. Playwright must configure a fake credential, queue text slots, review/edit/approve/apply one result, and observe a platform-content revision. No image slot may call the provider.

- [ ] **Step 2: Run RED**

Run the isolated stack and `cd web && pnpm exec playwright test tests/e2e/ai-text-generation.spec.ts`.

Expected: FAIL until real executor and review flow are wired.

- [ ] **Step 3: Add smoke harness and documentation**

The smoke command requires `OPENAI_SMOKE_TEST=1`, reads a rotated key only from `OPENAI_API_KEY`, generates one minimal structured title response, prints only response/model/usage IDs, and never writes the key. Document that normal configuration must use the admin page and that pasted/chat-exposed keys must be revoked.

- [ ] **Step 4: Run complete verification**

Run: `cd api && go test -race ./... && go vet ./...`

Run: `cd web && pnpm generate:api && pnpm test && pnpm typecheck && pnpm lint && pnpm exec next build --webpack`

Run: `docker compose config --quiet && git diff --check`

Run the fake-provider Playwright acceptance. Run the real smoke command only after the user supplies a newly rotated key outside chat.

Expected: all default checks PASS without contacting OpenAI; optional smoke records non-zero usage but never prints the key.

- [ ] **Step 5: Commit**

```bash
git add api/internal/ai/openai_text_integration_test.go api/cmd/openai-smoke web/tests/e2e/ai-text-generation.spec.ts README.md docker-compose.yml
git commit -m "test: verify OpenAI product text workflow"
```

## Completion Gate

- Existing development databases migrate without data loss, and only the migrator changes schema.
- A newly rotated admin-configured key is decrypted only inside the worker and never returned or logged.
- Title and SEO Responses requests use strict structured output and the versioned L0-L4 compiler.
- Multiple candidates are immutable, editable through separate fields, independently approved, and never auto-applied.
- Platform content and every before/after revision are auditable.
- Usage and OpenAI request/response IDs are recorded; estimates are labeled.
- Image jobs make no real provider call in Phase 2.
- Go race/vet, Web tests/type/lint/build, Compose, fake-provider E2E, and diff checks pass.
