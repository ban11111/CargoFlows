# OpenAI Product Image, Multi-turn Editing, and Gallery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Follow RED-GREEN-REFACTOR for every behavior change.

**Goal:** Generate only the image slots selected by a user, support edits from any prior generated image or a clean restart, retain every turn and candidate, and let the user freely compare and select a final image without overwriting history.

**Architecture:** Initial image slots remain part of the existing AI job. Each image job item owns one durable image thread. Every generate, edit, or restart action creates an immutable queued turn; a turn creates one auditable `AIExecution` per requested candidate and one immutable result object per successful execution. The worker compiles versioned L0-L4 instructions, reads approved source assets only from internal object storage, calls the Responses API image-generation tool with `store=false`, validates and stores returned bytes in a separate private bucket, and records provider IDs and usage. Edits explicitly include the chosen parent result plus the frozen original approved product images, so CargoFlow—not provider-side conversation retention—is the system of record. The Web gallery groups all turns by slot, keeps every image visible, and changes only a selected-result pointer.

**Official API decision:** Use the Responses API image-generation tool through a current supported mainline model, initially `gpt-5.6`, with `action:generate` for roots/restarts and `action:edit` for child turns. OpenAI documents Responses as the conversational/multi-turn choice and supports explicit image inputs, image-generation call outputs, `previous_response_id`, and forced generate/edit actions. CargoFlow intentionally does not rely on `previous_response_id` as its only history because requests use `store=false`; each edit reconstructs context from CargoFlow-owned image bytes. `gpt-image-2` behavior is managed by the image tool and currently processes edit/reference images at high fidelity automatically. Source: https://developers.openai.com/api/docs/guides/image-generation

**Tech Stack:** Go 1.25, GORM/MySQL, Gin, MinIO private bucket, OpenAI Responses API over a provider adapter, Next.js 16, React Query, Vitest, Playwright, Docker Compose.

## Global Constraints

- Web only. Do not change iOS in this phase.
- Old development data is outside the compatibility contract. No legacy AI image rows, SOPs, or snapshots are migrated; the user will re-upload data after release.
- The one shared administrator-managed OpenAI Project API key remains encrypted in the database. No per-user key and no key in browser storage.
- Only explicitly selected image slots run. Text-only jobs never send images.
- Every turn, execution, provider ID, token count, actor, parent link, prompt hash, and selected-result change is auditable.
- Generated results never replace source product assets and never delete an earlier result.
- “Edit” always names one existing parent result. “Restart” creates a new root and does not inherit a generated parent.
- OpenAI request bodies may contain ephemeral base64 image bytes, but database rows, logs, audits, error messages, and compiled-prompt snapshots may not.
- Workers read source images by internal object key. Do not send MinIO credentials, internal endpoints, object keys, or long-lived/signed asset URLs to OpenAI.
- Generated images live in a separate private bucket. Browser access uses authenticated short-lived URLs or an authenticated proxy.
- Validate decoded MIME type, byte limit, dimensions, aspect ratio, and SHA-256 before storage. Never trust provider-declared format alone.
- Default generation is one candidate at `medium` quality. Candidate count remains 1-4 and every candidate is a separately billed/audited provider call.
- Default Responses requests use `store=false`; moderation remains `auto`. User-correctable moderation failures are not automatically retried.
- Image generation can take up to roughly two minutes; all UI operations are asynchronous and polling-safe.

---

### Task 1: Persist Immutable Image Threads, Turns, and Results

**Files:**
- Modify: `api/internal/models/ai.go`
- Create: `api/internal/models/ai_image_test.go`
- Modify: `api/internal/database/database.go`
- Create: `api/internal/database/ai_image_test.go`

**Interfaces:**
- `AIImageThread`: one row per image `AIJobItem`, nullable selected result.
- `AIImageTurn`: immutable request intent plus mutable queue/lease status; operations `generate`, `edit`, `restart`.
- `AIImageResult`: immutable stored candidate linked to one execution and optional parent result.

- [ ] **Step 1: Write failing model and constraint tests**

Cover one thread per image job item; turn sequence uniqueness; edit requires a parent from the same thread; generate/restart forbid a parent; candidate index uniqueness per turn; result execution uniqueness; selected result must belong to the thread; non-empty JSON defaults; and no cascade that deletes stored history.

