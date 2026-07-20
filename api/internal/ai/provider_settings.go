package ai

import (
	"context"
	"errors"
	"strings"
	"time"

	"cargoflows/api/internal/models"
	"cargoflows/api/internal/secrets"
	"gorm.io/gorm"
)

const (
	openAIProvider                    = "openai"
	DefaultOpenAITextModel            = "gpt-5.6-terra"
	DefaultOpenAIImageModel           = "gpt-5.6"
	DefaultOpenAIImageMode            = "responses"
	DefaultOpenAIImageGenerationModel = "gpt-image-2"
)

var (
	ErrInvalidAPIKey             = errors.New("invalid API key")
	ErrCredentialVerification    = errors.New("credential verification failed")
	ErrProviderNotConfigured     = errors.New("provider is not configured")
	ErrProviderNotActive         = errors.New("provider is not active")
	ErrProviderModelsUnavailable = errors.New("provider models are unavailable")
	ErrProviderModelInvalid      = errors.New("provider model is invalid")
)

type ProviderVerification struct {
	Authenticated bool
}

type ProviderVerifier interface {
	Verify(ctx context.Context, apiKey string) (ProviderVerification, error)
}

type ProviderModel struct {
	ID                  string `json:"id"`
	OwnedBy             string `json:"owned_by"`
	SupportsText        bool   `json:"supports_text"`
	SupportsImageTool   bool   `json:"supports_image_tool"`
	SupportsImagesAPI   bool   `json:"supports_images_api"`
	CompatibilityReason string `json:"compatibility_reason,omitempty"`
}

type ModelConfiguration struct {
	TextModel            string
	ImageAPIMode         string
	ImageResponsesModel  string
	ImageGenerationModel string
}

type ProviderModelLister interface {
	ListModels(ctx context.Context, apiKey string) ([]ProviderModel, error)
}

type ProviderSettingView struct {
	Provider                  string     `json:"provider"`
	Status                    string     `json:"status"`
	KeyFingerprint            string     `json:"key_fingerprint"`
	TextModel                 string     `json:"text_model"`
	ImageModel                string     `json:"image_model"`
	ImageAPIMode              string     `json:"image_api_mode"`
	ImageResponsesModel       string     `json:"image_responses_model"`
	ImageGenerationModel      string     `json:"image_generation_model"`
	VerifiedAt                *time.Time `json:"verified_at"`
	ImageCapabilityVerifiedAt *time.Time `json:"image_capability_verified_at"`
	ImageResponsesVerifiedAt  *time.Time `json:"image_responses_verified_at"`
	ImageGenerationVerifiedAt *time.Time `json:"image_generation_verified_at"`
	LastUsedAt                *time.Time `json:"last_used_at"`
}

type ActiveOpenAICredential struct {
	SettingID            uint
	KeyFingerprint       string
	APIKey               []byte
	TextModel            string
	ImageModel           string
	ImageAPIMode         string
	ImageResponsesModel  string
	ImageGenerationModel string
}

type ProviderSettingsService struct {
	db       *gorm.DB
	box      *secrets.AESGCM
	verifier ProviderVerifier
}

func NewProviderSettingsService(db *gorm.DB, box *secrets.AESGCM, verifier ProviderVerifier) *ProviderSettingsService {
	return &ProviderSettingsService{db: db, box: box, verifier: verifier}
}

func (s *ProviderSettingsService) Get(ctx context.Context) (ProviderSettingView, error) {
	var row models.OpenAIProviderSetting
	err := s.db.WithContext(ctx).Where("provider = ?", openAIProvider).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ProviderSettingView{Provider: openAIProvider, Status: "unconfigured", TextModel: DefaultOpenAITextModel, ImageModel: DefaultOpenAIImageModel, ImageAPIMode: DefaultOpenAIImageMode, ImageResponsesModel: DefaultOpenAIImageModel, ImageGenerationModel: DefaultOpenAIImageGenerationModel}, nil
	}
	if err != nil {
		return ProviderSettingView{}, err
	}
	return providerSettingView(row), nil
}

func (s *ProviderSettingsService) ListModels(ctx context.Context) ([]ProviderModel, error) {
	lister, ok := s.verifier.(ProviderModelLister)
	if !ok {
		return nil, ErrProviderModelsUnavailable
	}
	credential, err := s.DecryptActiveCredential(ctx)
	if err != nil {
		return nil, err
	}
	defer clearByteSlice(credential.APIKey)
	models, err := lister.ListModels(ctx, string(credential.APIKey))
	if err != nil {
		return nil, ErrProviderModelsUnavailable
	}
	for index := range models {
		classifyProviderModel(&models[index])
	}
	return models, nil
}

