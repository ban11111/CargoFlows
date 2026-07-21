package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"cargoflows/api/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func seedQueueItems(t *testing.T, count int) (*gorm.DB, models.AIJob, []models.AIJobItem) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_busy_timeout=5000", uuid.NewString())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.AIWorkerSetting{}, &models.AIContentTemplate{}, &models.AIContentTemplateVersion{}, &models.AIContentSlot{}, &models.AIJob{}, &models.AIJobItem{}, &models.AIExecution{}, &models.AIAuditEvent{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.AIWorkerSetting{ID: workerSettingID, MaxWorkersPerJob: DefaultMaxWorkersPerJob, MaxWorkersGlobal: DefaultMaxWorkersGlobal}).Error; err != nil {
		t.Fatal(err)
	}
	template := models.AIContentTemplate{PublicID: uuid.NewString(), NameZH: "测试模板", NameEN: "Test template", TargetPlatform: "lazada", Status: models.AIContentTemplateActive, CreatedByID: 1}
	if err := db.Create(&template).Error; err != nil {
		t.Fatal(err)
	}
	version := models.AIContentTemplateVersion{PublicID: uuid.NewString(), AIContentTemplateID: template.ID, VersionNumber: 1, Status: models.AITemplatePublished, DefaultLocale: "zh-CN", PromptCompilerVersion: "v1", PlatformPrompt: "test", CreatedByID: 1}
	if err := db.Create(&version).Error; err != nil {
		t.Fatal(err)
	}
	slots := make([]models.AIContentSlot, count)
	slotSnapshots := make([]SlotFacts, count)
	selectedAssets := []AssetFacts{}
	for i := range slots {
		kind := models.AIContentSlotTitle
		if i%2 == 1 {
			kind = models.AIContentSlotImage
			if len(selectedAssets) == 0 {
				selectedAssets = append(selectedAssets, AssetFacts{PublicID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"})
			}
		}
		slotKey := fmt.Sprintf("slot-%d", i)
		slots[i] = models.AIContentSlot{PublicID: uuid.NewString(), AIContentTemplateVersionID: version.ID, SlotKey: slotKey, Kind: kind, NameZH: slotKey, NameEN: slotKey, Sequence: i + 1, PromptFragment: slotKey, ConstraintsJSON: []byte(`{}`), GenerationConfigJSON: []byte(`{}`), LayoutConfigJSON: []byte(`{}`)}
		if err := db.Create(&slots[i]).Error; err != nil {
			t.Fatal(err)
		}
		slotSnapshots[i] = SlotFacts{PublicID: slots[i].PublicID, SlotKey: slotKey, Kind: kind, Sequence: i + 1, PromptFragment: slotKey, Constraints: json.RawMessage(`{}`), GenerationConfig: json.RawMessage(`{}`), LayoutConfig: json.RawMessage(`{}`)}
	}
	snapshot := ProductSnapshotV1{Schema: ProductSnapshotSchemaV1, Template: TemplateFacts{TemplatePublicID: template.PublicID, VersionPublicID: version.PublicID, VersionNumber: version.VersionNumber, SelectedSlots: slotSnapshots}, SelectedAssets: selectedAssets}
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	job := models.AIJob{PublicID: uuid.NewString(), SKUID: 1, AIContentTemplateVersionID: version.ID, TargetPlatform: "lazada", Locale: "zh-CN", Status: models.AIJobQueued, SnapshotSchema: ProductSnapshotSchemaV1, InputSnapshotJSON: snapshotJSON, CreatedByID: 1}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	items := make([]models.AIJobItem, count)
	for i := range items {
		slotSnapshotJSON, err := json.Marshal(slotSnapshots[i])
		if err != nil {
			t.Fatal(err)
		}
		assetIDsJSON := []byte(`[]`)
		if slots[i].Kind == models.AIContentSlotImage {
			assetIDsJSON = []byte(`["aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"]`)
		}
		items[i] = models.AIJobItem{PublicID: uuid.NewString(), AIJobID: job.ID, AIContentSlotID: slots[i].ID, SlotKey: slots[i].SlotKey, SlotSnapshotJSON: slotSnapshotJSON, Kind: slots[i].Kind, Status: models.AIJobItemQueued, SelectedInputAssetIDsJSON: assetIDsJSON}
		if err := db.Create(&items[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db, job, items
}

func TestLeaseNextIsExclusiveAndExpiredLeaseCanRecover(t *testing.T) {
	db, _, items := seedQueueItems(t, 1)
	queue := NewQueue(db)
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	first, err := queue.LeaseNext(t.Context(), "worker-a", now, time.Minute)
	if err != nil || first == nil || first.PublicID != items[0].PublicID {
		t.Fatalf("first lease = %#v, %v", first, err)
	}
	second, err := queue.LeaseNext(t.Context(), "worker-b", now, time.Minute)
	if err != nil || second != nil {
		t.Fatalf("duplicate lease = %#v, %v", second, err)
	}
	recovered, err := queue.LeaseNext(t.Context(), "worker-b", now.Add(2*time.Minute), time.Minute)
	if err != nil || recovered == nil || recovered.PublicID != items[0].PublicID || recovered.LeaseOwner != "worker-b" {
		t.Fatalf("recovery = %#v, %v", recovered, err)
	}
}

func TestConcurrentLeaseReturnsEachItemOnce(t *testing.T) {
	db, _, items := seedQueueItems(t, 2)
	queue := NewQueue(db)
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	start := make(chan struct{})
	results := make(chan *LeasedItem, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, workerID := range []string{"worker-a", "worker-b"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			<-start
			item, err := queue.LeaseNext(t.Context(), id, now, time.Minute)
			results <- item
			errs <- err
		}(workerID)
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]bool{}
	for leased := range results {
		if leased == nil {
			t.Fatal("concurrent lease returned nil")
		}
		if got[leased.PublicID] {
			t.Fatalf("item %s was leased twice", leased.PublicID)
		}
		got[leased.PublicID] = true
	}
	if len(got) != len(items) {
		t.Fatalf("leased %d distinct items, want %d", len(got), len(items))
	}
}

func TestLeaseNextEnforcesPerJobAndGlobalWorkerLimits(t *testing.T) {
	db, firstJob, firstItems := seedQueueItems(t, 5)
	secondJob := firstJob
	secondJob.ID = 0
	secondJob.PublicID = uuid.NewString()
	secondJob.Status = models.AIJobQueued
	secondJob.StartedAt = nil
	if err := db.Create(&secondJob).Error; err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		item := firstItems[index]
		item.ID = 0
		item.PublicID = uuid.NewString()
		item.AIJobID = secondJob.ID
		item.Status = models.AIJobItemQueued
		item.CreatedAt = firstItems[len(firstItems)-1].CreatedAt.Add(time.Second)
		item.UpdatedAt = item.CreatedAt
		item.LeaseOwner = ""
		item.LeaseExpiresAt = nil
		item.AttemptCount = 0
		if err := db.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewWorkerSettingsService(db).Update(t.Context(), 1, WorkerConcurrency{MaxWorkersPerJob: 2, MaxWorkersGlobal: 3}); err != nil {
		t.Fatal(err)
	}
	queue := NewQueue(db)
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	leases := make([]*LeasedItem, 0, 3)
	for index := 0; index < 3; index++ {
		leased, err := queue.LeaseNext(t.Context(), fmt.Sprintf("worker-%d", index), now, time.Minute)
		if err != nil || leased == nil {
			t.Fatalf("lease %d = %#v, %v", index, leased, err)
		}
		leases = append(leases, leased)
	}
	if leases[0].jobID != firstJob.ID || leases[1].jobID != firstJob.ID || leases[2].jobID != secondJob.ID {
		t.Fatalf("job lease order = %d, %d, %d", leases[0].jobID, leases[1].jobID, leases[2].jobID)
	}
	blocked, err := queue.LeaseNext(t.Context(), "worker-blocked", now, time.Minute)
	if err != nil || blocked != nil {
		t.Fatalf("global limit lease = %#v, %v", blocked, err)
	}
	if err := queue.completeAt(t.Context(), *leases[0], now.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	replacement, err := queue.LeaseNext(t.Context(), "worker-replacement", now.Add(10*time.Second), time.Minute)
	if err != nil || replacement == nil || replacement.jobID != firstJob.ID {
		t.Fatalf("replacement lease = %#v, %v", replacement, err)
	}
}

func TestExpiredLeaseDoesNotConsumeWorkerCapacity(t *testing.T) {
	db, _, _ := seedQueueItems(t, 2)
	if _, err := NewWorkerSettingsService(db).Update(t.Context(), 1, WorkerConcurrency{MaxWorkersPerJob: 1, MaxWorkersGlobal: 1}); err != nil {
		t.Fatal(err)
	}
	queue := NewQueue(db)
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	first, err := queue.LeaseNext(t.Context(), "worker-a", now, time.Minute)
	if err != nil || first == nil {
		t.Fatalf("first lease = %#v, %v", first, err)
	}
	recovered, err := queue.LeaseNext(t.Context(), "worker-b", now.Add(2*time.Minute), time.Minute)
	if err != nil || recovered == nil || recovered.PublicID != first.PublicID || recovered.Attempt != first.Attempt+1 {
		t.Fatalf("recovered lease = %#v, %v", recovered, err)
	}
}

func TestConcurrentLeaseNeverExceedsGlobalWorkerLimit(t *testing.T) {
	db, originalJob, items := seedQueueItems(t, 6)
	for index := 1; index < len(items); index++ {
		job := originalJob
		job.ID = 0
		job.PublicID = uuid.NewString()
		job.Status = models.AIJobQueued
		job.StartedAt = nil
		if err := db.Create(&job).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&items[index]).Update("ai_job_id", job.ID).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewWorkerSettingsService(db).Update(t.Context(), 1, WorkerConcurrency{MaxWorkersPerJob: 3, MaxWorkersGlobal: 5}); err != nil {
		t.Fatal(err)
	}
	queue := NewQueue(db)
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	start := make(chan struct{})
	results := make(chan *LeasedItem, len(items))
	errorsFound := make(chan error, len(items))
	var wg sync.WaitGroup
	for index := range items {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			leased, err := queue.LeaseNext(t.Context(), fmt.Sprintf("worker-%d", worker), now, time.Minute)
			results <- leased
			errorsFound <- err
		}(index)
	}
	close(start)
	wg.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	leasedCount := 0
	for leased := range results {
		if leased != nil {
			leasedCount++
		}
	}
	if leasedCount != 5 {
		t.Fatalf("concurrent leases = %d, want 5", leasedCount)
	}
}

func TestLeaseNextClearsStaleResultBeforeNoCandidateRetry(t *testing.T) {
	db, _, _ := seedQueueItems(t, 1)
	queue := NewQueue(db)
	calls := 0
	queue.runTransaction = func(ctx context.Context, fn func(*gorm.DB) error) error {
		calls++
		if err := fn(db.WithContext(ctx)); err != nil {
			return err
		}
		if calls == 1 {
			return errLeaseContended
		}
		return nil
	}
	leased, err := queue.LeaseNext(t.Context(), "worker-a", time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if leased != nil {
		t.Fatalf("lease after retryable commit failure and empty retry = %#v, want nil", leased)
	}
}

func TestLeaseOwnerGuardsHeartbeatAndTerminalTransitions(t *testing.T) {
	db, _, _ := seedQueueItems(t, 1)
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	queue := newQueueWithClock(db, fixedClock{now: now.Add(20 * time.Second)})
	leased, err := queue.LeaseNext(t.Context(), "worker-a", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	stolen := *leased
	stolen.LeaseOwner = "worker-b"
	if err := queue.Heartbeat(t.Context(), stolen, now.Add(10*time.Second), time.Minute); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("heartbeat error = %v, want lease lost", err)
	}
	if err := queue.Complete(t.Context(), stolen); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("complete error = %v, want lease lost", err)
	}
	if err := queue.Fail(t.Context(), stolen, "safe message"); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("fail error = %v, want lease lost", err)
	}
	if err := queue.Heartbeat(t.Context(), *leased, now.Add(10*time.Second), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := queue.Complete(t.Context(), *leased); err != nil {
		t.Fatal(err)
	}
	if err := queue.Complete(t.Context(), *leased); err != nil {
		t.Fatalf("matching-owner completion must be idempotent: %v", err)
	}
}

func TestRecoveredAttemptRejectsOlderLeaseWithSameWorkerID(t *testing.T) {
	operations := map[string]func(*Queue, LeasedItem, time.Time) error{
		"heartbeat": func(queue *Queue, item LeasedItem, now time.Time) error {
			return queue.Heartbeat(t.Context(), item, now, time.Minute)
		},
		"complete": func(queue *Queue, item LeasedItem, _ time.Time) error {
			return queue.Complete(t.Context(), item)
		},
		"fail": func(queue *Queue, item LeasedItem, _ time.Time) error {
			return queue.Fail(t.Context(), item, "safe failure")
		},
	}
	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			db, _, _ := seedQueueItems(t, 1)
			now := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
			queue := newQueueWithClock(db, fixedClock{now: now.Add(2*time.Minute + time.Second)})
			older, err := queue.LeaseNext(t.Context(), "worker-a", now, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			newer, err := queue.LeaseNext(t.Context(), "worker-a", now.Add(2*time.Minute), time.Minute)
			if err != nil || newer == nil || newer.Attempt != older.Attempt+1 {
				t.Fatalf("recovered lease = %#v, %v", newer, err)
			}
			if err := operation(queue, *older, now.Add(2*time.Minute+time.Second)); !errors.Is(err, ErrLeaseLost) {
				t.Fatalf("older attempt transition error = %v, want lease lost", err)
			}
		})
	}
}

func TestRepeatedTerminalCallRequiresSameAttempt(t *testing.T) {
	db, _, _ := seedQueueItems(t, 1)
	now := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
	queue := newQueueWithClock(db, fixedClock{now: now.Add(2*time.Minute + time.Second)})
	older, _ := queue.LeaseNext(t.Context(), "worker-a", now, time.Minute)
	newer, _ := queue.LeaseNext(t.Context(), "worker-a", now.Add(2*time.Minute), time.Minute)
	if err := queue.Complete(t.Context(), *newer); err != nil {
		t.Fatal(err)
	}
	if err := queue.Complete(t.Context(), *newer); err != nil {
		t.Fatalf("same-attempt completion should be idempotent: %v", err)
	}
	if err := queue.Complete(t.Context(), *older); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("older-attempt repeated completion error = %v, want lease lost", err)
	}
}

func TestExpiredLeaseCannotCompleteOrFailBeforeRecovery(t *testing.T) {
	for _, status := range []models.AIJobItemStatus{models.AIJobItemCompleted, models.AIJobItemFailed} {
		t.Run(string(status), func(t *testing.T) {
			db, _, _ := seedQueueItems(t, 1)
			leasedAt := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
			queue := newQueueWithClock(db, fixedClock{now: leasedAt.Add(2 * time.Minute)})
			leased, err := queue.LeaseNext(t.Context(), "worker-a", leasedAt, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if status == models.AIJobItemCompleted {
				err = queue.Complete(t.Context(), *leased)
			} else {
				err = queue.Fail(t.Context(), *leased, "safe failure")
			}
			if !errors.Is(err, ErrLeaseLost) {
				t.Fatalf("expired transition error = %v, want lease lost", err)
			}
		})
	}
}

func TestParentJobAggregatesAfterEveryTransition(t *testing.T) {
	db, job, _ := seedQueueItems(t, 2)
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	queue := newQueueWithClock(db, fixedClock{now: now.Add(20 * time.Second)})
	first, _ := queue.LeaseNext(t.Context(), "worker-a", now, time.Minute)
	assertJobStatus(t, db, job.ID, models.AIJobRunning)
	if err := queue.Complete(t.Context(), *first); err != nil {
		t.Fatal(err)
	}
	assertJobStatus(t, db, job.ID, models.AIJobRunning)
	second, _ := queue.LeaseNext(t.Context(), "worker-a", now, time.Minute)
	if err := queue.Fail(t.Context(), *second, "generation failed"); err != nil {
		t.Fatal(err)
	}
	assertJobStatus(t, db, job.ID, models.AIJobPartial)
	var stored models.AIJobItem
	if err := db.Where("id = ?", second.itemID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.SafeError != "generation failed" || stored.InternalError != "" || stored.LeaseOwner != "" || stored.LeaseExpiresAt != nil {
		t.Fatalf("failed item leaked or retained lease: %#v", stored)
	}
}

func assertJobStatus(t *testing.T, db *gorm.DB, id uint, want models.AIJobStatus) {
	t.Helper()
	var job models.AIJob
	if err := db.First(&job, id).Error; err != nil {
		t.Fatal(err)
	}
	if job.Status != want {
		t.Fatalf("job status = %s, want %s", job.Status, want)
	}
}
