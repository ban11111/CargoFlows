package models

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

type AITemplateStatus string

const (
	AITemplateDraft     AITemplateStatus = "draft"
	AITemplatePublished AITemplateStatus = "published"
	AITemplateArchived  AITemplateStatus = "archived"
)

type AIContentTemplateStatus string

const (
	AIContentTemplateActive   AIContentTemplateStatus = "active"
	AIContentTemplateArchived AIContentTemplateStatus = "archived"
)

type AIContentSlotKind string

const (
	AIContentSlotImage          AIContentSlotKind = "image"
	AIContentSlotTitle          AIContentSlotKind = "title"
	AIContentSlotSEODescription AIContentSlotKind = "seo_description"
)

type AIJobStatus string

const (
	AIJobQueued    AIJobStatus = "queued"
	AIJobRunning   AIJobStatus = "running"
	AIJobPartial   AIJobStatus = "partial"
	AIJobCompleted AIJobStatus = "completed"
	AIJobFailed    AIJobStatus = "failed"
	AIJobCancelled AIJobStatus = "cancelled"
)

type AIJobItemStatus string

const (
	AIJobItemQueued    AIJobItemStatus = "queued"
	AIJobItemRunning   AIJobItemStatus = "running"
	AIJobItemCompleted AIJobItemStatus = "completed"
	AIJobItemFailed    AIJobItemStatus = "failed"
	AIJobItemCancelled AIJobItemStatus = "cancelled"
)

type AIExecutionOperation string

const (
	AIExecutionGenerate     AIExecutionOperation = "generate"
	AIExecutionEdit         AIExecutionOperation = "edit"
	AIExecutionRestart      AIExecutionOperation = "restart"
	AIExecutionTextGenerate AIExecutionOperation = "text_generate"
)

type AIExecutionStatus string

const (
	AIExecutionQueued         AIExecutionStatus = "queued"
	AIExecutionPreparing      AIExecutionStatus = "preparing"
	AIExecutionCallingOpenAI  AIExecutionStatus = "calling_openai"
	AIExecutionStoring        AIExecutionStatus = "storing"
	AIExecutionRendering      AIExecutionStatus = "rendering"
	AIExecutionCompleted      AIExecutionStatus = "completed"
	AIExecutionNeedsAttention AIExecutionStatus = "needs_attention"
	AIExecutionFailed         AIExecutionStatus = "failed"
	AIExecutionCancelled      AIExecutionStatus = "cancelled"
)

type AIImageTurnStatus string

const (
	AIImageTurnQueued         AIImageTurnStatus = "queued"
	AIImageTurnRunning        AIImageTurnStatus = "running"
	AIImageTurnCompleted      AIImageTurnStatus = "completed"
	AIImageTurnNeedsAttention AIImageTurnStatus = "needs_attention"
	AIImageTurnFailed         AIImageTurnStatus = "failed"
	AIImageTurnCancelled      AIImageTurnStatus = "cancelled"
)

type OpenAIProviderSetting struct {
	ID                        uint       `gorm:"primaryKey" json:"-"`
	Provider                  string     `gorm:"size:32;uniqueIndex;not null" json:"provider"`
	EncryptedAPIKey           []byte     `gorm:"type:blob;not null" json:"-"`
	EncryptionNonce           []byte     `gorm:"type:varbinary(32);not null" json:"-"`
	EncryptionKeyVersion      string     `gorm:"size:16;not null" json:"-"`
	KeyFingerprint            string     `gorm:"size:16;not null" json:"key_fingerprint"`
	Status                    string     `gorm:"size:32;not null" json:"status"`
	TextModel                 string     `gorm:"size:200;not null;default:gpt-5.6-terra" json:"text_model"`
	ImageModel                string     `gorm:"size:200;not null;default:gpt-5.6" json:"image_model"`
	ImageAPIMode              string     `gorm:"size:32;not null;default:responses" json:"image_api_mode"`
	ImageResponsesModel       string     `gorm:"size:200;not null;default:gpt-5.6" json:"image_responses_model"`
	ImageGenerationModel      string     `gorm:"size:200;not null;default:gpt-image-2" json:"image_generation_model"`
	VerifiedAt                *time.Time `json:"verified_at"`
	ImageCapabilityVerifiedAt *time.Time `json:"image_capability_verified_at"`
	ImageResponsesVerifiedAt  *time.Time `json:"image_responses_verified_at"`
	ImageGenerationVerifiedAt *time.Time `json:"image_generation_verified_at"`
	LastUsedAt                *time.Time `json:"last_used_at"`
	CreatedByID               uint       `gorm:"index;not null" json:"-"`
	UpdatedByID               uint       `gorm:"index;not null" json:"-"`
	CreatedAt                 time.Time  `json:"created_at"`
	UpdatedAt                 time.Time  `json:"updated_at"`
}

