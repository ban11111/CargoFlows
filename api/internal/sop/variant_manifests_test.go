package sop

import (
	"encoding/json"
	"errors"
	"testing"

	"cargoflow/api/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func variantManifestTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.User{}, &models.Product{}, &models.SKU{}, &models.Asset{}, &models.AIAuditEvent{},
		&models.ModelFamily{}, &models.ModelFamilyMember{}, &models.VariantIdentityManifest{},
		&models.VariantIdentityManifestVersion{}, &models.VariantDifferenceRegion{}, &models.VariantDifferenceRegionEvidenceAsset{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func validVariantIdentityJSON(t *testing.T) json.RawMessage {
	t.Helper()
	return json.RawMessage(`{"schema":"variant_identity_v1","colors":[{"key":"body","name":"Midnight blue","value":"#123ABC"}],"material":"aluminum","finish":"matte","texture":"smooth","labels":[{"key":"front_logo","text":"CargoFlow","region_key":"logo"}],"ports":[{"key":"usb_c","description":"USB-C charging port","region_key":"right_ports"}],"controls":[],"accessories":["charging cable"],"packaging":[],"other":[],"must_prove_with_target_assets":["body","logo","right_ports"]}`)
}

func createVariantManifestFamily(t *testing.T, db *gorm.DB, variationDimensions []string) (models.ModelFamily, models.SKU, models.SKU) {
	t.Helper()
	service := NewModelFamilyService(db)
	family, err := service.Create(t.Context(), CreateModelFamilyInput{Brand: "CargoFlow", NameZH: "同款", NameEN: "Same", ModelCode: "MANIFEST-" + t.Name(), CommonStructure: json.RawMessage(`{"schema":"model_family_common_structure_v1","invariants":["housing"]}`), VariationDimensions: variationDimensions, CreatedByID: 1})
	if err != nil {
		t.Fatal(err)
	}
	target := seedModelFamilySKU(t, db, "TARGET-"+t.Name())
	sibling := seedModelFamilySKU(t, db, "SIBLING-"+t.Name())
	if _, err := service.AddMember(t.Context(), family.PublicID, target.PublicID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AddMember(t.Context(), family.PublicID, sibling.PublicID, 1); err != nil {
		t.Fatal(err)
	}
	return *family, target, sibling
}

func TestVariantManifestLifecycleCopiesPublishedVersionAndLeavesItImmutable(t *testing.T) {
	db := variantManifestTestDB(t)
	_, target, _ := createVariantManifestFamily(t, db, []string{"color", "material", "finish", "texture", "labels", "ports", "accessories"})
	service := NewVariantManifestService(db)

	draft, err := service.CreateDraft(t.Context(), target.PublicID, CreateVariantManifestDraftInput{Identity: validVariantIdentityJSON(t), Regions: []VariantDifferenceRegionInput{{Key: "right_ports", DifferenceKind: models.DifferenceKindPorts, Strictness: models.DifferenceRegionExact, DescriptionEN: "Right-side ports", Shape: json.RawMessage(`{"kind":"rectangle","x":0.7,"y":0.2,"width":0.2,"height":0.4}`), RequiredViewKeys: []string{"right_ports"}}}, ActorID: 9})
	if err != nil {
		t.Fatal(err)
	}
	if draft.VersionNumber != 1 || draft.Status != models.VariantManifestDraft || len(draft.Regions) != 1 {
		t.Fatalf("unexpected draft = %#v", draft)
	}
	if _, err := service.CreateDraft(t.Context(), target.PublicID, CreateVariantManifestDraftInput{Identity: validVariantIdentityJSON(t), ActorID: 9}); !errors.Is(err, ErrVariantManifestDraftExists) {
		t.Fatalf("second draft error = %v", err)
	}

	asset := models.Asset{SKUID: target.ID, ObjectKey: "assets/final/target", OriginalURL: "private", ReviewStatus: "approved", MIMEType: "image/png", Width: 20, Height: 20, ByteCount: 100, SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	updated, err := service.UpdateDraft(t.Context(), draft.PublicID, UpdateVariantManifestDraftInput{Identity: validVariantIdentityJSON(t), Regions: []VariantDifferenceRegionInput{{Key: "right_ports", DifferenceKind: models.DifferenceKindPorts, Strictness: models.DifferenceRegionExact, DescriptionEN: "Right-side ports", Shape: json.RawMessage(`{"kind":"rectangle","x":0.7,"y":0.2,"width":0.2,"height":0.4}`), RequiredViewKeys: []string{"right_ports"}, EvidenceAssetIDs: []string{asset.PublicID}}}, ActorID: 10})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(updated.Regions[0].ShapeJSON); got != `{"kind":"rectangle","x":0.7,"y":0.2,"width":0.2,"height":0.4}` {
		t.Fatalf("shape was not normalized: %s", got)
	}
	published, err := service.Publish(t.Context(), draft.PublicID, 11)
	if err != nil {
		t.Fatal(err)
	}
	if published.Status != models.VariantManifestPublished || published.PublishedByID == nil || *published.PublishedByID != 11 {
		t.Fatalf("published = %#v", published)
	}
	if _, err := service.UpdateDraft(t.Context(), published.PublicID, UpdateVariantManifestDraftInput{Identity: validVariantIdentityJSON(t), ActorID: 12}); !errors.Is(err, ErrVariantManifestImmutable) {
		t.Fatalf("published update error = %v", err)
	}
	copied, err := service.CopyVersion(t.Context(), target.PublicID, published.PublicID, 12)
	if err != nil {
		t.Fatal(err)
	}
	if copied.VersionNumber != 2 || copied.Status != models.VariantManifestDraft || copied.PublicID == published.PublicID || len(copied.Regions) != 1 {
		t.Fatalf("copied = %#v", copied)
	}
}

func TestVariantManifestValidateReturnsStableExactEvidenceIssues(t *testing.T) {
	db := variantManifestTestDB(t)
	_, target, _ := createVariantManifestFamily(t, db, []string{"color", "material", "finish", "texture", "labels", "ports", "accessories"})
	service := NewVariantManifestService(db)
	draft, err := service.CreateDraft(t.Context(), target.PublicID, CreateVariantManifestDraftInput{Identity: validVariantIdentityJSON(t), Regions: []VariantDifferenceRegionInput{{Key: "right_ports", DifferenceKind: models.DifferenceKindPorts, Strictness: models.DifferenceRegionExact, DescriptionEN: "Right-side ports", Shape: json.RawMessage(`{"kind":"polygon","points":[[0.1,0.1],[0.4,0.1],[0.2,0.4]]}`)}}, ActorID: 9})
	if err != nil {
		t.Fatal(err)
	}
	issues, err := service.Validate(t.Context(), draft.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if !hasVariantIssueCode(issues, "exact_region_view_required") || !hasVariantIssueCode(issues, "exact_region_evidence_required") {
		t.Fatalf("issues = %#v", issues)
	}
	if _, err := service.Publish(t.Context(), draft.PublicID, 9); !errors.Is(err, ErrVariantManifestValidation) {
		t.Fatalf("publish invalid manifest error = %v", err)
	}
}

func TestVariantManifestRejectsCrossSKUUnapprovedEvidenceAndUnpublishedDimension(t *testing.T) {
	db := variantManifestTestDB(t)
	_, target, sibling := createVariantManifestFamily(t, db, []string{"color", "ports"})
	service := NewVariantManifestService(db)
	siblingAsset := models.Asset{SKUID: sibling.ID, ObjectKey: "assets/final/sibling", OriginalURL: "private", ReviewStatus: "approved", MIMEType: "image/png", Width: 20, Height: 20, ByteCount: 100, SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	if err := db.Create(&siblingAsset).Error; err != nil {
		t.Fatal(err)
	}
	identity := json.RawMessage(`{"schema":"variant_identity_v1","colors":[],"material":"aluminum","finish":"matte","texture":"smooth","labels":[],"ports":[],"controls":[],"accessories":[],"packaging":[],"other":[],"must_prove_with_target_assets":[]}`)
	_, err := service.CreateDraft(t.Context(), target.PublicID, CreateVariantManifestDraftInput{Identity: identity, Regions: []VariantDifferenceRegionInput{{Key: "right_ports", DifferenceKind: models.DifferenceKindPorts, Strictness: models.DifferenceRegionExact, DescriptionEN: "Right-side ports", Shape: json.RawMessage(`{"kind":"rectangle","x":0,"y":0,"width":1,"height":1}`), RequiredViewKeys: []string{"right_ports"}, EvidenceAssetIDs: []string{siblingAsset.PublicID}}}, ActorID: 9})
	if !errors.Is(err, ErrVariantManifestInvalid) {
		t.Fatalf("cross-SKU evidence error = %v", err)
	}
	_, err = service.CreateDraft(t.Context(), target.PublicID, CreateVariantManifestDraftInput{Identity: validVariantIdentityJSON(t), ActorID: 9})
	if !errors.Is(err, ErrVariantManifestInvalid) {
		t.Fatalf("unpublished material dimension error = %v", err)
	}
}

func TestVariantManifestRejectsUnknownUnsafeAndTrailingIdentityDocumentData(t *testing.T) {
	db := variantManifestTestDB(t)
	_, target, _ := createVariantManifestFamily(t, db, []string{"color", "material", "finish", "texture", "labels", "ports", "accessories"})
	service := NewVariantManifestService(db)
	for _, identity := range []json.RawMessage{
		json.RawMessage(`{"schema":"variant_identity_v1","colors":[],"material":"","finish":"","texture":"","labels":[],"ports":[],"controls":[],"accessories":[],"packaging":[],"other":[],"must_prove_with_target_assets":[],"object_key":"assets/final/leak"}`),
		append(validVariantIdentityJSON(t), []byte(` trailing-garbage`)...),
	} {
		if _, err := service.CreateDraft(t.Context(), target.PublicID, CreateVariantManifestDraftInput{Identity: identity, ActorID: 9}); !errors.Is(err, ErrVariantManifestInvalid) {
			t.Fatalf("unsafe identity error = %v", err)
		}
	}
}

func TestVariantManifestRejectsPublicationAfterFamilyIsArchived(t *testing.T) {
	db := variantManifestTestDB(t)
	family, target, _ := createVariantManifestFamily(t, db, []string{"color", "material", "finish", "texture", "labels", "ports", "accessories"})
	service := NewVariantManifestService(db)
	draft, err := service.CreateDraft(t.Context(), target.PublicID, CreateVariantManifestDraftInput{Identity: validVariantIdentityJSON(t), ActorID: 9})
	if err != nil {
		t.Fatal(err)
	}
	archived := models.ModelFamilyArchived
	if _, err := NewModelFamilyService(db).Update(t.Context(), family.PublicID, UpdateModelFamilyInput{Status: &archived, UpdatedByID: 9}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Publish(t.Context(), draft.PublicID, 9); !errors.Is(err, ErrModelFamilyArchived) {
		t.Fatalf("archived family publish error = %v", err)
	}
}

func hasVariantIssueCode(issues []VariantManifestValidationIssue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
