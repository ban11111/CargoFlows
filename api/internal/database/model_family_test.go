package database

import (
	"testing"
	"time"

	"cargoflows/api/internal/models"
)

func TestMigrateCreatesModelFamilyIdentityTables(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	for _, model := range []any{
		&models.ModelFamily{}, &models.ModelFamilyMember{}, &models.VariantIdentityManifest{},
		&models.VariantIdentityManifestVersion{}, &models.VariantDifferenceRegion{},
		&models.VariantDifferenceRegionEvidenceAsset{},
	} {
		if !db.Migrator().HasTable(model) {
			t.Fatalf("missing table for %T", model)
		}
	}
	for _, model := range []any{&models.SKU{}, &models.Asset{}} {
		if !db.Migrator().HasColumn(model, "public_id") {
			t.Fatalf("missing opaque public ID for %T", model)
		}
	}
	for _, column := range []string{"mime_type", "width", "height", "byte_count", "sha256"} {
		if !db.Migrator().HasColumn(&models.Asset{}, column) {
			t.Fatalf("missing immutable asset metadata column %q", column)
		}
	}
}

func TestModelFamilyAndVariantIdentityConstraints(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}

	active := "active"
	first := models.ModelFamilyMember{PublicID: "11111111-1111-4111-8111-111111111111", ModelFamilyID: 1, SKUID: 101, ActiveGuard: &active, AddedByID: 1}
	if err := db.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ModelFamilyMember{PublicID: "22222222-2222-4222-8222-222222222222", ModelFamilyID: 2, SKUID: first.SKUID, ActiveGuard: &active, AddedByID: 2}).Error; err == nil {
		t.Fatal("one SKU must have only one active family membership")
	}
	removedAt := time.Now().UTC()
	if err := db.Create(&models.ModelFamilyMember{PublicID: "33333333-3333-4333-8333-333333333333", ModelFamilyID: 2, SKUID: first.SKUID, AddedByID: 2, RemovedAt: &removedAt}).Error; err != nil {
		t.Fatalf("removed family membership should remain auditable: %v", err)
	}
	bypass := "bypass"
	if err := db.Create(&models.ModelFamilyMember{PublicID: "31313131-3131-4313-8313-313131313131", ModelFamilyID: 3, SKUID: first.SKUID, ActiveGuard: &bypass, AddedByID: 3}).Error; err == nil {
		t.Fatal("active family membership accepted a guard value that bypasses uniqueness")
	}
	if err := db.Create(&models.ModelFamilyMember{PublicID: "32323232-3232-4323-8323-323232323232", ModelFamilyID: 3, SKUID: 102, ActiveGuard: &bypass, AddedByID: 3, RemovedAt: &removedAt}).Error; err == nil {
		t.Fatal("removed family membership retained a non-null active guard")
	}
	firstManifest := models.VariantIdentityManifest{PublicID: "41414141-4141-4411-8411-414141414141", ModelFamilyID: 1, SKUID: first.SKUID, CreatedByID: 1}
	if err := db.Create(&firstManifest).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.VariantIdentityManifest{PublicID: "42424242-4242-4422-8422-424242424242", ModelFamilyID: 2, SKUID: first.SKUID, CreatedByID: 2}).Error; err != nil {
		t.Fatalf("SKU re-grouping must retain a separate family manifest: %v", err)
	}
	if err := db.Create(&models.VariantIdentityManifest{PublicID: "43434343-4343-4433-8433-434343434343", ModelFamilyID: 1, SKUID: first.SKUID, CreatedByID: 3}).Error; err == nil {
		t.Fatal("one SKU must have only one manifest within a model family")
	}

	draft := "draft"
	version := models.VariantIdentityManifestVersion{PublicID: "44444444-4444-4444-8444-444444444444", VariantIdentityManifestID: 1, VersionNumber: 1, Status: models.VariantManifestDraft, DraftGuard: &draft, IdentityJSON: []byte(`{}`), CreatedByID: 1}
	if err := db.Create(&version).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.VariantIdentityManifestVersion{PublicID: "55555555-5555-4555-8555-555555555555", VariantIdentityManifestID: 1, VersionNumber: 2, Status: models.VariantManifestDraft, DraftGuard: &draft, IdentityJSON: []byte(`{}`), CreatedByID: 1}).Error; err == nil {
		t.Fatal("one manifest must have only one draft version")
	}
	if err := db.Create(&models.VariantIdentityManifestVersion{PublicID: "66666666-6666-4666-8666-666666666666", VariantIdentityManifestID: 2, VersionNumber: 0, Status: models.VariantManifestPublished, IdentityJSON: []byte(`{}`), CreatedByID: 1}).Error; err == nil {
		t.Fatal("manifest version number must be positive")
	}
	if err := db.Create(&models.VariantIdentityManifestVersion{PublicID: "67676767-6767-4676-8676-676767676767", VariantIdentityManifestID: 2, VersionNumber: 1, Status: models.VariantManifestDraft, DraftGuard: &bypass, IdentityJSON: []byte(`{}`), CreatedByID: 1}).Error; err == nil {
		t.Fatal("draft manifest accepted a guard value that bypasses uniqueness")
	}
	if err := db.Create(&models.VariantIdentityManifestVersion{PublicID: "68686868-6868-4686-8686-686868686868", VariantIdentityManifestID: 3, VersionNumber: 1, Status: models.VariantManifestPublished, DraftGuard: &bypass, IdentityJSON: []byte(`{}`), CreatedByID: 1}).Error; err == nil {
		t.Fatal("published manifest retained a non-null draft guard")
	}

	region := models.VariantDifferenceRegion{PublicID: "77777777-7777-4777-8777-777777777777", VariantIdentityManifestVersionID: version.ID, Key: "right_ports", DifferenceKind: models.DifferenceKindPorts, Strictness: models.DifferenceRegionExact, ShapeJSON: []byte(`{}`), ForbiddenInheritanceJSON: []byte(`[]`), RequiredViewKeysJSON: []byte(`["right_ports"]`)}
	if err := db.Create(&region).Error; err != nil {
		t.Fatal(err)
	}
	evidence := models.VariantDifferenceRegionEvidenceAsset{VariantDifferenceRegionID: region.ID, AssetID: 11}
	if err := db.Create(&evidence).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&evidence).Error; err == nil {
		t.Fatal("difference region evidence mapping must be unique")
	}
}

func TestAssetMetadataIsImmutableAfterCreation(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	asset := models.Asset{SKUID: 1, ObjectKey: "photo-sessions/one/views/front/capture.jpg", OriginalURL: "https://assets.example.test/capture.jpg", ReviewStatus: "pending", MIMEType: "image/jpeg", Width: 10, Height: 20, ByteCount: 30, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	asset.MIMEType = "image/png"
	asset.Width = 11
	asset.Height = 21
	asset.ByteCount = 31
	asset.SHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if err := db.Save(&asset).Error; err != nil {
		t.Fatal(err)
	}
	var persisted models.Asset
	if err := db.First(&persisted, asset.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.MIMEType != "image/jpeg" || persisted.Width != 10 || persisted.Height != 20 || persisted.ByteCount != 30 || persisted.SHA256 != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("asset metadata changed after creation: %#v", persisted)
	}
}
