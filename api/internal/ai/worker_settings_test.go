package ai

import (
	"errors"
	"testing"

	"cargoflows/api/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestWorkerSettingsDefaultsAndPersistsValidUpdates(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.AIWorkerSetting{}); err != nil {
		t.Fatal(err)
	}
	service := NewWorkerSettingsService(db)
	defaults, err := service.Get(t.Context())
	if err != nil || defaults.MaxWorkersPerJob != 3 || defaults.MaxWorkersGlobal != 9 {
		t.Fatalf("defaults = %#v, %v", defaults, err)
	}
	updated, err := service.Update(t.Context(), 17, WorkerConcurrency{MaxWorkersPerJob: 4, MaxWorkersGlobal: 12})
	if err != nil || updated.MaxWorkersPerJob != 4 || updated.MaxWorkersGlobal != 12 {
		t.Fatalf("updated = %#v, %v", updated, err)
	}
	var row models.AIWorkerSetting
	if err := db.First(&row, workerSettingID).Error; err != nil || row.UpdatedByID != 17 {
		t.Fatalf("stored = %#v, %v", row, err)
	}
}

func TestWorkerSettingsRejectInvalidLimits(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.AIWorkerSetting{}); err != nil {
		t.Fatal(err)
	}
	service := NewWorkerSettingsService(db)
	for _, value := range []WorkerConcurrency{
		{MaxWorkersPerJob: 0, MaxWorkersGlobal: 9},
		{MaxWorkersPerJob: 3, MaxWorkersGlobal: 33},
		{MaxWorkersPerJob: 10, MaxWorkersGlobal: 9},
	} {
		if _, err := service.Update(t.Context(), 1, value); !errors.Is(err, ErrWorkerSettingInvalid) {
			t.Fatalf("Update(%#v) error = %v", value, err)
		}
	}
}
