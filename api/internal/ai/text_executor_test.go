package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"cargoflows/api/internal/models"
	"gorm.io/gorm"
)

type fakeActiveCredentialSource struct {
	calls      atomic.Int32
	credential ActiveOpenAICredential
	err        error
}

func (source *fakeActiveCredentialSource) DecryptActiveCredential(context.Context) (ActiveOpenAICredential, error) {
	source.calls.Add(1)
	return source.credential, source.err
}

type textProviderFunc func(context.Context, []byte, TextRequest) (TextResponse, error)

func (function textProviderFunc) Generate(ctx context.Context, key []byte, request TextRequest) (TextResponse, error) {
	return function(ctx, key, request)
}

type rotatingCredentialSource struct {
	db         *gorm.DB
	credential ActiveOpenAICredential
}

func (source *rotatingCredentialSource) DecryptActiveCredential(context.Context) (ActiveOpenAICredential, error) {
	if err := source.db.Model(&models.OpenAIProviderSetting{}).Where("id = ?", source.credential.SettingID).Update("key_fingerprint", "NEW1").Error; err != nil {
		return ActiveOpenAICredential{}, err
	}
	return source.credential, nil
}

func prepareTextExecutorLease(t *testing.T, candidateCount int) (*gorm.DB, LeasedItem, models.OpenAIProviderSetting) {
	t.Helper()
	db, _, items := seedQueueItems(t, 1)
	if err := db.AutoMigrate(&models.OpenAIProviderSetting{}, &models.AIExecution{}, &models.AIUsageLedger{}, &models.AITextResult{}); err != nil {
		t.Fatal(err)
	}
	var job models.AIJob
	if err := db.First(&job, items[0].AIJobID).Error; err != nil {
		t.Fatal(err)
	}
	var snapshot ProductSnapshotV1
	if err := json.Unmarshal(job.InputSnapshotJSON, &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.Locale = "zh-CN"
	snapshot.TargetPlatform = "lazada"
	snapshot.Template.PlatformPrompt = "Create accurate Lazada product content."
	snapshot.Product.Name = "Protective phone case"
	snapshot.SKU.Code = "CASE-001"
	countJSON, _ := json.Marshal(map[string]int{"candidate_count": candidateCount})
	snapshot.Template.SelectedSlots[0].GenerationConfig = countJSON
	snapshotJSON, _ := json.Marshal(snapshot)
	slotJSON, _ := json.Marshal(snapshot.Template.SelectedSlots[0])
	if err := db.Model(&models.AIJob{}).Where("id = ?", job.ID).Update("input_snapshot_json", snapshotJSON).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.AIJobItem{}).Where("id = ?", items[0].ID).Update("slot_snapshot_json", slotJSON).Error; err != nil {
		t.Fatal(err)
	}
	setting := models.OpenAIProviderSetting{
		Provider: "openai", EncryptedAPIKey: []byte("ciphertext"), EncryptionNonce: []byte("123456789012"), EncryptionKeyVersion: "v1",
		KeyFingerprint: "TEST", Status: "active", CreatedByID: 1, UpdatedByID: 1,
	}
	if err := db.Create(&setting).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	leased, err := NewQueue(db).LeaseNext(t.Context(), "worker-real", now, time.Minute)
	if err != nil || leased == nil {
		t.Fatalf("lease=%#v err=%v", leased, err)
	}
	return db, *leased, setting
}

