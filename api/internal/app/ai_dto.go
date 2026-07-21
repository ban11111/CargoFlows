package app

import (
	"encoding/json"
	"time"

	"cargoflows/api/internal/ai"
	"cargoflows/api/internal/models"
)

type openAISettingRequest struct {
	APIKey *string `json:"api_key"`
}

type openAIModelSelectionRequest struct {
	TextModel            *string `json:"text_model"`
	ImageModel           *string `json:"image_model"`
	ImageAPIMode         *string `json:"image_api_mode"`
	ImageResponsesModel  *string `json:"image_responses_model"`
	ImageGenerationModel *string `json:"image_generation_model"`
}

type openAIWorkerSettingRequest struct {
	MaxWorkersPerJob *int `json:"max_workers_per_job"`
	MaxWorkersGlobal *int `json:"max_workers_global"`
}

type openAITimeoutSettingRequest struct {
	TextRequestTimeoutSeconds  *int `json:"text_request_timeout_seconds"`
	ImageRequestTimeoutSeconds *int `json:"image_request_timeout_seconds"`
}

type createAIJobRequest struct {
	SKUID                     string                           `json:"sku_id"`
	TemplateVersionPublicID   string                           `json:"template_version_id"`
	SelectedSlotKeys          []string                         `json:"selected_slot_keys"`
	SelectedAssetIDs          *[]string                        `json:"selected_asset_ids"`
	SelectedStyleReferenceIDs []string                         `json:"selected_style_reference_ids"`
	SelectedBrandIconIDs      []string                         `json:"selected_brand_icon_ids"`
	SelectedReferenceItemIDs  []string                         `json:"selected_reference_item_ids"`
	Locale                    string                           `json:"locale"`
	OutputLocales             []string                         `json:"output_locales"`
	UserPreference            string                           `json:"user_preference"`
	GenerationOverrides       map[string]ai.GenerationOverride `json:"generation_overrides"`
	ImageCanvases             []ai.ImageCanvas                 `json:"image_canvases"`
}

type editAITextResultRequest struct {
	Structured json.RawMessage `json:"structured"`
}

type openAISettingDTO struct {
	Provider                   string     `json:"provider"`
	Status                     string     `json:"status"`
	KeyFingerprint             string     `json:"key_fingerprint"`
	TextModel                  string     `json:"text_model"`
	ImageModel                 string     `json:"image_model"`
	ImageAPIMode               string     `json:"image_api_mode"`
	ImageResponsesModel        string     `json:"image_responses_model"`
	ImageGenerationModel       string     `json:"image_generation_model"`
	VerifiedAt                 *time.Time `json:"verified_at"`
	ImageCapabilityVerifiedAt  *time.Time `json:"image_capability_verified_at"`
	ImageResponsesVerifiedAt   *time.Time `json:"image_responses_verified_at"`
	ImageGenerationVerifiedAt  *time.Time `json:"image_generation_verified_at"`
	LastUsedAt                 *time.Time `json:"last_used_at"`
	MaxWorkersPerJob           int        `json:"max_workers_per_job"`
	MaxWorkersGlobal           int        `json:"max_workers_global"`
	TextRequestTimeoutSeconds  int        `json:"text_request_timeout_seconds"`
	ImageRequestTimeoutSeconds int        `json:"image_request_timeout_seconds"`
}

type aiContentSlotRequest struct {
	SlotKey          string          `json:"slot_key"`
	Kind             string          `json:"kind"`
	NameZH           string          `json:"name_zh"`
	NameEN           string          `json:"name_en"`
	DescriptionZH    string          `json:"description_zh"`
	DescriptionEN    string          `json:"description_en"`
	Sequence         int             `json:"sequence"`
	Optional         bool            `json:"optional"`
	DefaultSelected  bool            `json:"default_selected"`
	PromptFragment   string          `json:"prompt_fragment"`
	Constraints      json.RawMessage `json:"constraints"`
	GenerationConfig json.RawMessage `json:"generation_config"`
	LayoutConfig     json.RawMessage `json:"layout_config"`
}

type aiContentTemplateMutationRequest struct {
	NameZH                string                 `json:"name_zh"`
	NameEN                string                 `json:"name_en"`
	TargetPlatform        string                 `json:"target_platform"`
	DefaultLocale         string                 `json:"default_locale"`
	PromptCompilerVersion string                 `json:"prompt_compiler_version"`
	PlatformPrompt        string                 `json:"platform_prompt"`
	Slots                 []aiContentSlotRequest `json:"slots"`
}

type copyAIContentTemplateVersionRequest struct {
	SourceVersionID *string `json:"source_version_id"`
}

type aiContentTemplateDTO struct {
	PublicID       string                         `json:"public_id"`
	NameZH         string                         `json:"name_zh"`
	NameEN         string                         `json:"name_en"`
	TargetPlatform string                         `json:"target_platform"`
	Status         models.AIContentTemplateStatus `json:"status"`
	CreatedAt      time.Time                      `json:"created_at"`
	UpdatedAt      time.Time                      `json:"updated_at"`
	Versions       []aiContentTemplateVersionDTO  `json:"versions"`
}

