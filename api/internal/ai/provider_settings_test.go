package ai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"cargoflows/api/internal/models"
	"cargoflows/api/internal/secrets"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeVerifier struct {
	result   ProviderVerification
	err      error
	keys     []string
	models   []ProviderModel
	modelErr error
}

func (f *fakeVerifier) ListModels(_ context.Context, apiKey string) ([]ProviderModel, error) {
	f.keys = append(f.keys, apiKey)
	return f.models, f.modelErr
}

func (f *fakeVerifier) Verify(_ context.Context, apiKey string) (ProviderVerification, error) {
	f.keys = append(f.keys, apiKey)
	return f.result, f.err
}

func providerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.OpenAIProviderSetting{}, &models.AIWorkerSetting{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func providerService(t *testing.T, db *gorm.DB, verifier ProviderVerifier) *ProviderSettingsService {
	t.Helper()
	box, err := secrets.NewAESGCM(bytes.Repeat([]byte{0x31}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return NewProviderSettingsService(db, box, verifier)
}

func TestConfigureStoresCiphertextAndNeverReturnsPlaintext(t *testing.T) {
	db := providerTestDB(t)
	verifier := &fakeVerifier{result: ProviderVerification{Authenticated: true}}
	service := providerService(t, db, verifier)
	view, err := service.Configure(t.Context(), 7, "sk-proj-very-secret-ABCD")
	if err != nil {
		t.Fatal(err)
	}
	if view.Provider != "openai" || view.Status != "active" || view.KeyFingerprint != "ABCD" || view.VerifiedAt == nil {
		t.Fatalf("unexpected view: %#v", view)
	}
	if strings.Contains(fmt.Sprintf("%#v", view), "very-secret") {
		t.Fatalf("leaked key: %#v", view)
	}
	var row models.OpenAIProviderSetting
	if err := db.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(row.EncryptedAPIKey, []byte("very-secret")) {
		t.Fatal("stored plaintext")
	}
	if row.CreatedByID != 7 || row.UpdatedByID != 7 {
		t.Fatalf("audit actors = %d/%d, want 7/7", row.CreatedByID, row.UpdatedByID)
	}
}

func TestGetReturnsUnconfiguredWithoutCreatingRow(t *testing.T) {
	db := providerTestDB(t)
	service := providerService(t, db, &fakeVerifier{})
	view, err := service.Get(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if view.Provider != "openai" || view.Status != "unconfigured" || view.KeyFingerprint != "" {
		t.Fatalf("unexpected view: %#v", view)
	}
	var count int64
	if err := db.Model(&models.OpenAIProviderSetting{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("row count = %d, err = %v", count, err)
	}
}

func TestListModelsUsesActiveDecryptedCredential(t *testing.T) {
	db := providerTestDB(t)
	verifier := &fakeVerifier{result: ProviderVerification{Authenticated: true}, models: []ProviderModel{{ID: "gpt-test", OwnedBy: "openai"}}}
	service := providerService(t, db, verifier)
	if _, err := service.Configure(t.Context(), 7, "sk-proj-very-secret-MODEL"); err != nil {
		t.Fatal(err)
	}
	models, err := service.ListModels(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "gpt-test" || verifier.keys[len(verifier.keys)-1] != "sk-proj-very-secret-MODEL" {
		t.Fatalf("models/keys = %#v / %#v", models, verifier.keys)
	}
}

func TestUpdateModelsValidatesPersistsAndReturnsRuntimeSelections(t *testing.T) {
	db := providerTestDB(t)
	verifier := &fakeVerifier{
		result: ProviderVerification{Authenticated: true},
		models: []ProviderModel{{ID: "gpt-5.6-terra", OwnedBy: "openai"}, {ID: "gpt-5.6", OwnedBy: "openai"}, {ID: "gpt-image-2", OwnedBy: "openai"}},
	}
	service := providerService(t, db, verifier)
	configured, err := service.Configure(t.Context(), 1, "sk-proj-model-selection-WXYZ")
	if err != nil {
		t.Fatal(err)
	}
	if configured.TextModel != DefaultOpenAITextModel || configured.ImageModel != DefaultOpenAIImageModel {
		t.Fatalf("defaults = %q/%q", configured.TextModel, configured.ImageModel)
	}

	view, err := service.UpdateModels(t.Context(), 9, ModelConfiguration{TextModel: " gpt-5.6-terra ", ImageAPIMode: "images", ImageResponsesModel: "gpt-5.6", ImageGenerationModel: "gpt-image-2"})
	if err != nil {
		t.Fatal(err)
	}
	if view.TextModel != "gpt-5.6-terra" || view.ImageModel != "gpt-image-2" || view.ImageAPIMode != "images" {
		t.Fatalf("view = %#v", view)
	}
	credential, err := service.DecryptActiveCredential(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer clearByteSlice(credential.APIKey)
	if credential.TextModel != "gpt-5.6-terra" || credential.ImageModel != "gpt-image-2" || credential.ImageAPIMode != "images" {
		t.Fatalf("credential models = %q/%q", credential.TextModel, credential.ImageModel)
	}
	var row models.OpenAIProviderSetting
	if err := db.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.UpdatedByID != 9 {
		t.Fatalf("updated actor = %d", row.UpdatedByID)
	}

	if _, err := service.UpdateModels(t.Context(), 10, ModelConfiguration{TextModel: "not-visible", ImageAPIMode: "images", ImageResponsesModel: "gpt-5.6", ImageGenerationModel: "gpt-image-2"}); !errors.Is(err, ErrProviderModelInvalid) {
		t.Fatalf("unknown model error = %v", err)
	}
	if _, err := service.UpdateModels(t.Context(), 10, ModelConfiguration{TextModel: "gpt-5.6-terra", ImageAPIMode: "responses", ImageResponsesModel: "gpt-image-2", ImageGenerationModel: "gpt-image-2"}); !errors.Is(err, ErrProviderModelInvalid) {
		t.Fatalf("gpt-image model must not be accepted as Responses orchestrator: %v", err)
	}
	if err := db.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.TextModel != "gpt-5.6-terra" || row.UpdatedByID != 9 {
		t.Fatalf("invalid update mutated row: %#v", row)
	}
}

func TestConfigureRejectsInvalidOrUnauthenticatedKey(t *testing.T) {
	db := providerTestDB(t)
	verifier := &fakeVerifier{}
	service := providerService(t, db, verifier)
	if _, err := service.Configure(t.Context(), 3, " too-short "); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("short key error = %v", err)
	}
	if len(verifier.keys) != 0 {
		t.Fatal("short key was sent to verifier")
	}
	if _, err := service.Configure(t.Context(), 3, "sk-proj-long-but-not-valid"); !errors.Is(err, ErrCredentialVerification) {
		t.Fatalf("unauthenticated key error = %v", err)
	}
}

func TestFailedRotationPreservesActiveCredential(t *testing.T) {
	db := providerTestDB(t)
	verifier := &fakeVerifier{result: ProviderVerification{Authenticated: true}}
	service := providerService(t, db, verifier)
	if _, err := service.Configure(t.Context(), 1, "sk-proj-original-secret-WXYZ"); err != nil {
		t.Fatal(err)
	}
	verifier.result.Authenticated = false
	verifier.err = errors.New("upstream rejected credential")
	if _, err := service.Configure(t.Context(), 2, "sk-proj-replacement-secret-ABCD"); !errors.Is(err, ErrCredentialVerification) {
		t.Fatalf("rotation error = %v", err)
	}
	plain, err := service.DecryptActiveKey(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != "sk-proj-original-secret-WXYZ" {
		t.Fatalf("active key changed: %q", plain)
	}
	var row models.OpenAIProviderSetting
	if err := db.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.KeyFingerprint != "WXYZ" || row.UpdatedByID != 1 {
		t.Fatalf("rotation mutated row: %#v", row)
	}
}

func TestSuccessfulRotationUpdatesSingletonAndAuditActor(t *testing.T) {
	db := providerTestDB(t)
	verifier := &fakeVerifier{result: ProviderVerification{Authenticated: true}}
	service := providerService(t, db, verifier)
	if _, err := service.Configure(t.Context(), 1, "sk-proj-original-secret-WXYZ"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Configure(t.Context(), 2, "  sk-proj-replacement-secret-ABCD  "); err != nil {
		t.Fatal(err)
	}
	var rows []models.OpenAIProviderSetting
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].CreatedByID != 1 || rows[0].UpdatedByID != 2 || rows[0].KeyFingerprint != "ABCD" {
		t.Fatalf("unexpected rows: %#v", rows)
	}
	plain, err := service.DecryptActiveKey(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != "sk-proj-replacement-secret-ABCD" {
		t.Fatalf("decrypted key = %q", plain)
	}
}

func TestDisableChangesStatusAndAuditMetadataOnly(t *testing.T) {
	db := providerTestDB(t)
	service := providerService(t, db, &fakeVerifier{result: ProviderVerification{Authenticated: true}})
	if _, err := service.Configure(t.Context(), 1, "sk-proj-original-secret-WXYZ"); err != nil {
		t.Fatal(err)
	}
	imageVerifiedAt := time.Now().UTC().Add(-2 * time.Hour)
	lastUsedAt := time.Now().UTC().Add(-time.Hour)
	previousUpdatedAt := time.Now().UTC().Add(-24 * time.Hour)
	if err := db.Model(&models.OpenAIProviderSetting{}).Where("provider = ?", "openai").Updates(map[string]any{
		"encryption_key_version":       "key-version-9",
		"image_capability_verified_at": imageVerifiedAt,
		"last_used_at":                 lastUsedAt,
		"updated_at":                   previousUpdatedAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	var before models.OpenAIProviderSetting
	if err := db.First(&before).Error; err != nil {
		t.Fatal(err)
	}
	view, err := service.Disable(t.Context(), 9)
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != "disabled" || view.KeyFingerprint != "WXYZ" {
		t.Fatalf("unexpected disabled view: %#v", view)
	}
	var after models.OpenAIProviderSetting
	if err := db.First(&after).Error; err != nil {
		t.Fatal(err)
	}
	if after.Status != "disabled" || after.UpdatedByID != 9 || !after.UpdatedAt.After(before.UpdatedAt) ||
		after.ID != before.ID || after.Provider != before.Provider || after.CreatedByID != before.CreatedByID || !after.CreatedAt.Equal(before.CreatedAt) ||
		!bytes.Equal(after.EncryptedAPIKey, before.EncryptedAPIKey) || !bytes.Equal(after.EncryptionNonce, before.EncryptionNonce) ||
		after.EncryptionKeyVersion != before.EncryptionKeyVersion || after.KeyFingerprint != before.KeyFingerprint ||
		!after.VerifiedAt.Equal(*before.VerifiedAt) || !after.ImageCapabilityVerifiedAt.Equal(*before.ImageCapabilityVerifiedAt) ||
		!after.LastUsedAt.Equal(*before.LastUsedAt) {
		t.Fatalf("disable changed credential fields: before=%#v after=%#v", before, after)
	}
	if _, err := service.DecryptActiveKey(t.Context()); !errors.Is(err, ErrProviderNotActive) {
		t.Fatalf("decrypt disabled error = %v", err)
	}
}

func TestDecryptActiveKeyRejectsWrongMasterKey(t *testing.T) {
	db := providerTestDB(t)
	verifier := &fakeVerifier{result: ProviderVerification{Authenticated: true}}
	service := providerService(t, db, verifier)
	const apiKey = "sk-proj-original-secret-WXYZ"
	if _, err := service.Configure(t.Context(), 1, apiKey); err != nil {
		t.Fatal(err)
	}

	wrongBox, err := secrets.NewAESGCM(bytes.Repeat([]byte{0x72}, 32))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := NewProviderSettingsService(db, wrongBox, verifier).DecryptActiveKey(t.Context())
	if err == nil {
		t.Fatal("wrong master key decrypted stored credential")
	}
	if len(plain) != 0 || bytes.Contains(plain, []byte(apiKey)) {
		t.Fatalf("wrong master key returned plaintext: %q", plain)
	}
}

func TestDisableAndDecryptRejectUnconfiguredSetting(t *testing.T) {
	service := providerService(t, providerTestDB(t), &fakeVerifier{})
	if _, err := service.Disable(t.Context(), 1); !errors.Is(err, ErrProviderNotConfigured) {
		t.Fatalf("disable error = %v", err)
	}
	if _, err := service.DecryptActiveKey(t.Context()); !errors.Is(err, ErrProviderNotActive) {
		t.Fatalf("decrypt error = %v", err)
	}
}
