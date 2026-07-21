package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"cargoflows/api/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrImageThreadNotFound    = errors.New("AI image thread not found")
	ErrImageTurnConflict      = errors.New("an image revision is already active")
	ErrImageTurnInvalid       = errors.New("image revision request is invalid")
	ErrImageResultNotSelected = errors.New("image result is not the selected version")
)

type ImageTurnActor struct {
	PublicID string `json:"public_id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
}
type ImageTurnMask struct {
	ObjectKey, SHA256 string
	Width, Height     int
	ByteCount         int64
}
type CreateImageTurnInput struct {
	JobPublicID, ItemPublicID, Operation, ParentResultPublicID, UserInstruction string
	ActorID                                                                     uint
	Mask                                                                        *ImageTurnMask
}
type ImageTurnResultDocument struct {
	PublicID       string    `json:"public_id"`
	CandidateIndex int       `json:"candidate_index"`
	ParentResultID string    `json:"parent_result_id,omitempty"`
	MediaURL       string    `json:"media_url"`
	Selected       bool      `json:"selected"`
	CreatedAt      time.Time `json:"created_at"`
}
type ImageTurnDocument struct {
	PublicID          string                      `json:"public_id"`
	Sequence          int                         `json:"sequence"`
	Operation         models.AIExecutionOperation `json:"operation"`
	ParentResultID    string                      `json:"parent_result_id,omitempty"`
	UserInstruction   string                      `json:"user_instruction"`
	MaskPresent       bool                        `json:"mask_present"`
	Status            models.AIImageTurnStatus    `json:"status"`
	Actor             ImageTurnActor              `json:"actor"`
	SafeError         string                      `json:"safe_error"`
	RequestedModel    string                      `json:"requested_model"`
	ActualModel       string                      `json:"actual_model"`
	APIMode           string                      `json:"api_mode"`
	ProviderRequestID string                      `json:"provider_request_id"`
	FailureCode       string                      `json:"failure_code"`
	CreatedAt         time.Time                   `json:"created_at"`
	CompletedAt       *time.Time                  `json:"completed_at"`
	Results           []ImageTurnResultDocument   `json:"results"`
}
type ImageThreadDocument struct {
	PublicID         string              `json:"public_id"`
	JobItemID        string              `json:"job_item_id"`
	SlotKey          string              `json:"slot_key"`
	SelectedResultID string              `json:"selected_result_id,omitempty"`
	Turns            []ImageTurnDocument `json:"turns"`
}

func (service *ImageResultService) ListThreads(ctx context.Context, jobPublicID string) ([]ImageThreadDocument, error) {
	var rows []struct {
		models.AIImageThread
		ItemPublicID string
		SlotKey      string
	}
	err := service.db.WithContext(ctx).Table("ai_image_threads AS thread").Select("thread.*, item.public_id AS item_public_id, item.slot_key").Joins("JOIN ai_job_items AS item ON item.id = thread.ai_job_item_id").Joins("JOIN ai_jobs AS job ON job.id = item.ai_job_id").Where("job.public_id = ?", jobPublicID).Order("item.id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make([]ImageThreadDocument, 0, len(rows))
	for _, row := range rows {
		var turns []models.AIImageTurn
		if err := service.db.WithContext(ctx).Where("ai_image_thread_id = ?", row.ID).Order("sequence").Find(&turns).Error; err != nil {
			return nil, err
		}
		doc := ImageThreadDocument{PublicID: row.PublicID, JobItemID: row.ItemPublicID, SlotKey: row.SlotKey, Turns: []ImageTurnDocument{}}
		for _, turn := range turns {
			actor := ImageTurnActor{}
			_ = json.Unmarshal(turn.ActorSnapshotJSON, &actor)
			turnDoc := ImageTurnDocument{PublicID: turn.PublicID, Sequence: turn.Sequence, Operation: turn.Operation, UserInstruction: turn.UserInstruction, MaskPresent: turn.MaskObjectKey != "", Status: turn.Status, Actor: actor, SafeError: turn.SafeError, CreatedAt: turn.CreatedAt, CompletedAt: turn.CompletedAt, Results: []ImageTurnResultDocument{}}
			var execution models.AIExecution
			if service.db.WithContext(ctx).Where("ai_image_turn_id = ?", turn.ID).Order("attempt_number DESC").First(&execution).Error == nil {
				turnDoc.RequestedModel, turnDoc.ActualModel, turnDoc.APIMode, turnDoc.ProviderRequestID, turnDoc.FailureCode = execution.RequestedModel, execution.ActualModel, execution.APIMode, execution.OpenAIRequestID, execution.FailureCode
				if turnDoc.ActualModel == "" {
					turnDoc.ActualModel = execution.Model
				}
				if turnDoc.SafeError == "" {
					turnDoc.SafeError = execution.SafeError
				}
			}
			var images []models.AIImageResult
			if err := service.db.WithContext(ctx).Where("ai_image_turn_id = ?", turn.ID).Order("candidate_index").Find(&images).Error; err != nil {
				return nil, err
			}
			for _, image := range images {
				selected := row.SelectedResultID != nil && *row.SelectedResultID == image.ID
				if selected {
					doc.SelectedResultID = image.PublicID
				}
				parent := ""
				if image.ParentResultID != nil {
					var p models.AIImageResult
					if service.db.Select("public_id").First(&p, *image.ParentResultID).Error == nil {
						parent = p.PublicID
					}
				}
				turnDoc.Results = append(turnDoc.Results, ImageTurnResultDocument{PublicID: image.PublicID, CandidateIndex: image.CandidateIndex, ParentResultID: parent, MediaURL: "/api/v1/ai-jobs/" + jobPublicID + "/image-results/" + image.PublicID + "/media", Selected: selected, CreatedAt: image.CreatedAt})
			}
			if turn.ParentResultID != nil {
				var p models.AIImageResult
				if service.db.Select("public_id").First(&p, *turn.ParentResultID).Error == nil {
					turnDoc.ParentResultID = p.PublicID
				}
			}
			doc.Turns = append(doc.Turns, turnDoc)
		}
		result = append(result, doc)
	}
	return result, nil
}

func (service *ImageResultService) CreateTurn(ctx context.Context, input CreateImageTurnInput) (ImageTurnDocument, error) {
	input.UserInstruction = strings.TrimSpace(input.UserInstruction)
	if (input.Operation != "edit" && input.Operation != "restart") || utf8.RuneCountInString(input.UserInstruction) > 1000 || input.ActorID == 0 || (input.Operation == "edit" && (input.ParentResultPublicID == "" || input.UserInstruction == "")) || (input.Operation == "restart" && input.ParentResultPublicID != "") || (input.Mask != nil && input.Operation != "edit") {
		return ImageTurnDocument{}, ErrImageTurnInvalid
	}
	var created models.AIImageTurn
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var thread models.AIImageThread
		q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Table("ai_image_threads AS thread").Select("thread.*").Joins("JOIN ai_job_items AS item ON item.id = thread.ai_job_item_id").Joins("JOIN ai_jobs AS job ON job.id = item.ai_job_id").Where("job.public_id = ? AND item.public_id = ? AND item.kind = ?", input.JobPublicID, input.ItemPublicID, models.AIContentSlotImage).First(&thread)
		if errors.Is(q.Error, gorm.ErrRecordNotFound) {
			return ErrImageThreadNotFound
		}
		if q.Error != nil {
			return q.Error
		}
		var active int64
		if err := tx.Model(&models.AIImageTurn{}).Where("ai_image_thread_id = ? AND status IN ?", thread.ID, []models.AIImageTurnStatus{models.AIImageTurnQueued, models.AIImageTurnRunning}).Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return ErrImageTurnConflict
		}
		var parent *models.AIImageResult
		if input.ParentResultPublicID != "" {
			var p models.AIImageResult
			if err := tx.Table("ai_image_results AS result").Select("result.*").Joins("JOIN ai_image_turns AS turn ON turn.id = result.ai_image_turn_id").Where("turn.ai_image_thread_id = ? AND result.public_id = ?", thread.ID, input.ParentResultPublicID).First(&p).Error; err != nil {
				return ErrImageTurnInvalid
			}
			parent = &p
		}
		if input.Mask != nil && (parent == nil || input.Mask.Width != parent.Width || input.Mask.Height != parent.Height || input.Mask.SHA256 == "" || input.Mask.ByteCount <= 0) {
			return ErrImageTurnInvalid
		}
		var last models.AIImageTurn
		if err := tx.Where("ai_image_thread_id = ?", thread.ID).Order("sequence DESC").First(&last).Error; err != nil {
			return err
		}
		var user models.User
		if err := tx.Unscoped().Select("public_id", "name", "email").First(&user, input.ActorID).Error; err != nil {
			return err
		}
		actorJSON, _ := json.Marshal(ImageTurnActor{PublicID: user.PublicID, Name: user.Name, Email: user.Email})
		created = models.AIImageTurn{PublicID: uuid.NewString(), AIImageThreadID: thread.ID, Sequence: last.Sequence + 1, Operation: models.AIExecutionOperation(input.Operation), RequestedCandidateCount: 1, Size: last.Size, Quality: last.Quality, Style: last.Style, UserInstruction: input.UserInstruction, ActorID: input.ActorID, ActorSnapshotJSON: actorJSON, Status: models.AIImageTurnQueued}
		if parent != nil {
			created.ParentResultID = &parent.ID
		}
		if input.Mask != nil {
			created.MaskObjectKey = input.Mask.ObjectKey
			created.MaskSHA256 = input.Mask.SHA256
			created.MaskWidth = input.Mask.Width
			created.MaskHeight = input.Mask.Height
			created.MaskByteCount = input.Mask.ByteCount
		}
		if err := tx.Create(&created).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.AIJobItem{}).Where("id = ?", thread.AIJobItemID).Updates(map[string]any{"status": models.AIJobItemQueued, "safe_error": "", "failure_code": "", "completed_at": nil, "lease_owner": "", "lease_expires_at": nil}).Error; err != nil {
			return err
		}
		var item models.AIJobItem
		if err := tx.Select("ai_job_id").First(&item, thread.AIJobItemID).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.AIJob{}).Where("id = ?", item.AIJobID).Updates(map[string]any{"status": models.AIJobQueued, "completed_at": nil}).Error; err != nil {
			return err
		}
		metadata, _ := json.Marshal(map[string]any{"operation": input.Operation, "sequence": created.Sequence, "parent_result_id": input.ParentResultPublicID, "mask_present": input.Mask != nil})
		jobID, itemID := item.AIJobID, thread.AIJobItemID
		return tx.Create(&models.AIAuditEvent{PublicID: uuid.NewString(), EventType: "ai_image.turn_created", EntityType: "ai_image_turn", EntityPublicID: created.PublicID, ActorID: &input.ActorID, AIJobID: &jobID, AIJobItemID: &itemID, MetadataJSON: metadata}).Error
	})
	if err != nil {
		return ImageTurnDocument{}, err
	}
	actor := ImageTurnActor{}
	_ = json.Unmarshal(created.ActorSnapshotJSON, &actor)
	return ImageTurnDocument{PublicID: created.PublicID, Sequence: created.Sequence, Operation: created.Operation, ParentResultID: input.ParentResultPublicID, UserInstruction: created.UserInstruction, MaskPresent: created.MaskObjectKey != "", Status: created.Status, Actor: actor, CreatedAt: created.CreatedAt, Results: []ImageTurnResultDocument{}}, nil
}

func (service *ImageResultService) Select(ctx context.Context, jobID, itemID, resultID string, actorID uint) error {
	return service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row struct{ ThreadID, ResultID, JobID, ItemID uint }
		err := tx.Table("ai_image_results AS result").Select("thread.id AS thread_id,result.id AS result_id,job.id AS job_id,item.id AS item_id").Joins("JOIN ai_image_turns AS turn ON turn.id=result.ai_image_turn_id").Joins("JOIN ai_image_threads AS thread ON thread.id=turn.ai_image_thread_id").Joins("JOIN ai_job_items AS item ON item.id=thread.ai_job_item_id").Joins("JOIN ai_jobs AS job ON job.id=item.ai_job_id").Where("job.public_id=? AND item.public_id=? AND result.public_id=?", jobID, itemID, resultID).Scan(&row).Error
		if err != nil || row.ResultID == 0 {
			return ErrImageResultNotFound
		}
		if err := tx.Model(&models.AIImageThread{}).Where("id=?", row.ThreadID).Update("selected_result_id", row.ResultID).Error; err != nil {
			return err
		}
		metadata, _ := json.Marshal(map[string]any{"selected_result_id": resultID})
		return tx.Create(&models.AIAuditEvent{PublicID: uuid.NewString(), EventType: "ai_image.result_selected", EntityType: "ai_image_result", EntityPublicID: resultID, ActorID: &actorID, AIJobID: &row.JobID, AIJobItemID: &row.ItemID, MetadataJSON: metadata}).Error
	})
}
