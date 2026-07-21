# CargoFlows

CargoFlows is an internal SKU inventory, product-content, product-capture SOP, and AI asset workflow system.

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

The root Makefile provides the normal development workflow:

```bash
make dev                    # Start backend and frontend
make re-dev                 # Rebuild/restart backend only
make dev backend            # Start backend only
make dev frontend           # Start frontend only
make re-dev backend         # Rebuild/recreate backend application services
make help                   # Show all commands and aliases
```

`SCOPE` syntax is also supported, for example `make dev SCOPE=backend`.
Frontend commands manage the existing `com.cargoflows.web-dev` launchd agent;
they do not start a second Next.js process in the terminal. `make dev frontend`
ensures the agent is loaded and running, while `make re-dev frontend` restarts it.
The system `com.cloudflare.cloudflared` daemon remains independently managed;
Make checks and reports its status without reinstalling or restarting it.

`make dev` does not force Docker image rebuilds. By default, `make re-dev`
rebuilds and recreates only `migrate`, `api`, and `worker`, while preserving the
MySQL and MinIO containers and their named volumes. Use `make re-dev frontend`
to restart the frontend launchd agent, or `make re-dev SCOPE=all` to rebuild the
backend and restart the frontend together.

Start infrastructure and the Go API:

```bash
docker compose up --build mysql minio api
```

The API seeds a shared development account for both the Web admin and iOS app on first boot:

```text
admin@cargoflows.cc / password123
```

Web login: `http://localhost:3005/login`

iOS login: use the same email and password after the API is running.

Without the launchd agent, the Web console can still be run manually in the foreground:

```bash
cd web
pnpm install
pnpm dev:3005
```

Open:

```text
http://localhost:3005
```

### Current development runtime

The current remote-development workstation uses a hybrid runtime:

- Docker Compose runs MySQL, MinIO, the Go API, migrations, and the AI worker.
- A user-level launchd agent runs the Next.js development server on port `3005`.
- A system-level `cloudflared` daemon publishes the Web server at
  `https://dev.cargoflows.cc`.
- The browser and iOS client reach the API through the same-origin Web BFF at
  `/api/proxy/*`; the Go API remains bound to localhost port `8080`.

Start or rebuild the backend stack with:

```bash
docker compose up -d --build mysql minio api worker
```

The root `.env` contains local runtime settings consumed by Compose. Never add
an OpenAI API key, Cloudflare Tunnel token, or plaintext administrator credential
to that file. OpenAI credentials must be configured through the super administrator UI
and remain encrypted in the database.

### launchd-managed Web development server

The checked-in, workstation-specific launchd definition is:

```text
scripts/launchd/com.cargoflows.web-dev.plist
```

It runs the locally installed Next.js CLI directly with Node 24, uses `web/` as
its working directory, listens on port `3005`, starts when the user logs in, and
uses `KeepAlive` to restart after either a clean or abnormal exit. It is installed
at:

```text
/Users/zhengbaiyi/Library/LaunchAgents/com.cargoflows.web-dev.plist
```

Install or reload it after changing the plist:

```bash
plutil -lint scripts/launchd/com.cargoflows.web-dev.plist
launchctl bootout gui/$(id -u)/com.cargoflows.web-dev 2>/dev/null || true
install -d -m 755 /Users/zhengbaiyi/Library/LaunchAgents
install -m 644 scripts/launchd/com.cargoflows.web-dev.plist \
  /Users/zhengbaiyi/Library/LaunchAgents/com.cargoflows.web-dev.plist
launchctl bootstrap gui/$(id -u) \
  /Users/zhengbaiyi/Library/LaunchAgents/com.cargoflows.web-dev.plist
```

Operational commands:

```bash
# Status
launchctl print gui/$(id -u)/com.cargoflows.web-dev

# Restart immediately
launchctl kickstart -k gui/$(id -u)/com.cargoflows.web-dev

# Follow Next.js output and errors
tail -f tmp/web-launchd.out.log
tail -f tmp/web-launchd.err.log

# Disable and unload
launchctl bootout gui/$(id -u)/com.cargoflows.web-dev
```

This is a LaunchAgent, so it starts after the macOS user logs in. If the site
must be available immediately after a reboot and before login, promote it to a
root-installed LaunchDaemon with an explicit unprivileged `UserName`; do not run
the Node process as root. If Homebrew upgrades or removes the versioned Node path,
update `ProgramArguments` in the plist and reload it.

### Cloudflare remote-development tunnel

The named Cloudflare Tunnel is a system launchd daemon:

```text
Label:  com.cloudflare.cloudflared
Origin: http://127.0.0.1:3005
Public: https://dev.cargoflows.cc
Logs:   /Library/Logs/com.cloudflare.cloudflared.err.log
```

The Tunnel token is stored outside the repository. Never copy it into README,
`.env`, source code, shell history, logs, or chat.