func TestTextExecutorPersistsCandidatesUsageAuditAndClearsCredential(t *testing.T) {
	db, leased, setting := prepareTextExecutorLease(t, 2)
	if err := db.Model(&models.AIJob{}).Where("id = ?", leased.jobID).Update("model_snapshot_json", []byte(`{"text_model":"snapshotted-text-model","image_api_mode":"responses","image_responses_model":"snapshotted-image-host","image_generation_model":"gpt-image-2"}`)).Error; err != nil {
		t.Fatal(err)
	}
	key := []byte("temporary-fake-api-key")
	source := &fakeActiveCredentialSource{credential: ActiveOpenAICredential{SettingID: setting.ID, KeyFingerprint: setting.KeyFingerprint, APIKey: key, TextModel: "selected-runtime-model"}}
	var providerCalls atomic.Int32
	provider := textProviderFunc(func(_ context.Context, received []byte, request TextRequest) (TextResponse, error) {
		providerCalls.Add(1)
		if !bytes.Equal(received, []byte("temporary-fake-api-key")) || request.Model != "snapshotted-text-model" || request.Prompt.CandidateCount != 2 || request.Metadata["execution_id"] == "" {
			t.Fatalf("provider input key=%q request=%#v", received, request)
		}
		return TextResponse{
			ResponseID: "resp_executor", RequestID: "req_executor", Model: "fake-model",
			OutputJSON: json.RawMessage(`{"candidates":[{"title":"Accurate phone case","keywords":["case"],"source_fields":["product.name"]},{"title":"Protective phone case","keywords":["protective"],"source_fields":["sku.code"]}]}`),
			Usage:      TextUsage{InputTextTokens: 100, OutputTextTokens: 40, TotalTokens: 140, ReasoningTokens: 5},
		}, nil
	})
	now := time.Date(2026, 7, 17, 10, 0, 10, 0, time.UTC)
	executor := newTextExecutorWithClock(db, source, provider, TextExecutorConfig{Model: "fake-model", ReasoningEffort: "low"}, fixedClock{now: now})
	if err := executor.Execute(t.Context(), leased); err != nil {
		t.Fatal(err)
	}
	if err := executor.Execute(t.Context(), leased); err != nil {
		t.Fatalf("completed execution must be idempotent: %v", err)
	}
	if providerCalls.Load() != 1 || source.calls.Load() != 1 {
		t.Fatalf("provider calls=%d credential calls=%d", providerCalls.Load(), source.calls.Load())
	}
	if !bytes.Equal(key, make([]byte, len(key))) {
		t.Fatalf("credential bytes were not cleared: %q", key)
	}

	var execution models.AIExecution
	if err := db.First(&execution).Error; err != nil {
		t.Fatal(err)
	}
	if execution.Status != models.AIExecutionCompleted || execution.OpenAIResponseID != "resp_executor" || execution.OpenAIRequestID != "req_executor" || execution.Model != "fake-model" || execution.InputTextTokens != 100 || execution.OutputTextTokens != 40 || execution.TotalTokens != 140 || execution.ReasoningTokens != 5 || execution.CompletedAt == nil || execution.CompiledPromptSHA256 == "" {
		t.Fatalf("execution=%#v", execution)
	}
	if stringsContainCredential(execution.CompiledPrompt, execution.NormalizedInputJSON, execution.RequestConfigJSON) {
		t.Fatal("plaintext credential persisted in execution")
	}
	var requestConfig map[string]any
	if err := json.Unmarshal(execution.RequestConfigJSON, &requestConfig); err != nil || requestConfig["model"] != "snapshotted-text-model" {
		t.Fatalf("request config = %#v err=%v", requestConfig, err)
	}
	var results []models.AITextResult
	if err := db.Order("candidate_index").Find(&results).Error; err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].CandidateIndex != 1 || results[1].CandidateIndex != 2 || results[0].Kind != models.AIContentSlotTitle || results[0].State != models.AITextResultCandidate {
		t.Fatalf("results=%#v", results)
	}
	var ledger models.AIUsageLedger
	if err := db.First(&ledger).Error; err != nil {
		t.Fatal(err)
	}
	if ledger.AIExecutionID != execution.ID || ledger.InputTextTokens != 100 || ledger.OutputTextTokens != 40 || ledger.TotalTokens != 140 || ledger.ReasoningTokens != 5 || ledger.OpenAIRequestID != "req_executor" || ledger.EstimatedAmount != nil || ledger.ReportedAmount != nil {
		t.Fatalf("ledger=%#v", ledger)
	}
	var audit models.AIAuditEvent
	if err := db.Where("event_type = ?", "ai_execution.text_completed").First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	var dispatchAudit models.AIAuditEvent
	if err := db.Where("event_type = ?", "ai_execution.text_dispatched").First(&dispatchAudit).Error; err != nil {
		t.Fatal(err)
	}
	var dispatchMetadata map[string]any
	if err := json.Unmarshal(dispatchAudit.MetadataJSON, &dispatchMetadata); err != nil || dispatchMetadata["model"] != "snapshotted-text-model" {
		t.Fatalf("dispatch metadata = %#v err=%v", dispatchMetadata, err)
	}
	var updatedSetting models.OpenAIProviderSetting
	if err := db.First(&updatedSetting, setting.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updatedSetting.LastUsedAt == nil || !updatedSetting.LastUsedAt.Equal(now) {
		t.Fatalf("last_used_at=%v", updatedSetting.LastUsedAt)
	}
}

