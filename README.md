# CargoFlow

CargoFlow is an internal SKU inventory, product-content, product-capture SOP, and AI asset workflow system.

## Modules

- `api/`: Go + Gin + GORM + MySQL backend.
- `web/`: Next.js App Router + React + TypeScript admin console.
- `ios/`: SwiftUI iOS client scaffold.

## UI Hard Rule

Every user-facing Web and iOS UI must provide both Simplified Chinese and English,
and must update immediately when the user switches language. New fixed labels,
validation messages, empty states, and navigation items must be added to both
translation catalogs. Managed product categories must always store a Chinese name
and an English name; all category displays must follow the selected language.

## Local Development

Start infrastructure and the Go API:

```bash
docker compose up --build mysql minio api
```

The API seeds a shared development account for both the Web admin and iOS app on first boot:

```text
admin@cargoflow.local / password123
```

Web login: `http://localhost:3005/login`

iOS login: use the same email and password after the API is running.

Run the Web console:

```bash
cd web
pnpm install
pnpm dev:3005
```

Open:

```text
http://localhost:3005
```

### AI worker modes

Dry-run mode exercises the template, snapshot, queue, and worker flow without
sending product content or images to OpenAI. It does not need an OpenAI API key.

Generate a fresh 32-byte local encryption master key in the API terminal. This
command assigns the base64 value to the environment without printing it:

```bash
export CARGOFLOW_SECRETS_MASTER_KEY="$(openssl rand -base64 32)"
```

The master key protects any credential configured later through the admin UI.
Do not commit it, paste it into logs, or reuse the development key in production.
Losing it makes encrypted credentials unrecoverable, so production deployments
must store it in their secret manager.

Start MySQL and MinIO, then run the API from the same terminal so it receives the
master key:

```bash
docker compose up -d --wait mysql minio
cd api
go run ./cmd/migrate
go run ./cmd/server
```

In a second terminal, start the worker in dry-run mode:

```bash
cd api
export AI_WORKER_DRY_RUN=true
export AI_WORKER_POLL_INTERVAL=1s
go run ./cmd/worker
```

The worker records zero-usage dry-run executions locally. To execute title and
SEO-description slots through OpenAI, first configure a newly rotated project
API key from the administrator OpenAI settings page, then start the worker with
`AI_WORKER_DRY_RUN=false` in a terminal that has the same
`CARGOFLOW_SECRETS_MASTER_KEY`. Never place an OpenAI key in shell commands,
environment files, source code, logs, or chat. Real image slots are rejected
without a provider call until image execution is implemented.

All normal AI jobs use that single administrator-managed credential. The API
encrypts it in the database, never returns the plaintext, and the worker decrypts
it only immediately before a provider call. Product text candidates remain
separate from formal platform content until an operator edits (optionally),
approves, previews, and explicitly applies one candidate; each application creates
an immutable revision.

### OpenAI verification

The default verification suite is deterministic and does not contact OpenAI:

```bash
cd api
go test ./internal/ai ./cmd/openai-smoke

cd ../web
../scripts/run-ai-text-e2e.sh
```

The Go integration test sends a real Responses-format request to a local fake
provider and verifies request sanitization, candidate persistence, token audit,
credential clearing, and zero provider calls for image slots. The Playwright
harness starts a clean MySQL/API/live worker/fake-provider stack, configures the
fake credential through the administrator page, and exercises the real Web,
authentication, API, encrypted credential, queue, provider, persistence, edit,
approval, preview, application, and revision workflow. It tears down its isolated
Compose project and volumes after the test.

An optional real-provider smoke command exists only for a developer-controlled
local check. It is disabled unless `OPENAI_SMOKE_TEST=1`, accepts a newly rotated
key only through the process environment, refuses non-official API base URLs,
uses `store=false`, and prints only validated
provider IDs, model, and token counts—never generated content, prompts, or the
credential. Do not run it in CI. Prefer the admin settings page for all normal
configuration. If a key has ever been pasted into chat, logs, source, or a command,
revoke it before doing anything else.