type AIContentTemplate struct {
	ID             uint                       `gorm:"primaryKey" json:"-"`
	PublicID       string                     `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	NameZH         string                     `gorm:"size:180;not null" json:"name_zh"`
	NameEN         string                     `gorm:"size:180;not null" json:"name_en"`
	TargetPlatform string                     `gorm:"size:80;index;not null" json:"target_platform"`
	Status         AIContentTemplateStatus    `gorm:"size:32;index;not null;default:active" json:"status"`
	CreatedByID    uint                       `gorm:"index;not null" json:"-"`
	CreatedAt      time.Time                  `json:"created_at"`
	UpdatedAt      time.Time                  `json:"updated_at"`
	Versions       []AIContentTemplateVersion `json:"versions,omitempty"`
}

type AIContentTemplateVersion struct {
	ID                    uint             `gorm:"primaryKey" json:"-"`
	PublicID              string           `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	AIContentTemplateID   uint             `gorm:"uniqueIndex:idx_ai_template_version,priority:1;uniqueIndex:idx_ai_template_draft_guard,priority:1;not null" json:"-"`
	VersionNumber         int              `gorm:"uniqueIndex:idx_ai_template_version,priority:2;not null" json:"version_number"`
	Status                AITemplateStatus `gorm:"size:32;index;not null;default:draft" json:"status"`
	DraftGuard            *string          `gorm:"size:16;uniqueIndex:idx_ai_template_draft_guard,priority:2;check:chk_ai_template_draft_guard,(status = 'draft' AND draft_guard IS NOT NULL AND draft_guard = 'draft') OR (status <> 'draft' AND draft_guard IS NULL)" json:"-"`
	DefaultLocale         string           `gorm:"size:32;not null;default:zh-CN" json:"default_locale"`
	PromptCompilerVersion string           `gorm:"size:64;not null" json:"prompt_compiler_version"`
	PlatformPrompt        string           `gorm:"type:text;not null" json:"platform_prompt"`
	CreatedByID           uint             `gorm:"index;not null" json:"-"`
	PublishedByID         *uint            `gorm:"index" json:"-"`
	PublishedAt           *time.Time       `json:"published_at"`
	ArchivedAt            *time.Time       `json:"archived_at"`
	CreatedAt             time.Time        `json:"created_at"`
	UpdatedAt             time.Time        `json:"updated_at"`
	Slots                 []AIContentSlot  `json:"slots,omitempty"`
}

