package ai

import (
	"context"
	"fmt"
	"time"

	"cargoflows/api/internal/models"
	"gorm.io/gorm"
)

const productionCutoverMessage = "Cancelled during production cutover."
const productionCutoverFailureCode = "production_cutover"

type CutoverCancellationResult struct {
	Jobs       int64
	Items      int64
	Executions int64
	ImageTurns int64
}

// CancelUnfinishedForProduction is an idempotent, one-time cutover operation.
// It preserves completed and failed work while making every remaining unit
// terminal before the production worker is allowed to start.
func CancelUnfinishedForProduction(ctx context.Context, db *gorm.DB, now time.Time) (CutoverCancellationResult, error) {
	var result CutoverCancellationResult
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var jobIDs []uint
		if err := tx.Model(&models.AIJobItem{}).
			Where("status IN ?", []models.AIJobItemStatus{models.AIJobItemQueued, models.AIJobItemRunning}).
			Distinct("ai_job_id").Pluck("ai_job_id", &jobIDs).Error; err != nil {
			return fmt.Errorf("list unfinished AI jobs: %w", err)
		}

		executionUpdate := tx.Model(&models.AIExecution{}).
			Where("status NOT IN ?", []models.AIExecutionStatus{models.AIExecutionCompleted, models.AIExecutionFailed, models.AIExecutionCancelled}).
			Updates(map[string]any{
				"status":           models.AIExecutionCancelled,
				"worker_id":        "",
				"lease_expires_at": nil,
				"safe_error":       productionCutoverMessage,
				"failure_code":     productionCutoverFailureCode,
				"internal_error":   "",
				"completed_at":     now,
			})
		if executionUpdate.Error != nil {
			return fmt.Errorf("cancel unfinished AI executions: %w", executionUpdate.Error)
		}
		result.Executions = executionUpdate.RowsAffected

		turnUpdate := tx.Model(&models.AIImageTurn{}).
			Where("status NOT IN ?", []models.AIImageTurnStatus{models.AIImageTurnCompleted, models.AIImageTurnFailed, models.AIImageTurnCancelled}).
			Updates(map[string]any{
				"status":           models.AIImageTurnCancelled,
				"lease_owner":      "",
				"lease_expires_at": nil,
				"safe_error":       productionCutoverMessage,
				"internal_error":   "",
				"completed_at":     now,
			})
		if turnUpdate.Error != nil {
			return fmt.Errorf("cancel unfinished AI image turns: %w", turnUpdate.Error)
		}
		result.ImageTurns = turnUpdate.RowsAffected

		itemUpdate := tx.Model(&models.AIJobItem{}).
			Where("status IN ?", []models.AIJobItemStatus{models.AIJobItemQueued, models.AIJobItemRunning}).
			Updates(map[string]any{
				"status":           models.AIJobItemCancelled,
				"lease_owner":      "",
				"lease_expires_at": nil,
				"safe_error":       productionCutoverMessage,
				"failure_code":     productionCutoverFailureCode,
				"internal_error":   "",
				"completed_at":     now,
			})
		if itemUpdate.Error != nil {
			return fmt.Errorf("cancel unfinished AI job items: %w", itemUpdate.Error)
		}
		result.Items = itemUpdate.RowsAffected

		for _, jobID := range jobIDs {
			if err := aggregateJob(tx, jobID, now); err != nil {
				return fmt.Errorf("aggregate cancelled AI job %d: %w", jobID, err)
			}
		}
		result.Jobs = int64(len(jobIDs))
		return nil
	})
	return result, err
}