Use these checks when the public domain returns `502 Bad Gateway`:

```bash
# 1. Is the Next.js origin running?
curl -I http://127.0.0.1:3005/

# 2. Is the Go backend healthy?
curl http://127.0.0.1:8080/healthz

# 3. Is the public route healthy?
curl -I https://dev.cargoflows.cc/
```

Interpretation and recovery:

- Local Web fails, API succeeds: restart
  `gui/$(id -u)/com.cargoflows.web-dev`.
- Local Web succeeds, public URL fails: inspect the cloudflared log, then run
  `sudo launchctl kickstart -k system/com.cloudflare.cloudflared`.
- Local Web and API both fail: inspect `docker compose ps`, then restore the
  backend stack and Web agent independently.
- The Mac has no Internet connection: the outbound Tunnel cannot serve traffic;
  cloudflared retries automatically after connectivity and DNS recover.

Cloudflare returning 502 is not by itself evidence that the Go backend failed.
It commonly means the Tunnel cannot connect to the Web origin on port `3005`.

### AI worker modes

Dry-run mode exercises the template, snapshot, queue, and worker flow without
sending product content or images to OpenAI. It does not need an OpenAI API key.

Generate a fresh 32-byte local encryption master key in the API terminal. This
command assigns the base64 value to the environment without printing it:

```bash
export CARGOFLOWS_SECRETS_MASTER_KEY="$(openssl rand -base64 32)"
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

The worker records zero-usage dry-run executions locally. To execute text and
image slots through OpenAI, first configure a newly rotated project
API key from the super administrator OpenAI settings page, then start the worker with
`AI_WORKER_DRY_RUN=false` in a terminal that has the same
`CARGOFLOWS_SECRETS_MASTER_KEY`. Never place an OpenAI key in shell commands,
environment files, source code, logs, or chat. Image jobs can create multiple
independently configured canvases; each canvas selects one or more published
image requirements, and a requirement may be reused across canvases.

All normal AI jobs use that single super-administrator-managed credential. The API
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

The local fake provider supports text Responses, the Responses image tool, and
Images generation/edit endpoints. It returns deterministic PNGs and records only
sanitized request facts (model, action, input count, mask/parent presence and safe
metadata), never prompts, image bytes, bearer credentials, or object locators.
The Go integration tests verify request sanitization, candidate/image decoding,
masked multipart edits, token audit, credential clearing, and provider-call controls. The Playwright
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
open CargoFlows.xcodeproj
```

Build a downloadable iOS Simulator package:

```bash
./scripts/package-ios.sh simulator
```

The package is written to `web/public/downloads/` and served by the landing page download button.
The iOS development build currently reaches the API through the named Cloudflare Web tunnel at
`https://dev.cargoflows.cc/api/proxy/`. Update
`CARGOFLOWS_API_BASE_URL` in `ios/project.yml` if the named development hostname changes.

The Web proxy also rewrites signed loopback MinIO upload tickets to
`/api/storage/...`. This lets browsers and iPhones upload through the same Cloudflare hostname
while keeping MinIO bound to localhost. Configure `MINIO_UPLOAD_BASE_URL` and
`MINIO_SOURCE_BUCKET` on the Web service if the local object-store address or bucket changes.

To export a signed development IPA, configure an Apple Developer signing identity and provisioning profile, then run:

```bash
./scripts/package-ios.sh archive
```

Automatic signing is persisted in `ios/project.yml`. Set
`CARGOFLOWS_DEVELOPMENT_TEAM=YOUR_TEAM_ID` only when overriding the configured team. The landing
page serves the signed IPA first, with the Simulator zip as a fallback when no IPA is present.

### TestFlight distribution

The iOS target uses Bundle ID `com.cargoflows.app`, marketing version `0.1.0`, and build number
`1`. Increase `CURRENT_PROJECT_VERSION` in `ios/project.yml` for every new successful delivery.
The generated Xcode project should never be the only place where version or signing settings are
changed because `xcodegen generate` overwrites it.

Before uploading, sign in to the configured team under Xcode > Settings > Accounts, create an
Apple Distribution certificate, register the Bundle ID, create the App Store Connect app record,
and add a 1024x1024 App Store icon. The first upload is best performed through Xcode Organizer so
that signing and App Store validation messages are visible. Later uploads can use:

```bash
./scripts/package-ios.sh testflight
```

This mode archives with automatic signing and uploads with the `app-store-connect` distribution
method. It does not publish a public App Store release. After Apple finishes processing the build,
complete export-compliance and beta test information in App Store Connect and assign the build to
an internal TestFlight group. See `ios/README.md` for the complete checklist.

If XcodeGen is not installed, create a new iOS App project in Xcode and add files from `ios/CargoFlows/`.

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
Username: cargoflows
Password: cargoflows123
```

The Web BFF calls the Go API through `/api/proxy/*`; the iOS app calls `http://127.0.0.1:8080/api/v1` by default.