func TestTextExecutorRunsV2BilingualSnapshot(t *testing.T) {
	db, leased, setting := prepareTextExecutorLease(t, 1)
	var job models.AIJob
	if err := db.First(&job, leased.jobID).Error; err != nil {
		t.Fatal(err)
	}
	var snapshot ProductSnapshotV1
	if err := json.Unmarshal(job.InputSnapshotJSON, &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.Schema = ProductSnapshotSchemaV2
	snapshot.Locale = "en"
	snapshot.OutputLocales = []string{"en", "zh-CN"}
	snapshotJSON, _ := json.Marshal(snapshot)
	localesJSON, _ := json.Marshal(snapshot.OutputLocales)
	if err := db.Model(&models.AIJob{}).Where("id = ?", job.ID).Updates(map[string]any{
		"snapshot_schema":     ProductSnapshotSchemaV2,
		"locale":              snapshot.Locale,
		"output_locales_json": localesJSON,
		"input_snapshot_json": snapshotJSON,
	}).Error; err != nil {
		t.Fatal(err)
	}

	key := []byte("temporary-fake-api-key")
	source := &fakeActiveCredentialSource{credential: ActiveOpenAICredential{SettingID: setting.ID, KeyFingerprint: setting.KeyFingerprint, APIKey: key}}
	var providerCalls atomic.Int32
	provider := textProviderFunc(func(_ context.Context, _ []byte, request TextRequest) (TextResponse, error) {
		providerCalls.Add(1)
		if request.Prompt.CompilerVersion != TextPromptCompilerVersion || !bytes.Contains(request.Prompt.InputJSON, []byte(`"output_locales":["en","zh-CN"]`)) {
			t.Fatalf("v2 bilingual prompt = %#v", request.Prompt)
		}
		return TextResponse{
			ResponseID: "resp_v2_bilingual", RequestID: "req_v2_bilingual", Model: "fake-model",
			OutputJSON: json.RawMessage(`{"candidates":[{"localizations":{"en":{"title":"Protective phone case","keywords":["case"]},"zh-CN":{"title":"轻薄防护手机壳","keywords":["手机壳"]}},"source_fields":["product.name"]}]}`),
			Usage:      TextUsage{InputTextTokens: 50, OutputTextTokens: 20, TotalTokens: 70},
		}, nil
	})
	executor := newTextExecutorWithClock(db, source, provider, TextExecutorConfig{Model: "fake-model"}, fixedClock{now: time.Date(2026, 7, 17, 10, 0, 10, 0, time.UTC)})
	if err := executor.Execute(t.Context(), leased); err != nil {
		t.Fatal(err)
	}
	if providerCalls.Load() != 1 {
		t.Fatalf("provider calls = %d", providerCalls.Load())
	}
	var result models.AITextResult
	if err := db.First(&result).Error; err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(result.RawStructuredJSON, []byte(`"en"`)) || !bytes.Contains(result.RawStructuredJSON, []byte(`"zh-CN"`)) {
		t.Fatalf("stored bilingual result = %s", result.RawStructuredJSON)
	}
}

func TestTextExecutorCreatesANewExecutionForRegeneratedAttempt(t *testing.T) {
	db, firstLease, setting := prepareTextExecutorLease(t, 1)
	base := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	firstSource := &fakeActiveCredentialSource{credential: ActiveOpenAICredential{SettingID: setting.ID, KeyFingerprint: setting.KeyFingerprint, APIKey: []byte("first-temporary-key")}}
	firstExecutor := newTextExecutorWithClock(db, firstSource, textProviderFunc(func(context.Context, []byte, TextRequest) (TextResponse, error) {
		return TextResponse{}, errors.New("temporary provider failure")
	}), TextExecutorConfig{Model: "fake-model"}, fixedClock{now: base.Add(10 * time.Second)})
	if err := firstExecutor.Execute(t.Context(), firstLease); err == nil {
		t.Fatal("first attempt unexpectedly succeeded")
	}
	if err := newQueueWithClock(db, fixedClock{now: base.Add(11 * time.Second)}).Fail(t.Context(), firstLease, "Text generation temporarily failed"); err != nil {
		t.Fatal(err)
	}
	var job models.AIJob
	var item models.AIJobItem
	if err := db.First(&job, firstLease.jobID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&item, firstLease.itemID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := NewJobService(db).RegenerateTextItem(t.Context(), job.PublicID, item.PublicID, 1); err != nil {
		t.Fatal(err)
	}
	secondLease, err := NewQueue(db).LeaseNext(t.Context(), "worker-regeneration", base.Add(20*time.Second), time.Minute)
	if err != nil || secondLease == nil || secondLease.Attempt != 2 {
		t.Fatalf("second lease = %#v, %v", secondLease, err)
	}
	secondSource := &fakeActiveCredentialSource{credential: ActiveOpenAICredential{SettingID: setting.ID, KeyFingerprint: setting.KeyFingerprint, APIKey: []byte("second-temporary-key")}}
	secondExecutor := newTextExecutorWithClock(db, secondSource, textProviderFunc(func(context.Context, []byte, TextRequest) (TextResponse, error) {
		return TextResponse{
			ResponseID: "resp_regenerated", RequestID: "req_regenerated", Model: "fake-model",
			OutputJSON: json.RawMessage(`{"candidates":[{"title":"Regenerated title","keywords":[],"source_fields":["product.name"]}]}`),
			Usage:      TextUsage{InputTextTokens: 4, OutputTextTokens: 3, TotalTokens: 7},
		}, nil
	}), TextExecutorConfig{Model: "fake-model"}, fixedClock{now: base.Add(30 * time.Second)})
	if err := secondExecutor.Execute(t.Context(), *secondLease); err != nil {
		t.Fatal(err)
	}
	if err := newQueueWithClock(db, fixedClock{now: base.Add(31 * time.Second)}).Complete(t.Context(), *secondLease); err != nil {
		t.Fatal(err)
	}
	var executions []models.AIExecution
	if err := db.Where("ai_job_item_id = ?", item.ID).Order("attempt_number ASC").Find(&executions).Error; err != nil {
		t.Fatal(err)
	}
	if len(executions) != 2 || executions[0].AttemptNumber != 1 || executions[1].AttemptNumber != 2 || executions[1].Status != models.AIExecutionCompleted {
		t.Fatalf("executions = %#v", executions)
	}
	var result models.AITextResult
	if err := db.Where("ai_execution_id = ?", executions[1].ID).First(&result).Error; err != nil {
		t.Fatal(err)
	}
}

func TestTextExecutorLoadsOnlySupplementalImagesAndRecordsImageTokens(t *testing.T) {
	db, leased, setting := prepareTextExecutorLease(t, 1)
	var job models.AIJob
	if err := db.First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.SOPView{}, &models.Asset{}); err != nil {
		t.Fatal(err)
	}
	supplementalView := models.SOPView{PublicID: "90909090-9090-4090-8090-909090909090", SOPVersionID: 1, Sequence: 2, Role: models.SOPViewCapture, ViewKind: models.SOPViewDetail, PresetKey: "supplemental_info", NameZH: "补充信息图片", NameEN: "Supplemental Product Information", AllowMultiple: true, CameraPositionZ: 1, ImageUpX: 1, Composition: models.Composition{FrameOccupancy: .95, AspectRatio: "4:5"}}
	if err := db.Create(&supplementalView).Error; err != nil {
		t.Fatal(err)
	}
	supplemental := models.Asset{PublicID: "91919191-9191-4191-8191-919191919191", SKUID: job.SKUID, PhotoSessionID: 1, SOPViewID: supplementalView.ID, ObjectKey: "approved/supplemental.png", OriginalURL: "private://supplemental", ReviewStatus: "approved", CapturedAt: time.Now().UTC()}
	if err := db.Create(&supplemental).Error; err != nil {
		t.Fatal(err)
	}
	var persistedSupplemental models.Asset
	if err := db.Where("public_id = ? AND sk_uid = ? AND review_status = ?", supplemental.PublicID, job.SKUID, "approved").First(&persistedSupplemental).Error; err != nil {
		t.Fatalf("supplemental fixture is not eligible: %v (job sku=%d asset=%#v)", err, job.SKUID, supplemental)
	}
	var snapshot ProductSnapshotV1
	if err := json.Unmarshal(job.InputSnapshotJSON, &snapshot); err != nil {
		t.Fatal(err)
	}
	fact := AssetFacts{PublicID: supplemental.PublicID, SourceType: AssetSourceProductInformation, MIMEType: "image/png", CapturedAt: supplemental.CapturedAt, View: AssetViewFacts{PublicID: supplementalView.PublicID, PresetKey: "supplemental_info", Name: LocalizedNameFacts{ZH: supplementalView.NameZH, EN: supplementalView.NameEN}, Role: supplementalView.Role, ViewKind: supplementalView.ViewKind}}
	snapshot.SelectedAssets = []AssetFacts{fact}
	snapshotJSON, _ := json.Marshal(snapshot)
	if err := db.Model(&job).Update("input_snapshot_json", snapshotJSON).Error; err != nil {
		t.Fatal(err)
	}

	imageBytes := pngFixture(t, 2, 2)
	objects := &memoryImageObjectStore{source: map[string]ImageInput{supplemental.ObjectKey: {MIMEType: "image/png", Bytes: imageBytes}}}
	source := &fakeActiveCredentialSource{credential: ActiveOpenAICredential{SettingID: setting.ID, KeyFingerprint: setting.KeyFingerprint, APIKey: []byte("temporary-fake-api-key")}}
	provider := textProviderFunc(func(_ context.Context, _ []byte, request TextRequest) (TextResponse, error) {
		if len(request.Inputs) != 1 || request.Inputs[0].MIMEType != "image/png" || !bytes.Equal(request.Inputs[0].Bytes, imageBytes) {
			t.Fatalf("provider image inputs = %#v", request.Inputs)
		}
		return TextResponse{ResponseID: "resp_images", RequestID: "req_images", Model: "fake-model", OutputJSON: json.RawMessage(`{"candidates":[{"title":"Document sourced title","keywords":[],"source_fields":["asset:91919191-9191-4191-8191-919191919191"]}]}`), Usage: TextUsage{InputTextTokens: 10, InputImageTokens: 5, OutputTextTokens: 4, TotalTokens: 19}}, nil
	})
	executor := newTextExecutorWithClock(db, source, provider, TextExecutorConfig{Model: "fake-model", Storage: NewImageStorage(objects)}, fixedClock{now: time.Date(2026, 7, 17, 10, 0, 10, 0, time.UTC)})
	if err := executor.Execute(t.Context(), leased); err != nil {
		t.Fatal(err)
	}
	var execution models.AIExecution
	if err := db.First(&execution).Error; err != nil {
		t.Fatal(err)
	}
	if execution.InputImageTokens != 5 || execution.InputTextTokens != 10 || execution.TotalTokens != 19 {
		t.Fatalf("execution token usage = %#v", execution)
	}
}

func TestTextExecutorMarksAmbiguousProviderFailureNeedsAttention(t *testing.T) {
	db, leased, setting := prepareTextExecutorLease(t, 1)
	key := []byte("temporary-fake-api-key")
	source := &fakeActiveCredentialSource{credential: ActiveOpenAICredential{SettingID: setting.ID, KeyFingerprint: setting.KeyFingerprint, APIKey: key}}
	provider := textProviderFunc(func(context.Context, []byte, TextRequest) (TextResponse, error) {
		return TextResponse{}, &TextProviderError{Kind: ErrTextProviderAmbiguousTimeout}
	})
	executor := newTextExecutorWithClock(db, source, provider, TextExecutorConfig{Model: "fake-model"}, fixedClock{now: time.Date(2026, 7, 17, 10, 0, 10, 0, time.UTC)})
	err := executor.Execute(t.Context(), leased)
	if !errors.Is(err, ErrTextProviderAmbiguousTimeout) || !bytes.Equal(key, make([]byte, len(key))) {
		t.Fatalf("error=%v key=%q", err, key)
	}
	var execution models.AIExecution
	if db.First(&execution).Error != nil || execution.Status != models.AIExecutionNeedsAttention || execution.SafeError == "" {
		t.Fatalf("execution=%#v", execution)
	}
	if execution.OpenAIProviderSettingID == nil || *execution.OpenAIProviderSettingID != setting.ID || execution.OpenAIKeyFingerprint != setting.KeyFingerprint {
		t.Fatalf("provider attribution missing: %#v", execution)
	}
	var failureAudit models.AIAuditEvent
	if err := db.Where("event_type = ?", "ai_execution.text_needs_attention").First(&failureAudit).Error; err != nil {
		t.Fatal(err)
	}
	var updatedSetting models.OpenAIProviderSetting
	if err := db.First(&updatedSetting, setting.ID).Error; err != nil || updatedSetting.LastUsedAt == nil {
		t.Fatalf("setting=%#v err=%v", updatedSetting, err)
	}
	var results, ledgers int64
	db.Model(&models.AITextResult{}).Count(&results)
	db.Model(&models.AIUsageLedger{}).Count(&ledgers)
	if results != 0 || ledgers != 0 {
		t.Fatalf("results=%d ledgers=%d", results, ledgers)
	}
}

func TestTextExecutorAppliesConfiguredProviderTimeout(t *testing.T) {
	db, leased, setting := prepareTextExecutorLease(t, 1)
	key := []byte("temporary-fake-api-key")
	source := &fakeActiveCredentialSource{credential: ActiveOpenAICredential{SettingID: setting.ID, KeyFingerprint: setting.KeyFingerprint, APIKey: key, TextRequestTimeoutSeconds: 47}}
	provider := textProviderFunc(func(ctx context.Context, _ []byte, _ TextRequest) (TextResponse, error) {
		deadline, ok := ctx.Deadline()
		remaining := time.Until(deadline)
		if !ok || remaining < 45*time.Second || remaining > 48*time.Second {
			t.Fatalf("provider deadline remaining = %s, present=%v", remaining, ok)
		}
		return TextResponse{}, &TextProviderError{Kind: ErrTextProviderAuthentication, RequestID: "req_timeout_config"}
	})
	executor := newTextExecutorWithClock(db, source, provider, TextExecutorConfig{Model: "fake-model"}, fixedClock{now: time.Date(2026, 7, 17, 10, 0, 10, 0, time.UTC)})
	if err := executor.Execute(t.Context(), leased); !errors.Is(err, ErrTextProviderAuthentication) {
		t.Fatalf("error = %v", err)
	}
}

func TestTextExecutorPersistsAmbiguousOutcomeAfterRequestContextCancellation(t *testing.T) {
	db, leased, setting := prepareTextExecutorLease(t, 1)
	key := []byte("temporary-fake-api-key")
	source := &fakeActiveCredentialSource{credential: ActiveOpenAICredential{SettingID: setting.ID, KeyFingerprint: setting.KeyFingerprint, APIKey: key}}
	ctx, cancel := context.WithCancel(t.Context())
	provider := textProviderFunc(func(context.Context, []byte, TextRequest) (TextResponse, error) {
		cancel()
		return TextResponse{}, &TextProviderError{Kind: ErrTextProviderAmbiguousTransport, RequestID: "req_cancelled"}
	})
	executor := newTextExecutorWithClock(db, source, provider, TextExecutorConfig{Model: "fake-model"}, fixedClock{now: time.Date(2026, 7, 17, 10, 0, 10, 0, time.UTC)})
	err := executor.Execute(ctx, leased)
	if !errors.Is(err, ErrTextProviderAmbiguousTransport) {
		t.Fatalf("error=%v", err)
	}
	var execution models.AIExecution
	if db.First(&execution).Error != nil || execution.Status != models.AIExecutionNeedsAttention || execution.OpenAIRequestID != "req_cancelled" {
		t.Fatalf("execution=%#v", execution)
	}
	var audit models.AIAuditEvent
	if err := db.Where("event_type = ?", "ai_execution.text_needs_attention").First(&audit).Error; err != nil {
		t.Fatal(err)
	}
}

func TestTextExecutorClassifiesProviderFailuresAndRejectsMalformedSuccess(t *testing.T) {
	tests := []struct {
		name        string
		response    TextResponse
		providerErr error
		wantStatus  models.AIExecutionStatus
		wantError   error
	}{
		{"authentication", TextResponse{}, &TextProviderError{Kind: ErrTextProviderAuthentication, RequestID: "req_auth"}, models.AIExecutionFailed, ErrTextProviderAuthentication},
		{"refusal", TextResponse{}, &TextProviderError{Kind: ErrTextProviderRefusal, RequestID: "req_refusal"}, models.AIExecutionFailed, ErrTextProviderRefusal},
		{"malformed success", TextResponse{ResponseID: "resp_bad", RequestID: "req_bad", Model: "fake-model", OutputJSON: json.RawMessage(`{"candidates":[null]}`), Usage: TextUsage{InputTextTokens: 1, OutputTextTokens: 1, TotalTokens: 2}}, nil, models.AIExecutionFailed, ErrTextProviderInvalidResponse},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db, leased, setting := prepareTextExecutorLease(t, 1)
			key := []byte("temporary-fake-api-key")
			source := &fakeActiveCredentialSource{credential: ActiveOpenAICredential{SettingID: setting.ID, KeyFingerprint: setting.KeyFingerprint, APIKey: key}}
			provider := textProviderFunc(func(context.Context, []byte, TextRequest) (TextResponse, error) { return tc.response, tc.providerErr })
			executor := newTextExecutorWithClock(db, source, provider, TextExecutorConfig{Model: "fake-model"}, fixedClock{now: time.Date(2026, 7, 17, 10, 0, 10, 0, time.UTC)})
			err := executor.Execute(t.Context(), leased)
			if !errors.Is(err, tc.wantError) || !bytes.Equal(key, make([]byte, len(key))) {
				t.Fatalf("error=%v key=%q", err, key)
			}
			var execution models.AIExecution
			if db.First(&execution).Error != nil || execution.Status != tc.wantStatus || execution.SafeError == "" {
				t.Fatalf("execution=%#v", execution)
			}
			if execution.OpenAIRequestID == "" && tc.name != "malformed success" {
				t.Fatalf("provider request ID not audited: %#v", execution)
			}
		})
	}
}

