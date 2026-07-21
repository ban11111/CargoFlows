package ai

import (
	"context"
	"errors"

	"cargoflows/api/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	DefaultMaxWorkersPerJob = 3
	DefaultMaxWorkersGlobal = 9
	MaxWorkerLimit          = 32
	workerSettingID         = 1
)

var ErrWorkerSettingInvalid = errors.New("worker concurrency setting is invalid")

type WorkerConcurrency struct {
	MaxWorkersPerJob int `json:"max_workers_per_job"`
	MaxWorkersGlobal int `json:"max_workers_global"`
}

type WorkerSettingsService struct{ db *gorm.DB }

func NewWorkerSettingsService(db *gorm.DB) *WorkerSettingsService {
	return &WorkerSettingsService{db: db}
}

func (s *WorkerSettingsService) Get(ctx context.Context) (WorkerConcurrency, error) {
	var row models.AIWorkerSetting
	err := s.db.WithContext(ctx).First(&row, workerSettingID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return WorkerConcurrency{MaxWorkersPerJob: DefaultMaxWorkersPerJob, MaxWorkersGlobal: DefaultMaxWorkersGlobal}, nil
	}
	if err != nil {
		return WorkerConcurrency{}, err
	}
	return workerConcurrency(row), nil
}

func (s *WorkerSettingsService) Update(ctx context.Context, actorID uint, value WorkerConcurrency) (WorkerConcurrency, error) {
	if err := validateWorkerConcurrency(value); err != nil {
		return WorkerConcurrency{}, err
	}
	row := models.AIWorkerSetting{ID: workerSettingID, MaxWorkersPerJob: DefaultMaxWorkersPerJob, MaxWorkersGlobal: DefaultMaxWorkersGlobal}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
		query := tx
		if tx.Dialector.Name() == "mysql" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(&row, workerSettingID).Error; err != nil {
			return err
		}
		if err := tx.Model(&row).Updates(map[string]any{
			"max_workers_per_job": value.MaxWorkersPerJob,
			"max_workers_global":  value.MaxWorkersGlobal,
			"updated_by_id":       actorID,
		}).Error; err != nil {
			return err
		}
		return tx.First(&row, workerSettingID).Error
	})
	if err != nil {
		return WorkerConcurrency{}, err
	}
	return workerConcurrency(row), nil
}

func validateWorkerConcurrency(value WorkerConcurrency) error {
	if value.MaxWorkersPerJob < 1 || value.MaxWorkersPerJob > MaxWorkerLimit || value.MaxWorkersGlobal < 1 || value.MaxWorkersGlobal > MaxWorkerLimit || value.MaxWorkersPerJob > value.MaxWorkersGlobal {
		return ErrWorkerSettingInvalid
	}
	return nil
}

func workerConcurrency(row models.AIWorkerSetting) WorkerConcurrency {
	value := WorkerConcurrency{MaxWorkersPerJob: row.MaxWorkersPerJob, MaxWorkersGlobal: row.MaxWorkersGlobal}
	if validateWorkerConcurrency(value) != nil {
		return WorkerConcurrency{MaxWorkersPerJob: DefaultMaxWorkersPerJob, MaxWorkersGlobal: DefaultMaxWorkersGlobal}
	}
	return value
}