type AIContentSlot struct {
	ID                         uint              `gorm:"primaryKey" json:"-"`
	PublicID                   string            `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	AIContentTemplateVersionID uint              `gorm:"index:idx_ai_slot_key,priority:1;index;not null" json:"-"`
	SlotKey                    string            `gorm:"size:80;index:idx_ai_slot_key,priority:2;not null" json:"slot_key"`
	Kind                       AIContentSlotKind `gorm:"size:32;index;not null" json:"kind"`
	NameZH                     string            `gorm:"size:180;not null" json:"name_zh"`
	NameEN                     string            `gorm:"size:180;not null" json:"name_en"`
	DescriptionZH              string            `gorm:"type:text" json:"description_zh"`
	DescriptionEN              string            `gorm:"type:text" json:"description_en"`
	Sequence                   int               `gorm:"not null" json:"sequence"`
	Optional                   bool              `gorm:"not null;default:false" json:"optional"`
	DefaultSelected            bool              `gorm:"not null;default:false" json:"default_selected"`
	PromptFragment             string            `gorm:"type:text;not null" json:"prompt_fragment"`
	ConstraintsJSON            []byte            `gorm:"type:json;not null" json:"constraints"`
	GenerationConfigJSON       []byte            `gorm:"type:json;not null" json:"generation_config"`
	LayoutConfigJSON           []byte            `gorm:"type:json;not null" json:"layout_config"`
	CreatedAt                  time.Time         `json:"created_at"`
	UpdatedAt                  time.Time         `json:"updated_at"`
}

type AIJob struct {
	ID                         uint        `gorm:"primaryKey" json:"-"`
	PublicID                   string      `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	SKUID                      uint        `gorm:"index;not null" json:"-"`
	AIContentTemplateVersionID uint        `gorm:"index;not null" json:"-"`
	TargetPlatform             string      `gorm:"size:80;index;not null" json:"target_platform"`
	Locale                     string      `gorm:"size:32;not null" json:"locale"`
	Status                     AIJobStatus `gorm:"size:32;index;not null;default:queued" json:"status"`
	SnapshotSchema             string      `gorm:"size:64;not null" json:"snapshot_schema"`
	InputSnapshotJSON          []byte      `gorm:"type:json;not null" json:"input_snapshot"`
	CreatedBySnapshotJSON      []byte      `gorm:"type:json;not null" json:"created_by_snapshot"`
	ModelSnapshotJSON          []byte      `gorm:"type:json;not null" json:"model_snapshot"`
	CreatedByID                uint        `gorm:"index;uniqueIndex:idx_ai_job_actor_idempotency,priority:1;not null" json:"-"`
	IdempotencyKey             *string     `gorm:"size:128;uniqueIndex:idx_ai_job_actor_idempotency,priority:2" json:"-"`
	RequestSHA256              string      `gorm:"size:64;not null;default:''" json:"-"`
	StartedAt                  *time.Time  `json:"started_at"`
	CompletedAt                *time.Time  `json:"completed_at"`
	CancelledAt                *time.Time  `json:"cancelled_at"`
	CreatedAt                  time.Time   `json:"created_at"`
	UpdatedAt                  time.Time   `json:"updated_at"`
	Items                      []AIJobItem `json:"items,omitempty"`
	SKU                        SKU         `json:"sku,omitempty"`
}

func (job *AIJob) BeforeCreate(*gorm.DB) error {
	if len(job.CreatedBySnapshotJSON) == 0 {
		job.CreatedBySnapshotJSON = []byte(`{}`)
	}
	if len(job.ModelSnapshotJSON) == 0 {
		job.ModelSnapshotJSON = []byte(`{}`)
	}
	return nil
}

