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
