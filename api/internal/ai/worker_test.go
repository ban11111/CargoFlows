package ai

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"cargoflows/api/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type executorFunc func(LeasedItem) error

func (f executorFunc) Execute(_ context.Context, item LeasedItem) error { return f(item) }

type contextExecutorFunc func(context.Context, LeasedItem) error

func (f contextExecutorFunc) Execute(ctx context.Context, item LeasedItem) error { return f(ctx, item) }

type mutableClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *mutableClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *mutableClock) Set(now time.Time) {
	c.mu.Lock()
	c.now = now
	c.mu.Unlock()
}

type fakeHeartbeatTicker struct {
	ch      chan time.Time
	stopped chan struct{}
	once    sync.Once
}

func newFakeHeartbeatTicker() *fakeHeartbeatTicker {
	return &fakeHeartbeatTicker{ch: make(chan time.Time), stopped: make(chan struct{})}
}

func (t *fakeHeartbeatTicker) C() <-chan time.Time { return t.ch }
func (t *fakeHeartbeatTicker) Stop()               { t.once.Do(func() { close(t.stopped) }) }

func TestWorkerHeartbeatsDuringBlockingExecution(t *testing.T) {
	db, job, _ := seedQueueItems(t, 1)
	base := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	clock := &mutableClock{now: base}
	ticker := newFakeHeartbeatTicker()
	started, release := make(chan struct{}), make(chan struct{})
	worker := NewWorker(NewQueue(db), "worker-a", time.Minute, clock, contextExecutorFunc(func(ctx context.Context, _ LeasedItem) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}))
	intervals := make(chan time.Duration, 1)
	worker.newTicker = func(interval time.Duration) heartbeatTicker {
		intervals <- interval
		return ticker
	}
	heartbeats := make(chan error)
	worker.heartbeat = func(ctx context.Context, item LeasedItem, now time.Time, ttl time.Duration) error {
		err := worker.queue.Heartbeat(ctx, item, now, ttl)
		heartbeats <- err
		return err
	}
	result := make(chan error, 1)
	go func() {
		worked, err := worker.RunOnce(t.Context())
		if !worked && err == nil {
			err = errors.New("worker found no work")
		}
		result <- err
	}()
	<-started
	interval := <-intervals
	if interval <= 0 || interval > time.Minute/2 {
		t.Fatalf("heartbeat interval = %s", interval)
	}
	for _, elapsed := range []time.Duration{30 * time.Second, 60 * time.Second} {
		clock.Set(base.Add(elapsed))
		ticker.ch <- base.Add(elapsed)
		if err := <-heartbeats; err != nil {
			t.Fatal(err)
		}
		assertLeaseExpiry(t, db, base.Add(elapsed+time.Minute))
	}
	recovered, err := NewQueue(db).LeaseNext(t.Context(), "worker-b", base.Add(90*time.Second), time.Minute)
	if err != nil || recovered != nil {
		t.Fatalf("active heartbeat lease was recovered: %#v, %v", recovered, err)
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	select {
	case <-ticker.stopped:
	default:
		t.Fatal("heartbeat ticker was not stopped")
	}
	assertJobStatus(t, db, job.ID, models.AIJobCompleted)
}