func TestTextExecutorAuditsCredentialRotationBetweenDecryptAndDispatch(t *testing.T) {
	db, leased, setting := prepareTextExecutorLease(t, 1)
	key := []byte("temporary-fake-api-key")
	source := &rotatingCredentialSource{db: db, credential: ActiveOpenAICredential{SettingID: setting.ID, KeyFingerprint: setting.KeyFingerprint, APIKey: key}}
	var providerCalls atomic.Int32
	provider := textProviderFunc(func(context.Context, []byte, TextRequest) (TextResponse, error) {
		providerCalls.Add(1)
		return TextResponse{}, nil
	})
	executor := newTextExecutorWithClock(db, source, provider, TextExecutorConfig{Model: "fake-model"}, fixedClock{now: time.Date(2026, 7, 17, 10, 0, 10, 0, time.UTC)})
	err := executor.Execute(t.Context(), leased)
	if !errors.Is(err, ErrProviderNotActive) || providerCalls.Load() != 0 || !bytes.Equal(key, make([]byte, len(key))) {
		t.Fatalf("error=%v calls=%d key=%q", err, providerCalls.Load(), key)
	}
	var execution models.AIExecution
	if db.First(&execution).Error != nil || execution.Status != models.AIExecutionFailed || execution.SafeError == "" {
		t.Fatalf("execution=%#v", execution)
	}
	var audit models.AIAuditEvent
	if err := db.Where("event_type = ?", "ai_execution.text_failed").First(&audit).Error; err != nil {
		t.Fatal(err)
	}
}