type AIJobItem struct {
	ID                        uint              `gorm:"primaryKey" json:"-"`
	PublicID                  string            `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	AIJobID                   uint              `gorm:"index;not null" json:"-"`
	AIContentSlotID           uint              `gorm:"index;not null" json:"-"`
	SlotKey                   string            `gorm:"size:80;not null" json:"slot_key"`
	SlotSnapshotJSON          []byte            `gorm:"type:json;not null" json:"slot_snapshot"`
	Kind                      AIContentSlotKind `gorm:"size:32;index;not null" json:"kind"`
	Status                    AIJobItemStatus   `gorm:"size:32;index;not null;default:queued" json:"status"`
	SelectedInputAssetIDsJSON []byte            `gorm:"type:json;not null" json:"selected_input_asset_ids"`
	CurrentCandidateID        *uint             `gorm:"index" json:"-"`
	EffectiveApprovedResultID *uint             `gorm:"index" json:"-"`
	AttemptCount              int               `gorm:"not null;default:0" json:"attempt_count"`
	LeaseOwner                string            `gorm:"size:120;index" json:"-"`
	LeaseExpiresAt            *time.Time        `gorm:"index" json:"-"`
	SafeError                 string            `gorm:"type:text" json:"safe_error"`
	FailureCode               string            `gorm:"size:80;index" json:"failure_code"`
	InternalError             string            `gorm:"type:text" json:"-"`
	StartedAt                 *time.Time        `json:"started_at"`
	CompletedAt               *time.Time        `json:"completed_at"`
	CreatedAt                 time.Time         `json:"created_at"`
	UpdatedAt                 time.Time         `json:"updated_at"`
	Executions                []AIExecution     `json:"executions,omitempty"`
}

type AIExecution struct {
	ID                        uint                 `gorm:"primaryKey" json:"-"`
	PublicID                  string               `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	AIJobItemID               uint                 `gorm:"index;uniqueIndex:idx_ai_execution_item_attempt,priority:1;not null" json:"-"`
	AIImageTurnID             *uint                `gorm:"index" json:"-"`
	ParentExecutionID         *uint                `gorm:"index" json:"-"`
	Operation                 AIExecutionOperation `gorm:"size:32;index;not null" json:"operation"`
	Status                    AIExecutionStatus    `gorm:"size:32;index;not null" json:"status"`
	AttemptNumber             int                  `gorm:"uniqueIndex:idx_ai_execution_item_attempt,priority:2;not null" json:"attempt_number"`
	L0PolicyVersion           string               `gorm:"size:64;not null" json:"l0_policy_version"`
	L1ProductContextVersion   string               `gorm:"size:64;not null" json:"l1_product_context_version"`
	L2TemplateVersionPublicID string               `gorm:"size:36;index;not null" json:"l2_template_version_public_id"`
	L3ContentSlotPublicID     string               `gorm:"size:36;index;not null" json:"l3_content_slot_public_id"`
	NormalizedInputJSON       []byte               `gorm:"type:json;not null" json:"normalized_input"`
	OrderedInputListJSON      []byte               `gorm:"type:json;not null" json:"ordered_input_list"`
	CompiledPrompt            string               `gorm:"type:longtext;not null" json:"compiled_prompt,omitempty"`
	CompiledPromptSHA256      string               `gorm:"size:64;index;not null" json:"compiled_prompt_sha256"`
	UserInstruction           string               `gorm:"type:text" json:"user_instruction"`
	OpenAIResponseID          string               `gorm:"size:255;index" json:"openai_response_id"`
	OpenAIRequestID           string               `gorm:"size:255;index" json:"openai_request_id"`
	OpenAIProviderSettingID   *uint                `gorm:"column:openai_provider_setting_id;index" json:"-"`
	OpenAIKeyFingerprint      string               `gorm:"column:openai_key_fingerprint;size:16" json:"-"`
	ProviderOutputJSON        []byte               `gorm:"type:json" json:"-"`
	Model                     string               `gorm:"size:120;index;not null" json:"model"`
	RequestedModel            string               `gorm:"size:120;index;not null;default:''" json:"requested_model"`
	ActualModel               string               `gorm:"size:120;index;not null;default:''" json:"actual_model"`
	APIMode                   string               `gorm:"size:32;index;not null;default:responses" json:"api_mode"`
	RequestConfigJSON         []byte               `gorm:"type:json;not null" json:"request_config"`
	InputTextTokens           int64                `gorm:"not null;default:0" json:"input_text_tokens"`
	InputImageTokens          int64                `gorm:"not null;default:0" json:"input_image_tokens"`
	OutputTextTokens          int64                `gorm:"not null;default:0" json:"output_text_tokens"`
	OutputImageTokens         int64                `gorm:"not null;default:0" json:"output_image_tokens"`
	ReasoningTokens           int64                `gorm:"not null;default:0" json:"reasoning_tokens"`
	TotalTokens               int64                `gorm:"not null;default:0" json:"total_tokens"`
	ReportedAmount            *float64             `json:"reported_amount"`
	EstimatedCost             *float64             `json:"estimated_cost"`
	Currency                  string               `gorm:"size:8" json:"currency"`
	WorkerID                  string               `gorm:"size:120;index" json:"worker_id"`
	LeaseExpiresAt            *time.Time           `gorm:"index" json:"lease_expires_at"`
	SafeError                 string               `gorm:"type:text" json:"safe_error"`
	FailureCode               string               `gorm:"size:80;index" json:"failure_code"`
	InternalError             string               `gorm:"type:text" json:"-"`
	StartedAt                 *time.Time           `json:"started_at"`
	CompletedAt               *time.Time           `json:"completed_at"`
	CreatedAt                 time.Time            `json:"created_at"`
	UpdatedAt                 time.Time            `json:"updated_at"`
}

