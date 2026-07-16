package models

import "time"

type AITemplateStatus string

const (
	AITemplateDraft     AITemplateStatus = "draft"
	AITemplatePublished AITemplateStatus = "published"
	AITemplateArchived  AITemplateStatus = "archived"
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
	AIExecutionPreparing      AIExecutionStatus = "preparing"
	AIExecutionCallingOpenAI  AIExecutionStatus = "calling_openai"
	AIExecutionStoring        AIExecutionStatus = "storing"
	AIExecutionRendering      AIExecutionStatus = "rendering"
	AIExecutionCompleted      AIExecutionStatus = "completed"
	AIExecutionNeedsAttention AIExecutionStatus = "needs_attention"
	AIExecutionFailed         AIExecutionStatus = "failed"
	AIExecutionCancelled      AIExecutionStatus = "cancelled"
)

type OpenAIProviderSetting struct {
	ID                        uint       `gorm:"primaryKey" json:"-"`
	Provider                  string     `gorm:"size:32;uniqueIndex;not null" json:"provider"`
	EncryptedAPIKey           []byte     `gorm:"type:blob;not null" json:"-"`
	EncryptionNonce           []byte     `gorm:"type:varbinary(32);not null" json:"-"`
	EncryptionKeyVersion      string     `gorm:"size:16;not null" json:"-"`
	KeyFingerprint            string     `gorm:"size:16;not null" json:"key_fingerprint"`
	Status                    string     `gorm:"size:32;not null" json:"status"`
	VerifiedAt                *time.Time `json:"verified_at"`
	ImageCapabilityVerifiedAt *time.Time `json:"image_capability_verified_at"`
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
	Status         AITemplateStatus           `gorm:"size:32;index;not null;default:draft" json:"status"`
	CreatedByID    uint                       `gorm:"index;not null" json:"created_by_id"`
	CreatedAt      time.Time                  `json:"created_at"`
	UpdatedAt      time.Time                  `json:"updated_at"`
	Versions       []AIContentTemplateVersion `json:"versions,omitempty"`
}

type AIContentTemplateVersion struct {
	ID                    uint             `gorm:"primaryKey" json:"-"`
	PublicID              string           `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	AIContentTemplateID   uint             `gorm:"uniqueIndex:idx_ai_template_version;not null" json:"-"`
	VersionNumber         int              `gorm:"uniqueIndex:idx_ai_template_version;not null" json:"version_number"`
	Status                AITemplateStatus `gorm:"size:32;index;not null;default:draft" json:"status"`
	DefaultLocale         string           `gorm:"size:32;not null;default:zh-CN" json:"default_locale"`
	PromptCompilerVersion string           `gorm:"size:64;not null" json:"prompt_compiler_version"`
	PlatformPrompt        string           `gorm:"type:text;not null" json:"platform_prompt"`
	CreatedByID           uint             `gorm:"index;not null" json:"created_by_id"`
	PublishedByID         *uint            `gorm:"index" json:"published_by_id"`
	PublishedAt           *time.Time       `json:"published_at"`
	ArchivedAt            *time.Time       `json:"archived_at"`
	CreatedAt             time.Time        `json:"created_at"`
	UpdatedAt             time.Time        `json:"updated_at"`
	Slots                 []AIContentSlot  `json:"slots,omitempty"`
}

type AIContentSlot struct {
	ID                         uint              `gorm:"primaryKey" json:"-"`
	PublicID                   string            `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	AIContentTemplateVersionID uint              `gorm:"uniqueIndex:idx_ai_slot_key;index;not null" json:"-"`
	SlotKey                    string            `gorm:"size:80;uniqueIndex:idx_ai_slot_key;not null" json:"slot_key"`
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
	SKUID                      uint        `gorm:"index;not null" json:"sku_id"`
	AIContentTemplateVersionID uint        `gorm:"index;not null" json:"template_version_id"`
	TargetPlatform             string      `gorm:"size:80;index;not null" json:"target_platform"`
	Locale                     string      `gorm:"size:32;not null" json:"locale"`
	Status                     AIJobStatus `gorm:"size:32;index;not null;default:queued" json:"status"`
	SnapshotSchema             string      `gorm:"size:64;not null" json:"snapshot_schema"`
	InputSnapshotJSON          []byte      `gorm:"type:json;not null" json:"input_snapshot"`
	// Deprecated: Task 6 removes this compatibility field with the legacy handlers.
	// It is intentionally neither persisted nor serialized.
	InputAssetIDs string `gorm:"-" json:"-"`
	CreatedByID                uint        `gorm:"index;not null" json:"created_by_id"`
	StartedAt                  *time.Time  `json:"started_at"`
	CompletedAt                *time.Time  `json:"completed_at"`
	CancelledAt                *time.Time  `json:"cancelled_at"`
	CreatedAt                  time.Time   `json:"created_at"`
	UpdatedAt                  time.Time   `json:"updated_at"`
	Items                      []AIJobItem `json:"items,omitempty"`
	SKU                        SKU         `json:"sku,omitempty"`
}

