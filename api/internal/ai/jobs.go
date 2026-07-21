package ai

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"cargoflows/api/internal/models"
	"cargoflows/api/internal/money"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ProductSnapshotSchemaV1 = "cargoflows_product_generation_v1"
	ProductSnapshotSchemaV2 = "cargoflows_product_generation_v2"
)

const (
	AssetSourceProductVisual      = "product_visual"
	AssetSourceProductInformation = "product_information"
)

var (
	ErrJobNotFound                    = errors.New("AI job not found")
	ErrSKUNotFound                    = errors.New("SKU not found")
	ErrTemplateVersionNotPublished    = errors.New("AI content template version is not published")
	ErrSlotSelectionInvalid           = errors.New("selected slots are invalid")
	ErrAssetNotEligible               = errors.New("selected assets must be approved and belong to the exact SKU")
	ErrPublishedSOPNotFound           = errors.New("published capture SOP not found for SKU category")
	ErrIdempotencyKeyInvalid          = errors.New("idempotency key is invalid")
	ErrIdempotencyConflict            = errors.New("idempotency key was already used for a different request")
	ErrLocaleInvalid                  = errors.New("locale is invalid")
	ErrOutputLocalesInvalid           = errors.New("output locales are invalid")
	ErrUserPreferenceInvalid          = errors.New("user preference is too long")
	ErrGenerationOverrideInvalid      = errors.New("generation override is not explicitly allowed by the published slot")
	ErrPublishedTemplateConfigInvalid = errors.New("published template configuration is invalid")
	ErrUserPreferenceNotAllowed       = errors.New("user preference is not allowed by every selected slot")
	ErrTemplateVersionIDInvalid       = errors.New("template version ID must be a UUID")
	ErrSKUIDInvalid                   = errors.New("SKU ID must be a UUID")
	ErrAssetIDInvalid                 = errors.New("asset IDs must be UUIDs")
	ErrStyleReferenceNotEligible      = errors.New("style references must be approved grants")
	ErrBrandIconNotEligible           = errors.New("brand icons must be active and belong to the SKU brand")
	ErrExternalReferenceNotEligible   = errors.New("external references must belong to a published same-category AI reference SOP")
	ErrCompatibleDeviceModelRequired  = errors.New("compatible device model is required for the selected output")
	ErrJobItemNotFound                = errors.New("AI job item not found")
	ErrTextItemRegenerationInvalid    = errors.New("only failed text items can be regenerated")
	ErrTextItemRegenerationConflict   = errors.New("AI text item is not failed")
)

type CreateJobInput struct {
	SKUID                     string
	TemplateVersionPublicID   string
	SelectedSlotKeys          []string
	SelectedAssetIDs          []string
	SelectedStyleReferenceIDs []string
	SelectedBrandIconIDs      []string
	SelectedReferenceItemIDs  []string
	Locale                    string
	OutputLocales             []string
	CreatedByID               uint
	IdempotencyKey            string
	UserPreference            string
	GenerationOverrides       map[string]GenerationOverride
	ImageCanvases             []ImageCanvas `json:"image_canvases,omitempty"`
}

type ImageCanvas struct {
	CanvasKey          string              `json:"canvas_key"`
	SlotKeys           []string            `json:"slot_keys"`
	GenerationOverride *GenerationOverride `json:"generation_override,omitempty"`
}

type GenerationOverride struct {
	CandidateCount *int    `json:"candidate_count,omitempty"`
	Size           *string `json:"size,omitempty"`
	Quality        *string `json:"quality,omitempty"`
	Style          *string `json:"style,omitempty"`
}

type LocalizedNameFacts struct {
	ZH string `json:"zh"`
	EN string `json:"en"`
}

type CategoryFacts struct {
	NameZH string `json:"name_zh"`
	NameEN string `json:"name_en"`
}

type ProductFacts struct {
	Name        string        `json:"name"`
	Brand       string        `json:"brand"`
	Description string        `json:"description"`
	Category    CategoryFacts `json:"category"`
}

type SKUFacts struct {
	PublicID              string   `json:"public_id"`
	Code                  string   `json:"code"`
	Color                 string   `json:"color"`
	Size                  string   `json:"size"`
	CompatibleDeviceModel string   `json:"compatible_device_model,omitempty"`
	PlatformTitle         string   `json:"platform_title"`
	SellingPoints         string   `json:"selling_points"`
	Tags                  []string `json:"tags"`
}

type VectorFacts struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

type SOPViewFacts struct {
	PublicID                string             `json:"public_id"`
	Sequence                int                `json:"sequence"`
	Role                    models.SOPViewRole `json:"role"`
	ViewKind                models.SOPViewKind `json:"view_kind"`
	PresetKey               string             `json:"preset_key"`
	Name                    LocalizedNameFacts `json:"name"`
	Instruction             LocalizedNameFacts `json:"instruction"`
	Required                bool               `json:"required"`
	AllowMultiple           bool               `json:"allow_multiple"`
	CameraPositionDirection VectorFacts        `json:"camera_position_direction"`
	ImageUpDirection        VectorFacts        `json:"image_up_direction"`
	Target                  VectorFacts        `json:"target"`
	Composition             models.Composition `json:"composition"`
}

type SOPFacts struct {
	PublicID         string             `json:"public_id"`
	VersionPublicID  string             `json:"version_public_id"`
	VersionNumber    int                `json:"version_number"`
	SchemaVersion    string             `json:"schema_version"`
	Name             LocalizedNameFacts `json:"name"`
	Description      LocalizedNameFacts `json:"description"`
	CoordinateSystem string             `json:"coordinate_system"`
	Views            []SOPViewFacts     `json:"views"`
}

type AssetFacts struct {
	PublicID   string         `json:"public_id"`
	SourceType string         `json:"source_type"`
	MIMEType   string         `json:"mime_type"`
	Width      int            `json:"width"`
	Height     int            `json:"height"`
	ByteCount  int64          `json:"byte_count"`
	SHA256     string         `json:"sha256"`
	CapturedAt time.Time      `json:"captured_at"`
	View       AssetViewFacts `json:"view"`
}

type AssetViewFacts struct {
	PublicID                string             `json:"public_id"`
	PresetKey               string             `json:"preset_key"`
	Name                    LocalizedNameFacts `json:"name"`
	Role                    models.SOPViewRole `json:"role"`
	ViewKind                models.SOPViewKind `json:"view_kind"`
	Instruction             LocalizedNameFacts `json:"instruction"`
	CameraPositionDirection VectorFacts        `json:"camera_position_direction"`
	ImageUpDirection        VectorFacts        `json:"image_up_direction"`
	Target                  VectorFacts        `json:"target"`
	Composition             models.Composition `json:"composition"`
}

type SlotFacts struct {
	PublicID              string                   `json:"public_id"`
	SlotKey               string                   `json:"slot_key"`
	Kind                  models.AIContentSlotKind `json:"kind"`
	Name                  LocalizedNameFacts       `json:"name"`
	Description           LocalizedNameFacts       `json:"description"`
	Sequence              int                      `json:"sequence"`
	Optional              bool                     `json:"optional"`
	DefaultSelected       bool                     `json:"default_selected"`
	PromptFragment        string                   `json:"prompt_fragment"`
	Constraints           json.RawMessage          `json:"constraints"`
	GenerationConfig      json.RawMessage          `json:"generation_config"`
	LayoutConfig          json.RawMessage          `json:"layout_config"`
	CompositeRequirements []SlotFacts              `json:"composite_requirements,omitempty"`
	CanvasKey             string                   `json:"canvas_key,omitempty"`
	CanvasGeneration      *GenerationOverride      `json:"canvas_generation_override,omitempty"`
}

type TemplateFacts struct {
	TemplatePublicID      string      `json:"template_public_id"`
	VersionPublicID       string      `json:"version_public_id"`
	VersionNumber         int         `json:"version_number"`
	PromptCompilerVersion string      `json:"prompt_compiler_version"`
	PlatformPrompt        string      `json:"platform_prompt"`
	SelectedSlots         []SlotFacts `json:"selected_slots"`
}

type ProductSnapshotV1 struct {
	Schema              string                        `json:"schema"`
	Locale              string                        `json:"locale"`
	OutputLocales       []string                      `json:"output_locales,omitempty"`
	TargetPlatform      string                        `json:"target_platform"`
	Product             ProductFacts                  `json:"product"`
	SKU                 SKUFacts                      `json:"sku"`
	SOP                 SOPFacts                      `json:"sop"`
	Template            TemplateFacts                 `json:"template"`
	SelectedAssets      []AssetFacts                  `json:"selected_assets"`
	UserPreference      string                        `json:"user_preference"`
	GenerationOverrides map[string]GenerationOverride `json:"generation_overrides"`
	ImageCanvases       []ImageCanvas                 `json:"image_canvases,omitempty"`
	StyleReferences     []StyleReferenceFacts         `json:"style_references,omitempty"`
	StructureReferences []StructureReferenceFacts     `json:"structure_references,omitempty"`
	BrandIcons          []BrandIconFacts              `json:"brand_icons,omitempty"`
	ReferenceSOPs       []ReferenceSOPFacts           `json:"reference_sops,omitempty"`
	ExternalReferences  []ExternalReferenceFacts      `json:"external_references,omitempty"`
}

