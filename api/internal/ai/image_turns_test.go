package ai

import (
	"errors"
	"testing"

	"cargoflows/api/internal/models"
	"github.com/google/uuid"
)

func TestAIAssetsCannotBecomeImageIdentityEvidence(t *testing.T) {
	slot := models.AIContentSlot{Kind: models.AIContentSlotImage, ConstraintsJSON: []byte(`{}`)}
	view := models.SOPView{PresetKey: "reference_front"}
	if !errors.Is(validateImageSlotAssets([]models.AIContentSlot{slot}, []models.Asset{{OriginType: "ai_generated", SOPView: view}}), ErrAssetNotEligible) {
		t.Fatal("AI-generated asset was accepted as product identity evidence")
	}
	if err := validateImageSlotAssets([]models.AIContentSlot{slot}, []models.Asset{{OriginType: "captured", SOPView: view}}); err != nil {
		t.Fatalf("real captured identity evidence rejected: %v", err)
	}
}

func TestImageTurnEditIsAuditedAndRequeuesExactlyOneThread(t *testing.T) {
	db, fixture := seedAIJobFixture(t)
	if err := db.AutoMigrate(&models.AIImageThread{}, &models.AIImageTurn{}, &models.AIImageResult{}); err != nil {
		t.Fatal(err)
	}
	job, err := NewJobService(db).Create(t.Context(), CreateJobInput{SKUID: fixture.SKU.PublicID, TemplateVersionPublicID: fixture.PublishedVersion.PublicID, SelectedSlotKeys: []string{"hero"}, SelectedAssetIDs: []string{fixture.ApprovedAsset.PublicID}, Locale: "zh-CN", CreatedByID: fixture.Operator.ID, IdempotencyKey: "image-turn-history-test"})
	if err != nil {
		t.Fatal(err)
	}
	var item models.AIJobItem
	if err := db.Where("public_id = ?", job.Items[0].PublicID).First(&item).Error; err != nil {
		t.Fatal(err)
	}
	thread := models.AIImageThread{PublicID: uuid.NewString(), AIJobItemID: item.ID}
	if err := db.Create(&thread).Error; err != nil {
		t.Fatal(err)
	}
	turn := models.AIImageTurn{PublicID: uuid.NewString(), AIImageThreadID: thread.ID, Sequence: 1, Operation: models.AIExecutionGenerate, ActorID: fixture.Operator.ID, Status: models.AIImageTurnCompleted}
	if err := db.Create(&turn).Error; err != nil {
		t.Fatal(err)
	}
	execution := models.AIExecution{PublicID: uuid.NewString(), AIJobItemID: item.ID, AIImageTurnID: &turn.ID, Operation: models.AIExecutionGenerate, Status: models.AIExecutionCompleted, AttemptNumber: 1, RequestedModel: "gpt-image-2", Model: "gpt-image-2", APIMode: "images", NormalizedInputJSON: []byte(`{}`), OrderedInputListJSON: []byte(`[]`), RequestConfigJSON: []byte(`{}`), ProviderOutputJSON: []byte(`{}`)}
	if err := db.Create(&execution).Error; err != nil {
		t.Fatal(err)
	}
	result := models.AIImageResult{PublicID: uuid.NewString(), AIImageTurnID: turn.ID, AIExecutionID: execution.ID, CandidateIndex: 1, ObjectKey: "private/root.png", MIMEType: "image/png", Width: 1024, Height: 1024, ByteCount: 10, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if err := db.Create(&result).Error; err != nil {
		t.Fatal(err)
	}
	doc, err := NewImageResultService(db).CreateTurn(t.Context(), CreateImageTurnInput{JobPublicID: job.PublicID, ItemPublicID: item.PublicID, Operation: "edit", ParentResultPublicID: result.PublicID, UserInstruction: "make the background ivory", ActorID: fixture.Operator.ID})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Sequence != 2 || doc.Actor.Email != fixture.Operator.Email || doc.ParentResultID != result.PublicID {
		t.Fatalf("unexpected turn: %#v", doc)
	}
	var events int64
	if err := db.Model(&models.AIAuditEvent{}).Where("event_type = ? AND actor_id = ?", "ai_image.turn_created", fixture.Operator.ID).Count(&events).Error; err != nil || events != 1 {
		t.Fatalf("audit count=%d err=%v", events, err)
	}
	if _, err := NewImageResultService(db).CreateTurn(t.Context(), CreateImageTurnInput{JobPublicID: job.PublicID, ItemPublicID: item.PublicID, Operation: "restart", ActorID: fixture.Operator.ID}); !errors.Is(err, ErrImageTurnConflict) {
		t.Fatalf("concurrent turn err=%v", err)
	}
}