type AIJobItem struct {
	ID                        uint              `gorm:"primaryKey" json:"-"`
	PublicID                  string            `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	AIJobID                   uint              `gorm:"index;not null" json:"-"`
	AIContentSlotID           uint              `gorm:"index;not null" json:"content_slot_id"`
	SlotKey                   string            `gorm:"size:80;not null" json:"slot_key"`
	SlotSnapshotJSON          []byte            `gorm:"type:json;not null" json:"slot_snapshot"`
	Kind                      AIContentSlotKind `gorm:"size:32;index;not null" json:"kind"`
	Status                    AIJobItemStatus   `gorm:"size:32;index;not null;default:queued" json:"status"`
	SelectedInputAssetIDsJSON []byte            `gorm:"type:json;not null" json:"selected_input_asset_ids"`
	CurrentCandidateID        *uint             `gorm:"index" json:"current_candidate_id"`
	EffectiveApprovedResultID *uint             `gorm:"index" json:"effective_approved_result_id"`
	AttemptCount              int               `gorm:"not null;default:0" json:"attempt_count"`
	LeaseOwner                string            `gorm:"size:120;index" json:"-"`
	LeaseExpiresAt            *time.Time        `gorm:"index" json:"-"`
	SafeError                 string            `gorm:"type:text" json:"safe_error"`
	InternalError             string            `gorm:"type:text" json:"-"`
	StartedAt                 *time.Time        `json:"started_at"`
	CompletedAt               *time.Time        `json:"completed_at"`
	CreatedAt                 time.Time         `json:"created_at"`
	UpdatedAt                 time.Time         `json:"updated_at"`
}

type AIExecution struct {
	ID                   uint                 `gorm:"primaryKey" json:"-"`
	PublicID             string               `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	AIJobItemID          uint                 `gorm:"index;not null" json:"job_item_id"`
	ParentExecutionID    *uint                `gorm:"index" json:"parent_execution_id"`
	Operation            AIExecutionOperation `gorm:"size:32;index;not null" json:"operation"`
	Status               AIExecutionStatus    `gorm:"size:32;index;not null" json:"status"`
	AttemptNumber        int                  `gorm:"not null" json:"attempt_number"`
	CompiledPrompt       string               `gorm:"type:longtext;not null" json:"compiled_prompt,omitempty"`
	CompiledPromptSHA256 string               `gorm:"size:64;index;not null" json:"compiled_prompt_sha256"`
	UserInstruction      string               `gorm:"type:text" json:"user_instruction"`
	OpenAIResponseID     string               `gorm:"size:255;index" json:"openai_response_id"`
	OpenAIRequestID      string               `gorm:"size:255;index" json:"openai_request_id"`
	Model                string               `gorm:"size:120;index;not null" json:"model"`
	RequestConfigJSON    []byte               `gorm:"type:json;not null" json:"request_config"`
	InputTextTokens      int64                `gorm:"not null;default:0" json:"input_text_tokens"`
	InputImageTokens     int64                `gorm:"not null;default:0" json:"input_image_tokens"`
	OutputTextTokens     int64                `gorm:"not null;default:0" json:"output_text_tokens"`
	OutputImageTokens    int64                `gorm:"not null;default:0" json:"output_image_tokens"`
	ReportedAmount       *float64             `json:"reported_amount"`
	EstimatedCost        *float64             `json:"estimated_cost"`
	Currency             string               `gorm:"size:8" json:"currency"`
	WorkerID             string               `gorm:"size:120;index" json:"worker_id"`
	LeaseExpiresAt       *time.Time           `gorm:"index" json:"lease_expires_at"`
	SafeError            string               `gorm:"type:text" json:"safe_error"`
	InternalError        string               `gorm:"type:text" json:"-"`
	StartedAt            *time.Time           `json:"started_at"`
	CompletedAt          *time.Time           `json:"completed_at"`
	CreatedAt            time.Time            `json:"created_at"`
	UpdatedAt            time.Time            `json:"updated_at"`
}

type AIAuditEvent struct {
	ID             uint      `gorm:"primaryKey" json:"-"`
	PublicID       string    `gorm:"size:36;uniqueIndex;not null" json:"public_id"`
	EventType      string    `gorm:"size:80;index;not null" json:"event_type"`
	EntityType     string    `gorm:"size:80;index;not null" json:"entity_type"`
	EntityPublicID string    `gorm:"size:36;index;not null" json:"entity_public_id"`
	ActorID        *uint     `gorm:"index" json:"actor_id"`
	AIJobID        *uint     `gorm:"index" json:"job_id"`
	AIJobItemID    *uint     `gorm:"index" json:"job_item_id"`
	AIExecutionID  *uint     `gorm:"index" json:"execution_id"`
	MetadataJSON   []byte    `gorm:"type:json;not null" json:"metadata"`
	CreatedAt      time.Time `gorm:"index" json:"created_at"`
}

type AIUsageLedger struct {
	ID                uint      `gorm:"primaryKey" json:"-"`
	AIExecutionID     uint      `gorm:"uniqueIndex;not null" json:"execution_id"`
	Model             string    `gorm:"size:120;index;not null" json:"model"`
	InputTextTokens   int64     `gorm:"not null;default:0" json:"input_text_tokens"`
	InputImageTokens  int64     `gorm:"not null;default:0" json:"input_image_tokens"`
	OutputTextTokens  int64     `gorm:"not null;default:0" json:"output_text_tokens"`
	OutputImageTokens int64     `gorm:"not null;default:0" json:"output_image_tokens"`
	ReportedAmount    *float64  `json:"reported_amount"`
	EstimatedAmount   *float64  `json:"estimated_amount"`
	Currency          string    `gorm:"size:8;not null" json:"currency"`
	OpenAIRequestID   string    `gorm:"size:255;index" json:"openai_request_id"`
	CreatedAt         time.Time `gorm:"index" json:"created_at"`
}
