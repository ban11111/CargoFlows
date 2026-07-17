package ai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"cargoflow/api/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrExecutionInputInvalid = errors.New("AI execution input invalid")

const defaultSafeExecutionError = "AI task execution failed"

type heartbeatTicker interface {
	C() <-chan time.Time
	Stop()
}

type systemHeartbeatTicker struct{ ticker *time.Ticker }

func (t systemHeartbeatTicker) C() <-chan time.Time { return t.ticker.C }
func (t systemHeartbeatTicker) Stop()               { t.ticker.Stop() }

type ItemExecutor interface {
	Execute(context.Context, LeasedItem) error
}

type Clock interface{ Now() time.Time }

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

type Worker struct {
	queue     *Queue
	id        string
	leaseTTL  time.Duration
	clock     Clock
	executor  ItemExecutor
	newTicker func(time.Duration) heartbeatTicker
	heartbeat func(context.Context, LeasedItem, time.Time, time.Duration) error
}

func NewWorker(queue *Queue, id string, leaseTTL time.Duration, clock Clock, executor ItemExecutor) *Worker {
	if clock == nil {
		clock = SystemClock{}
	}
	return &Worker{
		queue: queue, id: id, leaseTTL: leaseTTL, clock: clock, executor: executor,
		newTicker: func(interval time.Duration) heartbeatTicker {
			return systemHeartbeatTicker{ticker: time.NewTicker(interval)}
		},
		heartbeat: queue.Heartbeat,
	}
}

func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	item, err := w.queue.LeaseNext(ctx, w.id, w.clock.Now(), w.leaseTTL)
	if err != nil || item == nil {
		return false, err
	}
	executionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- w.executor.Execute(executionCtx, *item) }()
	ticker := w.newTicker(safeHeartbeatInterval(w.leaseTTL))
	var stopTicker sync.Once
	stop := func() { stopTicker.Do(ticker.Stop) }
	defer stop()
	for {
		select {
		case executionErr := <-result:
			cancel()
			stop()
			if executionErr != nil {
				if errors.Is(executionErr, ErrLeaseLost) {
					return true, ErrLeaseLost
				}
				return true, w.queue.failAt(ctx, *item, defaultSafeExecutionError, w.clock.Now())
			}
			return true, w.queue.completeAt(ctx, *item, w.clock.Now())
		case <-ticker.C():
			heartbeatErr := w.heartbeat(ctx, *item, w.clock.Now(), w.leaseTTL)
			if heartbeatErr == nil {
				continue
			}
			cancel()
			<-result
			stop()
			return true, heartbeatErr
		case <-ctx.Done():
			cancel()
			<-result
			stop()
			return true, ctx.Err()
		}
	}
}

func safeHeartbeatInterval(ttl time.Duration) time.Duration {
	interval := ttl / 3
	if interval <= 0 {
		return time.Nanosecond
	}
	return interval
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
		var job models.AIJob
		if err := tx.First(&job, item.AIJobID).Error; err != nil {
			return fmt.Errorf("load dry-run job: %w", err)
		}
		var version models.AIContentTemplateVersion
		if err := tx.Select("id", "public_id").First(&version, job.AIContentTemplateVersionID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return invalidExecutionInput("template version binding is missing")
			}
			return fmt.Errorf("load dry-run template version: %w", err)
		}
		var relationalSlot models.AIContentSlot
		if err := tx.Select("id", "public_id", "ai_content_template_version_id", "slot_key", "kind").First(&relationalSlot, item.AIContentSlotID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return invalidExecutionInput("content slot binding is missing")
			}
			return fmt.Errorf("load dry-run content slot: %w", err)
		}
		provenance, err := validateDryRunProvenance(job, item, version, relationalSlot)
		if err != nil {
			return err
		}
		var existing models.AIExecution
		err = tx.Where("ai_job_item_id = ? AND attempt_number = ? AND model = ?", item.ID, item.AttemptCount, "dry-run").First(&existing).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("find dry-run execution: %w", err)
		}
		hash := sha256.Sum256(nil)
		execution := models.AIExecution{
			PublicID: uuid.NewString(), AIJobItemID: item.ID, Operation: provenance.operation, Status: models.AIExecutionCompleted,
			AttemptNumber: item.AttemptCount, L0PolicyVersion: "phase-1-dry-run", L1ProductContextVersion: job.SnapshotSchema,
			L2TemplateVersionPublicID: provenance.templateVersionID, L3ContentSlotPublicID: provenance.slotPublicID,
			NormalizedInputJSON: job.InputSnapshotJSON, OrderedInputListJSON: item.SelectedInputAssetIDsJSON,
			CompiledPromptSHA256: hex.EncodeToString(hash[:]), Model: "dry-run", RequestConfigJSON: []byte(`{"dry_run":true}`),
			WorkerID: leased.LeaseOwner, StartedAt: &now, CompletedAt: &now,
		}
		if err := tx.Create(&execution).Error; err != nil {
			return fmt.Errorf("create dry-run execution: %w", err)
		}
		metadata, err := json.Marshal(map[string]any{"operation": provenance.operation, "status": models.AIExecutionCompleted, "usage": map[string]int64{"input_text_tokens": 0, "input_image_tokens": 0, "output_text_tokens": 0, "output_image_tokens": 0}})
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

type dryRunProvenance struct {
	operation         models.AIExecutionOperation
	templateVersionID string
	slotPublicID      string
}