type AIImageThread struct {
	ID               uint      `gorm:"primaryKey" json:"-"`
	PublicID         string    `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	AIJobItemID      uint      `gorm:"uniqueIndex;not null" json:"-"`
	SelectedResultID *uint     `gorm:"index" json:"-"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (thread *AIImageThread) BeforeSave(db *gorm.DB) error {
	if thread.SelectedResultID == nil || db == nil {
		return nil
	}
	var count int64
	err := db.Session(&gorm.Session{NewDB: true}).
		Table("ai_image_results AS result").
		Joins("JOIN ai_image_turns AS turn ON turn.id = result.ai_image_turn_id").
		Where("result.id = ? AND turn.ai_image_thread_id = ?", *thread.SelectedResultID, thread.ID).
		Count(&count).Error
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("selected image result must belong to the image thread")
	}
	return nil
}

type AIImageTurn struct {
	ID                          uint                 `gorm:"primaryKey" json:"-"`
	PublicID                    string               `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	AIImageThreadID             uint                 `gorm:"uniqueIndex:idx_ai_image_turn_sequence,priority:1;index;not null" json:"-"`
	Sequence                    int                  `gorm:"uniqueIndex:idx_ai_image_turn_sequence,priority:2;check:chk_ai_image_turn_sequence,sequence > 0;not null" json:"sequence"`
	Operation                   AIExecutionOperation `gorm:"size:32;index;not null;check:chk_ai_image_turn_parent,(operation = 'edit' AND parent_result_id IS NOT NULL) OR (operation IN ('generate','restart') AND parent_result_id IS NULL)" json:"operation"`
	ParentResultID              *uint                `gorm:"index" json:"-"`
	RequestedCandidateCount     int                  `gorm:"not null;default:1;check:chk_ai_image_turn_candidate_count,requested_candidate_count BETWEEN 1 AND 4" json:"requested_candidate_count"`
	Size                        string               `gorm:"size:32;not null;default:1024x1024" json:"size"`
	Quality                     string               `gorm:"size:32;not null;default:medium" json:"quality"`
	Style                       string               `gorm:"size:64;not null;default:default" json:"style"`
	UserInstruction             string               `gorm:"type:text" json:"user_instruction"`
	ActorSnapshotJSON           []byte               `gorm:"type:json;not null" json:"-"`
	MaskObjectKey               string               `gorm:"size:768" json:"-"`
	MaskSHA256                  string               `gorm:"size:64;index" json:"-"`
	MaskWidth                   int                  `gorm:"not null;default:0" json:"-"`
	MaskHeight                  int                  `gorm:"not null;default:0" json:"-"`
	MaskByteCount               int64                `gorm:"not null;default:0" json:"-"`
	CompiledRequestMetadataJSON []byte               `gorm:"type:json;not null" json:"-"`
	Status                      AIImageTurnStatus    `gorm:"size:32;index;not null;default:queued" json:"status"`
	ActorID                     uint                 `gorm:"index;not null" json:"-"`
	LeaseOwner                  string               `gorm:"size:120;index" json:"-"`
	LeaseExpiresAt              *time.Time           `gorm:"index" json:"-"`
	SafeError                   string               `gorm:"type:text" json:"safe_error"`
	InternalError               string               `gorm:"type:text" json:"-"`
	StartedAt                   *time.Time           `json:"started_at"`
	CompletedAt                 *time.Time           `json:"completed_at"`
	CreatedAt                   time.Time            `json:"created_at"`
	UpdatedAt                   time.Time            `json:"updated_at"`
}

func (turn *AIImageTurn) BeforeCreate(db *gorm.DB) error {
	if len(turn.CompiledRequestMetadataJSON) == 0 {
		turn.CompiledRequestMetadataJSON = []byte(`{}`)
	}
	if len(turn.ActorSnapshotJSON) == 0 {
		turn.ActorSnapshotJSON = []byte(`{}`)
	}
	if turn.Status == "" {
		turn.Status = AIImageTurnQueued
	}
	if turn.RequestedCandidateCount == 0 {
		turn.RequestedCandidateCount = 1
	}
	if turn.Size == "" {
		turn.Size = "1024x1024"
	}
	if turn.Quality == "" {
		turn.Quality = "medium"
	}
	if turn.Style == "" {
		turn.Style = "default"
	}
	if turn.Operation != AIExecutionEdit || turn.ParentResultID == nil || db == nil {
		return nil
	}
	var count int64
	err := db.Session(&gorm.Session{NewDB: true}).
		Table("ai_image_results AS result").
		Joins("JOIN ai_image_turns AS parent_turn ON parent_turn.id = result.ai_image_turn_id").
		Where("result.id = ? AND parent_turn.ai_image_thread_id = ?", *turn.ParentResultID, turn.AIImageThreadID).
		Count(&count).Error
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("parent image result must belong to the image thread")
	}
	return nil
}

type AIImageResult struct {
	ID                  uint      `gorm:"primaryKey" json:"-"`
	PublicID            string    `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	AIImageTurnID       uint      `gorm:"uniqueIndex:idx_ai_image_result_candidate,priority:1;index;not null" json:"-"`
	AIExecutionID       uint      `gorm:"uniqueIndex;not null" json:"-"`
	ParentResultID      *uint     `gorm:"index" json:"-"`
	CandidateIndex      int       `gorm:"uniqueIndex:idx_ai_image_result_candidate,priority:2;check:chk_ai_image_result_candidate_index,candidate_index > 0;not null" json:"candidate_index"`
	ObjectKey           string    `gorm:"size:768;not null" json:"-"`
	MIMEType            string    `gorm:"size:80;not null" json:"mime_type"`
	Width               int       `gorm:"not null;check:chk_ai_image_result_width,width > 0" json:"width"`
	Height              int       `gorm:"not null;check:chk_ai_image_result_height,height > 0" json:"height"`
	ByteCount           int64     `gorm:"not null;check:chk_ai_image_result_byte_count,byte_count > 0" json:"byte_count"`
	SHA256              string    `gorm:"size:64;index;not null" json:"sha256"`
	ProviderImageCallID string    `gorm:"size:255;index" json:"-"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func (result *AIImageResult) BeforeCreate(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	var turn AIImageTurn
	if err := db.Session(&gorm.Session{NewDB: true}).Select("id", "parent_result_id").First(&turn, result.AIImageTurnID).Error; err != nil {
		return err
	}
	if (turn.ParentResultID == nil) != (result.ParentResultID == nil) {
		return errors.New("image result parent must match its turn parent")
	}
	if turn.ParentResultID != nil && *turn.ParentResultID != *result.ParentResultID {
		return errors.New("image result parent must match its turn parent")
	}
	return nil
}

type AIAuditEvent struct {
	ID             uint      `gorm:"primaryKey" json:"-"`
	PublicID       string    `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	EventType      string    `gorm:"size:80;index;not null" json:"event_type"`
	EntityType     string    `gorm:"size:80;index;not null" json:"entity_type"`
	EntityPublicID string    `gorm:"size:36;index;not null" json:"entity_public_id"`
	IdempotencyKey *string   `gorm:"size:160;uniqueIndex" json:"-"`
	ActorID        *uint     `gorm:"index" json:"-"`
	AIJobID        *uint     `gorm:"index" json:"-"`
	AIJobItemID    *uint     `gorm:"index" json:"-"`
	AIExecutionID  *uint     `gorm:"index" json:"-"`
	MetadataJSON   []byte    `gorm:"type:json;not null" json:"metadata"`
	CreatedAt      time.Time `gorm:"index" json:"created_at"`
}