- [ ] **Step 2: Run RED**

Run: `cd api && go test ./internal/models ./internal/database -run 'AIImage|ImageTurn|ImageResult' -count=1`

- [ ] **Step 3: Implement the greenfield schema**

Add thread, turn, and result models. A turn stores public ID, thread ID, sequence, operation, parent result ID, requested candidate count, size, quality, style, user instruction, compiled-request metadata, status, actor, lease owner/expiry, safe error, and timestamps. A result stores public ID, turn ID, execution ID, parent result ID, candidate index, private object key, MIME, width, height, byte count, SHA-256, provider image-call ID, and timestamps. Add `AIExecution.AIImageTurnID` and `AIExecutionQueued` without changing text execution semantics.

- [ ] **Step 4: Run GREEN and migration checks**

Run: `cd api && go test ./internal/models ./internal/database -run 'AIImage|ImageTurn|ImageResult|Migrate' -count=1`

- [ ] **Step 5: Commit**

```bash
git add api/internal/models api/internal/database
git commit -m "feat(ai): add immutable image turn history"
```

### Task 2: Compile Versioned L0-L4 Image Requests

**Files:**
- Create: `api/internal/ai/image_prompt_compiler.go`
- Create: `api/internal/ai/image_prompt_compiler_test.go`
- Create: `api/internal/ai/testdata/image_generate_prompt.golden.json`
- Create: `api/internal/ai/testdata/image_edit_prompt.golden.json`

**Interfaces:**
- `CompileImagePrompt(snapshot ProductSnapshotV1, slot SlotFacts, turn ImageTurnInput) (CompiledImagePrompt, error)`.
- Output includes instructions, normalized non-binary input, ordered internal asset descriptors, tool config, layer versions, and SHA-256.

- [ ] **Step 1: Write failing golden and security tests**

Assert L0 safety and exact-product preservation; L1 `pcs_object_v1` coordinate explanation; L2 platform requirements; L3 slot/style/layout rules; L4 delimited optional user instruction; selected SOP view order; explicit output size/quality; edit parent semantics; restart semantics; and rejection of URLs, object keys, credentials, unsupported variables, unknown generation options, or mismatched parent threads in persisted prompt material.

- [ ] **Step 2: Run RED**

Run: `cd api && go test ./internal/ai -run 'CompileImagePrompt' -count=1`

- [ ] **Step 3: Implement the compiler**

Keep fixed policies in code as `l0-image-product-safety-v1` and `l1-image-product-context-v1`. Explain every SOP vector and normalized target. State that coordinates control viewpoint/composition only and cannot establish product claims. Require exact SKU identity, labels, color, proportions, package variant, and `allow_mirror=false`. Distinguish image description/copy requests from factual product data. Persist only internal asset public IDs, view facts, MIME/dimensions/hash, never bytes or locators.

- [ ] **Step 4: Run GREEN**

Run: `cd api && go test ./internal/ai -run 'CompileImagePrompt' -count=1`

- [ ] **Step 5: Commit**

```bash
git add api/internal/ai/image_prompt_compiler* api/internal/ai/testdata/image_*
git commit -m "feat(ai): compile layered product image prompts"
```

### Task 3: Add a Provider-neutral Responses Image Client

**Files:**
- Create: `api/internal/ai/image_provider.go`
- Create: `api/internal/ai/openai_image_responses_client.go`
- Create: `api/internal/ai/openai_image_responses_client_test.go`
- Modify: `api/internal/config/config.go`
- Modify: `api/internal/config/config_test.go`
- Modify: `api/.env.example`

**Interfaces:**
- `ImageProvider.Generate(context.Context, []byte, ImageRequest) (ImageResponse, error)`.
- Request carries ephemeral `ImageInput` bytes and a compiled non-binary prompt; response carries decoded image bytes, call/response/request IDs, model, MIME, and usage.

- [ ] **Step 1: Write failing fake-server contract tests**

