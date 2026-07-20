package app

import (
	"testing"

	"cargoflows/api/internal/database"
	"cargoflows/api/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestMigrateCreatesVersionedSOPTables(t *testing.T) {
	db := newTestDB(t)
	for _, model := range []any{&models.CaptureSOP{}, &models.SOPVersion{}, &models.SOPView{}, &models.SOPViewReferenceImage{}, &models.SOPReferenceUpload{}} {
		if !db.Migrator().HasTable(model) {
			t.Fatalf("missing table for %T", model)
		}
	}
}
