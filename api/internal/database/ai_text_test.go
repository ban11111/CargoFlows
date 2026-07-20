package database

import (
	"bytes"
	"testing"
	"time"

	"cargoflows/api/internal/models"
)

func TestMigrateCreatesAITextAndPlatformContentTables(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	for _, model := range []any{&models.AITextResult{}, &models.SKUPlatformContent{}, &models.SKUPlatformContentRevision{}} {
		if !db.Migrator().HasTable(model) {
			t.Fatalf("missing table for %T", model)
		}
	}
}

func TestAITextAndPlatformContentConstraintsAndJSONDefaults(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}

	result := models.AITextResult{PublicID: "text-result-1", AIExecutionID: 10, CandidateIndex: 1, Kind: models.AIContentSlotTitle}
	if err := db.Create(&result).Error; err != nil {
		t.Fatal(err)
	}
	if len(result.RawStructuredJSON) == 0 || len(result.ValidationJSON) == 0 || result.EditedStructuredJSON != nil {
		t.Fatalf("unexpected text result JSON defaults: %#v", result)
	}
	duplicateResult := models.AITextResult{PublicID: "text-result-2", AIExecutionID: result.AIExecutionID, CandidateIndex: result.CandidateIndex, Kind: models.AIContentSlotTitle}
	if err := db.Create(&duplicateResult).Error; err == nil {
		t.Fatal("candidate sequence must be unique per execution")
	}
	if result.ApprovedByID != nil || result.ApprovedAt != nil || result.AppliedByID != nil || result.AppliedAt != nil {
		t.Fatalf("review/application metadata must default to null: %#v", result)
	}
	now := time.Now().UTC()
	actor := uint(30)
	invalidApproval := models.AITextResult{PublicID: "text-result-invalid-approval", AIExecutionID: 11, CandidateIndex: 1, Kind: models.AIContentSlotTitle, State: models.AITextResultApproved}
	if err := db.Create(&invalidApproval).Error; err == nil {
		t.Fatal("approved result without approver and approval time must be rejected")
	}
	invalidApplication := models.AITextResult{PublicID: "text-result-invalid-application", AIExecutionID: 12, CandidateIndex: 1, Kind: models.AIContentSlotTitle, AppliedByID: &actor, AppliedAt: &now}
	if err := db.Create(&invalidApplication).Error; err == nil {
		t.Fatal("candidate result cannot carry application metadata")
	}
	invalidCandidateIndex := models.AITextResult{PublicID: "text-result-invalid-index", AIExecutionID: 13, CandidateIndex: -1, Kind: models.AIContentSlotTitle}
	if err := db.Create(&invalidCandidateIndex).Error; err == nil {
		t.Fatal("candidate index must be positive")
	}

	content := models.SKUPlatformContent{PublicID: "platform-content-1", SKUID: 20, Platform: "lazada", Locale: "zh-CN", UpdatedByID: 30}
	if err := db.Create(&content).Error; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content.SellingPointsJSON, []byte("[]")) || !bytes.Equal(content.SearchKeywordsJSON, []byte("[]")) || content.Revision != 1 {
		t.Fatalf("unexpected platform content defaults: %#v", content)
	}
	duplicateContent := models.SKUPlatformContent{PublicID: "platform-content-2", SKUID: content.SKUID, Platform: content.Platform, Locale: content.Locale, UpdatedByID: 30}
	if err := db.Create(&duplicateContent).Error; err == nil {
		t.Fatal("platform content must be unique per sku/platform/locale")
	}

	revisions := []models.SKUPlatformContentRevision{
		{PublicID: "revision-1", SKUPlatformContentID: content.ID, Revision: 1, ActorID: 30},
		{PublicID: "revision-2", SKUPlatformContentID: content.ID, Revision: 2, ActorID: 30},
	}
	if err := db.Create(&revisions).Error; err != nil {
		t.Fatal(err)
	}
	for _, revision := range revisions {
		if len(revision.BeforeJSON) == 0 || len(revision.AfterJSON) == 0 {
			t.Fatalf("revision JSON defaults must be non-empty: %#v", revision)
		}
	}
	duplicateRevision := models.SKUPlatformContentRevision{PublicID: "revision-duplicate", SKUPlatformContentID: content.ID, Revision: 2, ActorID: 30}
	if err := db.Create(&duplicateRevision).Error; err == nil {
		t.Fatal("revision number must be unique per platform content row")
	}
	negativeRevision := models.SKUPlatformContentRevision{PublicID: "revision-negative", SKUPlatformContentID: content.ID, Revision: -1, ActorID: 30}
	if err := db.Create(&negativeRevision).Error; err == nil {
		t.Fatal("revision number must be positive")
	}
}