type AIUsageLedger struct {
	ID                uint      `gorm:"primaryKey" json:"-"`
	AIExecutionID     uint      `gorm:"uniqueIndex;not null" json:"-"`
	Model             string    `gorm:"size:120;index;not null" json:"model"`
	InputTextTokens   int64     `gorm:"not null;default:0" json:"input_text_tokens"`
	InputImageTokens  int64     `gorm:"not null;default:0" json:"input_image_tokens"`
	OutputTextTokens  int64     `gorm:"not null;default:0" json:"output_text_tokens"`
	OutputImageTokens int64     `gorm:"not null;default:0" json:"output_image_tokens"`
	ReasoningTokens   int64     `gorm:"not null;default:0" json:"reasoning_tokens"`
	TotalTokens       int64     `gorm:"not null;default:0" json:"total_tokens"`
	ReportedAmount    *float64  `json:"reported_amount"`
	EstimatedAmount   *float64  `json:"estimated_amount"`
	Currency          string    `gorm:"size:8;not null" json:"currency"`
	OpenAIRequestID   string    `gorm:"size:255;index" json:"openai_request_id"`
	CreatedAt         time.Time `gorm:"index" json:"created_at"`
}

type AITextResultState string

const (
	AITextResultCandidate AITextResultState = "candidate"
	AITextResultApproved  AITextResultState = "approved"
	AITextResultRejected  AITextResultState = "rejected"
)

