package ai

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"cargoflow/api/internal/models"
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
	if err := db.AutoMigrate(&models.AIJob{}, &models.AIJobItem{}, &models.AIExecution{}, &models.AIAuditEvent{}); err != nil {
		t.Fatal(err)
	}
	job := models.AIJob{PublicID: uuid.NewString(), SKUID: 1, AIContentTemplateVersionID: 1, TargetPlatform: "lazada", Locale: "zh-CN", Status: models.AIJobQueued, SnapshotSchema: ProductSnapshotSchemaV1, InputSnapshotJSON: []byte(`{"schema":"ai_product_snapshot.v1"}`), CreatedByID: 1}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	items := make([]models.AIJobItem, count)
	for i := range items {
		kind := models.AIContentSlotTitle
		if i%2 == 1 {
			kind = models.AIContentSlotImage
		}
		items[i] = models.AIJobItem{PublicID: uuid.NewString(), AIJobID: job.ID, AIContentSlotID: uint(i + 10), SlotKey: fmt.Sprintf("slot-%d", i), SlotSnapshotJSON: []byte(fmt.Sprintf(`{"public_id":"%s"}`, uuid.NewString())), Kind: kind, Status: models.AIJobItemQueued, SelectedInputAssetIDsJSON: []byte(`[]`)}
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