For a one-off local smoke check, enter the rotated key without terminal echo and
run the command in the same shell:

```bash
read -rs OPENAI_API_KEY
export OPENAI_API_KEY OPENAI_SMOKE_TEST=1
cd api
go run ./cmd/openai-smoke
unset OPENAI_API_KEY OPENAI_SMOKE_TEST
```

The same opt-in harness is available through the Compose `smoke` profile after
setting those environment values: `docker compose --profile smoke run --rm openai-smoke`.

The Web product-capture SOP routes are:

- `/sop-templates` for lifecycle management;
- `/sop-templates/new` to create a V1 draft;
- `/sop-templates/{sopId}/versions/{versionId}` to edit a concrete draft version.

Regenerate the typed Web API contract after changing `api/openapi.yaml`:

```bash
cd web
pnpm generate:api
```

For iOS:

```bash
cd ios
xcodegen generate
open CargoFlow.xcodeproj
```

Build a downloadable iOS Simulator package:

```bash
./scripts/package-ios.sh simulator
```

The package is written to `web/public/downloads/` and served by the landing page download button.
The iOS development build currently reaches the API through the active Cloudflare Web tunnel at
`https://dev.cargoflows.cc/api/proxy/`. Update
`CARGOFLOW_API_BASE_URL` in `ios/project.yml` when the quick-tunnel URL changes.

The Web proxy also rewrites signed loopback MinIO upload tickets to
`/api/storage/...`. This lets browsers and iPhones upload through the same Cloudflare hostname
while keeping MinIO bound to localhost. Configure `MINIO_UPLOAD_BASE_URL` and
`MINIO_SOURCE_BUCKET` on the Web service if the local object-store address or bucket changes.

To export a signed development IPA, configure an Apple Developer signing identity and provisioning profile, then run:

```bash
./scripts/package-ios.sh archive
```

Automatic signing is persisted in `ios/project.yml`. Set
`CARGOFLOW_DEVELOPMENT_TEAM=YOUR_TEAM_ID` only when overriding the configured team. The landing
page serves the signed IPA first, with the Simulator zip as a fallback when no IPA is present.

If XcodeGen is not installed, create a new iOS App project in Xcode and add files from `ios/CargoFlow/`.

## Product-Capture SOP Model

A capture SOP describes only the product views to photograph. It is not a generic workflow and does not model setup, lighting, review, approval, or AI-generation steps.

Every SOP version uses the right-handed `pcs_object_v1` object coordinate system. Its origin is the product bounding-box center and coordinates are normalized: `+X` is product top, `+Y` is product left, and `+Z` is product front. Pose vectors encode camera angle and final-image orientation, not camera distance.

Each new V1 draft starts with exactly one required, fixed `reference_front` View at sequence 1. It uses camera direction `[0, 0, 1]`, image-up direction `[1, 0, 0]`, and target `[0, 0, 0]`. The `packaging_front` preset is a separate optional standard View for the complete package front.

Versions move through `draft`, `published`, and `archived` states. Drafts are editable; publishing validates and freezes the version. Published versions reject mutation. Create the next draft by copying a selected published version: the copy receives a new version UUID and fresh View UUIDs while retaining the structured instructions and immutable reference-media relationships.

The iOS capture flow lists published versions for the SKU category, lets the photographer select an exact version, and creates the photo session against that version UUID. Its View checklist tracks required and optional captures independently; only required Views gate completion.

This release is a greenfield structured-data design. Development and deployment assume a clean database; there is no legacy SOP migration, backfill, or compatibility layer.

## API Defaults

- API: `http://localhost:8080`
- MySQL: `localhost:3306`
- MinIO API: `http://localhost:9000`
- MinIO Console: `http://localhost:9001`

### Local MinIO Console Login

Use these development-only credentials at `http://localhost:9001`:

```text
Username: cargoflow
Password: cargoflow123
```

The Web BFF calls the Go API through `/api/proxy/*`; the iOS app calls `http://127.0.0.1:8080/api/v1` by default.
