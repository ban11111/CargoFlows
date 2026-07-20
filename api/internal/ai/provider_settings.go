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

const openAIProvider = "openai"

var (
	ErrInvalidAPIKey          = errors.New("invalid API key")
	ErrCredentialVerification = errors.New("credential verification failed")
	ErrProviderNotConfigured  = errors.New("provider is not configured")
	ErrProviderNotActive      = errors.New("provider is not active")
)

type ProviderVerification struct {
	Authenticated bool
}

type ProviderVerifier interface {
	Verify(ctx context.Context, apiKey string) (ProviderVerification, error)
}

type ProviderSettingView struct {
	Provider                  string     `json:"provider"`
	Status                    string     `json:"status"`
	KeyFingerprint            string     `json:"key_fingerprint"`
	VerifiedAt                *time.Time `json:"verified_at"`
	ImageCapabilityVerifiedAt *time.Time `json:"image_capability_verified_at"`
	LastUsedAt                *time.Time `json:"last_used_at"`
}

type ActiveOpenAICredential struct {
	SettingID      uint
	KeyFingerprint string
	APIKey         []byte
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
		return ProviderSettingView{Provider: openAIProvider, Status: "unconfigured"}, nil
	}
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
	return ActiveOpenAICredential{SettingID: row.ID, KeyFingerprint: row.KeyFingerprint, APIKey: plain}, nil
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
		VerifiedAt:                row.VerifiedAt,
		ImageCapabilityVerifiedAt: row.ImageCapabilityVerifiedAt,
		LastUsedAt:                row.LastUsedAt,
	}
}
