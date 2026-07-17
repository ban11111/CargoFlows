package database

import (
	"testing"

	"cargoflow/api/internal/models"
)

func TestMigrateCreatesAIImageHistoryTables(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	for _, model := range []any{&models.AIImageThread{}, &models.AIImageTurn{}, &models.AIImageResult{}} {
		if !db.Migrator().HasTable(model) {
			t.Fatalf("missing table for %T", model)
		}
	}
	if !db.Migrator().HasColumn(&models.AIExecution{}, "ai_image_turn_id") {
		t.Fatal("missing image-turn provenance on AI execution")
	}
}

func TestAIImageHistoryConstraintsAndSelection(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}

	threadA := models.AIImageThread{PublicID: "image-thread-a", AIJobItemID: 101}
	threadB := models.AIImageThread{PublicID: "image-thread-b", AIJobItemID: 102}
	if err := db.Create(&threadA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&threadB).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.AIImageThread{PublicID: "image-thread-duplicate", AIJobItemID: threadA.AIJobItemID}).Error; err == nil {
		t.Fatal("only one image thread may exist per job item")
	}

	rootA := models.AIImageTurn{PublicID: "image-turn-a-1", AIImageThreadID: threadA.ID, Sequence: 1, Operation: models.AIExecutionGenerate, ActorID: 201}
	rootB := models.AIImageTurn{PublicID: "image-turn-b-1", AIImageThreadID: threadB.ID, Sequence: 1, Operation: models.AIExecutionRestart, ActorID: 201}
	if err := db.Create(&rootA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&rootB).Error; err != nil {
		t.Fatal(err)
	}
	if len(rootA.CompiledRequestMetadataJSON) == 0 || rootA.Status != models.AIImageTurnQueued || rootA.RequestedCandidateCount != 1 {
		t.Fatalf("unexpected persisted turn defaults: %#v", rootA)
	}
	duplicateSequence := models.AIImageTurn{PublicID: "image-turn-a-duplicate", AIImageThreadID: threadA.ID, Sequence: 1, Operation: models.AIExecutionGenerate, ActorID: 201}
	if err := db.Create(&duplicateSequence).Error; err == nil {
		t.Fatal("turn sequence must be unique per thread")
	}

	resultA := models.AIImageResult{PublicID: "image-result-a", AIImageTurnID: rootA.ID, AIExecutionID: 301, CandidateIndex: 1, ObjectKey: "generated/a.png", MIMEType: "image/png", Width: 1024, Height: 1024, ByteCount: 100, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	resultB := models.AIImageResult{PublicID: "image-result-b", AIImageTurnID: rootB.ID, AIExecutionID: 302, CandidateIndex: 1, ObjectKey: "generated/b.png", MIMEType: "image/png", Width: 1024, Height: 1024, ByteCount: 100, SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	if err := db.Create(&resultA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&resultB).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.AIImageResult{PublicID: "image-result-duplicate-candidate", AIImageTurnID: rootA.ID, AIExecutionID: 303, CandidateIndex: 1, ObjectKey: "generated/c.png", MIMEType: "image/png", Width: 1024, Height: 1024, ByteCount: 100, SHA256: resultA.SHA256}).Error; err == nil {
		t.Fatal("candidate index must be unique per turn")
	}
	if err := db.Create(&models.AIImageResult{PublicID: "image-result-duplicate-execution", AIImageTurnID: rootA.ID, AIExecutionID: resultA.AIExecutionID, CandidateIndex: 2, ObjectKey: "generated/d.png", MIMEType: "image/png", Width: 1024, Height: 1024, ByteCount: 100, SHA256: resultA.SHA256}).Error; err == nil {
		t.Fatal("an execution may produce only one durable image result")
	}
	if err := db.Create(&models.AIImageResult{PublicID: "image-result-bad-index", AIImageTurnID: rootA.ID, AIExecutionID: 304, CandidateIndex: 0, ObjectKey: "generated/e.png", MIMEType: "image/png", Width: 1024, Height: 1024, ByteCount: 100, SHA256: resultA.SHA256}).Error; err == nil {
		t.Fatal("candidate index must be positive")
	}

	parentA := resultA.ID
	validEdit := models.AIImageTurn{PublicID: "image-turn-a-2", AIImageThreadID: threadA.ID, Sequence: 2, Operation: models.AIExecutionEdit, ParentResultID: &parentA, ActorID: 201}
	if err := db.Create(&validEdit).Error; err != nil {
		t.Fatal(err)
	}
	wrongParent := resultB.ID
	if err := db.Create(&models.AIImageResult{PublicID: "image-result-wrong-parent", AIImageTurnID: validEdit.ID, AIExecutionID: 305, ParentResultID: &wrongParent, CandidateIndex: 1, ObjectKey: "generated/f.png", MIMEType: "image/png", Width: 1024, Height: 1024, ByteCount: 100, SHA256: resultA.SHA256}).Error; err == nil {
		t.Fatal("image result parent must exactly match the turn parent")
	}
	for _, invalid := range []models.AIImageTurn{
		{PublicID: "image-turn-edit-no-parent", AIImageThreadID: threadA.ID, Sequence: 3, Operation: models.AIExecutionEdit, ActorID: 201},
		{PublicID: "image-turn-generate-parent", AIImageThreadID: threadA.ID, Sequence: 4, Operation: models.AIExecutionGenerate, ParentResultID: &parentA, ActorID: 201},
		{PublicID: "image-turn-restart-parent", AIImageThreadID: threadA.ID, Sequence: 5, Operation: models.AIExecutionRestart, ParentResultID: &parentA, ActorID: 201},
		{PublicID: "image-turn-cross-thread", AIImageThreadID: threadB.ID, Sequence: 2, Operation: models.AIExecutionEdit, ParentResultID: &parentA, ActorID: 201},
	} {
		if err := db.Create(&invalid).Error; err == nil {
			t.Fatalf("invalid turn was accepted: %#v", invalid)
		}
	}
	tooManyCandidates := models.AIImageTurn{PublicID: "image-turn-too-many", AIImageThreadID: threadA.ID, Sequence: 6, Operation: models.AIExecutionGenerate, RequestedCandidateCount: 5, ActorID: 201}
	if err := db.Create(&tooManyCandidates).Error; err == nil {
		t.Fatal("candidate count must remain between one and four")
	}

	threadA.SelectedResultID = &resultA.ID
	if err := db.Save(&threadA).Error; err != nil {
		t.Fatal(err)
	}
	threadA.SelectedResultID = &resultB.ID
	if err := db.Save(&threadA).Error; err == nil {
		t.Fatal("selected result must belong to the same image thread")
	}

	if err := db.Delete(&threadA).Error; err != nil {
		t.Fatal(err)
	}
	var turnCount, resultCount int64
	db.Model(&models.AIImageTurn{}).Where("ai_image_thread_id = ?", threadA.ID).Count(&turnCount)
	db.Model(&models.AIImageResult{}).Where("ai_image_turn_id = ?", rootA.ID).Count(&resultCount)
	if turnCount == 0 || resultCount == 0 {
		t.Fatalf("deleting a thread cascaded into immutable history: turns=%d results=%d", turnCount, resultCount)
	}
}