func TestTextExecutorRecoversStoredResponseWithoutAnotherProviderCall(t *testing.T) {
	db, leased, setting := prepareTextExecutorLease(t, 1)
	source := &fakeActiveCredentialSource{}
	var providerCalls atomic.Int32
	provider := textProviderFunc(func(context.Context, []byte, TextRequest) (TextResponse, error) {
		providerCalls.Add(1)
		return TextResponse{}, errors.New("must not call provider")
	})
	now := time.Date(2026, 7, 17, 10, 0, 10, 0, time.UTC)
	executor := newTextExecutorWithClock(db, source, provider, TextExecutorConfig{Model: "fake-model"}, fixedClock{now: now})
	prepared, err := executor.prepare(t.Context(), leased)
	if err != nil {
		t.Fatal(err)
	}
	output := []byte(`{"candidates":[{"title":"Recovered phone case","keywords":[],"source_fields":["product.name"]}]}`)
	if err := db.Model(&models.AIExecution{}).Where("id = ?", prepared.execution.ID).Updates(map[string]any{
		"status": models.AIExecutionStoring, "provider_output_json": output, "open_ai_response_id": "resp_recovered", "open_ai_request_id": "req_recovered",
		"model": "fake-model", "input_text_tokens": 8, "output_text_tokens": 4, "openai_provider_setting_id": setting.ID, "openai_key_fingerprint": setting.KeyFingerprint,
	}).Error; err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	recovered, err := NewQueue(db).LeaseNext(t.Context(), "worker-recovery", base.Add(2*time.Minute), time.Minute)
	if err != nil || recovered == nil || recovered.Attempt != leased.Attempt+1 {
		t.Fatalf("recovered=%#v err=%v", recovered, err)
	}
	recoveryExecutor := newTextExecutorWithClock(db, source, provider, TextExecutorConfig{Model: "fake-model"}, fixedClock{now: base.Add(2*time.Minute + 10*time.Second)})
	if err := recoveryExecutor.Execute(t.Context(), *recovered); err != nil {
		t.Fatal(err)
	}
	if providerCalls.Load() != 0 || source.calls.Load() != 0 {
		t.Fatalf("provider calls=%d credential calls=%d", providerCalls.Load(), source.calls.Load())
	}
	var execution models.AIExecution
	if db.First(&execution, prepared.execution.ID).Error != nil || execution.Status != models.AIExecutionCompleted {
		t.Fatalf("execution=%#v", execution)
	}
	var count int64
	db.Model(&models.AITextResult{}).Count(&count)
	if count != 1 {
		t.Fatalf("candidate count=%d", count)
	}
}

