package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"cargoflow/api/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const defaultSafeExecutionError = "AI task execution failed"

type ItemExecutor interface {
	Execute(context.Context, LeasedItem) error
}

type Clock interface{ Now() time.Time }

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

type Worker struct {
	queue    *Queue
	id       string
	leaseTTL time.Duration
	clock    Clock
	executor ItemExecutor
}

func NewWorker(queue *Queue, id string, leaseTTL time.Duration, clock Clock, executor ItemExecutor) *Worker {
	if clock == nil {
		clock = SystemClock{}
	}
	return &Worker{queue: queue, id: id, leaseTTL: leaseTTL, clock: clock, executor: executor}
}

func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	item, err := w.queue.LeaseNext(ctx, w.id, w.clock.Now(), w.leaseTTL)
	if err != nil || item == nil {
		return false, err
	}
	if err := w.executor.Execute(ctx, *item); err != nil {
		if errors.Is(err, ErrLeaseLost) {
			return true, ErrLeaseLost
		}
		return true, w.queue.failAt(ctx, *item, defaultSafeExecutionError, w.clock.Now())
	}
	return true, w.queue.completeAt(ctx, *item, w.clock.Now())
}

type DryRunExecutor struct {
	db    *gorm.DB
	clock Clock
}

func NewDryRunExecutor(db *gorm.DB) *DryRunExecutor {
	return newDryRunExecutorWithClock(db, SystemClock{})
}

func newDryRunExecutorWithClock(db *gorm.DB, clock Clock) *DryRunExecutor {
	if clock == nil {
		clock = SystemClock{}
	}
	return &DryRunExecutor{db: db, clock: clock}
}

func (e *DryRunExecutor) Execute(ctx context.Context, leased LeasedItem) error {
	if leased.itemID == 0 || leased.LeaseOwner == "" {
		return ErrInvalidLease
	}
	return e.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := e.clock.Now()
		query := tx
		if tx.Dialector.Name() == "mysql" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		var item models.AIJobItem
		if err := query.First(&item, leased.itemID).Error; err != nil {
			return fmt.Errorf("load dry-run item: %w", err)
		}
		if item.Status != models.AIJobItemRunning || item.LeaseOwner != leased.LeaseOwner || item.AttemptCount != leased.Attempt || item.LeaseExpiresAt == nil || !item.LeaseExpiresAt.After(now) {
			return ErrLeaseLost
		}
		var existing models.AIExecution
		err := tx.Where("ai_job_item_id = ? AND attempt_number = ? AND model = ?", item.ID, item.AttemptCount, "dry-run").First(&existing).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("find dry-run execution: %w", err)
		}
		var job models.AIJob
		if err := tx.First(&job, item.AIJobID).Error; err != nil {
			return fmt.Errorf("load dry-run job: %w", err)
		}
		operation := models.AIExecutionTextGenerate
		if item.Kind == models.AIContentSlotImage {
			operation = models.AIExecutionGenerate
		}
		var slot struct {
			PublicID string `json:"public_id"`
		}
		_ = json.Unmarshal(item.SlotSnapshotJSON, &slot)
		if slot.PublicID == "" {
			slot.PublicID = "dry-run"
		}
		var snapshot ProductSnapshotV1
		_ = json.Unmarshal(job.InputSnapshotJSON, &snapshot)
		templateVersionID := snapshot.Template.VersionPublicID
		if templateVersionID == "" {
			templateVersionID = "dry-run"
		}
		hash := sha256.Sum256(nil)
		execution := models.AIExecution{
			PublicID: uuid.NewString(), AIJobItemID: item.ID, Operation: operation, Status: models.AIExecutionCompleted,
			AttemptNumber: item.AttemptCount, L0PolicyVersion: "phase-1-dry-run", L1ProductContextVersion: job.SnapshotSchema,
			L2TemplateVersionPublicID: templateVersionID, L3ContentSlotPublicID: slot.PublicID,
			NormalizedInputJSON: job.InputSnapshotJSON, OrderedInputListJSON: item.SelectedInputAssetIDsJSON,
			CompiledPromptSHA256: hex.EncodeToString(hash[:]), Model: "dry-run", RequestConfigJSON: []byte(`{"dry_run":true}`),
			WorkerID: leased.LeaseOwner, StartedAt: &now, CompletedAt: &now,
		}
		if err := tx.Create(&execution).Error; err != nil {
			return fmt.Errorf("create dry-run execution: %w", err)
		}
		metadata, err := json.Marshal(map[string]any{"operation": operation, "status": models.AIExecutionCompleted, "usage": map[string]int64{"input_text_tokens": 0, "input_image_tokens": 0, "output_text_tokens": 0, "output_image_tokens": 0}})
		if err != nil {
			return fmt.Errorf("marshal dry-run audit: %w", err)
		}
		jobID, itemID, executionID := job.ID, item.ID, execution.ID
		audit := models.AIAuditEvent{PublicID: uuid.NewString(), EventType: "ai_execution.dry_run_completed", EntityType: "ai_execution", EntityPublicID: execution.PublicID, AIJobID: &jobID, AIJobItemID: &itemID, AIExecutionID: &executionID, MetadataJSON: metadata}
		if err := tx.Create(&audit).Error; err != nil {
			return fmt.Errorf("audit dry-run execution: %w", err)
		}
		return nil
	})
}