Assert `POST /v1/responses`; bearer auth; `store:false`; configured mainline model; input text plus exact image content blocks; `tools:[{type:"image_generation",action:"generate|edit",size,quality,moderation:"auto"}]`; forced tool choice where supported; all `image_generation_call` outputs parsed; base64 bounds; request-ID capture; token usage; zero request/response logging; blocked/refusal classification; no retry for ambiguous timeout or moderation/user errors; bounded retry only when demonstrably safe.

- [ ] **Step 2: Run RED**

Run: `cd api && go test ./internal/ai -run 'ResponsesImageClient' -count=1`

- [ ] **Step 3: Implement the HTTP adapter**

Default `OPENAI_IMAGE_TOOL_MODEL=gpt-5.6`, timeout 180 seconds, `store=false`, moderation `auto`. Use data URLs constructed in memory from validated source bytes. For edit turns include the chosen parent first, followed by the frozen original approved product references. Decode output with a strict maximum size and clear credential/input/output byte slices after use.

- [ ] **Step 4: Run GREEN**

Run: `cd api && go test ./internal/ai ./internal/config -run 'ResponsesImageClient|OpenAIImage' -count=1`

- [ ] **Step 5: Commit**

```bash
git add api/internal/ai/image_provider.go api/internal/ai/openai_image_responses_client* api/internal/config api/.env.example
git commit -m "feat(ai): add OpenAI Responses image client"
```

### Task 4: Add Private Generated-image Storage and Validation

**Files:**
- Create: `api/internal/ai/image_storage.go`
- Create: `api/internal/ai/image_storage_test.go`
- Modify: `api/internal/config/config.go`
- Modify: `api/internal/app/object_store.go`
- Modify: `api/cmd/worker/main.go`
- Modify: `docker-compose.yml`

**Interfaces:**
- `ImageObjectStore` reads frozen source objects by internal key, writes generated objects by server-chosen key, and issues short authenticated read access through the API layer.

- [ ] **Step 1: Write failing validation/storage tests**

Cover PNG/JPEG/WebP magic detection, decode failure, decompression bombs, max bytes/pixels, exact dimensions/aspect tolerance, SHA-256, deterministic safe object-key prefix, private bucket policy, idempotent same-hash storage, cleanup after DB failure, and no caller-provided output key.

- [ ] **Step 2: Run RED**

Run: `cd api && go test ./internal/ai ./internal/app -run 'ImageStorage|GeneratedBucket' -count=1`

- [ ] **Step 3: Implement storage**

Use a separate `MINIO_AI_BUCKET=cargoflow-ai-private` without public-read policy. Store originals such as `generated/{job_public_id}/{item_public_id}/{turn_public_id}/{candidate_index}-{sha256}.{ext}`. Generate thumbnails server-side only if needed for gallery performance; retain the original immutable object.

- [ ] **Step 4: Run GREEN with MinIO integration**

Run focused unit tests, then run a disposable MinIO integration that proves anonymous GET is denied while authenticated worker read/write succeeds.

- [ ] **Step 5: Commit**

```bash
git add api/internal/ai/image_storage* api/internal/config api/internal/app/object_store.go api/cmd/worker docker-compose.yml
git commit -m "feat(ai): store generated images privately"
```

### Task 5: Execute Initial and Follow-up Image Turns Idempotently

**Files:**
- Create: `api/internal/ai/image_turn_queue.go`
- Create: `api/internal/ai/image_turn_queue_test.go`
- Create: `api/internal/ai/image_executor.go`
- Create: `api/internal/ai/image_executor_test.go`
- Modify: `api/internal/ai/worker.go`
- Modify: `api/internal/ai/worker_test.go`
- Modify: `api/cmd/worker/main.go`

**Interfaces:**
- Initial image `AIJobItem` creates one thread and root turn, then uses the same executor.
- Follow-up turns are leased independently by `ImageTurnQueue`.
- One candidate equals one `AIExecution` and one provider call.

- [ ] **Step 1: Write failing queue/executor tests**

Cover selected slots only; candidate count 1-4; partial-turn recovery without duplicating completed candidates; per-candidate execution/usage/audit; credential decryption and clearing per call; internal source reads; parent-first edit inputs; restart excluding generated parents; lease heartbeat; cancellation; auth/rate-limit/moderation/refusal/invalid-image failures; ambiguous outcome to `needs_attention`; storage-before-DB crash recovery; and no auto-deletion of any result.