func TestHeartbeatLeaseLossCancelsExecutorAndSkipsTerminalTransition(t *testing.T) {
	db, _, _ := seedQueueItems(t, 1)
	base := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	clock := &mutableClock{now: base}
	ticker := newFakeHeartbeatTicker()
	started := make(chan struct{})
	leasedItems := make(chan LeasedItem, 1)
	queue := NewQueue(db)
	worker := NewWorker(queue, "worker-a", time.Minute, clock, contextExecutorFunc(func(ctx context.Context, item LeasedItem) error {
		leasedItems <- item
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}))
	worker.newTicker = func(time.Duration) heartbeatTicker { return ticker }
	result := make(chan error, 1)
	go func() {
		_, err := worker.RunOnce(t.Context())
		result <- err
	}()
	<-started
	newer, err := NewQueue(db).LeaseNext(t.Context(), "worker-b", base.Add(2*time.Minute), time.Minute)
	if err != nil || newer == nil {
		t.Fatalf("recovered lease = %#v, %v", newer, err)
	}
	clock.Set(base.Add(2*time.Minute + time.Second))
	ticker.ch <- clock.Now()
	if err := <-result; !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("worker error = %v, want lease lost", err)
	}
	older := <-leasedItems
	if err := queue.Complete(t.Context(), older); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale completion error = %v, want lease lost", err)
	}
	if err := queue.Fail(t.Context(), older, "safe failure"); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale failure error = %v, want lease lost", err)
	}
	var item models.AIJobItem
	if err := db.Where("public_id = ?", newer.PublicID).First(&item).Error; err != nil {
		t.Fatal(err)
	}
	if item.Status != models.AIJobItemRunning || item.LeaseOwner != "worker-b" || item.AttemptCount != newer.Attempt {
		t.Fatalf("stale worker changed recovered item: %#v", item)
	}
}

func assertLeaseExpiry(t *testing.T, db *gorm.DB, want time.Time) {
	t.Helper()
	var item models.AIJobItem
	if err := db.First(&item).Error; err != nil {
		t.Fatal(err)
	}
	if item.LeaseExpiresAt == nil || !item.LeaseExpiresAt.Equal(want) {
		t.Fatalf("lease expiry = %v, want %s", item.LeaseExpiresAt, want)
	}
}

func TestWorkerRunOnceCompletesItemAndReturnsWhetherWorkWasFound(t *testing.T) {
	db, job, _ := seedQueueItems(t, 1)
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	worker := NewWorker(NewQueue(db), "worker-a", time.Minute, fixedClock{now: now}, executorFunc(func(LeasedItem) error { return nil }))
	worked, err := worker.RunOnce(t.Context())
	if err != nil || !worked {
		t.Fatalf("first run = %v, %v", worked, err)
	}
	assertJobStatus(t, db, job.ID, models.AIJobCompleted)
	worked, err = worker.RunOnce(t.Context())
	if err != nil || worked {
		t.Fatalf("empty run = %v, %v", worked, err)
	}
}

