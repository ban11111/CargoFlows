package ai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"cargoflow/api/internal/models"
	"cargoflow/api/internal/secrets"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeVerifier struct {
	result ProviderVerification
	err    error
	keys   []string
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
	if err := db.AutoMigrate(&models.OpenAIProviderSetting{}); err != nil {
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
	if after.Status != "disabled" || after.UpdatedByID != 9 || after.CreatedByID != before.CreatedByID ||
		!bytes.Equal(after.EncryptedAPIKey, before.EncryptedAPIKey) || !bytes.Equal(after.EncryptionNonce, before.EncryptionNonce) ||
		after.KeyFingerprint != before.KeyFingerprint || !after.VerifiedAt.Equal(*before.VerifiedAt) {
		t.Fatalf("disable changed credential fields: before=%#v after=%#v", before, after)
	}
	if _, err := service.DecryptActiveKey(t.Context()); !errors.Is(err, ErrProviderNotActive) {
		t.Fatalf("decrypt disabled error = %v", err)
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