- [ ] **Step 2: Run RED**

Run: `cd api && go test -race ./internal/ai -run 'ImageTurnQueue|ImageExecutor|ImageWorker' -count=1`

- [ ] **Step 3: Implement state transitions**

Compile and persist non-binary request metadata before provider calls. Call outside DB transactions. Persist provider response metadata before object storage when recovery requires it, but never persist base64 bytes. Validate/store each image, then atomically create result, usage ledger, and audit. A turn completes when all requested candidates are durable. Initial item completion follows root-turn completion; follow-up failures do not change the original job’s completed text/image history.

- [ ] **Step 4: Run race and recovery tests**

Run: `cd api && go test -race ./internal/ai -run 'ImageTurnQueue|ImageExecutor|ImageWorker|Lease' -count=5`

- [ ] **Step 5: Commit**

```bash
git add api/internal/ai/image_* api/internal/ai/worker* api/cmd/worker
git commit -m "feat(ai): execute multi-turn image requests"
```

### Task 6: Add Image History, Edit, Restart, Selection, and Access APIs

**Files:**
- Create: `api/internal/ai/image_results.go`
- Create: `api/internal/ai/image_results_test.go`
- Modify: `api/internal/app/ai_dto.go`
- Modify: `api/internal/app/ai_handlers.go`
- Modify: `api/internal/app/ai_handlers_test.go`
- Modify: `api/internal/app/router.go`
- Modify: `api/openapi.yaml`
- Regenerate: `web/src/lib/openapi-types.ts`

**Endpoints:**
- `GET /ai-jobs/{job}/image-threads`
- `POST /ai-jobs/{job}/items/{item}/image-turns` with operation `edit|restart`, optional parent result, instruction, and candidate count.
- `POST /ai-jobs/{job}/items/{item}/image-results/{result}/select`
- `GET /ai-jobs/{job}/items/{item}/image-results/{result}/content` or a short-lived access descriptor.

- [ ] **Step 1: Write failing lifecycle/RBAC tests**

Test operator/admin access, viewer denial, cross-job/thread/parent rejection, edit parent required, restart parent forbidden, immutable completed turn input, one selected result per thread, idempotent selection, selection history audit, short-lived access without object-key disclosure, safe errors, and complete chronological history including failed turns.

- [ ] **Step 2: Run RED**

Run: `cd api && go test ./internal/ai ./internal/app -run 'ImageThread|ImageTurn|ImageResult' -count=1`

- [ ] **Step 3: Implement API contracts**

Selection changes only `AIImageThread.SelectedResultID`; it never mutates or deletes a result. `edit` accepts any result in the same thread, not only the currently selected one. `restart` uses the original frozen job snapshot. Return public IDs and access URLs only; never expose compiled prompts, provider output, object keys, internal errors, or credentials.

- [ ] **Step 4: Run GREEN and regenerate types**

Run: `cd api && go test ./internal/ai ./internal/app -run 'ImageThread|ImageTurn|ImageResult' -count=1 && cd ../web && pnpm generate:api && pnpm typecheck`

- [ ] **Step 5: Commit**

```bash
git add api/internal/ai/image_results* api/internal/app api/openapi.yaml web/src/lib/openapi-types.ts
git commit -m "feat(api): add multi-turn image gallery lifecycle"
```

### Task 7: Build the Web Image History Gallery and Turn Composer

**Files:**
- Create: `web/src/components/ai/image-history-gallery.tsx`
- Create: `web/src/components/ai/image-history-gallery.test.tsx`
- Modify: `web/src/app/(dashboard)/ai-jobs/[jobId]/page.tsx`
- Modify: `web/src/app/(dashboard)/ai-jobs/[jobId]/page.test.tsx`
- Modify: `web/src/lib/i18n.tsx`

**UX:**
- Slot tabs or sections; chronological turn groups; visible parent/restart relationship; full history retained.
- Select any image, edit from any image, regenerate/restart, compare current selection, and poll queued/running turns.

- [ ] **Step 1: Write failing bilingual component tests**

