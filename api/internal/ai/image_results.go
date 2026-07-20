package ai

import (
	"context"
	"errors"
	"time"

	"cargoflows/api/internal/models"
	"gorm.io/gorm"
)

var ErrImageResultNotFound = errors.New("AI image result not found")

// ImageResultDocument is deliberately limited to display metadata. Object keys,
// provider IDs, and prompts remain server-only.
type ImageResultDocument struct {
	PublicID        string    `json:"public_id"`
	JobItemPublicID string    `json:"job_item_id"`
	CandidateIndex  int       `json:"candidate_index"`
	MIMEType        string    `json:"mime_type"`
	Width           int       `json:"width"`
	Height          int       `json:"height"`
	ByteCount       int64     `json:"byte_count"`
	MediaURL        string    `json:"media_url"`
	CreatedAt       time.Time `json:"created_at"`
}

type ImageResultService struct{ db *gorm.DB }

func NewImageResultService(db *gorm.DB) *ImageResultService { return &ImageResultService{db: db} }

func (service *ImageResultService) List(ctx context.Context, jobPublicID string) ([]ImageResultDocument, error) {
	if _, err := service.jobID(ctx, jobPublicID); err != nil {
		return nil, err
	}
	var rows []imageResultRow
	err := service.db.WithContext(ctx).
		Table("ai_image_results AS result").
		Select("result.*, item.public_id AS job_item_public_id").
		Joins("JOIN ai_executions AS execution ON execution.id = result.ai_execution_id").
		Joins("JOIN ai_job_items AS item ON item.id = execution.ai_job_item_id").
		Joins("JOIN ai_jobs AS job ON job.id = item.ai_job_id").
		Where("job.public_id = ?", jobPublicID).
		Order("item.id ASC, result.candidate_index ASC, result.id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	values := make([]ImageResultDocument, 0, len(rows))
	for _, row := range rows {
		values = append(values, imageResultDocument(row))
	}
	return values, nil
}

func (service *ImageResultService) GetForJob(ctx context.Context, jobPublicID, resultPublicID string) (models.AIImageResult, error) {
	var result models.AIImageResult
	err := service.db.WithContext(ctx).
		Table("ai_image_results AS result").
		Select("result.*").
		Joins("JOIN ai_executions AS execution ON execution.id = result.ai_execution_id").
		Joins("JOIN ai_job_items AS item ON item.id = execution.ai_job_item_id").
		Joins("JOIN ai_jobs AS job ON job.id = item.ai_job_id").
		Where("job.public_id = ? AND result.public_id = ?", jobPublicID, resultPublicID).
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.AIImageResult{}, ErrImageResultNotFound
	}
	return result, err
}

type imageResultRow struct {
	models.AIImageResult
	JobItemPublicID string `gorm:"column:job_item_public_id"`
}

func (service *ImageResultService) jobID(ctx context.Context, publicID string) (uint, error) {
	var job models.AIJob
	err := service.db.WithContext(ctx).Select("id").Where("public_id = ?", publicID).First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, ErrJobNotFound
	}
	return job.ID, err
}

func imageResultDocument(row imageResultRow) ImageResultDocument {
	return ImageResultDocument{PublicID: row.PublicID, JobItemPublicID: row.JobItemPublicID, CandidateIndex: row.CandidateIndex, MIMEType: row.MIMEType, Width: row.Width, Height: row.Height, ByteCount: row.ByteCount, MediaURL: "/api/v1/ai-jobs/{job_id}/image-results/" + row.PublicID + "/media", CreatedAt: row.CreatedAt}
}
