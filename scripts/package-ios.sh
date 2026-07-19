#!/bin/zsh
set -euo pipefail

SCRIPT_DIR="${0:A:h}"
REPO_ROOT="${SCRIPT_DIR:h}"
IOS_DIR="$REPO_ROOT/ios"
DOWNLOAD_DIR="$REPO_ROOT/web/public/downloads"
BUILD_ROOT="$REPO_ROOT/ios/build"
MODE="${1:-simulator}"

FULL_XCODE_DEVELOPER_DIR="/Applications/Xcode.app/Contents/Developer"
ACTIVE_DEVELOPER_DIR="$(xcode-select -p 2>/dev/null || true)"

if [[ "$ACTIVE_DEVELOPER_DIR" != *"Xcode.app/Contents/Developer"* ]]; then
  if [[ -d "$FULL_XCODE_DEVELOPER_DIR" ]]; then
    export DEVELOPER_DIR="$FULL_XCODE_DEVELOPER_DIR"
  else
    print -u2 "Full Xcode was not found. Install Xcode and run: sudo xcode-select --switch /Applications/Xcode.app/Contents/Developer"
    exit 1
  fi
fi

if ! xcodebuild -version >/dev/null 2>&1; then
  print -u2 "xcodebuild is unavailable. Open Xcode once, finish first-launch setup, and retry."
  exit 1
fi

if ! command -v xcodegen >/dev/null 2>&1; then
  print -u2 "XcodeGen is required. Install it, then retry."
  exit 1
fi

mkdir -p "$DOWNLOAD_DIR" "$BUILD_ROOT"
cd "$IOS_DIR"
xcodegen generate

case "$MODE" in
  simulator)
    DERIVED_DATA="$BUILD_ROOT/DerivedData-Simulator"
    APP_PATH="$DERIVED_DATA/Build/Products/Release-iphonesimulator/CargoFlow.app"
    OUTPUT_PATH="$DOWNLOAD_DIR/CargoFlow-iOS-Simulator.zip"

    xcodebuild \
      -project CargoFlow.xcodeproj \
      -scheme CargoFlow \
      -configuration Release \
      -sdk iphonesimulator \
      -destination "generic/platform=iOS Simulator" \
      -derivedDataPath "$DERIVED_DATA" \
      CODE_SIGNING_ALLOWED=NO \
      build

    if [[ ! -d "$APP_PATH" ]]; then
      print -u2 "Build completed, but CargoFlow.app was not found at $APP_PATH"
      exit 1
    fi

    ditto -c -k --sequesterRsrc --keepParent "$APP_PATH" "$OUTPUT_PATH"
    cd "$DOWNLOAD_DIR"
    shasum -a 256 "${OUTPUT_PATH:t}" > "${OUTPUT_PATH:t}.sha256"
    print "Created $OUTPUT_PATH"
    ;;

  archive)
    PROJECT_TEAM_ID="${CARGOFLOW_DEVELOPMENT_TEAM:-$(xcodebuild -project CargoFlow.xcodeproj -scheme CargoFlow -configuration Release -showBuildSettings 2>/dev/null | awk '/DEVELOPMENT_TEAM =/ { print $3; exit }')}"
    if [[ -z "$PROJECT_TEAM_ID" ]]; then
      print -u2 "Set DEVELOPMENT_TEAM in ios/project.yml or CARGOFLOW_DEVELOPMENT_TEAM before creating an IPA."
      exit 1
    fi

    ARCHIVE_PATH="$BUILD_ROOT/CargoFlow.xcarchive"
    EXPORT_PATH="$BUILD_ROOT/Export"
    EXPORT_OPTIONS="$BUILD_ROOT/ExportOptions.plist"
    OUTPUT_PATH="$DOWNLOAD_DIR/CargoFlow.ipa"
    EXPORT_METHOD="${CARGOFLOW_EXPORT_METHOD:-debugging}"

    plutil -create xml1 "$EXPORT_OPTIONS"
    plutil -insert method -string "$EXPORT_METHOD" "$EXPORT_OPTIONS"
    plutil -insert signingStyle -string automatic "$EXPORT_OPTIONS"
    plutil -insert teamID -string "$PROJECT_TEAM_ID" "$EXPORT_OPTIONS"
    plutil -insert destination -string export "$EXPORT_OPTIONS"

    xcodebuild \
      -project CargoFlow.xcodeproj \
      -scheme CargoFlow \
      -configuration Release \
      -destination "generic/platform=iOS" \
      -archivePath "$ARCHIVE_PATH" \
      DEVELOPMENT_TEAM="$PROJECT_TEAM_ID" \
      -allowProvisioningUpdates \
      archive

    xcodebuild \
      -exportArchive \
      -archivePath "$ARCHIVE_PATH" \
      -exportPath "$EXPORT_PATH" \
      -exportOptionsPlist "$EXPORT_OPTIONS" \
      -allowProvisioningUpdates

    IPA_PATH="$(find "$EXPORT_PATH" -maxdepth 1 -name '*.ipa' -print -quit)"
    if [[ -z "$IPA_PATH" ]]; then
      print -u2 "Archive export completed, but no IPA was produced."
      exit 1
    fi

    cp "$IPA_PATH" "$OUTPUT_PATH"
    cd "$DOWNLOAD_DIR"
    shasum -a 256 "${OUTPUT_PATH:t}" > "${OUTPUT_PATH:t}.sha256"
    print "Created $OUTPUT_PATH"
    ;;

  *)
    print -u2 "Usage: scripts/package-ios.sh [simulator|archive]"
    exit 2
    ;;
esac