Cover multiple slots, multiple turns, failed turn with older images intact, selected badge, selecting an older candidate, edit from non-selected candidate, restart confirmation, optional prompt, candidate count, busy/poll state, responsive gallery, keyboard/focus behavior, alt text, authenticated image loading, sanitized errors, and no object key/provider prompt/secret rendering.

- [ ] **Step 2: Run RED**

Run: `cd web && pnpm test -- src/components/ai/image-history-gallery.test.tsx 'src/app/(dashboard)/ai-jobs/[jobId]/page.test.tsx'`

- [ ] **Step 3: Implement the gallery**

Use stable aspect-ratio cards, lazy-loaded thumbnails, a larger comparison view, and a compact turn timeline. An edit action opens a composer anchored to the chosen parent. A restart action clearly states it returns to original approved product images and fixed L0-L3 rules. Never hide earlier generations after a new turn.

- [ ] **Step 4: Run Web verification and mobile browser QA**

Run: `cd web && pnpm test && pnpm typecheck && pnpm lint && pnpm exec next build --webpack`. Verify at 390px and desktop widths with no horizontal overflow and accessible focus order.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/ai/image-history-gallery* 'web/src/app/(dashboard)/ai-jobs/[jobId]' web/src/lib/i18n.tsx
git commit -m "feat(web): add multi-turn image history gallery"
```

### Task 8: Verify the Full Fake-provider Image Workflow

**Files:**
- Modify: `api/cmd/fake-openai/main.go`
- Create: `api/internal/ai/openai_image_integration_test.go`
- Create: `web/tests/e2e/ai-image-generation.spec.ts`
- Modify: `scripts/run-ai-text-e2e.sh` or create `scripts/run-ai-image-e2e.sh`
- Modify: `README.md`
- Modify: `docker-compose.yml`

- [ ] **Step 1: Extend the fake provider safely**

Return deterministic base64 test images for generate/edit actions, record only sanitized request facts, and expose test endpoints only inside the loopback-bound isolated E2E profile. Assert edit receives parent plus original references and restart does not receive a generated parent.

- [ ] **Step 2: Add full real-stack Playwright acceptance**

Using a PID-unique Compose project and dynamic loopback ports, configure the fake credential through the admin page; create a template with two image slots; select only one; generate two candidates; select candidate 2; edit candidate 1; confirm all three images remain; select the edited child; restart; confirm the new root and every prior image remain. Assert the unselected slot made zero calls and verify DB/API audit/usage through public contracts.

- [ ] **Step 3: Run complete verification**

Run Go race/vet, all Web checks/build, Compose config/diff checks, private-bucket integration, and isolated fake-provider Playwright. Do not run a real image smoke test with a chat-exposed key. A future opt-in real smoke must use a newly rotated key outside chat and official HTTPS only.

- [ ] **Step 4: Independent security/code review**

Review for key/image leakage, public object access, cross-SKU parent edits, silent history replacement, duplicate billing after retries, ambiguous provider outcomes, prompt injection, moderation handling, and E2E isolation. Resolve every P1/P2.

- [ ] **Step 5: Commit**

```bash
git add api/cmd/fake-openai api/internal/ai/openai_image_integration_test.go web/tests/e2e/ai-image-generation.spec.ts scripts README.md docker-compose.yml
git commit -m "test: verify OpenAI multi-turn image workflow"
```

## Completion Gate

- Users can choose any subset of suite slots; only selected slots produce calls and cost.
- Title/SEO behavior from Phase 2 remains unchanged.
- Every image candidate and every edit/restart turn remains visible and immutable.
- Users can select any prior or current result; selection is audited and does not delete history.
- Edits can branch from any result; restarts create new roots from frozen original data.
- All provider requests use the shared encrypted administrator key, `store=false`, safe metadata, and no external asset locator leakage.
- Generated originals are in a private bucket and are inaccessible anonymously.
- Each candidate has an execution, usage ledger, provider/request IDs, prompt hash, actor, and parent relationship.
- Retry/recovery cannot silently duplicate a paid call; ambiguous outcomes require attention.
- Full Go race/vet, Web tests/type/lint/build, Compose, private storage checks, isolated fake-provider image E2E, and independent review pass.