type BrandIconFacts struct {
	PublicID  string `json:"public_id"`
	Name      string `json:"name"`
	Notes     string `json:"notes"`
	MIMEType  string `json:"mime_type"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	ByteCount int64  `json:"byte_count"`
	SHA256    string `json:"sha256"`
}

type ReferenceSOPFacts struct {
	PublicID        string             `json:"public_id"`
	VersionPublicID string             `json:"version_public_id"`
	VersionNumber   int                `json:"version_number"`
	CategoryID      uint               `json:"category_id"`
	Name            LocalizedNameFacts `json:"name"`
	Description     LocalizedNameFacts `json:"description"`
}

type ExternalReferenceFacts struct {
	PublicID           string                    `json:"public_id"`
	SOPPublicID        string                    `json:"sop_public_id"`
	VersionPublicID    string                    `json:"version_public_id"`
	Purpose            models.AIReferencePurpose `json:"purpose"`
	Caption            LocalizedNameFacts        `json:"caption"`
	AllowedGuidance    LocalizedNameFacts        `json:"allowed_guidance"`
	ForbiddenGuidance  LocalizedNameFacts        `json:"forbidden_guidance"`
	SourceName         string                    `json:"source_name"`
	SourceURL          string                    `json:"source_url,omitempty"`
	SHA256             string                    `json:"sha256"`
	ReviewedByPublicID string                    `json:"reviewed_by"`
}

type StyleReferenceFacts struct {
	PublicID           string             `json:"public_id"`
	Version            int                `json:"version"`
	SourceSKUPublicID  string             `json:"source_sku_id"`
	Description        LocalizedNameFacts `json:"description"`
	DerivativeSHA256   string             `json:"derivative_sha256"`
	ReviewedByPublicID string             `json:"reviewed_by"`
}

type StructureReferenceFacts struct {
	PublicID            string          `json:"public_id"`
	Version             int             `json:"version"`
	SourceSKUPublicID   string          `json:"source_sku_id"`
	ModelFamilyPublicID string          `json:"model_family_id"`
	Role                string          `json:"role"`
	AllowedAttributes   json.RawMessage `json:"allowed_attributes"`
	ForbiddenAttributes json.RawMessage `json:"forbidden_attributes"`
	DerivativeSHA256    string          `json:"derivative_sha256"`
	ReviewedByPublicID  string          `json:"reviewed_by"`
}

type JobCreatorSnapshot struct {
	PublicID string `json:"public_id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
}

type JobModelSnapshot struct {
	TextModel            string `json:"text_model"`
	ImageAPIMode         string `json:"image_api_mode"`
	ImageResponsesModel  string `json:"image_responses_model"`
	ImageGenerationModel string `json:"image_generation_model"`
}

type JobExecutionDocument struct {
	PublicID           string                      `json:"public_id"`
	Operation          models.AIExecutionOperation `json:"operation"`
	Status             models.AIExecutionStatus    `json:"status"`
	AttemptNumber      int                         `json:"attempt_number"`
	RequestedModel     string                      `json:"requested_model"`
	ActualModel        string                      `json:"actual_model"`
	APIMode            string                      `json:"api_mode"`
	ProviderRequestID  string                      `json:"provider_request_id"`
	InputTextTokens    int64                       `json:"input_text_tokens"`
	CachedInputTokens  int64                       `json:"cached_input_tokens"`
	InputImageTokens   int64                       `json:"input_image_tokens"`
	OutputTextTokens   int64                       `json:"output_text_tokens"`
	OutputImageTokens  int64                       `json:"output_image_tokens"`
	ReasoningTokens    int64                       `json:"reasoning_tokens"`
	TotalTokens        int64                       `json:"total_tokens"`
	ServiceTier        string                      `json:"service_tier"`
	PricingStatus      string                      `json:"pricing_status"`
	EstimatedAmountUSD string                      `json:"estimated_amount_usd"`
	FailureCode        string                      `json:"failure_code"`
	SafeError          string                      `json:"safe_error"`
	StartedAt          *time.Time                  `json:"started_at"`
	CompletedAt        *time.Time                  `json:"completed_at"`
}

type JobFailureDocument struct {
	Code              string `json:"code"`
	SafeMessage       string `json:"safe_message"`
	RecoveryAction    string `json:"recovery_action"`
	Model             string `json:"model"`
	APIMode           string `json:"api_mode"`
	ProviderRequestID string `json:"provider_request_id"`
}

type JobItemDocument struct {
	PublicID              string                   `json:"public_id"`
	SlotKey               string                   `json:"slot_key"`
	Kind                  models.AIContentSlotKind `json:"kind"`
	Status                models.AIJobItemStatus   `json:"status"`
	SlotSnapshot          json.RawMessage          `json:"slot_snapshot"`
	SelectedInputAssetIDs []string                 `json:"selected_input_asset_ids"`
	AttemptCount          int                      `json:"attempt_count"`
	SafeError             string                   `json:"safe_error"`
	Failure               *JobFailureDocument      `json:"failure"`
	Executions            []JobExecutionDocument   `json:"executions"`
	StartedAt             *time.Time               `json:"started_at"`
	CompletedAt           *time.Time               `json:"completed_at"`
	CreatedAt             time.Time                `json:"created_at"`
	UpdatedAt             time.Time                `json:"updated_at"`
}

type JobDocument struct {
	PublicID                string             `json:"public_id"`
	SKUID                   string             `json:"sku_id"`
	TemplateVersionPublicID string             `json:"template_version_id"`
	TargetPlatform          string             `json:"target_platform"`
	Locale                  string             `json:"locale"`
	OutputLocales           []string           `json:"output_locales"`
	Status                  models.AIJobStatus `json:"status"`
	SnapshotSchema          string             `json:"snapshot_schema"`
	InputSnapshot           json.RawMessage    `json:"input_snapshot"`
	CreatedBy               JobCreatorSnapshot `json:"created_by"`
	CreatedBySnapshot       JobCreatorSnapshot `json:"created_by_snapshot"`
	ModelSnapshot           JobModelSnapshot   `json:"model_snapshot"`
	StartedAt               *time.Time         `json:"started_at"`
	CompletedAt             *time.Time         `json:"completed_at"`
	CancelledAt             *time.Time         `json:"cancelled_at"`
	CreatedAt               time.Time          `json:"created_at"`
	UpdatedAt               time.Time          `json:"updated_at"`
	Items                   []JobItemDocument  `json:"items"`
	TotalTokens             int64              `json:"total_tokens"`
	EstimatedAmountUSD      string             `json:"estimated_amount_usd"`
	ReconciledAmountUSD     string             `json:"reconciled_amount_usd"`
	ReconciliationStatus    string             `json:"reconciliation_status"`
	Replayed                bool               `json:"-"`
}

type JobListFilters struct {
	CreatedBy string
	Model     string
	APIMode   string
}

type JobService struct{ db *gorm.DB }

func NewJobService(db *gorm.DB) *JobService { return &JobService{db: db} }

// RegenerateTextItem requeues one failed text slot while preserving every
// previous execution and result as immutable job history.
func (s *JobService) RegenerateTextItem(ctx context.Context, jobPublicID, itemPublicID string, actorID uint) (JobDocument, error) {
	var document JobDocument
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx
		if tx.Dialector.Name() == "mysql" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		var job models.AIJob
		if err := query.Where("public_id = ?", jobPublicID).First(&job).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrJobNotFound
			}
			return err
		}
		var item models.AIJobItem
		if err := query.Where("public_id = ? AND ai_job_id = ?", itemPublicID, job.ID).First(&item).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrJobItemNotFound
			}
			return err
		}
		if item.Kind != models.AIContentSlotTitle && item.Kind != models.AIContentSlotSEODescription {
			return ErrTextItemRegenerationInvalid
		}
		if item.Status != models.AIJobItemFailed {
			return ErrTextItemRegenerationConflict
		}
		updated := tx.Model(&models.AIJobItem{}).
			Where("id = ? AND status = ?", item.ID, models.AIJobItemFailed).
			Updates(map[string]any{
				"status": models.AIJobItemQueued, "safe_error": "", "failure_code": "", "internal_error": "",
				"lease_owner": "", "lease_expires_at": nil, "completed_at": nil,
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrTextItemRegenerationConflict
		}
		now := time.Now().UTC()
		if err := aggregateJob(tx, job.ID, now); err != nil {
			return err
		}
		metadata, err := json.Marshal(map[string]any{"attempt_count": item.AttemptCount, "requested_attempt_number": item.AttemptCount + 1, "previous_failure_code": item.FailureCode})
		if err != nil {
			return err
		}
		jobID, itemID := job.ID, item.ID
		audit := models.AIAuditEvent{PublicID: uuid.NewString(), EventType: "ai_job_item.text_regeneration_requested", EntityType: "ai_job_item", EntityPublicID: item.PublicID, ActorID: &actorID, AIJobID: &jobID, AIJobItemID: &itemID, MetadataJSON: metadata}
		if err := tx.Create(&audit).Error; err != nil {
			return fmt.Errorf("audit AI text regeneration: %w", err)
		}
		document, err = documentFromPersistedJob(tx, models.AIJob{ID: job.ID})
		return err
	})
	return document, err
}

