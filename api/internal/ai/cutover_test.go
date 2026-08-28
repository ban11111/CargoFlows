package ai

import (
	"testing"
	"time"

	"cargoflows/api/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCancelUnfinishedForProductionIsIdempotentAndPreservesCompletedWork(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.AIJob{}, &models.AIJobItem{}, &models.AIExecution{}, &models.AIImageThread{}, &models.AIImageTurn{}); err != nil {
		t.Fatal(err)
	}

	partialJob := cutoverTestJob()
	cancelledJob := cutoverTestJob()
	if err := db.Create(&partialJob).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&cancelledJob).Error; err != nil {
		t.Fatal(err)
	}
	completed := cutoverTestItem(partialJob.ID, models.AIJobItemCompleted)
	queued := cutoverTestItem(partialJob.ID, models.AIJobItemQueued)
	running := cutoverTestItem(cancelledJob.ID, models.AIJobItemRunning)
	for _, item := range []*models.AIJobItem{&completed, &queued, &running} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	execution := models.AIExecution{
		PublicID: uuid.NewString(), AIJobItemID: queued.ID, Operation: models.AIExecutionTextGenerate,
		Status: models.AIExecutionCallingOpenAI, AttemptNumber: 1, NormalizedInputJSON: []byte(`{}`),
		OrderedInputListJSON: []byte(`[]`), RequestConfigJSON: []byte(`{}`), WorkerID: "old-worker",
	}
	if err := db.Create(&execution).Error; err != nil {
		t.Fatal(err)
	}
	thread := models.AIImageThread{PublicID: uuid.NewString(), AIJobItemID: running.ID}
	if err := db.Create(&thread).Error; err != nil {
		t.Fatal(err)
	}
	turn := models.AIImageTurn{
		PublicID: uuid.NewString(), AIImageThreadID: thread.ID, Sequence: 1, Operation: models.AIExecutionGenerate,
		RequestedCandidateCount: 1, ActorSnapshotJSON: []byte(`{}`), CompiledRequestMetadataJSON: []byte(`{}`),
		Status: models.AIImageTurnRunning, LeaseOwner: "old-worker",
	}
	if err := db.Create(&turn).Error; err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	result, err := CancelUnfinishedForProduction(t.Context(), db, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Jobs != 2 || result.Items != 2 || result.Executions != 1 || result.ImageTurns != 1 {
		t.Fatalf("cutover result = %#v", result)
	}

	if err := db.First(&completed, completed.ID).Error; err != nil || completed.Status != models.AIJobItemCompleted {
		t.Fatalf("completed item changed: status=%s err=%v", completed.Status, err)
	}
	if err := db.First(&queued, queued.ID).Error; err != nil || queued.Status != models.AIJobItemCancelled || queued.LeaseOwner != "" || queued.FailureCode != productionCutoverFailureCode {
		t.Fatalf("queued item = %#v err=%v", queued, err)
	}
	if err := db.First(&running, running.ID).Error; err != nil || running.Status != models.AIJobItemCancelled || running.LeaseOwner != "" {
		t.Fatalf("running item = %#v err=%v", running, err)
	}
	if err := db.First(&execution, execution.ID).Error; err != nil || execution.Status != models.AIExecutionCancelled || execution.WorkerID != "" {
		t.Fatalf("execution = %#v err=%v", execution, err)
	}
	if err := db.First(&turn, turn.ID).Error; err != nil || turn.Status != models.AIImageTurnCancelled || turn.LeaseOwner != "" {
		t.Fatalf("turn = %#v err=%v", turn, err)
	}
	if err := db.First(&partialJob, partialJob.ID).Error; err != nil || partialJob.Status != models.AIJobPartial || partialJob.CancelledAt != nil {
		t.Fatalf("partial job = %#v err=%v", partialJob, err)
	}
	if err := db.First(&cancelledJob, cancelledJob.ID).Error; err != nil || cancelledJob.Status != models.AIJobCancelled || cancelledJob.CancelledAt == nil {
		t.Fatalf("cancelled job = %#v err=%v", cancelledJob, err)
	}

	second, err := CancelUnfinishedForProduction(t.Context(), db, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if second != (CutoverCancellationResult{}) {
		t.Fatalf("second cutover = %#v", second)
	}
}

func cutoverTestJob() models.AIJob {
	return models.AIJob{
		PublicID: uuid.NewString(), SKUID: 1, AIContentTemplateVersionID: 1, TargetPlatform: "test", Locale: "en",
		Status: models.AIJobQueued, SnapshotSchema: ProductSnapshotSchemaV1, InputSnapshotJSON: []byte(`{}`),
		CreatedBySnapshotJSON: []byte(`{}`), ModelSnapshotJSON: []byte(`{}`), CreatedByID: 1,
	}
}

func cutoverTestItem(jobID uint, status models.AIJobItemStatus) models.AIJobItem {
	return models.AIJobItem{
		PublicID: uuid.NewString(), AIJobID: jobID, AIContentSlotID: 1, SlotKey: uuid.NewString(),
		Kind: models.AIContentSlotTitle, Status: status, SlotSnapshotJSON: []byte(`{}`), SelectedInputAssetIDsJSON: []byte(`[]`),
	}
}