func TestTextExecutorDoesNotRepeatUnresolvedCallingExecution(t *testing.T) {
	db, leased, _ := prepareTextExecutorLease(t, 1)
	source := &fakeActiveCredentialSource{}
	provider := textProviderFunc(func(context.Context, []byte, TextRequest) (TextResponse, error) {
		t.Fatal("provider must not be called for unresolved calling execution")
		return TextResponse{}, nil
	})
	executor := newTextExecutorWithClock(db, source, provider, TextExecutorConfig{Model: "fake-model"}, fixedClock{now: time.Date(2026, 7, 17, 10, 0, 10, 0, time.UTC)})
	prepared, err := executor.prepare(t.Context(), leased)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.AIExecution{}).Where("id = ?", prepared.execution.ID).Update("status", models.AIExecutionCallingOpenAI).Error; err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	recovered, err := NewQueue(db).LeaseNext(t.Context(), "worker-recovery", base.Add(2*time.Minute), time.Minute)
	if err != nil || recovered == nil || recovered.Attempt != leased.Attempt+1 {
		t.Fatalf("recovered=%#v err=%v", recovered, err)
	}
	recoveryExecutor := newTextExecutorWithClock(db, source, provider, TextExecutorConfig{Model: "fake-model"}, fixedClock{now: base.Add(2*time.Minute + 10*time.Second)})
	err = recoveryExecutor.Execute(t.Context(), *recovered)
	if !errors.Is(err, ErrExecutionNeedsAttention) || source.calls.Load() != 0 {
		t.Fatalf("error=%v credential calls=%d", err, source.calls.Load())
	}
	var execution models.AIExecution
	if db.First(&execution, prepared.execution.ID).Error != nil || execution.Status != models.AIExecutionNeedsAttention {
		t.Fatalf("execution=%#v", execution)
	}
}

