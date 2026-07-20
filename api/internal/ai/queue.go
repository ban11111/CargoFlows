package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cargoflows/api/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrLeaseLost       = errors.New("AI job item lease lost")
	ErrInvalidLease    = errors.New("invalid AI job item lease")
	ErrInvalidWorkerID = errors.New("worker ID is required")
)

const sqliteLeaseAttempts = 20

type LeasedItem struct {
	PublicID    string
	JobPublicID string
	SlotKey     string
	Kind        models.AIContentSlotKind
	LeaseOwner  string
	Attempt     int

	itemID uint
	jobID  uint
}

type Queue struct {
	db             *gorm.DB
	clock          Clock
	runTransaction func(context.Context, func(*gorm.DB) error) error
}

func NewQueue(db *gorm.DB) *Queue { return newQueueWithClock(db, SystemClock{}) }

func newQueueWithClock(db *gorm.DB, clock Clock) *Queue {
	if clock == nil {
		clock = SystemClock{}
	}
	return &Queue{
		db:    db,
		clock: clock,
		runTransaction: func(ctx context.Context, fn func(*gorm.DB) error) error {
			return db.WithContext(ctx).Transaction(fn)
		},
	}
}

func (q *Queue) LeaseNext(ctx context.Context, workerID string, now time.Time, ttl time.Duration) (*LeasedItem, error) {
	if strings.TrimSpace(workerID) == "" {
		return nil, ErrInvalidWorkerID
	}
	if ttl <= 0 {
		return nil, ErrInvalidLease
	}
	var leased *LeasedItem
	for attempt := 0; attempt < sqliteLeaseAttempts; attempt++ {
		leased = nil
		err := q.runTransaction(ctx, func(tx *gorm.DB) error {
			item, found, err := selectLeaseCandidate(tx, now)
			if err != nil || !found {
				return err
			}
			expiresAt := now.Add(ttl)
			updates := map[string]any{
				"status":           models.AIJobItemRunning,
				"lease_owner":      workerID,
				"lease_expires_at": expiresAt,
				"attempt_count":    gorm.Expr("attempt_count + 1"),
				"started_at":       gorm.Expr("COALESCE(started_at, ?)", now),
				"completed_at":     nil,
				"safe_error":       "",
				"internal_error":   "",
			}
			result := tx.Model(&models.AIJobItem{}).
				Where("id = ? AND (status = ? OR (status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?))", item.ID, models.AIJobItemQueued, models.AIJobItemRunning, now).
				Updates(updates)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errLeaseContended
			}
			if err := aggregateJob(tx, item.AIJobID, now); err != nil {
				return err
			}
			var job models.AIJob
			if err := tx.Select("id", "public_id").First(&job, item.AIJobID).Error; err != nil {
				return fmt.Errorf("load leased AI job: %w", err)
			}
			leased = &LeasedItem{PublicID: item.PublicID, JobPublicID: job.PublicID, SlotKey: item.SlotKey, Kind: item.Kind, LeaseOwner: workerID, Attempt: item.AttemptCount + 1, itemID: item.ID, jobID: item.AIJobID}
			return nil
		})
		if err == nil {
			return leased, nil
		}
		if !errors.Is(err, errLeaseContended) && !isSQLiteBusy(err) {
			return nil, fmt.Errorf("lease AI job item: %w", err)
		}
	}
	return nil, fmt.Errorf("lease AI job item: %w", errLeaseContended)
}

var errLeaseContended = errors.New("AI job item lease contention")

func selectLeaseCandidate(tx *gorm.DB, now time.Time) (models.AIJobItem, bool, error) {
	query := tx.Where("status = ? OR (status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?)", models.AIJobItemQueued, models.AIJobItemRunning, now).
		Order("created_at ASC, id ASC")
	if tx.Dialector.Name() == "mysql" {
		query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
	}
	var item models.AIJobItem
	err := query.First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.AIJobItem{}, false, nil
	}
	if err != nil {
		return models.AIJobItem{}, false, err
	}
	return item, true, nil
}