func (s *JobService) Create(ctx context.Context, input CreateJobInput) (JobDocument, error) {
	normalized, requestHash, err := normalizeCreateJobInput(input)
	if err != nil {
		return JobDocument{}, err
	}
	var result JobDocument
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if existing, found, err := findIdempotentJob(tx.Clauses(clause.Locking{Strength: "UPDATE"}), normalized.CreatedByID, normalized.IdempotencyKey); err != nil {
			return err
		} else if found {
			if existing.RequestSHA256 != requestHash {
				return ErrIdempotencyConflict
			}
			result, err = documentFromPersistedJob(tx, existing)
			if err == nil {
				result.Replayed = true
			}
			return err
		}
		sku, err := loadJobSKU(tx, normalized.SKUID)
		if err != nil {
			return err
		}
		version, template, err := loadPublishedTemplateVersion(tx.Clauses(clause.Locking{Strength: "UPDATE"}), normalized.TemplateVersionPublicID)
		if err != nil {
			return err
		}
		selectedSlots, err := selectJobSlots(version.Slots, normalized.SelectedSlotKeys)
		if err != nil {
			return err
		}
		if requiresCompatibleDeviceModel(selectedSlots) && strings.TrimSpace(sku.CompatibleDeviceModel) == "" {
			return ErrCompatibleDeviceModelRequired
		}
		if err := validateUserPreference(selectedSlots, normalized.UserPreference); err != nil {
			return err
		}
		if err := validateGenerationOverrides(selectedSlots, normalized.GenerationOverrides); err != nil {
			return err
		}
		if len(normalized.SelectedStyleReferenceIDs) > 0 && !hasImageSlot(selectedSlots) {
			return ErrStyleReferenceNotEligible
		}
		if len(normalized.SelectedBrandIconIDs) > 0 && !hasImageSlot(selectedSlots) {
			return ErrBrandIconNotEligible
		}
		referenceSOPs, externalReferences, err := loadExternalReferences(tx, sku.Product.CategoryID, normalized.SelectedReferenceItemIDs)
		if err != nil {
			return err
		}
		if !hasImageSlot(selectedSlots) {
			for _, reference := range externalReferences {
				if reference.Purpose != models.AIReferenceCopyInspiration {
					return ErrExternalReferenceNotEligible
				}
			}
		}
		resolvedCanvases, err := resolveImageCanvases(selectedSlots, normalized.ImageCanvases)
		if err != nil {
			return err
		}
		assets, assetIDs, err := loadEligibleAssets(tx.Clauses(clause.Locking{Strength: "UPDATE"}), sku.ID, normalized.SelectedAssetIDs)
		if err != nil {
			return err
		}
		if err := validateImageSlotAssets(selectedSlots, assets); err != nil {
			return err
		}
		sop, captureSOPPublicID, err := loadPublishedSOP(tx.Clauses(clause.Locking{Strength: "UPDATE"}), sku.Product.CategoryID)
		if err != nil {
			return err
		}
		locale := normalized.Locale
		outputLocales := append([]string(nil), normalized.OutputLocales...)
		orderedCanvases := imageCanvasFacts(resolvedCanvases)
		styleReferences := []StyleReferenceFacts{}
		structureReferences := []StructureReferenceFacts{}
		brandIcons := []BrandIconFacts{}
		if hasImageSlot(selectedSlots) {
			brandIcons, err = loadBrandIcons(tx, sku.Product.BrandID, normalized.SelectedBrandIconIDs)
			if err != nil {
				return err
			}
			styleReferences, err = loadStyleReferences(tx, normalized.SelectedStyleReferenceIDs)
			if err != nil {
				return err
			}
			structureReferences, err = loadStructureReferences(tx, sku.ID)
			if err != nil {
				return err
			}
		}
		snapshot := makeProductSnapshot(sku, sop, captureSOPPublicID, template, version, selectedSlots, assets, locale, outputLocales, normalized.UserPreference, normalized.GenerationOverrides, orderedCanvases)
		snapshot.StyleReferences = styleReferences
		snapshot.StructureReferences = structureReferences
		snapshot.BrandIcons = brandIcons
		snapshot.ReferenceSOPs = referenceSOPs
		snapshot.ExternalReferences = externalReferences
		snapshotJSON, err := json.Marshal(snapshot)
		if err != nil {
			return fmt.Errorf("marshal AI job snapshot: %w", err)
		}
		creatorSnapshot, modelSnapshot, err := loadJobAuditSnapshots(tx, normalized.CreatedByID)
		if err != nil {
			return err
		}
		creatorJSON, err := json.Marshal(creatorSnapshot)
		if err != nil {
			return err
		}
		modelJSON, err := json.Marshal(modelSnapshot)
		if err != nil {
			return err
		}
		key := normalized.IdempotencyKey
		outputLocalesJSON, err := json.Marshal(outputLocales)
		if err != nil {
			return err
		}
		job := models.AIJob{PublicID: uuid.NewString(), SKUID: sku.ID, AIContentTemplateVersionID: version.ID, TargetPlatform: template.TargetPlatform, Locale: locale, OutputLocalesJSON: outputLocalesJSON, Status: models.AIJobQueued, SnapshotSchema: ProductSnapshotSchemaV2, InputSnapshotJSON: snapshotJSON, CreatedBySnapshotJSON: creatorJSON, ModelSnapshotJSON: modelJSON, CreatedByID: normalized.CreatedByID, IdempotencyKey: &key, RequestSHA256: requestHash}
		if err := tx.Create(&job).Error; err != nil {
			return fmt.Errorf("create AI job: %w", err)
		}
		items := make([]models.AIJobItem, 0, len(selectedSlots))
		informationAssetIDs := make([]string, 0)
		for _, asset := range assets {
			if asset.SOPView.PresetKey == "supplemental_info" {
				informationAssetIDs = append(informationAssetIDs, asset.PublicID)
			}
		}
		for _, slot := range selectedSlots {
			if slot.Kind == models.AIContentSlotImage && len(resolvedCanvases) > 0 {
				continue
			}
			facts := slotFacts(slot)
			slotSnapshot, err := json.Marshal(facts)
			if err != nil {
				return fmt.Errorf("marshal AI job slot: %w", err)
			}
			ids := []string{}
			if slot.Kind == models.AIContentSlotImage {
				ids = assetIDs
			} else {
				ids = informationAssetIDs
			}
			idsJSON, err := json.Marshal(ids)
			if err != nil {
				return fmt.Errorf("marshal AI job assets: %w", err)
			}
			item := models.AIJobItem{PublicID: uuid.NewString(), AIJobID: job.ID, AIContentSlotID: slot.ID, SlotKey: slot.SlotKey, SlotSnapshotJSON: slotSnapshot, Kind: slot.Kind, Status: models.AIJobItemQueued, SelectedInputAssetIDsJSON: idsJSON}
			if err := tx.Create(&item).Error; err != nil {
				return fmt.Errorf("create AI job item: %w", err)
			}
			items = append(items, item)
		}
		for index, canvas := range resolvedCanvases {
			anchor := canvas.Slots[0]
			facts := slotFacts(anchor)
			facts.CanvasKey = canvas.CanvasKey
			facts.CanvasGeneration = canvas.GenerationOverride
			facts.CompositeRequirements = make([]SlotFacts, 0, len(canvas.Slots))
			for _, requirement := range canvas.Slots {
				facts.CompositeRequirements = append(facts.CompositeRequirements, slotFacts(requirement))
			}
			facts.Name = canvasSlotName(index+1, canvas.Slots)
			slotSnapshot, err := json.Marshal(facts)
			if err != nil {
				return fmt.Errorf("marshal AI image canvas: %w", err)
			}
			idsJSON, err := json.Marshal(assetIDs)
			if err != nil {
				return fmt.Errorf("marshal AI canvas assets: %w", err)
			}
			item := models.AIJobItem{PublicID: uuid.NewString(), AIJobID: job.ID, AIContentSlotID: anchor.ID, SlotKey: anchor.SlotKey, SlotSnapshotJSON: slotSnapshot, Kind: models.AIContentSlotImage, Status: models.AIJobItemQueued, SelectedInputAssetIDsJSON: idsJSON}
			if err := tx.Create(&item).Error; err != nil {
				return fmt.Errorf("create AI canvas item: %w", err)
			}
			items = append(items, item)
		}
		job.Items = items
		job.SKU = sku
		metadata, _ := json.Marshal(map[string]any{"snapshot_schema": ProductSnapshotSchemaV2, "output_locales": outputLocales, "slot_keys": normalized.SelectedSlotKeys, "asset_count": len(assetIDs), "brand_icon_ids": normalized.SelectedBrandIconIDs, "style_reference_ids": normalized.SelectedStyleReferenceIDs, "external_reference_ids": normalized.SelectedReferenceItemIDs, "structure_reference_count": len(structureReferences), "request_sha256": requestHash, "image_canvases": orderedCanvases, "created_by": creatorSnapshot, "model_snapshot": modelSnapshot})
		jobID, actorID := job.ID, normalized.CreatedByID
		audit := models.AIAuditEvent{PublicID: uuid.NewString(), EventType: "ai_job.created", EntityType: "ai_job", EntityPublicID: job.PublicID, ActorID: &actorID, AIJobID: &jobID, MetadataJSON: metadata}
		if err := tx.Create(&audit).Error; err != nil {
			return fmt.Errorf("audit AI job creation: %w", err)
		}
		result = jobDocument(job, version.PublicID)
		return nil
	})
	if err == nil || errors.Is(err, ErrIdempotencyConflict) {
		return result, err
	}
	// A concurrent creator may win the unique actor/key race after this
	// transaction rolls back. Re-read and compare the canonical request hash.
	if doc, recovered, recoveryErr := s.recoverIdempotentCreate(ctx, normalized, requestHash); recovered {
		return doc, recoveryErr
	}
	return JobDocument{}, err
}

func hasImageSlot(slots []models.AIContentSlot) bool {
	for _, slot := range slots {
		if slot.Kind == models.AIContentSlotImage {
			return true
		}
	}
	return false
}