type AITextResult struct {
	ID                   uint              `gorm:"primaryKey" json:"-"`
	PublicID             string            `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	AIExecutionID        uint              `gorm:"uniqueIndex:idx_ai_text_execution_candidate,priority:1;not null" json:"-"`
	CandidateIndex       int               `gorm:"uniqueIndex:idx_ai_text_execution_candidate,priority:2;check:chk_ai_text_candidate_index,candidate_index > 0;not null" json:"candidate_index"`
	Kind                 AIContentSlotKind `gorm:"size:32;index;not null" json:"kind"`
	RawStructuredJSON    []byte            `gorm:"type:json;not null" json:"-"`
	ValidationJSON       []byte            `gorm:"type:json;not null" json:"-"`
	EditedStructuredJSON []byte            `gorm:"type:json" json:"-"`
	State                AITextResultState `gorm:"size:32;index;not null;default:candidate;check:chk_ai_text_result_lifecycle,(state = 'candidate' AND approved_by_id IS NULL AND approved_at IS NULL AND rejected_by_id IS NULL AND rejected_at IS NULL AND applied_by_id IS NULL AND applied_at IS NULL) OR (state = 'approved' AND approved_by_id IS NOT NULL AND approved_at IS NOT NULL AND rejected_by_id IS NULL AND rejected_at IS NULL AND ((applied_by_id IS NULL AND applied_at IS NULL) OR (applied_by_id IS NOT NULL AND applied_at IS NOT NULL))) OR (state = 'rejected' AND rejected_by_id IS NOT NULL AND rejected_at IS NOT NULL AND approved_by_id IS NULL AND approved_at IS NULL AND applied_by_id IS NULL AND applied_at IS NULL)" json:"state"`
	EditedByID           *uint             `gorm:"index" json:"-"`
	EditedAt             *time.Time        `json:"edited_at"`
	ApprovedByID         *uint             `gorm:"index" json:"-"`
	ApprovedAt           *time.Time        `json:"approved_at"`
	RejectedByID         *uint             `gorm:"index" json:"-"`
	RejectedAt           *time.Time        `json:"rejected_at"`
	AppliedByID          *uint             `gorm:"index" json:"-"`
	AppliedAt            *time.Time        `json:"applied_at"`
	CreatedAt            time.Time         `json:"created_at"`
	UpdatedAt            time.Time         `json:"updated_at"`
}

func (result *AITextResult) BeforeCreate(*gorm.DB) error {
	if len(result.RawStructuredJSON) == 0 {
		result.RawStructuredJSON = []byte(`{}`)
	}
	if len(result.ValidationJSON) == 0 {
		result.ValidationJSON = []byte(`[]`)
	}
	if result.State == "" {
		result.State = AITextResultCandidate
	}
	return nil
}

type SKUPlatformContent struct {
	ID                   uint      `gorm:"primaryKey" json:"-"`
	PublicID             string    `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	SKUID                uint      `gorm:"column:sku_id;uniqueIndex:idx_sku_platform_content,priority:1;not null" json:"-"`
	Platform             string    `gorm:"size:80;uniqueIndex:idx_sku_platform_content,priority:2;not null" json:"platform"`
	Locale               string    `gorm:"size:32;uniqueIndex:idx_sku_platform_content,priority:3;not null" json:"locale"`
	Title                string    `gorm:"size:500;not null" json:"title"`
	ShortDescription     string    `gorm:"type:text;not null" json:"short_description"`
	LongDescription      string    `gorm:"type:longtext;not null" json:"long_description"`
	SellingPointsJSON    []byte    `gorm:"type:json;not null" json:"-"`
	SearchKeywordsJSON   []byte    `gorm:"type:json;not null" json:"-"`
	SourceAITextResultID *uint     `gorm:"index" json:"-"`
	Revision             int       `gorm:"not null;default:1;check:chk_sku_platform_content_revision,revision > 0" json:"revision"`
	UpdatedByID          uint      `gorm:"index;not null" json:"-"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func (content *SKUPlatformContent) BeforeCreate(*gorm.DB) error {
	if len(content.SellingPointsJSON) == 0 {
		content.SellingPointsJSON = []byte(`[]`)
	}
	if len(content.SearchKeywordsJSON) == 0 {
		content.SearchKeywordsJSON = []byte(`[]`)
	}
	if content.Revision == 0 {
		content.Revision = 1
	}
	return nil
}

type SKUPlatformContentRevision struct {
	ID                   uint      `gorm:"primaryKey" json:"-"`
	PublicID             string    `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	SKUPlatformContentID uint      `gorm:"uniqueIndex:idx_platform_content_revision,priority:1;not null" json:"-"`
	Revision             int       `gorm:"uniqueIndex:idx_platform_content_revision,priority:2;check:chk_platform_content_history_revision,revision > 0;not null" json:"revision"`
	BeforeJSON           []byte    `gorm:"type:json;not null" json:"-"`
	AfterJSON            []byte    `gorm:"type:json;not null" json:"-"`
	SourceAITextResultID *uint     `gorm:"index" json:"-"`
	ActorID              uint      `gorm:"index;not null" json:"-"`
	CreatedAt            time.Time `json:"created_at"`
}

func (revision *SKUPlatformContentRevision) BeforeCreate(*gorm.DB) error {
	if len(revision.BeforeJSON) == 0 {
		revision.BeforeJSON = []byte(`{}`)
	}
	if len(revision.AfterJSON) == 0 {
		revision.AfterJSON = []byte(`{}`)
	}
	return nil
}