func (q *Queue) Heartbeat(ctx context.Context, item LeasedItem, now time.Time, ttl time.Duration) error {
	if ttl <= 0 || item.itemID == 0 || item.LeaseOwner == "" {
		return ErrInvalidLease
	}
	result := q.db.WithContext(ctx).Model(&models.AIJobItem{}).
		Where("id = ? AND status = ? AND lease_owner = ? AND attempt_count = ? AND lease_expires_at > ?", item.itemID, models.AIJobItemRunning, item.LeaseOwner, item.Attempt, now).
		Update("lease_expires_at", now.Add(ttl))
	if result.Error != nil {
		return fmt.Errorf("heartbeat AI job item: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (q *Queue) Complete(ctx context.Context, item LeasedItem) error {
	return q.completeAt(ctx, item, q.clock.Now())
}

func (q *Queue) Fail(ctx context.Context, item LeasedItem, safeError string) error {
	return q.failAt(ctx, item, safeError, q.clock.Now())
}

func (q *Queue) completeAt(ctx context.Context, item LeasedItem, now time.Time) error {
	return q.finish(ctx, item, models.AIJobItemCompleted, "", now)
}

func (q *Queue) failAt(ctx context.Context, item LeasedItem, safeError string, now time.Time) error {
	return q.finish(ctx, item, models.AIJobItemFailed, safeError, now)
}

func (q *Queue) finish(ctx context.Context, leased LeasedItem, status models.AIJobItemStatus, safeError string, now time.Time) error {
	if leased.itemID == 0 || leased.LeaseOwner == "" {
		return ErrInvalidLease
	}
	return q.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if status == models.AIJobItemFailed {
			var latest models.AIExecution
			executionQuery := tx.Where("ai_job_item_id = ?", leased.itemID).Order("attempt_number DESC, id DESC")
			if tx.Dialector.Name() == "mysql" {
				executionQuery = executionQuery.Clauses(clause.Locking{Strength: "UPDATE"})
			}
			err := executionQuery.First(&latest).Error
			if err == nil && latest.Status == models.AIExecutionCompleted {
				status, safeError = models.AIJobItemCompleted, ""
			} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("load latest AI execution before failure: %w", err)
			}
		}
		var item models.AIJobItem
		query := tx
		if tx.Dialector.Name() == "mysql" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(&item, leased.itemID).Error; err != nil {
			return fmt.Errorf("load AI job item transition: %w", err)
		}
		if item.Status == status && item.AttemptCount == leased.Attempt {
			return nil
		}
		if item.Status != models.AIJobItemRunning || item.LeaseOwner != leased.LeaseOwner || item.AttemptCount != leased.Attempt || item.LeaseExpiresAt == nil || !item.LeaseExpiresAt.After(now) {
			return ErrLeaseLost
		}
		updates := map[string]any{"status": status, "lease_owner": "", "lease_expires_at": nil, "completed_at": now, "safe_error": safeError, "internal_error": ""}
		result := tx.Model(&models.AIJobItem{}).
			Where("id = ? AND status = ? AND lease_owner = ? AND attempt_count = ? AND lease_expires_at > ?", item.ID, models.AIJobItemRunning, leased.LeaseOwner, leased.Attempt, now).
			Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("transition AI job item: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrLeaseLost
		}
		return aggregateJob(tx, item.AIJobID, now)
	})
}

func aggregateJob(tx *gorm.DB, jobID uint, now time.Time) error {
	// Serialize projections for a parent job. Item rows are transitioned first,
	// then every worker takes the same parent lock before reading the aggregate;
	// this avoids a late transaction overwriting a terminal projection with a
	// stale running snapshot when sibling items finish concurrently in MySQL.
	if tx.Dialector.Name() == "mysql" {
		var job models.AIJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&job, jobID).Error; err != nil {
			return fmt.Errorf("lock AI job aggregate: %w", err)
		}
	}
	var counts []struct {
		Status models.AIJobItemStatus
		Count  int64
	}
	if err := tx.Model(&models.AIJobItem{}).Select("status, COUNT(*) AS count").Where("ai_job_id = ?", jobID).Group("status").Scan(&counts).Error; err != nil {
		return fmt.Errorf("aggregate AI job items: %w", err)
	}
	byStatus := make(map[models.AIJobItemStatus]int64, len(counts))
	var total int64
	for _, count := range counts {
		byStatus[count.Status] = count.Count
		total += count.Count
	}
	if total == 0 {
		return errors.New("AI job has no items")
	}
	queued, running := byStatus[models.AIJobItemQueued], byStatus[models.AIJobItemRunning]
	completed, failed, cancelled := byStatus[models.AIJobItemCompleted], byStatus[models.AIJobItemFailed], byStatus[models.AIJobItemCancelled]
	status := models.AIJobRunning
	var completedAt any
	switch {
	case queued == total:
		status = models.AIJobQueued
	case queued > 0 || running > 0:
		status = models.AIJobRunning
	case completed == total:
		status, completedAt = models.AIJobCompleted, now
	case failed == total:
		status, completedAt = models.AIJobFailed, now
	case cancelled == total:
		status, completedAt = models.AIJobCancelled, now
	case completed > 0 && failed+cancelled > 0:
		status, completedAt = models.AIJobPartial, now
	default:
		status, completedAt = models.AIJobFailed, now
	}
	updates := map[string]any{"status": status, "completed_at": completedAt}
	if status != models.AIJobQueued {
		updates["started_at"] = gorm.Expr("COALESCE(started_at, ?)", now)
	}
	if err := tx.Model(&models.AIJob{}).Where("id = ?", jobID).Updates(updates).Error; err != nil {
		return fmt.Errorf("update AI job aggregate: %w", err)
	}
	return nil
}

func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") || strings.Contains(message, "database table is locked")
}
