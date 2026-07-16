# CargoFlow

CargoFlow is an internal SKU inventory, product-content, SOP photo, and AI asset workflow system.

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

The API seeds a development user on first boot:

```text
admin@cargoflow.local / password123
```

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

For iOS:

```bash
cd ios
xcodegen generate
open CargoFlow.xcodeproj
```

If XcodeGen is not installed, create a new iOS App project in Xcode and add files from `ios/CargoFlow/`.

## API Defaults

- API: `http://localhost:8080`
- MySQL: `localhost:3306`
- MinIO API: `http://localhost:9000`
- MinIO Console: `http://localhost:9001`

The Web BFF calls the Go API through `/api/proxy/*`; the iOS app calls `http://127.0.0.1:8080/api/v1` by default.