func (s *JobService) recoverIdempotentCreate(ctx context.Context, input CreateJobInput, requestHash string) (JobDocument, bool, error) {
	existing, found, lookupErr := findIdempotentJob(s.db.WithContext(ctx), input.CreatedByID, input.IdempotencyKey)
	if lookupErr != nil || !found {
		return JobDocument{}, false, nil
	}
	if existing.RequestSHA256 != requestHash {
		return JobDocument{}, true, ErrIdempotencyConflict
	}
	doc, err := documentFromPersistedJob(s.db.WithContext(ctx), existing)
	if err != nil {
		return JobDocument{}, true, err
	}
	doc.Replayed = true
	return doc, true, nil
}

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)
var localePattern = regexp.MustCompile(`^[A-Za-z]{2,3}(?:-[A-Za-z0-9]{2,8})*$`)
var canvasKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,79}$`)

func normalizeCreateJobInput(input CreateJobInput) (CreateJobInput, string, error) {
	legacyLocaleRequest := len(input.OutputLocales) == 0
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if !idempotencyKeyPattern.MatchString(input.IdempotencyKey) {
		return input, "", ErrIdempotencyKeyInvalid
	}
	parsedTemplateID, err := uuid.Parse(strings.TrimSpace(input.TemplateVersionPublicID))
	if err != nil {
		return input, "", ErrTemplateVersionIDInvalid
	}
	input.TemplateVersionPublicID = parsedTemplateID.String()
	parsedSKUID, err := uuid.Parse(strings.TrimSpace(input.SKUID))
	if err != nil || parsedSKUID == uuid.Nil {
		return input, "", ErrSKUIDInvalid
	}
	input.SKUID = parsedSKUID.String()
	input.Locale = strings.TrimSpace(input.Locale)
	if len(input.OutputLocales) == 0 {
		locale, err := normalizeLocale(input.Locale)
		if err != nil {
			return input, "", err
		}
		input.Locale, input.OutputLocales = locale, []string{locale}
	} else {
		if !validOutputLocales(input.OutputLocales) {
			return input, "", ErrOutputLocalesInvalid
		}
		input.OutputLocales = append([]string(nil), input.OutputLocales...)
		if input.Locale != "" {
			locale, err := normalizeLocale(input.Locale)
			if err != nil || locale != input.OutputLocales[0] {
				return input, "", ErrOutputLocalesInvalid
			}
		}
		input.Locale = input.OutputLocales[0]
	}
	input.UserPreference = strings.TrimSpace(input.UserPreference)
	if utf8.RuneCountInString(input.UserPreference) > 1000 {
		return input, "", ErrUserPreferenceInvalid
	}
	slots := append([]string(nil), input.SelectedSlotKeys...)
	sort.Strings(slots)
	assets := make([]string, 0, len(input.SelectedAssetIDs))
	for _, value := range input.SelectedAssetIDs {
		parsed, err := uuid.Parse(strings.TrimSpace(value))
		if err != nil || parsed == uuid.Nil {
			return input, "", ErrAssetIDInvalid
		}
		assets = append(assets, parsed.String())
	}
	sort.Strings(assets)
	assets = dedupeStrings(assets)
	input.SelectedAssetIDs = assets
	styles := make([]string, 0, len(input.SelectedStyleReferenceIDs))
	for _, value := range input.SelectedStyleReferenceIDs {
		parsed, err := uuid.Parse(strings.TrimSpace(value))
		if err != nil || parsed == uuid.Nil {
			return input, "", ErrStyleReferenceNotEligible
		}
		styles = append(styles, parsed.String())
	}
	sort.Strings(styles)
	input.SelectedStyleReferenceIDs = dedupeStrings(styles)
	brandIcons := make([]string, 0, len(input.SelectedBrandIconIDs))
	for _, value := range input.SelectedBrandIconIDs {
		parsed, err := uuid.Parse(strings.TrimSpace(value))
		if err != nil || parsed == uuid.Nil {
			return input, "", ErrBrandIconNotEligible
		}
		brandIcons = append(brandIcons, parsed.String())
	}
	sort.Strings(brandIcons)
	input.SelectedBrandIconIDs = dedupeStrings(brandIcons)
	if len(input.SelectedBrandIconIDs) > 8 {
		return input, "", ErrBrandIconNotEligible
	}
	references := make([]string, 0, len(input.SelectedReferenceItemIDs))
	for _, value := range input.SelectedReferenceItemIDs {
		parsed, err := uuid.Parse(strings.TrimSpace(value))
		if err != nil || parsed == uuid.Nil {
			return input, "", ErrExternalReferenceNotEligible
		}
		references = append(references, parsed.String())
	}
	sort.Strings(references)
	input.SelectedReferenceItemIDs = dedupeStrings(references)
	if input.GenerationOverrides == nil {
		input.GenerationOverrides = map[string]GenerationOverride{}
	}
	if len(input.ImageCanvases) > 20 {
		return input, "", ErrSlotSelectionInvalid
	}
	seenCanvasKeys := make(map[string]struct{}, len(input.ImageCanvases))
	for index := range input.ImageCanvases {
		canvas := &input.ImageCanvases[index]
		canvas.CanvasKey = strings.TrimSpace(canvas.CanvasKey)
		if !canvasKeyPattern.MatchString(canvas.CanvasKey) {
			return input, "", ErrSlotSelectionInvalid
		}
		if _, duplicate := seenCanvasKeys[canvas.CanvasKey]; duplicate {
			return input, "", ErrSlotSelectionInvalid
		}
		seenCanvasKeys[canvas.CanvasKey] = struct{}{}
		keys := append([]string(nil), canvas.SlotKeys...)
		for keyIndex := range keys {
			keys[keyIndex] = strings.TrimSpace(keys[keyIndex])
			if keys[keyIndex] == "" {
				return input, "", ErrSlotSelectionInvalid
			}
		}
		sort.Strings(keys)
		if len(keys) == 0 || len(dedupeStrings(keys)) != len(keys) {
			return input, "", ErrSlotSelectionInvalid
		}
		canvas.SlotKeys = keys
	}
	canonical := struct {
		SKUID              string                        `json:"sku_id"`
		Template           string                        `json:"template_version_id"`
		Slots              []string                      `json:"selected_slot_keys"`
		Assets             []string                      `json:"selected_asset_ids"`
		StyleReferences    []string                      `json:"selected_style_reference_ids,omitempty"`
		BrandIcons         []string                      `json:"selected_brand_icon_ids,omitempty"`
		ExternalReferences []string                      `json:"selected_reference_item_ids,omitempty"`
		Locale             string                        `json:"locale"`
		OutputLocales      []string                      `json:"output_locales,omitempty"`
		Preference         string                        `json:"user_preference"`
		Overrides          map[string]GenerationOverride `json:"generation_overrides"`
		Canvases           []ImageCanvas                 `json:"image_canvases,omitempty"`
	}{input.SKUID, input.TemplateVersionPublicID, slots, assets, input.SelectedStyleReferenceIDs, input.SelectedBrandIconIDs, input.SelectedReferenceItemIDs, input.Locale, func() []string {
		if legacyLocaleRequest {
			return nil
		}
		return input.OutputLocales
	}(), input.UserPreference, input.GenerationOverrides, input.ImageCanvases}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return input, "", err
	}
	digest := sha256.Sum256(encoded)
	return input, fmt.Sprintf("%x", digest[:]), nil
}

func normalizeLocale(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > 32 || !localePattern.MatchString(value) {
		return "", ErrLocaleInvalid
	}
	parts := strings.Split(value, "-")
	parts[0] = strings.ToLower(parts[0])
	if len(parts) > 1 && len(parts[1]) == 2 {
		parts[1] = strings.ToUpper(parts[1])
	}
	return strings.Join(parts, "-"), nil
}

func validOutputLocales(values []string) bool {
	if len(values) == 1 {
		return values[0] == "en" || values[0] == "zh-CN"
	}
	return len(values) == 2 && values[0] == "en" && values[1] == "zh-CN"
}

func dedupeStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func findIdempotentJob(db *gorm.DB, actorID uint, key string) (models.AIJob, bool, error) {
	var job models.AIJob
	err := db.Where("created_by_id = ? AND idempotency_key = ?", actorID, key).First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return job, false, nil
	}
	return job, err == nil, err
}

func documentFromPersistedJob(db *gorm.DB, job models.AIJob) (JobDocument, error) {
	if err := db.Preload("SKU").Preload("Items", func(q *gorm.DB) *gorm.DB { return q.Order("created_at ASC, id ASC") }).Preload("Items.Executions", func(q *gorm.DB) *gorm.DB { return q.Order("attempt_number ASC, id ASC") }).First(&job, job.ID).Error; err != nil {
		return JobDocument{}, err
	}
	ids, err := versionPublicIDs(db, []uint{job.AIContentTemplateVersionID})
	if err != nil {
		return JobDocument{}, err
	}
	doc := jobDocument(job, ids[job.AIContentTemplateVersionID])
	if err := enrichCostDocument(db, job.ID, &doc); err != nil {
		return JobDocument{}, err
	}
	return doc, nil
}

func (s *JobService) List(ctx context.Context) ([]JobDocument, error) {
	return s.ListFiltered(ctx, JobListFilters{})
}

func (s *JobService) ListFiltered(ctx context.Context, filters JobListFilters) ([]JobDocument, error) {
	var jobs []models.AIJob
	query := s.db.WithContext(ctx).Preload("SKU").Preload("Items", func(db *gorm.DB) *gorm.DB { return db.Order("created_at ASC, id ASC") }).Preload("Items.Executions", func(db *gorm.DB) *gorm.DB { return db.Order("attempt_number ASC, id ASC") })
	if value := strings.TrimSpace(filters.CreatedBy); value != "" {
		query = query.Where("LOWER(created_by_snapshot_json) LIKE ?", "%"+strings.ToLower(value)+"%")
	}
	if value := strings.TrimSpace(filters.Model); value != "" {
		query = query.Where("model_snapshot_json LIKE ?", "%"+value+"%")
	}
	if value := strings.TrimSpace(filters.APIMode); value != "" {
		query = query.Where("model_snapshot_json LIKE ?", "%\"image_api_mode\":\""+value+"\"%")
	}
	if err := query.Order("created_at DESC, id DESC").Find(&jobs).Error; err != nil {
		return nil, err
	}
	versionIDs := make([]uint, 0, len(jobs))
	for _, job := range jobs {
		versionIDs = append(versionIDs, job.AIContentTemplateVersionID)
	}
	publicIDs, err := versionPublicIDs(s.db.WithContext(ctx), versionIDs)
	if err != nil {
		return nil, err
	}
	result := make([]JobDocument, 0, len(jobs))
	for _, job := range jobs {
		doc := jobDocument(job, publicIDs[job.AIContentTemplateVersionID])
		if err := enrichCostDocument(s.db.WithContext(ctx), job.ID, &doc); err != nil {
			return nil, err
		}
		result = append(result, doc)
	}
	return result, nil
}

func (s *JobService) Get(ctx context.Context, publicID string) (JobDocument, error) {
	var job models.AIJob
	if err := s.db.WithContext(ctx).Preload("SKU").Preload("Items", func(db *gorm.DB) *gorm.DB { return db.Order("created_at ASC, id ASC") }).Preload("Items.Executions", func(db *gorm.DB) *gorm.DB { return db.Order("attempt_number ASC, id ASC") }).Where("public_id = ?", publicID).First(&job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return JobDocument{}, ErrJobNotFound
		}
		return JobDocument{}, err
	}
	ids, err := versionPublicIDs(s.db.WithContext(ctx), []uint{job.AIContentTemplateVersionID})
	if err != nil {
		return JobDocument{}, err
	}
	doc := jobDocument(job, ids[job.AIContentTemplateVersionID])
	if err := enrichCostDocument(s.db.WithContext(ctx), job.ID, &doc); err != nil {
		return JobDocument{}, err
	}
	return doc, nil
}

// enrichCostDocument applies only the latest immutable allocation snapshot for
// each supplier bucket. It intentionally labels the value as reconciled rather
// than as a request-level invoice amount.
func enrichCostDocument(db *gorm.DB, jobID uint, doc *JobDocument) error {
	if !db.Migrator().HasTable(&models.AIReconciliationAllocation{}) {
		return nil
	}
	var rows []models.AIReconciliationAllocation
	if err := db.Where("ai_job_id = ?", jobID).Order("open_ai_cost_bucket_id ASC, version ASC, id ASC").Find(&rows).Error; err != nil {
		return err
	}
	latest := map[uint]models.AIReconciliationAllocation{}
	for _, row := range rows {
		latest[row.OpenAICostBucketID] = row
	}
	if len(latest) == 0 {
		return nil
	}
	total := money.Must("0")
	for _, row := range latest {
		total.Add(total, money.Must(row.AllocatedAmountUSD))
	}
	doc.ReconciledAmountUSD = money.Format(total)
	doc.ReconciliationStatus = "reconciled_allocation"
	return nil
}

func loadJobSKU(tx *gorm.DB, publicID string) (models.SKU, error) {
	var sku models.SKU
	err := tx.Preload("Product.CatalogCategory").Preload("Product.BrandRecord").Preload("Tags", func(db *gorm.DB) *gorm.DB { return db.Order("tags.name ASC, tags.id ASC") }).Where("public_id = ?", publicID).First(&sku).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return sku, ErrSKUNotFound
	}
	return sku, err
}

func loadPublishedTemplateVersion(tx *gorm.DB, publicID string) (models.AIContentTemplateVersion, models.AIContentTemplate, error) {
	var version models.AIContentTemplateVersion
	if err := tx.Preload("Slots", func(db *gorm.DB) *gorm.DB { return db.Order("sequence ASC, id ASC") }).Where("public_id = ?", publicID).First(&version).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return version, models.AIContentTemplate{}, ErrTemplateVersionNotFound
		}
		return version, models.AIContentTemplate{}, err
	}
	if version.Status != models.AITemplatePublished {
		return version, models.AIContentTemplate{}, ErrTemplateVersionNotPublished
	}
	var template models.AIContentTemplate
	if err := tx.Session(&gorm.Session{NewDB: true}).First(&template, version.AIContentTemplateID).Error; err != nil {
		return version, template, err
	}
	return version, template, nil
}

func selectJobSlots(slots []models.AIContentSlot, keys []string) ([]models.AIContentSlot, error) {
	if len(keys) == 0 {
		return nil, ErrSlotSelectionInvalid
	}
	requested := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if key == "" {
			return nil, ErrSlotSelectionInvalid
		}
		if _, exists := requested[key]; exists {
			return nil, ErrSlotSelectionInvalid
		}
		requested[key] = struct{}{}
	}
	selected := make([]models.AIContentSlot, 0, len(keys))
	for _, slot := range slots {
		if _, ok := requested[slot.SlotKey]; ok {
			selected = append(selected, slot)
			delete(requested, slot.SlotKey)
		}
	}
	if len(requested) != 0 {
		return nil, ErrSlotSelectionInvalid
	}
	return selected, nil
}

func loadEligibleAssets(tx *gorm.DB, skuID uint, requested []string) ([]models.Asset, []string, error) {
	set := make(map[string]struct{}, len(requested))
	for _, id := range requested {
		set[id] = struct{}{}
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return []models.Asset{}, []string{}, nil
	}
	var assets []models.Asset
	if err := tx.Preload("SOPView").Where("public_id IN ?", ids).Where(&models.Asset{SKUID: skuID, ReviewStatus: "approved"}).Where("origin_type <> ?", "ai_generated").Order("public_id ASC").Find(&assets).Error; err != nil {
		return nil, nil, err
	}
	if len(assets) != len(ids) {
		return nil, nil, ErrAssetNotEligible
	}
	return assets, ids, nil
}

func loadStyleReferences(tx *gorm.DB, requested []string) ([]StyleReferenceFacts, error) {
	if len(requested) == 0 {
		return []StyleReferenceFacts{}, nil
	}
	var grants []models.StyleReferenceGrant
	if err := tx.Preload("Asset.SKU").Where("public_id IN ? AND status = ?", requested, "approved").Order("public_id").Find(&grants).Error; err != nil {
		return nil, err
	}
	if len(grants) != len(requested) {
		return nil, ErrStyleReferenceNotEligible
	}
	users, err := publicUserIDs(tx, reviewerIDsFromStyleGrants(grants))
	if err != nil {
		return nil, err
	}
	result := make([]StyleReferenceFacts, 0, len(grants))
	for _, grant := range grants {
		if grant.DerivativeObjectKey == "" || grant.DerivativeSHA256 == "" {
			return nil, ErrStyleReferenceNotEligible
		}
		result = append(result, StyleReferenceFacts{PublicID: grant.PublicID, Version: grant.Version, SourceSKUPublicID: grant.Asset.SKU.PublicID, Description: LocalizedNameFacts{ZH: grant.DescriptionZH, EN: grant.DescriptionEN}, DerivativeSHA256: grant.DerivativeSHA256, ReviewedByPublicID: users[grant.ReviewedByID]})
	}
	return result, nil
}

func loadBrandIcons(tx *gorm.DB, brandID *uint, requested []string) ([]BrandIconFacts, error) {
	if len(requested) == 0 {
		return []BrandIconFacts{}, nil
	}
	if brandID == nil || len(requested) > 8 {
		return nil, ErrBrandIconNotEligible
	}
	var icons []models.BrandIcon
	if err := tx.Where("public_id IN ? AND brand_id = ? AND status = ?", requested, *brandID, "active").Order("sort_order ASC, public_id ASC").Find(&icons).Error; err != nil {
		return nil, err
	}
	if len(icons) != len(requested) {
		return nil, ErrBrandIconNotEligible
	}
	result := make([]BrandIconFacts, 0, len(icons))
	for _, icon := range icons {
		if icon.ObjectKey == "" || icon.SHA256 == "" {
			return nil, ErrBrandIconNotEligible
		}
		result = append(result, BrandIconFacts{PublicID: icon.PublicID, Name: icon.Name, Notes: icon.Notes, MIMEType: icon.MIMEType, Width: icon.Width, Height: icon.Height, ByteCount: icon.ByteCount, SHA256: icon.SHA256})
	}
	return result, nil
}

func loadExternalReferences(tx *gorm.DB, categoryID uint, requested []string) ([]ReferenceSOPFacts, []ExternalReferenceFacts, error) {
	if len(requested) == 0 {
		return []ReferenceSOPFacts{}, []ExternalReferenceFacts{}, nil
	}
	var items []models.AIReferenceItem
	if err := tx.Where("public_id IN ?", requested).Order("public_id").Find(&items).Error; err != nil || len(items) != len(requested) {
		return nil, nil, ErrExternalReferenceNotEligible
	}
	versionIDs := make([]uint, 0, len(items))
	for _, item := range items {
		versionIDs = append(versionIDs, item.AIReferenceSOPVersionID)
	}
	var versions []models.AIReferenceSOPVersion
	if err := tx.Where("id IN ? AND status = ?", versionIDs, models.SOPVersionPublished).Find(&versions).Error; err != nil {
		return nil, nil, err
	}
	versionsByID := make(map[uint]models.AIReferenceSOPVersion, len(versions))
	sopIDs := make([]uint, 0, len(versions))
	publisherIDs := make([]uint, 0, len(versions))
	for _, version := range versions {
		versionsByID[version.ID] = version
		sopIDs = append(sopIDs, version.AIReferenceSOPID)
		if version.PublishedByID != nil {
			publisherIDs = append(publisherIDs, *version.PublishedByID)
		}
	}
	var sops []models.AIReferenceSOP
	if err := tx.Where("id IN ? AND category_id = ?", sopIDs, categoryID).Find(&sops).Error; err != nil {
		return nil, nil, err
	}
	sopsByID := make(map[uint]models.AIReferenceSOP, len(sops))
	for _, sop := range sops {
		sopsByID[sop.ID] = sop
	}
	users, err := publicUserIDs(tx, publisherIDs)
	if err != nil {
		return nil, nil, err
	}
	frozenSOPs := make([]ReferenceSOPFacts, 0)
	seenVersions := map[uint]bool{}
	references := make([]ExternalReferenceFacts, 0, len(items))
	for _, item := range items {
		version, ok := versionsByID[item.AIReferenceSOPVersionID]
		if !ok {
			return nil, nil, ErrExternalReferenceNotEligible
		}
		sop, ok := sopsByID[version.AIReferenceSOPID]
		if !ok || !validExternalReferencePurpose(item.Purpose) || item.ObjectKey == "" || item.SHA256 == "" || !item.RightsConfirmed {
			return nil, nil, ErrExternalReferenceNotEligible
		}
		reviewer := ""
		if version.PublishedByID != nil {
			reviewer = users[*version.PublishedByID]
		}
		if !seenVersions[version.ID] {
			frozenSOPs = append(frozenSOPs, ReferenceSOPFacts{PublicID: sop.PublicID, VersionPublicID: version.PublicID, VersionNumber: version.VersionNumber, CategoryID: sop.CategoryID, Name: LocalizedNameFacts{ZH: version.NameZH, EN: version.NameEN}, Description: LocalizedNameFacts{ZH: version.DescriptionZH, EN: version.DescriptionEN}})
			seenVersions[version.ID] = true
		}
		references = append(references, ExternalReferenceFacts{PublicID: item.PublicID, SOPPublicID: sop.PublicID, VersionPublicID: version.PublicID, Purpose: item.Purpose, Caption: LocalizedNameFacts{ZH: item.CaptionZH, EN: item.CaptionEN}, AllowedGuidance: LocalizedNameFacts{ZH: item.AllowedGuidanceZH, EN: item.AllowedGuidanceEN}, ForbiddenGuidance: LocalizedNameFacts{ZH: item.ForbiddenGuidanceZH, EN: item.ForbiddenGuidanceEN}, SourceName: item.SourceName, SourceURL: item.SourceURL, SHA256: item.SHA256, ReviewedByPublicID: reviewer})
	}
	sort.Slice(frozenSOPs, func(i, j int) bool { return frozenSOPs[i].VersionPublicID < frozenSOPs[j].VersionPublicID })
	return frozenSOPs, references, nil
}

func validExternalReferencePurpose(value models.AIReferencePurpose) bool {
	return value == models.AIReferenceVisualStyle || value == models.AIReferenceUsageEffect || value == models.AIReferenceCopyInspiration
}

func reviewerIDsFromStyleGrants(values []models.StyleReferenceGrant) []uint {
	ids := make([]uint, 0, len(values))
	for _, value := range values {
		ids = append(ids, value.ReviewedByID)
	}
	return ids
}
func publicUserIDs(tx *gorm.DB, ids []uint) (map[uint]string, error) {
	result := map[uint]string{}
	if len(ids) == 0 {
		return result, nil
	}
	var rows []struct {
		ID       uint
		PublicID string
	}
	if err := tx.Unscoped().Model(&models.User{}).Select("id", "public_id").Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.ID] = row.PublicID
	}
	return result, nil
}

func loadStructureReferences(tx *gorm.DB, targetSKUID uint) ([]StructureReferenceFacts, error) {
	var membership models.ModelFamilyMember
	if err := tx.Where("sk_uid = ? AND removed_at IS NULL", targetSKUID).First(&membership).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return []StructureReferenceFacts{}, nil
	} else if err != nil {
		return nil, err
	}
	var references []models.ModelFamilyReferenceAsset
	if err := tx.Preload("Asset.SKU").Preload("ModelFamily").Where("model_family_id = ? AND status = ?", membership.ModelFamilyID, "approved").Order("public_id").Find(&references).Error; err != nil {
		return nil, err
	}
	ids := make([]uint, 0, len(references))
	for _, value := range references {
		ids = append(ids, value.ReviewedByID)
	}
	users, err := publicUserIDs(tx, ids)
	if err != nil {
		return nil, err
	}
	result := make([]StructureReferenceFacts, 0, len(references))
	for _, reference := range references {
		if reference.Role != "geometry_only" && reference.Role != "viewpoint_only" && reference.Role != "detail_geometry" {
			continue
		}
		if reference.DerivativeObjectKey == "" || reference.DerivativeSHA256 == "" {
			continue
		}
		result = append(result, StructureReferenceFacts{PublicID: reference.PublicID, Version: reference.Version, SourceSKUPublicID: reference.Asset.SKU.PublicID, ModelFamilyPublicID: reference.ModelFamily.PublicID, Role: reference.Role, AllowedAttributes: cloneJSON(reference.AllowedAttributesJSON), ForbiddenAttributes: cloneJSON(reference.ForbiddenAttributesJSON), DerivativeSHA256: reference.DerivativeSHA256, ReviewedByPublicID: users[reference.ReviewedByID]})
	}
	return result, nil
}

func validateImageSlotAssets(slots []models.AIContentSlot, assets []models.Asset) error {
	availableViews := make(map[string]struct{}, len(assets))
	visualCount := 0
	for _, asset := range assets {
		// Approved AI output is a publishable channel asset, never identity or
		// factual evidence for a later generation. Requiring a real anchor here
		// prevents recursive product drift before any provider request is made.
		if asset.OriginType == "ai_generated" {
			continue
		}
		if asset.SOPView.PresetKey == "supplemental_info" {
			continue
		}
		visualCount++
		availableViews[asset.SOPView.PresetKey] = struct{}{}
	}
	for _, slot := range slots {
		if slot.Kind != models.AIContentSlotImage {
			continue
		}
		if visualCount == 0 {
			return ErrAssetNotEligible
		}
		var constraints map[string]json.RawMessage
		if err := json.Unmarshal(slot.ConstraintsJSON, &constraints); err != nil || constraints == nil {
			return ErrPublishedTemplateConfigInvalid
		}
		if raw, exists := constraints["required_views"]; exists {
			var required []string
			if string(raw) == "null" || json.Unmarshal(raw, &required) != nil || required == nil {
				return ErrPublishedTemplateConfigInvalid
			}
			for _, view := range required {
				if strings.TrimSpace(view) == "" {
					return ErrPublishedTemplateConfigInvalid
				}
				if _, ok := availableViews[view]; !ok {
					return ErrAssetNotEligible
				}
			}
		}
	}
	return nil
}

func validateGenerationOverrides(slots []models.AIContentSlot, overrides map[string]GenerationOverride) error {
	byKey := map[string]models.AIContentSlot{}
	for _, slot := range slots {
		byKey[slot.SlotKey] = slot
	}
	for key, override := range overrides {
		slot, ok := byKey[key]
		if !ok {
			return ErrGenerationOverrideInvalid
		}
		var config map[string]any
		if json.Unmarshal(slot.GenerationConfigJSON, &config) != nil || config == nil {
			return ErrPublishedTemplateConfigInvalid
		}
		if slot.Kind != models.AIContentSlotImage && (override.Size != nil || override.Quality != nil || override.Style != nil) {
			return ErrGenerationOverrideInvalid
		}
		if override.CandidateCount != nil {
			if *override.CandidateCount < 1 || *override.CandidateCount > 4 {
				return ErrGenerationOverrideInvalid
			}
			if !validRuntimeNumberList(config["allowed_candidate_count"], 1, 4) {
				return ErrPublishedTemplateConfigInvalid
			}
			if !allowedNumber(config["allowed_candidate_count"], float64(*override.CandidateCount)) {
				return ErrGenerationOverrideInvalid
			}
		}
		if override.Size != nil {
			if !supportedImageSize(*override.Size) {
				return ErrGenerationOverrideInvalid
			}
			if !validRuntimeStringList(config["allowed_sizes"], func(v string) bool { return supportedImageSize(v) }) {
				return ErrPublishedTemplateConfigInvalid
			}
			if !allowedString(config["allowed_sizes"], *override.Size) {
				return ErrGenerationOverrideInvalid
			}
		}
		if override.Quality != nil {
			if !supportedQuality(*override.Quality) {
				return ErrGenerationOverrideInvalid
			}
			if !validRuntimeStringList(config["allowed_qualities"], supportedQuality) {
				return ErrPublishedTemplateConfigInvalid
			}
			if !allowedString(config["allowed_qualities"], *override.Quality) {
				return ErrGenerationOverrideInvalid
			}
		}
		if override.Style != nil {
			if strings.TrimSpace(*override.Style) != *override.Style || len(*override.Style) == 0 || utf8.RuneCountInString(*override.Style) > 80 {
				return ErrGenerationOverrideInvalid
			}
			if !validRuntimeStringList(config["allowed_styles"], func(v string) bool { return strings.TrimSpace(v) == v && v != "" && utf8.RuneCountInString(v) <= 80 }) {
				return ErrPublishedTemplateConfigInvalid
			}
			if !allowedString(config["allowed_styles"], *override.Style) {
				return ErrGenerationOverrideInvalid
			}
		}
		if override.CandidateCount == nil && override.Size == nil && override.Quality == nil && override.Style == nil {
			return ErrGenerationOverrideInvalid
		}
	}
	return nil
}

type resolvedImageCanvas struct {
	CanvasKey          string
	Slots              []models.AIContentSlot
	GenerationOverride *GenerationOverride
}

func resolveImageCanvases(selected []models.AIContentSlot, canvases []ImageCanvas) ([]resolvedImageCanvas, error) {
	if len(canvases) == 0 {
		return nil, nil
	}
	selectedImageKeys := make(map[string]struct{})
	for _, slot := range selected {
		if slot.Kind == models.AIContentSlotImage {
			selectedImageKeys[slot.SlotKey] = struct{}{}
		}
	}
	covered := make(map[string]struct{}, len(selectedImageKeys))
	result := make([]resolvedImageCanvas, 0, len(canvases))
	for _, canvas := range canvases {
		wanted := make(map[string]struct{}, len(canvas.SlotKeys))
		for _, key := range canvas.SlotKeys {
			wanted[key] = struct{}{}
		}
		resolved := resolvedImageCanvas{CanvasKey: canvas.CanvasKey, GenerationOverride: canvas.GenerationOverride, Slots: make([]models.AIContentSlot, 0, len(wanted))}
		for _, slot := range selected {
			if _, ok := wanted[slot.SlotKey]; !ok {
				continue
			}
			if slot.Kind != models.AIContentSlotImage {
				return nil, ErrSlotSelectionInvalid
			}
			resolved.Slots = append(resolved.Slots, slot)
			covered[slot.SlotKey] = struct{}{}
			delete(wanted, slot.SlotKey)
		}
		if len(resolved.Slots) == 0 || len(wanted) != 0 {
			return nil, ErrSlotSelectionInvalid
		}
		if canvas.GenerationOverride != nil {
			if err := validateGenerationOverrides([]models.AIContentSlot{resolved.Slots[0]}, map[string]GenerationOverride{resolved.Slots[0].SlotKey: *canvas.GenerationOverride}); err != nil {
				return nil, err
			}
		}
		result = append(result, resolved)
	}
	if len(covered) != len(selectedImageKeys) {
		return nil, ErrSlotSelectionInvalid
	}
	return result, nil
}

func imageCanvasFacts(canvases []resolvedImageCanvas) []ImageCanvas {
	result := make([]ImageCanvas, 0, len(canvases))
	for _, canvas := range canvases {
		keys := make([]string, 0, len(canvas.Slots))
		for _, slot := range canvas.Slots {
			keys = append(keys, slot.SlotKey)
		}
		result = append(result, ImageCanvas{CanvasKey: canvas.CanvasKey, SlotKeys: keys, GenerationOverride: canvas.GenerationOverride})
	}
	return result
}

func canvasSlotName(number int, slots []models.AIContentSlot) LocalizedNameFacts {
	zh := make([]string, 0, len(slots))
	en := make([]string, 0, len(slots))
	for _, slot := range slots {
		zh = append(zh, slot.NameZH)
		en = append(en, slot.NameEN)
	}
	return LocalizedNameFacts{ZH: fmt.Sprintf("画布 %d：%s", number, strings.Join(zh, " + ")), EN: fmt.Sprintf("Canvas %d: %s", number, strings.Join(en, " + "))}
}

func validateUserPreference(slots []models.AIContentSlot, preference string) error {
	if preference == "" {
		return nil
	}
	for _, slot := range slots {
		var config map[string]any
		if json.Unmarshal(slot.GenerationConfigJSON, &config) != nil || config == nil {
			return ErrPublishedTemplateConfigInvalid
		}
		value, exists := config["allow_user_extra_prompt"]
		if !exists {
			return ErrUserPreferenceNotAllowed
		}
		allowed, ok := value.(bool)
		if !ok {
			return ErrPublishedTemplateConfigInvalid
		}
		if !allowed {
			return ErrUserPreferenceNotAllowed
		}
	}
	return nil
}
func supportedImageSize(value string) bool {
	switch value {
	case "1024x1024", "1536x1024", "1024x1536":
		return true
	}
	return false
}
func supportedQuality(value string) bool {
	switch value {
	case "low", "medium", "high", "auto":
		return true
	}
	return false
}
func validRuntimeNumberList(value any, min, max int) bool {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return false
	}
	seen := map[int]bool{}
	for _, item := range items {
		number, ok := item.(float64)
		integer := int(number)
		if !ok || number != float64(integer) || integer < min || integer > max || seen[integer] {
			return false
		}
		seen[integer] = true
	}
	return true
}
func validRuntimeStringList(value any, valid func(string) bool) bool {
	items, ok := value.([]any)
	if !ok || len(items) == 0 || len(items) > 20 {
		return false
	}
	seen := map[string]bool{}
	for _, item := range items {
		text, ok := item.(string)
		if !ok || !valid(text) || seen[text] {
			return false
		}
		seen[text] = true
	}
	return true
}
func allowedNumber(value any, want float64) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if number, ok := item.(float64); ok && number == want {
			return true
		}
	}
	return false
}
func allowedString(value any, want string) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if text, ok := item.(string); ok && text == want {
			return true
		}
	}
	return false
}

func loadPublishedSOP(tx *gorm.DB, categoryID uint) (models.SOPVersion, string, error) {
	var version models.SOPVersion
	err := tx.Preload("Views", func(db *gorm.DB) *gorm.DB { return db.Order("sequence ASC, id ASC") }).Joins("JOIN capture_sops ON capture_sops.id = sop_versions.capture_sop_id").Where("capture_sops.category_id = ? AND sop_versions.status = ?", categoryID, models.SOPVersionPublished).Order("sop_versions.published_at DESC, sop_versions.id DESC").First(&version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return version, "", ErrPublishedSOPNotFound
	}
	if err != nil {
		return version, "", err
	}
	var sop models.CaptureSOP
	if err := tx.Session(&gorm.Session{NewDB: true}).Select("public_id").First(&sop, version.CaptureSOPID).Error; err != nil {
		return version, "", err
	}
	return version, sop.PublicID, nil
}

func makeProductSnapshot(sku models.SKU, sop models.SOPVersion, captureSOPPublicID string, template models.AIContentTemplate, version models.AIContentTemplateVersion, slots []models.AIContentSlot, assets []models.Asset, locale string, outputLocales []string, preference string, overrides map[string]GenerationOverride, canvases []ImageCanvas) ProductSnapshotV1 {
	tags := make([]string, 0, len(sku.Tags))
	for _, tag := range sku.Tags {
		tags = append(tags, tag.Name)
	}
	views := make([]SOPViewFacts, 0, len(sop.Views))
	for _, view := range sop.Views {
		views = append(views, SOPViewFacts{PublicID: view.PublicID, Sequence: view.Sequence, Role: view.Role, ViewKind: view.ViewKind, PresetKey: view.PresetKey, Name: LocalizedNameFacts{ZH: view.NameZH, EN: view.NameEN}, Instruction: LocalizedNameFacts{ZH: view.InstructionZH, EN: view.InstructionEN}, Required: view.Required, AllowMultiple: view.AllowMultiple, CameraPositionDirection: VectorFacts{X: view.CameraPositionX, Y: view.CameraPositionY, Z: view.CameraPositionZ}, ImageUpDirection: VectorFacts{X: view.ImageUpX, Y: view.ImageUpY, Z: view.ImageUpZ}, Target: VectorFacts{X: view.TargetX, Y: view.TargetY, Z: view.TargetZ}, Composition: view.Composition})
	}
	assetFacts := make([]AssetFacts, 0, len(assets))
	for _, asset := range assets {
		view := asset.SOPView
		sourceType := AssetSourceProductVisual
		if view.PresetKey == "supplemental_info" {
			sourceType = AssetSourceProductInformation
		}
		assetFacts = append(assetFacts, AssetFacts{PublicID: asset.PublicID, SourceType: sourceType, MIMEType: asset.MIMEType, Width: asset.Width, Height: asset.Height, ByteCount: asset.ByteCount, SHA256: asset.SHA256, CapturedAt: asset.CapturedAt, View: AssetViewFacts{PublicID: view.PublicID, PresetKey: view.PresetKey, Name: LocalizedNameFacts{ZH: view.NameZH, EN: view.NameEN}, Role: view.Role, ViewKind: view.ViewKind, Instruction: LocalizedNameFacts{ZH: view.InstructionZH, EN: view.InstructionEN}, CameraPositionDirection: VectorFacts{X: view.CameraPositionX, Y: view.CameraPositionY, Z: view.CameraPositionZ}, ImageUpDirection: VectorFacts{X: view.ImageUpX, Y: view.ImageUpY, Z: view.ImageUpZ}, Target: VectorFacts{X: view.TargetX, Y: view.TargetY, Z: view.TargetZ}, Composition: view.Composition}})
	}
	selectedSlots := make([]SlotFacts, 0, len(slots))
	for _, slot := range slots {
		selectedSlots = append(selectedSlots, slotFacts(slot))
	}
	if overrides == nil {
		overrides = map[string]GenerationOverride{}
	}
	return ProductSnapshotV1{Schema: ProductSnapshotSchemaV2, Locale: locale, OutputLocales: append([]string(nil), outputLocales...), TargetPlatform: template.TargetPlatform, Product: ProductFacts{Name: sku.Product.Name, Brand: sku.Product.Brand, Description: sku.Product.Description, Category: CategoryFacts{NameZH: sku.Product.CatalogCategory.Name, NameEN: sku.Product.CatalogCategory.NameEN}}, SKU: SKUFacts{PublicID: sku.PublicID, Code: sku.Code, Color: sku.Color, Size: sku.Size, CompatibleDeviceModel: sku.CompatibleDeviceModel, PlatformTitle: sku.PlatformTitle, SellingPoints: sku.SellingPoints, Tags: tags}, SOP: SOPFacts{PublicID: captureSOPPublicID, VersionPublicID: sop.PublicID, VersionNumber: sop.VersionNumber, SchemaVersion: sop.SchemaVersion, Name: LocalizedNameFacts{ZH: sop.NameZH, EN: sop.NameEN}, Description: LocalizedNameFacts{ZH: sop.DescriptionZH, EN: sop.DescriptionEN}, CoordinateSystem: sop.CoordinateSystem, Views: views}, Template: TemplateFacts{TemplatePublicID: template.PublicID, VersionPublicID: version.PublicID, VersionNumber: version.VersionNumber, PromptCompilerVersion: version.PromptCompilerVersion, PlatformPrompt: version.PlatformPrompt, SelectedSlots: selectedSlots}, SelectedAssets: assetFacts, UserPreference: preference, GenerationOverrides: overrides, ImageCanvases: canvases}
}

func requiresCompatibleDeviceModel(slots []models.AIContentSlot) bool {
	for _, slot := range slots {
		var constraints map[string]any
		if json.Unmarshal(slot.ConstraintsJSON, &constraints) == nil {
			if required, ok := constraints["requires_compatible_device_model"].(bool); ok && required {
				return true
			}
		}
	}
	return false
}

func slotFacts(slot models.AIContentSlot) SlotFacts {
	return SlotFacts{PublicID: slot.PublicID, SlotKey: slot.SlotKey, Kind: slot.Kind, Name: LocalizedNameFacts{ZH: slot.NameZH, EN: slot.NameEN}, Description: LocalizedNameFacts{ZH: slot.DescriptionZH, EN: slot.DescriptionEN}, Sequence: slot.Sequence, Optional: slot.Optional, DefaultSelected: slot.DefaultSelected, PromptFragment: slot.PromptFragment, Constraints: cloneJSON(slot.ConstraintsJSON), GenerationConfig: cloneJSON(slot.GenerationConfigJSON), LayoutConfig: cloneJSON(slot.LayoutConfigJSON)}
}
func cloneJSON(value []byte) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage(`{}`)
	}
	return append(json.RawMessage(nil), value...)
}

func versionPublicIDs(db *gorm.DB, ids []uint) (map[uint]string, error) {
	result := map[uint]string{}
	if len(ids) == 0 {
		return result, nil
	}
	var rows []struct {
		ID       uint
		PublicID string
	}
	if err := db.Model(&models.AIContentTemplateVersion{}).Select("id", "public_id").Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.ID] = row.PublicID
	}
	return result, nil
}

func jobDocument(job models.AIJob, versionPublicID string) JobDocument {
	creator := JobCreatorSnapshot{}
	modelSnapshot := JobModelSnapshot{}
	_ = json.Unmarshal(job.CreatedBySnapshotJSON, &creator)
	_ = json.Unmarshal(job.ModelSnapshotJSON, &modelSnapshot)
	items := make([]JobItemDocument, 0, len(job.Items))
	totalTokens := int64(0)
	estimatedTotal := money.Must("0")
	allPriced := true
	for _, item := range job.Items {
		ids := []string{}
		_ = json.Unmarshal(item.SelectedInputAssetIDsJSON, &ids)
		executions := make([]JobExecutionDocument, 0, len(item.Executions))
		for _, execution := range item.Executions {
			actual := execution.ActualModel
			if actual == "" && execution.Status == models.AIExecutionCompleted {
				actual = execution.Model
			}
			requested := execution.RequestedModel
			if requested == "" {
				requested = execution.Model
			}
			pricingStatus, estimated := defaultString(execution.PricingStatus, "unpriced"), defaultString(execution.EstimatedAmountUSD, "0.00000000")
			if pricingStatus == "priced" {
				estimatedTotal.Add(estimatedTotal, money.Must(estimated))
			} else if execution.TotalTokens == 0 && pricingStatus == "unpriced" {
				pricingStatus = "not_applicable"
			} else {
				allPriced = false
			}
			totalTokens += execution.TotalTokens
			executions = append(executions, JobExecutionDocument{PublicID: execution.PublicID, Operation: execution.Operation, Status: execution.Status, AttemptNumber: execution.AttemptNumber, RequestedModel: requested, ActualModel: actual, APIMode: defaultString(execution.APIMode, "responses"), ProviderRequestID: execution.OpenAIRequestID, InputTextTokens: execution.InputTextTokens, CachedInputTokens: execution.CachedInputTokens, InputImageTokens: execution.InputImageTokens, OutputTextTokens: execution.OutputTextTokens, OutputImageTokens: execution.OutputImageTokens, ReasoningTokens: execution.ReasoningTokens, TotalTokens: execution.TotalTokens, ServiceTier: defaultString(execution.ServiceTier, "default"), PricingStatus: pricingStatus, EstimatedAmountUSD: estimated, FailureCode: execution.FailureCode, SafeError: execution.SafeError, StartedAt: execution.StartedAt, CompletedAt: execution.CompletedAt})
		}
		var failure *JobFailureDocument
		if item.SafeError != "" || item.FailureCode != "" {
			failure = &JobFailureDocument{Code: defaultString(item.FailureCode, "internal_execution_error"), SafeMessage: item.SafeError, RecoveryAction: recoveryActionForFailure(item.FailureCode)}
			if len(executions) > 0 {
				latest := executions[len(executions)-1]
				failure.Model = defaultString(latest.ActualModel, latest.RequestedModel)
				failure.APIMode = latest.APIMode
				failure.ProviderRequestID = latest.ProviderRequestID
			}
		}
		items = append(items, JobItemDocument{PublicID: item.PublicID, SlotKey: item.SlotKey, Kind: item.Kind, Status: item.Status, SlotSnapshot: cloneJSON(item.SlotSnapshotJSON), SelectedInputAssetIDs: ids, AttemptCount: item.AttemptCount, SafeError: item.SafeError, Failure: failure, Executions: executions, StartedAt: item.StartedAt, CompletedAt: item.CompletedAt, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt})
	}
	reconciliationStatus := "unpriced"
	if allPriced {
		reconciliationStatus = "pending"
	}
	return JobDocument{PublicID: job.PublicID, SKUID: job.SKU.PublicID, TemplateVersionPublicID: versionPublicID, TargetPlatform: job.TargetPlatform, Locale: job.Locale, OutputLocales: outputLocalesForJob(job), Status: job.Status, SnapshotSchema: job.SnapshotSchema, InputSnapshot: append(json.RawMessage(nil), job.InputSnapshotJSON...), CreatedBy: creator, CreatedBySnapshot: creator, ModelSnapshot: modelSnapshot, StartedAt: job.StartedAt, CompletedAt: job.CompletedAt, CancelledAt: job.CancelledAt, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt, Items: items, TotalTokens: totalTokens, EstimatedAmountUSD: money.Format(estimatedTotal), ReconciledAmountUSD: "0.00000000", ReconciliationStatus: reconciliationStatus}
}

func outputLocalesForJob(job models.AIJob) []string {
	var locales []string
	if json.Unmarshal(job.OutputLocalesJSON, &locales) == nil && validOutputLocales(locales) {
		return locales
	}
	if job.Locale != "" {
		return []string{job.Locale}
	}
	return []string{}
}

func loadJobAuditSnapshots(tx *gorm.DB, actorID uint) (JobCreatorSnapshot, JobModelSnapshot, error) {
	var user models.User
	if err := tx.Unscoped().Select("public_id", "name", "email").First(&user, actorID).Error; err != nil {
		return JobCreatorSnapshot{}, JobModelSnapshot{}, err
	}
	creator := JobCreatorSnapshot{PublicID: user.PublicID, Name: user.Name, Email: user.Email}
	modelSnapshot := JobModelSnapshot{TextModel: DefaultOpenAITextModel, ImageAPIMode: DefaultOpenAIImageMode, ImageResponsesModel: DefaultOpenAIImageModel, ImageGenerationModel: DefaultOpenAIImageGenerationModel}
	if !tx.Migrator().HasTable(&models.OpenAIProviderSetting{}) {
		return creator, modelSnapshot, nil
	}
	var setting models.OpenAIProviderSetting
	if err := tx.Where("provider = ? AND status = ?", openAIProvider, "active").First(&setting).Error; err == nil {
		modelSnapshot = JobModelSnapshot{TextModel: configuredModel(setting.TextModel, DefaultOpenAITextModel), ImageAPIMode: configuredImageMode(setting.ImageAPIMode), ImageResponsesModel: configuredModel(setting.ImageResponsesModel, DefaultOpenAIImageModel), ImageGenerationModel: configuredModel(setting.ImageGenerationModel, DefaultOpenAIImageGenerationModel)}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return JobCreatorSnapshot{}, JobModelSnapshot{}, err
	}
	return creator, modelSnapshot, nil
}

func recoveryActionForFailure(code string) string {
	switch code {
	case "openai_authentication_failed", "openai_model_incompatible", "openai_access_denied":
		return "review_openai_settings"
	case "openai_rate_limited", "openai_timeout_ambiguous", "openai_transport_ambiguous", "openai_server_error_ambiguous":
		return "retry_later"
	case "openai_moderation_blocked", "openai_refused", "invalid_input":
		return "adjust_input"
	case "storage_unavailable":
		return "contact_support"
	default:
		return "create_new_job"
	}
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func decodeJobModelSnapshot(value []byte) JobModelSnapshot {
	var snapshot JobModelSnapshot
	_ = json.Unmarshal(value, &snapshot)
	return snapshot
}
