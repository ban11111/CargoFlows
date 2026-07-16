package database

import (
	"reflect"
	"testing"

	"cargoflow/api/internal/models"
	"cargoflow/api/internal/sop"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestMigrateCreatesAIFoundationTables(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	for _, model := range []any{&models.OpenAIProviderSetting{}, &models.AIContentTemplate{}, &models.AIContentTemplateVersion{}, &models.AIContentSlot{}, &models.AIJob{}, &models.AIJobItem{}, &models.AIExecution{}, &models.AIAuditEvent{}, &models.AIUsageLedger{}} {
		if !db.Migrator().HasTable(model) {
			t.Fatalf("missing table for %T", model)
		}
	}
	for _, column := range []string{"l0_policy_version", "l1_product_context_version", "l2_template_version_public_id", "l3_content_slot_public_id", "normalized_input_json", "ordered_input_list_json"} {
		if !db.Migrator().HasColumn(&models.AIExecution{}, column) {
			t.Fatalf("missing AI execution provenance column %q", column)
		}
	}
	if db.Migrator().HasColumn(&models.AIJob{}, "input_asset_ids") {
		t.Fatal("legacy input_asset_ids compatibility field must not be persisted")
	}
}

func TestSeedCreatesPublishedPhoneCaseCaptureSOPFromExactPresets(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := Seed(db); err != nil {
		t.Fatal(err)
	}

	var version models.SOPVersion
	if err := db.Preload("Views", func(tx *gorm.DB) *gorm.DB { return tx.Order("sequence ASC") }).Where("status = ?", models.SOPVersionPublished).First(&version).Error; err != nil {
		t.Fatal(err)
	}
	if version.SchemaVersion != "1.0" || version.CoordinateSystem != "pcs_object_v1" {
		t.Fatalf("unexpected schema metadata: %#v", version)
	}
	if _, err := uuid.Parse(version.PublicID); err != nil {
		t.Fatalf("invalid version UUID: %v", err)
	}
	wantKeys := []string{"reference_front", "back", "left", "bottom", "right", "top", "detail_label", "packaging_front"}
	if len(version.Views) != len(wantKeys) {
		t.Fatalf("expected %d views, got %d", len(wantKeys), len(version.Views))
	}
	for index, key := range wantKeys {
		view := version.Views[index]
		if view.PresetKey != key || view.Sequence != index+1 {
			t.Fatalf("view %d: expected %q at sequence %d, got %#v", index, key, index+1, view)
		}
		if _, err := uuid.Parse(view.PublicID); err != nil {
			t.Fatalf("view %q has invalid UUID: %v", key, err)
		}
		preset, ok := sop.PresetByKey(key)
		if !ok {
			t.Fatalf("preset %q is missing", key)
		}
		pose, err := sop.CanonicalizePose(preset.CameraPosition, preset.ImageUp)
		if err != nil {
			t.Fatal(err)
		}
		if view.Role != preset.Role || view.ViewKind != preset.Kind || view.NameZH != preset.NameZH || view.NameEN != preset.NameEN || view.InstructionZH != preset.InstructionZH || view.InstructionEN != preset.InstructionEN || view.Required != preset.Required || view.Composition != preset.Composition {
			t.Fatalf("view %q metadata drifted from preset", key)
		}
		if got := []float64{view.CameraPositionX, view.CameraPositionY, view.CameraPositionZ}; !reflect.DeepEqual(got, pose.CameraPosition[:]) {
			t.Fatalf("view %q camera position: got %v want %v", key, got, pose.CameraPosition)
		}
		if got := []float64{view.ImageUpX, view.ImageUpY, view.ImageUpZ}; !reflect.DeepEqual(got, pose.ImageUp[:]) {
			t.Fatalf("view %q image up: got %v want %v", key, got, pose.ImageUp)
		}
		if got := []float64{view.TargetX, view.TargetY, view.TargetZ}; !reflect.DeepEqual(got, preset.Target[:]) {
			t.Fatalf("view %q target: got %v want %v", key, got, preset.Target)
		}
	}
	if !version.Views[0].Required || version.Views[6].Required || version.Views[7].Required {
		t.Fatal("seed required flags do not match presets")
	}
}