func (s *ProviderSettingsService) UpdateModels(ctx context.Context, actorID uint, value any, legacyImageModel ...string) (ProviderSettingView, error) {
	config, ok := value.(ModelConfiguration)
	if !ok {
		textModel, textOK := value.(string)
		if !textOK || len(legacyImageModel) != 1 {
			return ProviderSettingView{}, ErrProviderModelInvalid
		}
		config = ModelConfiguration{TextModel: textModel, ImageAPIMode: "responses", ImageResponsesModel: legacyImageModel[0], ImageGenerationModel: DefaultOpenAIImageGenerationModel}
	}
	config.TextModel = strings.TrimSpace(config.TextModel)
	config.ImageAPIMode = strings.TrimSpace(config.ImageAPIMode)
	config.ImageResponsesModel = strings.TrimSpace(config.ImageResponsesModel)
	config.ImageGenerationModel = strings.TrimSpace(config.ImageGenerationModel)
	if config.TextModel == "" || (config.ImageAPIMode != "responses" && config.ImageAPIMode != "images") || config.ImageResponsesModel == "" || config.ImageGenerationModel == "" || len(config.TextModel) > 200 || len(config.ImageResponsesModel) > 200 || len(config.ImageGenerationModel) > 200 {
		return ProviderSettingView{}, ErrProviderModelInvalid
	}
	available, err := s.ListModels(ctx)
	if err != nil {
		return ProviderSettingView{}, err
	}
	known := make(map[string]ProviderModel, len(available))
	for _, model := range available {
		known[model.ID] = model
	}
	if model, ok := known[config.TextModel]; !ok || !model.SupportsText {
		return ProviderSettingView{}, ErrProviderModelInvalid
	}
	if model, ok := known[config.ImageResponsesModel]; !ok || !model.SupportsImageTool {
		return ProviderSettingView{}, ErrProviderModelInvalid
	}
	if model, ok := known[config.ImageGenerationModel]; !ok || !model.SupportsImagesAPI {
		return ProviderSettingView{}, ErrProviderModelInvalid
	}

	var row models.OpenAIProviderSetting
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("provider = ? AND status = ?", openAIProvider, "active").First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProviderNotActive
			}
			return err
		}
		activeImageModel := config.ImageResponsesModel
		if config.ImageAPIMode == "images" {
			activeImageModel = config.ImageGenerationModel
		}
		updates := map[string]any{"text_model": config.TextModel, "image_model": activeImageModel, "image_api_mode": config.ImageAPIMode, "image_responses_model": config.ImageResponsesModel, "image_generation_model": config.ImageGenerationModel, "updated_by_id": actorID}
		if row.ImageResponsesModel != config.ImageResponsesModel {
			updates["image_responses_verified_at"] = nil
		}
		if row.ImageGenerationModel != config.ImageGenerationModel {
			updates["image_generation_verified_at"] = nil
		}
		if row.ImageModel != activeImageModel || row.ImageAPIMode != config.ImageAPIMode {
			updates["image_capability_verified_at"] = nil
		}
		if err := tx.Model(&row).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(&row, row.ID).Error
	})
	if err != nil {
		return ProviderSettingView{}, err
	}
	return providerSettingView(row), nil
}

func (s *ProviderSettingsService) Configure(ctx context.Context, actorID uint, apiKey string) (ProviderSettingView, error) {
	apiKey = strings.TrimSpace(apiKey)
	if len(apiKey) < 20 {
		return ProviderSettingView{}, ErrInvalidAPIKey
	}
	verification, err := s.verifier.Verify(ctx, apiKey)
	if err != nil || !verification.Authenticated {
		return ProviderSettingView{}, ErrCredentialVerification
	}
	sealed, err := s.box.Seal([]byte(apiKey))
	if err != nil {
		return ProviderSettingView{}, err
	}

	verifiedAt := time.Now().UTC()
	var saved models.OpenAIProviderSetting
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Where("provider = ?", openAIProvider).First(&saved).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			saved = models.OpenAIProviderSetting{
				Provider:             openAIProvider,
				EncryptedAPIKey:      sealed.Ciphertext,
				EncryptionNonce:      sealed.Nonce,
				EncryptionKeyVersion: sealed.KeyVersion,
				KeyFingerprint:       fingerprint(apiKey),
				Status:               "active",
				TextModel:            DefaultOpenAITextModel,
				ImageModel:           DefaultOpenAIImageModel,
				ImageAPIMode:         DefaultOpenAIImageMode,
				ImageResponsesModel:  DefaultOpenAIImageModel,
				ImageGenerationModel: DefaultOpenAIImageGenerationModel,
				VerifiedAt:           &verifiedAt,
				CreatedByID:          actorID,
				UpdatedByID:          actorID,
			}
			return tx.Create(&saved).Error
		}
		if err != nil {
			return err
		}
		updates := map[string]any{
			"encrypted_api_key":            sealed.Ciphertext,
			"encryption_nonce":             sealed.Nonce,
			"encryption_key_version":       sealed.KeyVersion,
			"key_fingerprint":              fingerprint(apiKey),
			"status":                       "active",
			"verified_at":                  verifiedAt,
			"image_capability_verified_at": nil,
			"last_used_at":                 nil,
			"updated_by_id":                actorID,
		}
		if err := tx.Model(&saved).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(&saved, saved.ID).Error
	})
	if err != nil {
		return ProviderSettingView{}, err
	}
	return providerSettingView(saved), nil
}

