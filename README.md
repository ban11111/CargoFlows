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