func TestTextExecutorDoesNotRepeatCompletedExecutionAfterStaleReLease(t *testing.T) {
	db, leased, setting := prepareTextExecutorLease(t, 1)
	key := []byte("temporary-fake-api-key")
	source := &fakeActiveCredentialSource{credential: ActiveOpenAICredential{SettingID: setting.ID, KeyFingerprint: setting.KeyFingerprint, APIKey: key}}
	var calls atomic.Int32
	provider := textProviderFunc(func(context.Context, []byte, TextRequest) (TextResponse, error) {
		calls.Add(1)
		return TextResponse{ResponseID: "resp_once", RequestID: "req_once", Model: "fake-model", OutputJSON: json.RawMessage(`{"candidates":[{"title":"One completed title","keywords":[],"source_fields":["product.name"]}]}`), Usage: TextUsage{InputTextTokens: 2, OutputTextTokens: 2, TotalTokens: 4}}, nil
	})
	base := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	executor := newTextExecutorWithClock(db, source, provider, TextExecutorConfig{Model: "fake-model"}, fixedClock{now: base.Add(10 * time.Second)})
	if err := executor.Execute(t.Context(), leased); err != nil {
		t.Fatal(err)
	}
	recovered, err := NewQueue(db).LeaseNext(t.Context(), "worker-recovery", base.Add(2*time.Minute), time.Minute)
	if err != nil || recovered == nil {
		t.Fatalf("recovered=%#v err=%v", recovered, err)
	}
	recoveryExecutor := newTextExecutorWithClock(db, source, provider, TextExecutorConfig{Model: "fake-model"}, fixedClock{now: base.Add(2*time.Minute + 10*time.Second)})
	if err := recoveryExecutor.Execute(t.Context(), *recovered); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("provider calls=%d", calls.Load())
	}
}

func TestTextExecutorPersistsPaidResponseAfterLeaseWasReclaimed(t *testing.T) {
	db, leased, setting := prepareTextExecutorLease(t, 1)
	key := []byte("temporary-fake-api-key")
	source := &fakeActiveCredentialSource{credential: ActiveOpenAICredential{SettingID: setting.ID, KeyFingerprint: setting.KeyFingerprint, APIKey: key}}
	base := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	var recovered *LeasedItem
	var calls atomic.Int32
	provider := textProviderFunc(func(context.Context, []byte, TextRequest) (TextResponse, error) {
		calls.Add(1)
		var err error
		recovered, err = NewQueue(db).LeaseNext(t.Context(), "worker-recovery", base.Add(2*time.Minute), time.Minute)
		if err != nil || recovered == nil {
			t.Fatalf("recovered=%#v err=%v", recovered, err)
		}
		return TextResponse{ResponseID: "resp_late", RequestID: "req_late", Model: "fake-model", OutputJSON: json.RawMessage(`{"candidates":[{"title":"Late paid response","keywords":[],"source_fields":["product.name"]}]}`), Usage: TextUsage{InputTextTokens: 3, OutputTextTokens: 2, TotalTokens: 5, ReasoningTokens: 1}}, nil
	})
	executor := newTextExecutorWithClock(db, source, provider, TextExecutorConfig{Model: "fake-model"}, fixedClock{now: base.Add(10 * time.Second)})
	if err := executor.Execute(t.Context(), leased); err != nil {
		t.Fatal(err)
	}
	var execution models.AIExecution
	if db.First(&execution).Error != nil || execution.Status != models.AIExecutionCompleted || execution.OpenAIResponseID != "resp_late" || execution.TotalTokens != 5 || execution.ReasoningTokens != 1 {
		t.Fatalf("execution=%#v", execution)
	}
	var results, ledgers int64
	db.Model(&models.AITextResult{}).Count(&results)
	db.Model(&models.AIUsageLedger{}).Count(&ledgers)
	if results != 1 || ledgers != 1 || calls.Load() != 1 {
		t.Fatalf("results=%d ledgers=%d calls=%d", results, ledgers, calls.Load())
	}
	recoveryExecutor := newTextExecutorWithClock(db, source, provider, TextExecutorConfig{Model: "fake-model"}, fixedClock{now: base.Add(2*time.Minute + 10*time.Second)})
	if err := recoveryExecutor.Execute(t.Context(), *recovered); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("provider repeated after late response: %d", calls.Load())
	}
}