func (s *ProviderSettingsService) Disable(ctx context.Context, actorID uint) (ProviderSettingView, error) {
	var row models.OpenAIProviderSetting
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Where("provider = ?", openAIProvider).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrProviderNotConfigured
		}
		if err != nil {
			return err
		}
		if err := tx.Model(&row).Updates(map[string]any{"status": "disabled", "updated_by_id": actorID}).Error; err != nil {
			return err
		}
		return tx.First(&row, row.ID).Error
	})
	if err != nil {
		return ProviderSettingView{}, err
	}
	return providerSettingView(row), nil
}

func (s *ProviderSettingsService) DecryptActiveKey(ctx context.Context) ([]byte, error) {
	credential, err := s.DecryptActiveCredential(ctx)
	if err != nil {
		return nil, err
	}
	return credential.APIKey, nil
}

func (s *ProviderSettingsService) DecryptActiveCredential(ctx context.Context) (ActiveOpenAICredential, error) {
	var row models.OpenAIProviderSetting
	err := s.db.WithContext(ctx).Where("provider = ? AND status = ?", openAIProvider, "active").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ActiveOpenAICredential{}, ErrProviderNotActive
	}
	if err != nil {
		return ActiveOpenAICredential{}, err
	}
	plain, err := s.box.Open(secrets.EncryptedValue{
		Ciphertext: row.EncryptedAPIKey,
		Nonce:      row.EncryptionNonce,
		KeyVersion: row.EncryptionKeyVersion,
	})
	if err != nil {
		return ActiveOpenAICredential{}, err
	}
	return ActiveOpenAICredential{
		SettingID: row.ID, KeyFingerprint: row.KeyFingerprint, APIKey: plain,
		TextModel:            configuredModel(row.TextModel, DefaultOpenAITextModel),
		ImageModel:           configuredModel(row.ImageModel, activeImageModel(row)),
		ImageAPIMode:         configuredImageMode(row.ImageAPIMode),
		ImageResponsesModel:  configuredModel(row.ImageResponsesModel, DefaultOpenAIImageModel),
		ImageGenerationModel: configuredModel(row.ImageGenerationModel, DefaultOpenAIImageGenerationModel),
	}, nil
}

func configuredImageMode(value string) string {
	if value == "images" {
		return "images"
	}
	return "responses"
}

func activeImageModel(row models.OpenAIProviderSetting) string {
	if configuredImageMode(row.ImageAPIMode) == "images" {
		return configuredModel(row.ImageGenerationModel, DefaultOpenAIImageGenerationModel)
	}
	return configuredModel(row.ImageResponsesModel, DefaultOpenAIImageModel)
}

func classifyProviderModel(model *ProviderModel) {
	if model == nil {
		return
	}
	id := strings.ToLower(model.ID)
	model.SupportsImagesAPI = strings.HasPrefix(id, "gpt-image-")
	mainline := strings.HasPrefix(id, "gpt-5") || strings.HasPrefix(id, "gpt-4.1") || strings.HasPrefix(id, "gpt-4o") || strings.HasPrefix(id, "o3") || strings.HasPrefix(id, "o4")
	incompatible := strings.Contains(id, "realtime") || strings.Contains(id, "audio") || strings.Contains(id, "transcribe") || strings.Contains(id, "tts") || strings.Contains(id, "search") || strings.Contains(id, "embedding") || strings.Contains(id, "moderation")
	model.SupportsText = mainline && !incompatible && !model.SupportsImagesAPI
	model.SupportsImageTool = model.SupportsText
	if !model.SupportsText && !model.SupportsImagesAPI {
		model.CompatibilityReason = "This model is not supported by CargoFlows text or image workflows"
	}
}

func configuredModel(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}

func fingerprint(apiKey string) string {
	if len(apiKey) <= 4 {
		return apiKey
	}
	return apiKey[len(apiKey)-4:]
}

func providerSettingView(row models.OpenAIProviderSetting) ProviderSettingView {
	return ProviderSettingView{
		Provider:                  row.Provider,
		Status:                    row.Status,
		KeyFingerprint:            row.KeyFingerprint,
		TextModel:                 configuredModel(row.TextModel, DefaultOpenAITextModel),
		ImageModel:                configuredModel(row.ImageModel, activeImageModel(row)),
		ImageAPIMode:              configuredImageMode(row.ImageAPIMode),
		ImageResponsesModel:       configuredModel(row.ImageResponsesModel, DefaultOpenAIImageModel),
		ImageGenerationModel:      configuredModel(row.ImageGenerationModel, DefaultOpenAIImageGenerationModel),
		VerifiedAt:                row.VerifiedAt,
		ImageCapabilityVerifiedAt: row.ImageCapabilityVerifiedAt,
		ImageResponsesVerifiedAt:  row.ImageResponsesVerifiedAt,
		ImageGenerationVerifiedAt: row.ImageGenerationVerifiedAt,
		LastUsedAt:                row.LastUsedAt,
	}
}