type aiContentTemplateVersionDTO struct {
	PublicID              string                  `json:"public_id"`
	VersionNumber         int                     `json:"version_number"`
	Status                models.AITemplateStatus `json:"status"`
	DefaultLocale         string                  `json:"default_locale"`
	PromptCompilerVersion string                  `json:"prompt_compiler_version"`
	PlatformPrompt        string                  `json:"platform_prompt"`
	PublishedAt           *time.Time              `json:"published_at"`
	ArchivedAt            *time.Time              `json:"archived_at"`
	CreatedAt             time.Time               `json:"created_at"`
	UpdatedAt             time.Time               `json:"updated_at"`
	Slots                 []aiContentSlotDTO      `json:"slots"`
}

type aiContentSlotDTO struct {
	PublicID         string                   `json:"public_id"`
	SlotKey          string                   `json:"slot_key"`
	Kind             models.AIContentSlotKind `json:"kind"`
	NameZH           string                   `json:"name_zh"`
	NameEN           string                   `json:"name_en"`
	DescriptionZH    string                   `json:"description_zh"`
	DescriptionEN    string                   `json:"description_en"`
	Sequence         int                      `json:"sequence"`
	Optional         bool                     `json:"optional"`
	DefaultSelected  bool                     `json:"default_selected"`
	PromptFragment   string                   `json:"prompt_fragment"`
	Constraints      json.RawMessage          `json:"constraints"`
	GenerationConfig json.RawMessage          `json:"generation_config"`
	LayoutConfig     json.RawMessage          `json:"layout_config"`
}

type aiTemplateValidationDTO struct {
	Code   string               `json:"code"`
	Issues []ai.ValidationIssue `json:"issues"`
}

func openAISettingDTOFromView(value ai.ProviderSettingView) openAISettingDTO {
	return openAISettingDTO(value)
}

func templateMutationInput(req aiContentTemplateMutationRequest) ai.UpdateTemplateVersionInput {
	slots := make([]ai.SlotInput, 0, len(req.Slots))
	for _, slot := range req.Slots {
		slots = append(slots, ai.SlotInput{
			SlotKey: slot.SlotKey, Kind: slot.Kind, NameZH: slot.NameZH, NameEN: slot.NameEN,
			DescriptionZH: slot.DescriptionZH, DescriptionEN: slot.DescriptionEN, Sequence: slot.Sequence,
			Optional: slot.Optional, DefaultSelected: slot.DefaultSelected, PromptFragment: slot.PromptFragment,
			Constraints: objectOrEmpty(slot.Constraints), GenerationConfig: objectOrEmpty(slot.GenerationConfig), LayoutConfig: objectOrEmpty(slot.LayoutConfig),
		})
	}
	return ai.UpdateTemplateVersionInput{
		NameZH: req.NameZH, NameEN: req.NameEN, TargetPlatform: req.TargetPlatform,
		DefaultLocale: req.DefaultLocale, PromptCompilerVersion: req.PromptCompilerVersion,
		PlatformPrompt: req.PlatformPrompt, Slots: slots,
	}
}

func objectOrEmpty(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage(`{}`)
	}
	return append(json.RawMessage(nil), value...)
}

func aiContentTemplateDTOFromModel(value models.AIContentTemplate) aiContentTemplateDTO {
	versions := make([]aiContentTemplateVersionDTO, 0, len(value.Versions))
	for _, version := range value.Versions {
		versions = append(versions, aiContentTemplateVersionDTOFromModel(version))
	}
	return aiContentTemplateDTO{
		PublicID: value.PublicID, NameZH: value.NameZH, NameEN: value.NameEN,
		TargetPlatform: value.TargetPlatform, Status: value.Status, CreatedAt: value.CreatedAt,
		UpdatedAt: value.UpdatedAt, Versions: versions,
	}
}

func aiContentTemplateVersionDTOFromModel(value models.AIContentTemplateVersion) aiContentTemplateVersionDTO {
	slots := make([]aiContentSlotDTO, 0, len(value.Slots))
	for _, slot := range value.Slots {
		slots = append(slots, aiContentSlotDTO{
			PublicID: slot.PublicID, SlotKey: slot.SlotKey, Kind: slot.Kind, NameZH: slot.NameZH,
			NameEN: slot.NameEN, DescriptionZH: slot.DescriptionZH, DescriptionEN: slot.DescriptionEN,
			Sequence: slot.Sequence, Optional: slot.Optional, DefaultSelected: slot.DefaultSelected,
			PromptFragment: slot.PromptFragment, Constraints: cloneJSONObject(slot.ConstraintsJSON),
			GenerationConfig: cloneJSONObject(slot.GenerationConfigJSON), LayoutConfig: cloneJSONObject(slot.LayoutConfigJSON),
		})
	}
	return aiContentTemplateVersionDTO{
		PublicID: value.PublicID, VersionNumber: value.VersionNumber, Status: value.Status,
		DefaultLocale: value.DefaultLocale, PromptCompilerVersion: value.PromptCompilerVersion,
		PlatformPrompt: value.PlatformPrompt, PublishedAt: value.PublishedAt, ArchivedAt: value.ArchivedAt,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, Slots: slots,
	}
}

func cloneJSONObject(value []byte) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage(`{}`)
	}
	return append(json.RawMessage(nil), value...)
}
