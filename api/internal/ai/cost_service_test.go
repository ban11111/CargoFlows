package ai

import (
	"context"
	"testing"
	"time"

	"cargoflows/api/internal/models"
	"cargoflows/api/internal/money"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestReconciliationAllocationsConserveBucketsAndVersionOnlyOnChange(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.AIJob{}, &models.AIJobItem{}, &models.AIExecution{}, &models.AIUsageLedger{}, &models.OpenAICostBucket{}, &models.AIReconciliationAllocation{}); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	jobA := createReconciliationJob(t, db, day.Add(time.Hour), "1.00000000", "priced")
	jobB := createReconciliationJob(t, db, day.Add(2*time.Hour), "3.00000000", "priced")
	bucket := models.OpenAICostBucket{BucketDate: day, ProjectID: "proj", APIKeyID: "key", LineItem: "responses", ActualAmountUSD: "1.00000000", SourceJSON: []byte(`{}`), Status: "open", SyncedAt: day.Add(3 * time.Hour)}
	if err := db.Create(&bucket).Error; err != nil {
		t.Fatal(err)
	}
	service := &CostService{db: db}
	if err := service.reconcile(context.Background(), day, day.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	assertAllocation(t, db, bucket.ID, 1, jobA.ID, "0.25000000")
	assertAllocation(t, db, bucket.ID, 1, jobB.ID, "0.75000000")
	if err := service.reconcile(context.Background(), day, day.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&models.AIReconciliationAllocation{}).Where("open_ai_cost_bucket_id = ?", bucket.ID).Count(&count)
	if count != 2 {
		t.Fatalf("unchanged reconciliation created %d rows", count)
	}
	if err := db.Model(&bucket).Update("actual_amount_usd", "2.00000000").Error; err != nil {
		t.Fatal(err)
	}
	if err := service.reconcile(context.Background(), day, day.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	assertAllocation(t, db, bucket.ID, 2, jobA.ID, "0.50000000")
	assertAllocation(t, db, bucket.ID, 2, jobB.ID, "1.50000000")
}

func TestReconciliationStopsAllocationWhenUsageIsUnpriced(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.AIJob{}, &models.AIJobItem{}, &models.AIExecution{}, &models.AIUsageLedger{}, &models.OpenAICostBucket{}, &models.AIReconciliationAllocation{}); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	createReconciliationJob(t, db, day.Add(time.Hour), "1.00000000", "priced")
	createReconciliationJob(t, db, day.Add(2*time.Hour), "0.00000000", "unpriced")
	bucket := models.OpenAICostBucket{BucketDate: day, ProjectID: "proj", APIKeyID: "key", LineItem: "images", ActualAmountUSD: "2.00000000", SourceJSON: []byte(`{}`), Status: "open", SyncedAt: day}
	db.Create(&bucket)
	if err := (&CostService{db: db}).reconcile(context.Background(), day, day.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&models.AIReconciliationAllocation{}).Where("open_ai_cost_bucket_id = ?", bucket.ID).Count(&count)
	if count != 0 {
		t.Fatalf("allocations=%d", count)
	}
	db.First(&bucket, bucket.ID)
	if bucket.Status != "needs_attention" {
		t.Fatalf("status=%s", bucket.Status)
	}
}

func createReconciliationJob(t *testing.T, db *gorm.DB, at time.Time, estimate, status string) models.AIJob {
	t.Helper()
	job := models.AIJob{PublicID: uuid.NewString(), SKUID: 1, AIContentTemplateVersionID: 1, TargetPlatform: "test", Locale: "en", Status: models.AIJobCompleted, SnapshotSchema: "test", InputSnapshotJSON: []byte(`{}`), CreatedByID: 1}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	item := models.AIJobItem{PublicID: uuid.NewString(), AIJobID: job.ID, AIContentSlotID: 1, SlotKey: uuid.NewString(), Kind: models.AIContentSlotTitle, Status: models.AIJobItemCompleted, SlotSnapshotJSON: []byte(`{}`), SelectedInputAssetIDsJSON: []byte(`[]`)}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	execution := models.AIExecution{PublicID: uuid.NewString(), AIJobItemID: item.ID, Operation: models.AIExecutionTextGenerate, Status: models.AIExecutionCompleted, AttemptNumber: 1, Model: "gpt", NormalizedInputJSON: []byte(`{}`), OrderedInputListJSON: []byte(`[]`), RequestConfigJSON: []byte(`{}`)}
	if err := db.Create(&execution).Error; err != nil {
		t.Fatal(err)
	}
	ledger := models.AIUsageLedger{AIExecutionID: execution.ID, Model: "gpt", InputTextTokens: 1, TotalTokens: 1, Currency: "USD", PricingStatus: status, EstimatedAmountUSD: estimate, CreatedAt: at}
	if err := db.Create(&ledger).Error; err != nil {
		t.Fatal(err)
	}
	return job
}

func assertAllocation(t *testing.T, db *gorm.DB, bucketID uint, version int, jobID uint, expected string) {
	t.Helper()
	var row models.AIReconciliationAllocation
	if err := db.Where("open_ai_cost_bucket_id = ? AND version = ? AND ai_job_id = ?", bucketID, version, jobID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if money.Format(money.Must(row.AllocatedAmountUSD)) != expected {
		t.Fatalf("allocation=%s want=%s", row.AllocatedAmountUSD, expected)
	}
}
