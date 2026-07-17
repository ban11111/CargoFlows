package sop

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"cargoflow/api/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

func TestVariantManifestReGroupingPreservesHistoryAndSelectsCurrentFamilyOnly(t *testing.T) {
	db := variantManifestTestDB(t)
	familyA, target, _ := createVariantManifestFamily(t, db, []string{"color", "material", "finish", "texture", "labels", "ports", "accessories"})
	manifests := NewVariantManifestService(db)
	oldDraft, err := manifests.CreateDraft(t.Context(), target.PublicID, CreateVariantManifestDraftInput{Identity: validVariantIdentityJSON(t), ActorID: 9})
	if err != nil {
		t.Fatal(err)
	}
	oldPublished, err := manifests.Publish(t.Context(), oldDraft.PublicID, 9)
	if err != nil {
		t.Fatal(err)
	}

	families := NewModelFamilyService(db)
	familyB, err := families.Create(t.Context(), CreateModelFamilyInput{Brand: "CargoFlow", NameZH: "新同款", NameEN: "New family", ModelCode: "REGROUP-" + t.Name(), CommonStructure: json.RawMessage(`{"schema":"model_family_common_structure_v1","invariants":["housing"]}`), VariationDimensions: []string{"color", "material", "finish", "texture", "labels", "ports", "accessories"}, CreatedByID: 10})
	if err != nil {
		t.Fatal(err)
	}
	var oldMember models.ModelFamilyMember
	if err := db.Where("model_family_id = ? AND sk_uid = ? AND removed_at IS NULL", familyA.ID, target.ID).First(&oldMember).Error; err != nil {
		t.Fatal(err)
	}
	if err := families.RemoveMember(t.Context(), familyA.PublicID, oldMember.PublicID, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := families.AddMember(t.Context(), familyB.PublicID, target.PublicID, 10); err != nil {
		t.Fatal(err)
	}

	newIdentity := json.RawMessage(strings.Replace(string(validVariantIdentityJSON(t)), "Midnight blue", "Family B blue", 1))
	newDraft, err := manifests.CreateDraft(t.Context(), target.PublicID, CreateVariantManifestDraftInput{Identity: newIdentity, ActorID: 10})
	if err != nil {
		t.Fatal(err)
	}
	newPublished, err := manifests.Publish(t.Context(), newDraft.PublicID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if newPublished.PublicID == oldPublished.PublicID || newPublished.VersionNumber != 1 {
		t.Fatalf("new family manifest = %#v", newPublished)
	}
	current, err := manifests.GetForSKU(t.Context(), target.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if current.PublicID != newPublished.PublicID || strings.Contains(string(current.IdentityJSON), "Midnight blue") {
		t.Fatalf("current manifest = %#v; old = %#v", current, oldPublished)
	}
}

func TestVariantManifestRequiresAllIdentityKeysAndNormalizesArrayFacts(t *testing.T) {
	db := variantManifestTestDB(t)
	_, target, _ := createVariantManifestFamily(t, db, []string{"color", "material", "finish", "texture", "labels", "ports", "accessories"})
	service := NewVariantManifestService(db)
	missingControls := json.RawMessage(strings.Replace(string(validVariantIdentityJSON(t)), `,"controls":[]`, ``, 1))
	if _, err := service.CreateDraft(t.Context(), target.PublicID, CreateVariantManifestDraftInput{Identity: missingControls, ActorID: 9}); !errors.Is(err, ErrVariantManifestInvalid) {
		t.Fatalf("missing required key error = %v", err)
	}
	nullArrays := json.RawMessage(strings.NewReplacer(`"colors":[{"key":"body","name":"Midnight blue","value":"#123ABC"}]`, `"colors":null`, `"labels":[{"key":"front_logo","text":"CargoFlow","region_key":"logo"}]`, `"labels":null`, `"ports":[{"key":"usb_c","description":"USB-C charging port","region_key":"right_ports"}]`, `"ports":null`, `"controls":[]`, `"controls":null`, `"accessories":["charging cable"]`, `"accessories":null`, `"packaging":[]`, `"packaging":null`, `"other":[]`, `"other":null`, `"must_prove_with_target_assets":["body","logo","right_ports"]`, `"must_prove_with_target_assets":null`).Replace(string(validVariantIdentityJSON(t))))
	draft, err := service.CreateDraft(t.Context(), target.PublicID, CreateVariantManifestDraftInput{Identity: nullArrays, ActorID: 9})
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]json.RawMessage
	if err := json.Unmarshal(draft.IdentityJSON, &stored); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"colors", "labels", "ports", "controls", "accessories", "packaging", "other", "must_prove_with_target_assets"} {
		if got := string(stored[key]); got != "[]" {
			t.Fatalf("%s = %s, want normalized array", key, got)
		}
	}
}

func TestVariantManifestConcurrentPublishPreventsDraftWriteAfterPublication(t *testing.T) {
	db := variantManifestTestDB(t)
	_, target, _ := createVariantManifestFamily(t, db, []string{"color", "material", "finish", "texture", "labels", "ports", "accessories"})
	service := NewVariantManifestService(db)
	draft, err := service.CreateDraft(t.Context(), target.PublicID, CreateVariantManifestDraftInput{Identity: validVariantIdentityJSON(t), ActorID: 9})
	if err != nil {
		t.Fatal(err)
	}

	injected := false
	if err := db.Callback().Update().Before("gorm:update").Register("test_pause_variant_identity_draft_write", func(tx *gorm.DB) {
		updates, ok := tx.Statement.Dest.(map[string]any)
		if injected || !ok || tx.Statement.Table != "variant_identity_manifest_versions" || updates["identity_json"] == nil {
			return
		}
		injected = true
		if err := tx.Session(&gorm.Session{NewDB: true, SkipHooks: true}).Model(&models.VariantIdentityManifestVersion{}).Where("id = ?", draft.ID).Updates(map[string]any{"status": models.VariantManifestPublished, "draft_guard": nil}).Error; err != nil {
			t.Fatalf("inject competing publication: %v", err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Update().Remove("test_pause_variant_identity_draft_write")

	updatedIdentity := json.RawMessage(strings.Replace(string(validVariantIdentityJSON(t)), "Midnight blue", "Ocean blue", 1))
	if _, err := service.UpdateDraft(t.Context(), draft.PublicID, UpdateVariantManifestDraftInput{Identity: updatedIdentity, ActorID: 10}); !errors.Is(err, ErrVariantManifestImmutable) {
		t.Fatalf("concurrent update error = %v", err)
	}
	if !injected {
		t.Fatal("competing publication was not interleaved before draft write")
	}
	if _, err := service.Publish(t.Context(), draft.PublicID, 11); err != nil {
		t.Fatalf("publish after rejected draft write: %v", err)
	}
	var stored models.VariantIdentityManifestVersion
	if err := db.Where("public_id = ?", draft.PublicID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != models.VariantManifestPublished || strings.Contains(string(stored.IdentityJSON), "Ocean blue") {
		t.Fatalf("published state was overwritten: %#v", stored)
	}
}

func TestVariantManifestWriteRejectsInterleavedReGroup(t *testing.T) {
	for _, operation := range []struct {
		name string
		run  func(*VariantManifestService, models.VariantIdentityManifestVersion) error
	}{
		{name: "update", run: func(service *VariantManifestService, draft models.VariantIdentityManifestVersion) error {
			identity := json.RawMessage(strings.Replace(string(validVariantIdentityJSON(t)), "Midnight blue", "Ocean blue", 1))
			_, err := service.UpdateDraft(t.Context(), draft.PublicID, UpdateVariantManifestDraftInput{Identity: identity, ActorID: 10})
			return err
		}},
		{name: "publish", run: func(service *VariantManifestService, draft models.VariantIdentityManifestVersion) error {
			_, err := service.Publish(t.Context(), draft.PublicID, 10)
			return err
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			db := variantManifestTestDB(t)
			familyA, target, _ := createVariantManifestFamily(t, db, []string{"color", "material", "finish", "texture", "labels", "ports", "accessories"})
			service := NewVariantManifestService(db)
			draft, err := service.CreateDraft(t.Context(), target.PublicID, CreateVariantManifestDraftInput{Identity: validVariantIdentityJSON(t), ActorID: 9})
			if err != nil {
				t.Fatal(err)
			}
			familyB, err := NewModelFamilyService(db).Create(t.Context(), CreateModelFamilyInput{Brand: "CargoFlow", NameZH: "新同款", NameEN: "New family", ModelCode: "INTERLEAVE-" + operation.name + t.Name(), CommonStructure: json.RawMessage(`{"schema":"model_family_common_structure_v1","invariants":["housing"]}`), VariationDimensions: []string{"color", "material", "finish", "texture", "labels", "ports", "accessories"}, CreatedByID: 10})
			if err != nil {
				t.Fatal(err)
			}
			var oldMember models.ModelFamilyMember
			if err := db.Where("model_family_id = ? AND sk_uid = ? AND removed_at IS NULL", familyA.ID, target.ID).First(&oldMember).Error; err != nil {
				t.Fatal(err)
			}

			memberQueries, injected := 0, false
			if err := db.Callback().Query().Before("gorm:query").Register("test_interleave_variant_manifest_regroup", func(tx *gorm.DB) {
				if tx.Statement.Table != "model_family_members" {
					return
				}
				memberQueries++
				if injected || memberQueries != 2 {
					return
				}
				injected = true
				now := time.Now().UTC()
				if err := tx.Session(&gorm.Session{NewDB: true, SkipHooks: true}).Model(&models.ModelFamilyMember{}).Where("id = ?", oldMember.ID).Updates(map[string]any{"removed_at": now, "removed_by_id": uint(10), "active_guard": nil}).Error; err != nil {
					t.Fatalf("interleave remove member: %v", err)
				}
				active := "active"
				if err := tx.Session(&gorm.Session{NewDB: true, SkipHooks: true}).Create(&models.ModelFamilyMember{PublicID: uuid.NewString(), ModelFamilyID: familyB.ID, SKUID: target.ID, ActiveGuard: &active, AddedByID: 10}).Error; err != nil {
					t.Fatalf("interleave add member: %v", err)
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer db.Callback().Query().Remove("test_interleave_variant_manifest_regroup")

			if err := operation.run(service, *draft); !errors.Is(err, ErrVariantManifestInvalid) {
				t.Fatalf("interleaved %s error = %v", operation.name, err)
			}
			if !injected {
				t.Fatal("re-group did not interleave before final membership revalidation")
			}
			var stored models.VariantIdentityManifestVersion
			if err := db.Where("public_id = ?", draft.PublicID).First(&stored).Error; err != nil {
				t.Fatal(err)
			}
			if stored.Status != models.VariantManifestDraft || strings.Contains(string(stored.IdentityJSON), "Ocean blue") {
				t.Fatalf("stale write committed after re-group: %#v", stored)
			}
		})
	}
}

// TestManifestAndMembershipWritesUseMySQLUpdateLockOrder inspects GORM's
// locking clause rather than SQLite's generated SQL because SQLite intentionally
// omits FOR UPDATE. The same clause is emitted by the MySQL dialect, making this
// a deterministic regression for the lock order that protects production rows.
func TestManifestAndMembershipWritesUseMySQLUpdateLockOrder(t *testing.T) {
	assertUpdateLockOrder := func(t *testing.T, db *gorm.DB, run func() error, want []string) {
		t.Helper()
		var got []string
		callbackName := "test_manifest_lock_order_" + uuid.NewString()
		if err := db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
			lockingClause, ok := tx.Statement.Clauses["FOR"]
			if !ok {
				return
			}
			locking, ok := lockingClause.Expression.(clause.Locking)
			if !ok || locking.Strength != "UPDATE" {
				return
			}
			switch tx.Statement.Table {
			case "model_families", "skus", "model_family_members", "variant_identity_manifests", "variant_identity_manifest_versions":
				got = append(got, tx.Statement.Table)
			}
		}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })
		if err := run(); err != nil {
			t.Fatal(err)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("UPDATE lock order = %v, want %v", got, want)
		}
	}

	newDraft := func(t *testing.T) (*gorm.DB, *ModelFamilyService, *VariantManifestService, models.ModelFamily, models.SKU, models.VariantIdentityManifestVersion) {
		t.Helper()
		db := variantManifestTestDB(t)
		family, target, _ := createVariantManifestFamily(t, db, []string{"color", "material", "finish", "texture", "labels", "ports", "accessories"})
		manifests := NewVariantManifestService(db)
		draft, err := manifests.CreateDraft(t.Context(), target.PublicID, CreateVariantManifestDraftInput{Identity: validVariantIdentityJSON(t), ActorID: 9})
		if err != nil {
			t.Fatal(err)
		}
		return db, NewModelFamilyService(db), manifests, family, target, *draft
	}

	t.Run("create", func(t *testing.T) {
		db := variantManifestTestDB(t)
		family, target, _ := createVariantManifestFamily(t, db, []string{"color", "material", "finish", "texture", "labels", "ports", "accessories"})
		second := seedModelFamilySKU(t, db, "LOCK-ORDER-CREATE-"+t.Name())
		if _, err := NewModelFamilyService(db).AddMember(t.Context(), family.PublicID, second.PublicID, 1); err != nil {
			t.Fatal(err)
		}
		_ = target
		assertUpdateLockOrder(t, db, func() error {
			_, err := NewVariantManifestService(db).CreateDraft(t.Context(), second.PublicID, CreateVariantManifestDraftInput{Identity: validVariantIdentityJSON(t), ActorID: 9})
			return err
		}, []string{"model_families", "skus", "model_family_members", "variant_identity_manifests"})
	})

	t.Run("copy", func(t *testing.T) {
		db, _, manifests, _, target, draft := newDraft(t)
		published, err := manifests.Publish(t.Context(), draft.PublicID, 9)
		if err != nil {
			t.Fatal(err)
		}
		assertUpdateLockOrder(t, db, func() error {
			_, err := manifests.CopyVersion(t.Context(), target.PublicID, published.PublicID, 10)
			return err
		}, []string{"model_families", "skus", "model_family_members", "variant_identity_manifests", "variant_identity_manifest_versions"})
	})

	for _, operation := range []struct {
		name string
		run  func(*VariantManifestService, models.VariantIdentityManifestVersion) error
	}{
		{name: "update", run: func(service *VariantManifestService, draft models.VariantIdentityManifestVersion) error {
			_, err := service.UpdateDraft(t.Context(), draft.PublicID, UpdateVariantManifestDraftInput{Identity: validVariantIdentityJSON(t), ActorID: 10})
			return err
		}},
		{name: "publish", run: func(service *VariantManifestService, draft models.VariantIdentityManifestVersion) error {
			_, err := service.Publish(t.Context(), draft.PublicID, 10)
			return err
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			db, _, manifests, _, _, draft := newDraft(t)
			assertUpdateLockOrder(t, db, func() error { return operation.run(manifests, draft) }, []string{"model_families", "skus", "model_family_members", "variant_identity_manifest_versions", "model_families", "skus", "model_family_members"})
		})
	}

	for _, operation := range []struct {
		name string
		run  func(*ModelFamilyService, models.ModelFamily, models.SKU) error
	}{
		{name: "member_add", run: func(service *ModelFamilyService, family models.ModelFamily, sku models.SKU) error {
			_, err := service.AddMember(t.Context(), family.PublicID, sku.PublicID, 10)
			return err
		}},
		{name: "member_remove", run: func(service *ModelFamilyService, family models.ModelFamily, sku models.SKU) error {
			var member models.ModelFamilyMember
			if err := service.db.Where("model_family_id = ? AND sk_uid = ? AND removed_at IS NULL", family.ID, sku.ID).First(&member).Error; err != nil {
				return err
			}
			return service.RemoveMember(t.Context(), family.PublicID, member.PublicID, 10)
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			db := variantManifestTestDB(t)
			family, target, _ := createVariantManifestFamily(t, db, []string{"color", "ports"})
			if operation.name == "member_add" {
				target = seedModelFamilySKU(t, db, "LOCK-ORDER-ADD-"+t.Name())
			}
			service := NewModelFamilyService(db)
			assertUpdateLockOrder(t, db, func() error { return operation.run(service, family, target) }, []string{"model_families", "skus", "model_family_members"})
		})
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