func TestLatePaidResponseReconcilesItemFailedByRecoveryWorker(t *testing.T) {
	db, leased, setting := prepareTextExecutorLease(t, 1)
	key := []byte("temporary-fake-api-key")
	source := &fakeActiveCredentialSource{credential: ActiveOpenAICredential{SettingID: setting.ID, KeyFingerprint: setting.KeyFingerprint, APIKey: key}}
	started := make(chan struct{})
	release := make(chan struct{})
	provider := textProviderFunc(func(context.Context, []byte, TextRequest) (TextResponse, error) {
		close(started)
		<-release
		return TextResponse{ResponseID: "resp_after_failure", RequestID: "req_after_failure", Model: "fake-model", OutputJSON: json.RawMessage(`{"candidates":[{"title":"Recovered paid title","keywords":[],"source_fields":["product.name"]}]}`), Usage: TextUsage{InputTextTokens: 4, OutputTextTokens: 3, TotalTokens: 7, ReasoningTokens: 1}}, nil
	})
	base := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	oldExecutor := newTextExecutorWithClock(db, source, provider, TextExecutorConfig{Model: "fake-model"}, fixedClock{now: base.Add(10 * time.Second)})
	oldCtx, cancelOld := context.WithCancel(t.Context())
	oldResult := make(chan error, 1)
	go func() { oldResult <- oldExecutor.Execute(oldCtx, leased) }()
	<-started
	recoveryQueue := NewQueue(db)
	recovered, err := recoveryQueue.LeaseNext(t.Context(), "worker-recovery", base.Add(2*time.Minute), time.Minute)
	if err != nil || recovered == nil {
		t.Fatalf("recovered=%#v err=%v", recovered, err)
	}
	recoveryExecutor := newTextExecutorWithClock(db, source, provider, TextExecutorConfig{Model: "fake-model"}, fixedClock{now: base.Add(2*time.Minute + 10*time.Second)})
	if err := recoveryExecutor.Execute(t.Context(), *recovered); !errors.Is(err, ErrExecutionNeedsAttention) {
		t.Fatalf("recovery error=%v", err)
	}
	if err := recoveryQueue.failAt(t.Context(), *recovered, defaultSafeExecutionError, base.Add(2*time.Minute+11*time.Second)); err != nil {
		t.Fatal(err)
	}
	var failedItem models.AIJobItem
	if db.First(&failedItem, recovered.itemID).Error != nil || failedItem.Status != models.AIJobItemFailed {
		t.Fatalf("item before late response=%#v", failedItem)
	}
	cancelOld()
	close(release)
	if err := <-oldResult; err != nil {
		t.Fatal(err)
	}
	var item models.AIJobItem
	if db.First(&item, recovered.itemID).Error != nil || item.Status != models.AIJobItemCompleted || item.SafeError != "" {
		t.Fatalf("reconciled item=%#v", item)
	}
	var job models.AIJob
	if db.First(&job, recovered.jobID).Error != nil || job.Status != models.AIJobCompleted {
		t.Fatalf("reconciled job=%#v", job)
	}
	var execution models.AIExecution
	if db.First(&execution).Error != nil || execution.Status != models.AIExecutionCompleted || execution.OpenAIResponseID != "resp_after_failure" {
		t.Fatalf("execution=%#v", execution)
	}
}

func TestTextExecutorFailureAuditIsIdempotentAndPreservesStrongRequestID(t *testing.T) {
	db, leased, _ := prepareTextExecutorLease(t, 1)
	executor := newTextExecutorWithClock(db, &fakeActiveCredentialSource{}, textProviderFunc(nil), TextExecutorConfig{Model: "fake-model"}, fixedClock{now: time.Date(2026, 7, 17, 10, 0, 10, 0, time.UTC)})
	prepared, err := executor.prepare(t.Context(), leased)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.AIExecution{}).Where("id = ?", prepared.execution.ID).Update("status", models.AIExecutionCallingOpenAI).Error; err != nil {
		t.Fatal(err)
	}
	if err := executor.markProviderFailure(t.Context(), prepared.execution.ID, models.AIExecutionNeedsAttention, "ambiguous", ""); err != nil {
		t.Fatal(err)
	}
	if err := executor.markProviderFailure(t.Context(), prepared.execution.ID, models.AIExecutionFailed, "authentication failed", "req_strong"); err != nil {
		t.Fatal(err)
	}
	var execution models.AIExecution
	if db.First(&execution, prepared.execution.ID).Error != nil || execution.OpenAIRequestID != "req_strong" {
		t.Fatalf("execution=%#v", execution)
	}
	var count int64
	db.Model(&models.AIAuditEvent{}).Where("ai_execution_id = ? AND event_type = ?", prepared.execution.ID, "ai_execution.text_needs_attention").Count(&count)
	if count != 1 {
		t.Fatalf("terminal audit count=%d", count)
	}
	db.Model(&models.AIAuditEvent{}).Where("ai_execution_id = ? AND event_type = ?", prepared.execution.ID, "ai_execution.text_failed").Count(&count)
	if count != 0 {
		t.Fatalf("mismatched failed audit count=%d", count)
	}
}

func TestRealRoutingExecutorRejectsImageWithoutCredentialOrProviderCall(t *testing.T) {
	db, _, _ := seedQueueItems(t, 2)
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	queue := NewQueue(db)
	textItem, _ := queue.LeaseNext(t.Context(), "worker-real", now, time.Minute)
	if err := queue.completeAt(t.Context(), *textItem, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	imageItem, err := queue.LeaseNext(t.Context(), "worker-real", now, time.Minute)
	if err != nil || imageItem == nil || imageItem.Kind != models.AIContentSlotImage {
		t.Fatalf("image lease=%#v err=%v", imageItem, err)
	}
	source := &fakeActiveCredentialSource{}
	var providerCalls atomic.Int32
	provider := textProviderFunc(func(context.Context, []byte, TextRequest) (TextResponse, error) {
		providerCalls.Add(1)
		return TextResponse{}, nil
	})
	router := NewKindRoutingExecutor(false, NewDryRunExecutor(db), NewTextExecutor(db, source, provider, TextExecutorConfig{Model: "fake-model"}))
	if err := router.Execute(t.Context(), *imageItem); !errors.Is(err, ErrRealImageGenerationUnsupported) {
		t.Fatalf("error=%v", err)
	}
	if source.calls.Load() != 0 || providerCalls.Load() != 0 {
		t.Fatalf("credential calls=%d provider calls=%d", source.calls.Load(), providerCalls.Load())
	}
}

func stringsContainCredential(values ...any) bool {
	for _, value := range values {
		var raw []byte
		switch typed := value.(type) {
		case string:
			raw = []byte(typed)
		case []byte:
			raw = typed
		}
		if bytes.Contains(raw, []byte("temporary-fake-api-key")) {
			return true
		}
	}
	return false
}
