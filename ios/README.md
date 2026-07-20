# CargoFlow iOS

SwiftUI iOS client scaffold for SKU lookup, inventory adjustment, SOP-guided photo capture, and asset upload.

## Project Setup

Recommended:

```bash
cd ios
xcodegen generate
open CargoFlow.xcodeproj
```

If XcodeGen is not installed, create a new iOS App project in Xcode and add the files under `CargoFlow/`.

## Development Settings

- Default API base URL: `https://dev.cargoflows.cc/api/proxy/`
- Seed login after the Go API starts: `admin@cargoflow.local` / `password123`

The Cloudflare URL is injected through the `CargoFlowAPIBaseURL` Info.plist value. Change
`CARGOFLOW_API_BASE_URL` in `project.yml` if the Named Tunnel hostname changes.

## Packaging

From the repository root, build the downloadable Simulator package:

```bash
./scripts/package-ios.sh simulator
```

For an installable IPA, Xcode must have a valid Apple Developer signing identity and provisioning profile:

```bash
./scripts/package-ios.sh archive
```

The project persists automatic signing with Team ID `XDUSZQ42JP`. Override it for another
developer account with `CARGOFLOW_DEVELOPMENT_TEAM=YOUR_TEAM_ID`.

The signed development IPA is written to `web/public/downloads/CargoFlow.ipa`. It can be
installed with Xcode or Apple Configurator only on devices included in the provisioning profile.

## TestFlight

The project uses Bundle ID `com.cargoflow.app`, marketing version `0.1.0`, and an explicit build
number. Increment `CURRENT_PROJECT_VERSION` in `project.yml` before every successful upload.

Before the first upload:

1. Sign in under Xcode > Settings > Accounts and select team `XDUSZQ42JP`.
2. Create an Apple Distribution certificate if the Mac does not already have one.
3. Register `com.cargoflow.app` and create its app record in App Store Connect.
4. Add a non-transparent 1024x1024 App Store icon under
   `CargoFlow/Assets.xcassets/AppIcon.appiconset`.
5. Confirm `https://dev.cargoflows.cc/api/proxy/` and its API, worker, database, object storage,
   and Cloudflare Tunnel are available to remote devices.

For the first delivery, Xcode Organizer is the easiest place to resolve signing warnings:

1. Run `xcodegen generate` and open `CargoFlow.xcodeproj`.
2. Select `Any iOS Device (arm64)` and choose Product > Archive.
3. In Organizer, choose Distribute App > App Store Connect > Upload.

After the first upload has succeeded, the same archive-and-upload flow is available from the
repository root:

```bash
./scripts/package-ios.sh testflight
```

The command deliberately preserves the version and build number from `project.yml`. It fails
early when the App Icon is missing and warns when no local signing identity is visible; automatic
signing can still continue with an Xcode-managed cloud certificate. Uploaded builds appear in App
Store Connect after Apple's processing step; answer the export-compliance questions and add the
build to an internal TestFlight group before inviting testers.