func TestWorkerStoresOnlySafeExecutorError(t *testing.T) {
	db, job, items := seedQueueItems(t, 1)
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	worker := NewWorker(NewQueue(db), "worker-a", time.Minute, fixedClock{now: now}, executorFunc(func(LeasedItem) error { return errors.New("dial tcp 10.0.0.8: secret") }))
	worked, err := worker.RunOnce(t.Context())
	if err != nil || !worked {
		t.Fatalf("run = %v, %v", worked, err)
	}
	assertJobStatus(t, db, job.ID, models.AIJobFailed)
	var stored models.AIJobItem
	if err := db.Where("id = ?", items[0].ID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.SafeError != defaultSafeExecutionError || stored.InternalError != "" {
		t.Fatalf("stored errors = safe %q internal %q", stored.SafeError, stored.InternalError)
	}
}

func TestDryRunExecutorCreatesOneCompletedExecutionAndAudit(t *testing.T) {
	db, _, _ := seedQueueItems(t, 1)
	queue := NewQueue(db)
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	leased, err := queue.LeaseNext(t.Context(), "worker-a", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	executor := newDryRunExecutorWithClock(db, fixedClock{now: now.Add(10 * time.Second)})
	if err := executor.Execute(t.Context(), *leased); err != nil {
		t.Fatal(err)
	}
	if err := executor.Execute(t.Context(), *leased); err != nil {
		t.Fatalf("dry-run execution must be idempotent: %v", err)
	}
	var executions []models.AIExecution
	if err := db.Find(&executions).Error; err != nil {
		t.Fatal(err)
	}
	if len(executions) != 1 {
		t.Fatalf("execution count = %d, want 1", len(executions))
	}
	execution := executions[0]
	if execution.Status != models.AIExecutionCompleted || execution.Operation != models.AIExecutionTextGenerate || execution.InputTextTokens != 0 || execution.InputImageTokens != 0 || execution.OutputTextTokens != 0 || execution.OutputImageTokens != 0 || execution.Model != "dry-run" || execution.CompletedAt == nil {
		t.Fatalf("unexpected dry-run execution: %#v", execution)
	}
	var audits []models.AIAuditEvent
	if err := db.Find(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if len(audits) != 1 || audits[0].EventType != "ai_execution.dry_run_completed" || audits[0].AIExecutionID == nil || *audits[0].AIExecutionID != execution.ID {
		t.Fatalf("unexpected audit events: %#v", audits)
	}
}

func TestExecutionProvenanceAcceptsV2BilingualSnapshot(t *testing.T) {
	db, job, _ := seedQueueItems(t, 1)
	var snapshot ProductSnapshotV1
	if err := json.Unmarshal(job.InputSnapshotJSON, &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.Schema = ProductSnapshotSchemaV2
	snapshot.Locale = "en"
	snapshot.OutputLocales = []string{"en", "zh-CN"}
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	localesJSON, err := json.Marshal(snapshot.OutputLocales)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.AIJob{}).Where("id = ?", job.ID).Updates(map[string]any{
		"snapshot_schema":     ProductSnapshotSchemaV2,
		"locale":              snapshot.Locale,
		"output_locales_json": localesJSON,
		"input_snapshot_json": snapshotJSON,
	}).Error; err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 7, 22, 2, 0, 0, 0, time.UTC)
	leased, err := NewQueue(db).LeaseNext(t.Context(), "worker-v2", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := newDryRunExecutorWithClock(db, fixedClock{now: now.Add(time.Second)}).Execute(t.Context(), *leased); err != nil {
		t.Fatalf("execute v2 bilingual snapshot: %v", err)
	}
	var execution models.AIExecution
	if err := db.First(&execution).Error; err != nil {
		t.Fatal(err)
	}
	if execution.Status != models.AIExecutionCompleted || execution.L1ProductContextVersion != ProductSnapshotSchemaV2 {
		t.Fatalf("v2 execution = %#v", execution)
	}
}

func TestDryRunExecutorRejectsLostLeaseWithoutWrites(t *testing.T) {
	db, _, _ := seedQueueItems(t, 1)
	queue := NewQueue(db)
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	leased, _ := queue.LeaseNext(t.Context(), "worker-a", now, time.Minute)
	recovered, _ := queue.LeaseNext(t.Context(), "worker-b", now.Add(2*time.Minute), time.Minute)
	if err := newDryRunExecutorWithClock(db, fixedClock{now: now.Add(2*time.Minute + time.Second)}).Execute(t.Context(), *leased); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale execute error = %v, want lease lost", err)
	}
	if err := newDryRunExecutorWithClock(db, fixedClock{now: now.Add(2*time.Minute + time.Second)}).Execute(t.Context(), *recovered); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&models.AIExecution{}).Count(&count)
	if count != 1 {
		t.Fatalf("execution count = %d, want 1", count)
	}
}

func TestDryRunExecutorRejectsOlderAttemptRecoveredBySameWorker(t *testing.T) {
	db, _, _ := seedQueueItems(t, 1)
	queue := NewQueue(db)
	now := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
	older, err := queue.LeaseNext(t.Context(), "worker-a", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	newer, err := queue.LeaseNext(t.Context(), "worker-a", now.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	executor := newDryRunExecutorWithClock(db, fixedClock{now: now.Add(2*time.Minute + time.Second)})
	if err := executor.Execute(t.Context(), *older); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("older same-owner execute error = %v, want lease lost", err)
	}
	if err := executor.Execute(t.Context(), *newer); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&models.AIExecution{}).Count(&count)
	if count != 1 {
		t.Fatalf("execution count = %d, want 1", count)
	}
}

func TestDryRunExecutorRejectsExpiredLeaseBeforeRecovery(t *testing.T) {
	db, _, _ := seedQueueItems(t, 1)
	leasedAt := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	queue := NewQueue(db)
	leased, err := queue.LeaseNext(t.Context(), "worker-a", leasedAt, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := newDryRunExecutorWithClock(db, fixedClock{now: leasedAt.Add(2 * time.Minute)}).Execute(t.Context(), *leased); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("expired execute error = %v, want lease lost", err)
	}
	var count int64
	db.Model(&models.AIExecution{}).Count(&count)
	if count != 0 {
		t.Fatalf("execution count = %d, want 0", count)
	}
}

func TestDryRunExecutorRejectsCorruptProvenanceWithoutWrites(t *testing.T) {
	tests := map[string]func(*gorm.DB, models.AIJobItem){
		"unknown item kind": func(db *gorm.DB, item models.AIJobItem) {
			db.Model(&models.AIJobItem{}).Where("id = ?", item.ID).Update("kind", "audio")
		},
		"malformed job snapshot": func(db *gorm.DB, item models.AIJobItem) {
			db.Model(&models.AIJob{}).Where("id = ?", item.AIJobID).Update("input_snapshot_json", []byte(`{"schema":`))
		},
		"wrong snapshot schema": func(db *gorm.DB, item models.AIJobItem) {
			db.Model(&models.AIJob{}).Where("id = ?", item.AIJobID).Update("snapshot_schema", "legacy")
		},
		"invalid template UUID": func(db *gorm.DB, item models.AIJobItem) {
			var snapshot ProductSnapshotV1
			var job models.AIJob
			db.First(&job, item.AIJobID)
			_ = json.Unmarshal(job.InputSnapshotJSON, &snapshot)
			snapshot.Template.VersionPublicID = "not-a-uuid"
			encoded, _ := json.Marshal(snapshot)
			db.Model(&models.AIJob{}).Where("id = ?", item.AIJobID).Update("input_snapshot_json", encoded)
		},
		"malformed slot snapshot": func(db *gorm.DB, item models.AIJobItem) {
			db.Model(&models.AIJobItem{}).Where("id = ?", item.ID).Update("slot_snapshot_json", []byte(`{"public_id":`))
		},
		"invalid slot UUID": func(db *gorm.DB, item models.AIJobItem) {
			db.Model(&models.AIJobItem{}).Where("id = ?", item.ID).Update("slot_snapshot_json", []byte(`{"public_id":"not-a-uuid"}`))
		},
		"invalid asset ID array": func(db *gorm.DB, item models.AIJobItem) {
			db.Model(&models.AIJobItem{}).Where("id = ?", item.ID).Update("selected_input_asset_ids_json", []byte(`null`))
		},
		"relational template version mismatch": func(db *gorm.DB, item models.AIJobItem) {
			db.Model(&models.AIContentTemplateVersion{}).Where("id = (SELECT ai_content_template_version_id FROM ai_jobs WHERE id = ?)", item.AIJobID).Update("public_id", uuid.NewString())
		},
		"relational slot mismatch": func(db *gorm.DB, item models.AIJobItem) {
			db.Model(&models.AIContentSlot{}).Where("id = ?", item.AIContentSlotID).Update("public_id", uuid.NewString())
		},
		"selected slot missing": func(db *gorm.DB, item models.AIJobItem) {
			var job models.AIJob
			db.First(&job, item.AIJobID)
			var snapshot ProductSnapshotV1
			_ = json.Unmarshal(job.InputSnapshotJSON, &snapshot)
			snapshot.Template.SelectedSlots = []SlotFacts{}
			encoded, _ := json.Marshal(snapshot)
			db.Model(&models.AIJob{}).Where("id = ?", item.AIJobID).Update("input_snapshot_json", encoded)
		},
		"selected slot conflicts": func(db *gorm.DB, item models.AIJobItem) {
			var job models.AIJob
			db.First(&job, item.AIJobID)
			var snapshot ProductSnapshotV1
			_ = json.Unmarshal(job.InputSnapshotJSON, &snapshot)
			snapshot.Template.SelectedSlots[0].Kind = models.AIContentSlotImage
			encoded, _ := json.Marshal(snapshot)
			db.Model(&models.AIJob{}).Where("id = ?", item.AIJobID).Update("input_snapshot_json", encoded)
		},
		"text item has input assets": func(db *gorm.DB, item models.AIJobItem) {
			db.Model(&models.AIJobItem{}).Where("id = ?", item.ID).Update("selected_input_asset_ids_json", []byte(`["aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"]`))
		},
	}
	for name, corrupt := range tests {
		t.Run(name, func(t *testing.T) {
			db, _, items := seedQueueItems(t, 1)
			corrupt(db, items[0])
			now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
			leased, err := NewQueue(db).LeaseNext(t.Context(), "worker-a", now, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			err = newDryRunExecutorWithClock(db, fixedClock{now: now.Add(time.Second)}).Execute(t.Context(), *leased)
			if !errors.Is(err, ErrExecutionInputInvalid) {
				t.Fatalf("execute error = %v, want invalid execution input", err)
			}
			var executions, audits int64
			db.Model(&models.AIExecution{}).Count(&executions)
			db.Model(&models.AIAuditEvent{}).Count(&audits)
			if executions != 0 || audits != 0 {
				t.Fatalf("writes after corrupt provenance: executions=%d audits=%d", executions, audits)
			}
		})
	}
}

func TestDryRunExecutorRejectsImageAssetMissingFromSnapshot(t *testing.T) {
	db, _, _ := seedQueueItems(t, 2)
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	queue := newQueueWithClock(db, fixedClock{now: now.Add(10 * time.Second)})
	textItem, err := queue.LeaseNext(t.Context(), "worker-a", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Complete(t.Context(), *textItem); err != nil {
		t.Fatal(err)
	}
	imageItem, err := queue.LeaseNext(t.Context(), "worker-a", now, time.Minute)
	if err != nil || imageItem == nil || imageItem.Kind != models.AIContentSlotImage {
		t.Fatalf("image lease = %#v, %v", imageItem, err)
	}
	if err := db.Model(&models.AIJobItem{}).Where("public_id = ?", imageItem.PublicID).Update("selected_input_asset_ids_json", []byte(`["bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"]`)).Error; err != nil {
		t.Fatal(err)
	}
	err = newDryRunExecutorWithClock(db, fixedClock{now: now.Add(20 * time.Second)}).Execute(t.Context(), *imageItem)
	if !errors.Is(err, ErrExecutionInputInvalid) {
		t.Fatalf("execute error = %v, want invalid execution input", err)
	}
	var executions, audits int64
	db.Model(&models.AIExecution{}).Count(&executions)
	db.Model(&models.AIAuditEvent{}).Count(&audits)
	if executions != 0 || audits != 0 {
		t.Fatalf("writes after missing asset provenance: executions=%d audits=%d", executions, audits)
	}
}

func TestWorkerStoresFixedSafeFailureForCorruptProvenance(t *testing.T) {
	db, job, items := seedQueueItems(t, 1)
	if err := db.Model(&models.AIJobItem{}).Where("id = ?", items[0].ID).Update("slot_snapshot_json", []byte(`{"public_id":"bad"}`)).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	clock := fixedClock{now: now}
	worker := NewWorker(NewQueue(db), "worker-a", time.Minute, clock, newDryRunExecutorWithClock(db, clock))
	worked, err := worker.RunOnce(t.Context())
	if err != nil || !worked {
		t.Fatalf("run = %v, %v", worked, err)
	}
	assertJobStatus(t, db, job.ID, models.AIJobFailed)
	var stored models.AIJobItem
	db.First(&stored, items[0].ID)
	if stored.SafeError != "invalid slot UUID" || stored.FailureCode != "invalid_input" || stored.InternalError != "" {
		t.Fatalf("stored errors = safe %q internal %q", stored.SafeError, stored.InternalError)
	}
}
