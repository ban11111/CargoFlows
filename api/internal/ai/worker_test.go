package ai

import (
	"context"
	"errors"
	"testing"
	"time"

	"cargoflow/api/internal/models"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type executorFunc func(LeasedItem) error

func (f executorFunc) Execute(_ context.Context, item LeasedItem) error { return f(item) }

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