func validateDryRunProvenance(job models.AIJob, item models.AIJobItem, version models.AIContentTemplateVersion, relationalSlot models.AIContentSlot) (dryRunProvenance, error) {
	operation := models.AIExecutionTextGenerate
	switch item.Kind {
	case models.AIContentSlotImage:
		operation = models.AIExecutionGenerate
	case models.AIContentSlotTitle, models.AIContentSlotSEODescription:
		operation = models.AIExecutionTextGenerate
	default:
		return dryRunProvenance{}, invalidExecutionInput("unsupported slot kind")
	}
	if job.SnapshotSchema != ProductSnapshotSchemaV1 {
		return dryRunProvenance{}, invalidExecutionInput("unsupported job snapshot schema")
	}
	var snapshot ProductSnapshotV1
	if err := decodeStrictJSON(job.InputSnapshotJSON, &snapshot); err != nil {
		return dryRunProvenance{}, invalidExecutionInput("malformed job snapshot")
	}
	if snapshot.Schema != ProductSnapshotSchemaV1 {
		return dryRunProvenance{}, invalidExecutionInput("job snapshot schema mismatch")
	}
	templateVersionID, err := uuid.Parse(snapshot.Template.VersionPublicID)
	if err != nil || templateVersionID == uuid.Nil {
		return dryRunProvenance{}, invalidExecutionInput("invalid template version UUID")
	}
	relationalVersionID, err := uuid.Parse(version.PublicID)
	if err != nil || relationalVersionID == uuid.Nil || templateVersionID != relationalVersionID || version.ID != job.AIContentTemplateVersionID {
		return dryRunProvenance{}, invalidExecutionInput("template version binding mismatch")
	}
	var slot SlotFacts
	if err := decodeStrictJSON(item.SlotSnapshotJSON, &slot); err != nil {
		return dryRunProvenance{}, invalidExecutionInput("malformed slot snapshot")
	}
	slotPublicID, err := uuid.Parse(slot.PublicID)
	if err != nil || slotPublicID == uuid.Nil {
		return dryRunProvenance{}, invalidExecutionInput("invalid slot UUID")
	}
	relationalSlotID, err := uuid.Parse(relationalSlot.PublicID)
	if err != nil || relationalSlotID == uuid.Nil || slotPublicID != relationalSlotID || relationalSlot.AIContentTemplateVersionID != version.ID || relationalSlot.ID != item.AIContentSlotID {
		return dryRunProvenance{}, invalidExecutionInput("content slot binding mismatch")
	}
	if slot.SlotKey != item.SlotKey || slot.SlotKey != relationalSlot.SlotKey || slot.Kind != item.Kind || slot.Kind != relationalSlot.Kind {
		return dryRunProvenance{}, invalidExecutionInput("slot snapshot mismatch")
	}
	matchingSelectedSlots := 0
	for _, selectedSlot := range snapshot.Template.SelectedSlots {
		if selectedSlot.PublicID != relationalSlot.PublicID && selectedSlot.SlotKey != relationalSlot.SlotKey {
			continue
		}
		if selectedSlot.PublicID != relationalSlot.PublicID || selectedSlot.SlotKey != relationalSlot.SlotKey || selectedSlot.Kind != relationalSlot.Kind {
			return dryRunProvenance{}, invalidExecutionInput("selected slot provenance mismatch")
		}
		matchingSelectedSlots++
	}
	if matchingSelectedSlots != 1 {
		return dryRunProvenance{}, invalidExecutionInput("selected slot provenance is missing or duplicated")
	}
	var assetIDs []uint
	if err := decodeStrictJSON(item.SelectedInputAssetIDsJSON, &assetIDs); err != nil || assetIDs == nil {
		return dryRunProvenance{}, invalidExecutionInput("invalid selected asset ID array")
	}
	seen := make(map[uint]struct{}, len(assetIDs))
	for _, id := range assetIDs {
		if id == 0 {
			return dryRunProvenance{}, invalidExecutionInput("invalid selected asset ID")
		}
		if _, duplicate := seen[id]; duplicate {
			return dryRunProvenance{}, invalidExecutionInput("duplicate selected asset ID")
		}
		seen[id] = struct{}{}
	}
	if (item.Kind == models.AIContentSlotTitle || item.Kind == models.AIContentSlotSEODescription) && len(assetIDs) != 0 {
		return dryRunProvenance{}, invalidExecutionInput("text item must not have selected assets")
	}
	if item.Kind == models.AIContentSlotImage && len(assetIDs) == 0 {
		return dryRunProvenance{}, invalidExecutionInput("image item requires selected assets")
	}
	snapshotAssetIDs := make(map[uint]struct{}, len(snapshot.SelectedAssets))
	for _, asset := range snapshot.SelectedAssets {
		if asset.ID == 0 {
			return dryRunProvenance{}, invalidExecutionInput("invalid snapshot asset ID")
		}
		if _, duplicate := snapshotAssetIDs[asset.ID]; duplicate {
			return dryRunProvenance{}, invalidExecutionInput("duplicate snapshot asset ID")
		}
		snapshotAssetIDs[asset.ID] = struct{}{}
	}
	for _, id := range assetIDs {
		if _, exists := snapshotAssetIDs[id]; !exists {
			return dryRunProvenance{}, invalidExecutionInput("selected asset is absent from job snapshot")
		}
	}
	return dryRunProvenance{operation: operation, templateVersionID: templateVersionID.String(), slotPublicID: slotPublicID.String()}, nil
}

func invalidExecutionInput(reason string) error {
	return fmt.Errorf("%w: %s", ErrExecutionInputInvalid, reason)
}

func decodeStrictJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}
