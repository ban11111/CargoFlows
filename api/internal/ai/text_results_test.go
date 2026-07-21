package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"cargoflows/api/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func prepareTextResultService(t *testing.T) (*TextResultService, *gorm.DB, models.AIJob, models.AIJobItem, []models.AITextResult) {
	t.Helper()
	db, leased, setting := prepareTextExecutorLease(t, 2)
	if err := db.AutoMigrate(&models.SKU{}, &models.SKUPlatformContent{}, &models.SKUPlatformContentRevision{}); err != nil {
		t.Fatal(err)
	}
	sku := models.SKU{ID: 1, ProductID: 1, Code: "RESULT-SKU", PlatformTitle: "legacy title", SellingPoints: "legacy points", Status: "active"}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatal(err)
	}
	key := []byte("temporary-fake-api-key")
	source := &fakeActiveCredentialSource{credential: ActiveOpenAICredential{SettingID: setting.ID, KeyFingerprint: setting.KeyFingerprint, APIKey: key}}
	provider := textProviderFunc(func(context.Context, []byte, TextRequest) (TextResponse, error) {
		return TextResponse{ResponseID: "resp_review", RequestID: "req_review", Model: "fake-model", OutputJSON: json.RawMessage(`{"candidates":[{"title":"First generated title","keywords":["first"],"source_fields":["product.name"]},{"title":"Second generated title","keywords":["second"],"source_fields":["sku.code"]}]}`), Usage: TextUsage{InputTextTokens: 2, OutputTextTokens: 2, TotalTokens: 4}}, nil
	})
	executor := newTextExecutorWithClock(db, source, provider, TextExecutorConfig{Model: "fake-model"}, fixedClock{now: time.Date(2026, 7, 17, 10, 0, 10, 0, time.UTC)})
	if err := executor.Execute(t.Context(), leased); err != nil {
		t.Fatal(err)
	}
	var job models.AIJob
	var item models.AIJobItem
	var results []models.AITextResult
	if db.First(&item, leased.itemID).Error != nil || db.First(&job, item.AIJobID).Error != nil || db.Order("candidate_index").Find(&results).Error != nil {
		t.Fatal("load result fixture")
	}
	return NewTextResultService(db), db, job, item, results
}

func TestTextResultLifecycleKeepsRawImmutableAndRequiresEffectiveApproval(t *testing.T) {
	service, db, job, item, results := prepareTextResultService(t)
	originalRaw := append([]byte(nil), results[0].RawStructuredJSON...)
	listed, err := service.List(t.Context(), job.PublicID)
	if err != nil || len(listed) != 2 {
		t.Fatalf("list=%#v err=%v", listed, err)
	}
	edited := json.RawMessage(`{"title":"Edited approved title","keywords":["edited"],"source_fields":["product.name"]}`)
	if _, err := service.Edit(t.Context(), job.PublicID, item.PublicID, results[0].PublicID, 10, edited); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Edit(t.Context(), job.PublicID, item.PublicID, results[0].PublicID, 10, json.RawMessage(`{"title":3}`)); !errors.Is(err, ErrTextResultInvalid) {
		t.Fatalf("invalid edit error=%v", err)
	}
	var stored models.AITextResult
	db.First(&stored, results[0].ID)
	if string(stored.RawStructuredJSON) != string(originalRaw) || string(stored.EditedStructuredJSON) != string(edited) {
		t.Fatalf("stored result=%#v", stored)
	}
	if _, err := service.Approve(t.Context(), job.PublicID, item.PublicID, results[0].PublicID, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(t.Context(), job.PublicID, item.PublicID, results[1].PublicID, 11); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(t.Context(), job.PublicID, item.PublicID, results[0].PublicID, 10); !errors.Is(err, ErrTextResultNotEffective) {
		t.Fatalf("non-effective apply error=%v", err)
	}
	applied, err := service.Apply(t.Context(), job.PublicID, item.PublicID, results[1].PublicID, 11)
	if err != nil || applied.Content.Title != "Second generated title" || applied.Content.Revision != 1 || applied.Replayed {
		t.Fatalf("apply=%#v err=%v", applied, err)
	}
	replayed, err := service.Apply(t.Context(), job.PublicID, item.PublicID, results[1].PublicID, 11)
	if err != nil || !replayed.Replayed || replayed.Content.Revision != 1 {
		t.Fatalf("replay=%#v err=%v", replayed, err)
	}
	if _, err := service.Approve(t.Context(), job.PublicID, item.PublicID, results[0].PublicID, 10); err != nil {
		t.Fatal(err)
	}
	replayedAfterApprovalChanged, err := service.Apply(t.Context(), job.PublicID, item.PublicID, results[1].PublicID, 11)
	if err != nil || !replayedAfterApprovalChanged.Replayed || replayedAfterApprovalChanged.Content.Revision != 1 {
		t.Fatalf("replay after effective approval changed=%#v err=%v", replayedAfterApprovalChanged, err)
	}
	var sku models.SKU
	db.First(&sku, job.SKUID)
	if sku.PlatformTitle != "legacy title" || sku.SellingPoints != "legacy points" {
		t.Fatalf("legacy SKU fields were overwritten: %#v", sku)
	}
	var revisions int64
	db.Model(&models.SKUPlatformContentRevision{}).Count(&revisions)
	if revisions != 1 {
		t.Fatalf("revision count=%d", revisions)
	}
	seoSlot := models.AIContentSlot{PublicID: uuid.NewString(), AIContentTemplateVersionID: job.AIContentTemplateVersionID, SlotKey: "seo-review", Kind: models.AIContentSlotSEODescription, NameZH: "SEO", NameEN: "SEO", Sequence: 2, PromptFragment: "seo", ConstraintsJSON: []byte(`{}`), GenerationConfigJSON: []byte(`{}`), LayoutConfigJSON: []byte(`{}`)}
	if err := db.Create(&seoSlot).Error; err != nil {
		t.Fatal(err)
	}
	seoSlotJSON, _ := json.Marshal(SlotFacts{PublicID: seoSlot.PublicID, SlotKey: seoSlot.SlotKey, Kind: seoSlot.Kind, PromptFragment: seoSlot.PromptFragment, Constraints: json.RawMessage(`{}`), GenerationConfig: json.RawMessage(`{}`), LayoutConfig: json.RawMessage(`{}`)})
	seoItem := models.AIJobItem{PublicID: uuid.NewString(), AIJobID: job.ID, AIContentSlotID: seoSlot.ID, SlotKey: seoSlot.SlotKey, SlotSnapshotJSON: seoSlotJSON, Kind: seoSlot.Kind, Status: models.AIJobItemCompleted, SelectedInputAssetIDsJSON: []byte(`[]`), AttemptCount: 1}
	if err := db.Create(&seoItem).Error; err != nil {
		t.Fatal(err)
	}
	seoExecution := models.AIExecution{PublicID: uuid.NewString(), AIJobItemID: seoItem.ID, Operation: models.AIExecutionTextGenerate, Status: models.AIExecutionCompleted, AttemptNumber: 1, L0PolicyVersion: "l0", L1ProductContextVersion: "l1", L2TemplateVersionPublicID: uuid.NewString(), L3ContentSlotPublicID: seoSlot.PublicID, NormalizedInputJSON: []byte(`{}`), OrderedInputListJSON: []byte(`[]`), CompiledPrompt: "safe", CompiledPromptSHA256: strings.Repeat("b", 64), Model: "fake", RequestConfigJSON: []byte(`{}`)}
	if err := db.Create(&seoExecution).Error; err != nil {
		t.Fatal(err)
	}
	seoResult := models.AITextResult{PublicID: uuid.NewString(), AIExecutionID: seoExecution.ID, CandidateIndex: 1, Kind: models.AIContentSlotSEODescription, RawStructuredJSON: []byte(`{"short_description":"Short SEO copy","selling_points":["Point one"],"long_description":"Long SEO description","search_keywords":["keyword"],"source_fields":["product.name"]}`), ValidationJSON: []byte(`[]`)}
	if err := db.Create(&seoResult).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(t.Context(), job.PublicID, seoItem.PublicID, seoResult.PublicID, 11); err != nil {
		t.Fatal(err)
	}
	seoApplied, err := service.Apply(t.Context(), job.PublicID, seoItem.PublicID, seoResult.PublicID, 11)
	if err != nil || seoApplied.Content.Title != "Second generated title" || seoApplied.Content.ShortDescription != "Short SEO copy" || seoApplied.Content.Revision != 2 {
		t.Fatalf("SEO apply=%#v err=%v", seoApplied, err)
	}
	titleReplay, err := service.Apply(t.Context(), job.PublicID, item.PublicID, results[1].PublicID, 11)
	if err != nil || !titleReplay.Replayed || titleReplay.Content.Revision != 1 || titleReplay.Content.Title != "Second generated title" {
		t.Fatalf("historical title replay=%#v err=%v", titleReplay, err)
	}
	history, err := service.GetPlatformContent(t.Context(), sku.PublicID, job.TargetPlatform, job.Locale)
	if err != nil || history.Content == nil || len(history.Revisions) != 2 || history.Revisions[0].Revision != 2 {
		t.Fatalf("history=%#v err=%v", history, err)
	}
}

func TestBilingualTextResultAppliesEnglishAndChineseAtomically(t *testing.T) {
	service, db, job, item, results := prepareTextResultService(t)
	var snapshot ProductSnapshotV1
	if err := json.Unmarshal(job.InputSnapshotJSON, &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.Schema, snapshot.Locale, snapshot.OutputLocales = ProductSnapshotSchemaV2, "en", []string{"en", "zh-CN"}
	snapshotJSON, _ := json.Marshal(snapshot)
	localesJSON, _ := json.Marshal(snapshot.OutputLocales)
	if err := db.Model(&job).Updates(map[string]any{"snapshot_schema": ProductSnapshotSchemaV2, "locale": "en", "output_locales_json": localesJSON, "input_snapshot_json": snapshotJSON}).Error; err != nil {
		t.Fatal(err)
	}
	job.SnapshotSchema, job.Locale, job.OutputLocalesJSON, job.InputSnapshotJSON = ProductSnapshotSchemaV2, "en", localesJSON, snapshotJSON
	bilingual := []byte(`{"localizations":{"en":{"title":"Clear protective phone case","keywords":["case"]},"zh-CN":{"title":"轻薄透明保护手机壳","keywords":["手机壳"]}},"source_fields":["product.name"]}`)
	if err := db.Model(&results[0]).Update("raw_structured_json", bilingual).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(t.Context(), job.PublicID, item.PublicID, results[0].PublicID, 10); err != nil {
		t.Fatal(err)
	}
	preview, err := service.Preview(t.Context(), job.PublicID, item.PublicID, results[0].PublicID)
	if err != nil || len(preview.Localizations) != 2 || preview.Localizations[0].Locale != "en" {
		t.Fatalf("preview=%#v err=%v", preview, err)
	}
	applied, err := service.Apply(t.Context(), job.PublicID, item.PublicID, results[0].PublicID, 10)
	if err != nil || len(applied.Contents) != 2 || applied.Contents[0].Locale != "en" || applied.Contents[1].Locale != "zh-CN" {
		t.Fatalf("applied=%#v err=%v", applied, err)
	}
	var contents []models.SKUPlatformContent
	if err := db.Order("id ASC").Find(&contents).Error; err != nil || len(contents) != 2 || contents[0].Title != "Clear protective phone case" || contents[1].Title != "轻薄透明保护手机壳" {
		t.Fatalf("contents=%#v err=%v", contents, err)
	}
	var revisions int64
	db.Model(&models.SKUPlatformContentRevision{}).Count(&revisions)
	if revisions != 2 {
		t.Fatalf("revision count=%d", revisions)
	}
	replayed, err := service.Apply(t.Context(), job.PublicID, item.PublicID, results[0].PublicID, 10)
	if err != nil || !replayed.Replayed || len(replayed.Contents) != 2 {
		t.Fatalf("replay=%#v err=%v", replayed, err)
	}
}

func TestBilingualTextResultRollsBackBothLocalesWhenOneRevisionFails(t *testing.T) {
	service, db, job, item, results := prepareTextResultService(t)
	var snapshot ProductSnapshotV1
	if err := json.Unmarshal(job.InputSnapshotJSON, &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.Schema, snapshot.Locale, snapshot.OutputLocales = ProductSnapshotSchemaV2, "en", []string{"en", "zh-CN"}
	snapshotJSON, _ := json.Marshal(snapshot)
	localesJSON, _ := json.Marshal(snapshot.OutputLocales)
	if err := db.Model(&job).Updates(map[string]any{"snapshot_schema": ProductSnapshotSchemaV2, "locale": "en", "output_locales_json": localesJSON, "input_snapshot_json": snapshotJSON}).Error; err != nil {
		t.Fatal(err)
	}
	bilingual := []byte(`{"localizations":{"en":{"title":"Clear protective phone case","keywords":["case"]},"zh-CN":{"title":"轻薄透明保护手机壳","keywords":["手机壳"]}},"source_fields":["product.name"]}`)
	if err := db.Model(&results[0]).Update("raw_structured_json", bilingual).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(t.Context(), job.PublicID, item.PublicID, results[0].PublicID, 10); err != nil {
		t.Fatal(err)
	}
	revisions := 0
	if err := db.Callback().Create().Before("gorm:create").Register("test:fail_second_bilingual_revision", func(tx *gorm.DB) {
		if tx.Statement.Table != "sku_platform_content_revisions" {
			return
		}
		revisions++
		if revisions == 2 {
			tx.AddError(errors.New("injected second locale failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(t.Context(), job.PublicID, item.PublicID, results[0].PublicID, 10); err == nil {
		t.Fatal("expected the second locale failure")
	}
	var contentCount, revisionCount int64
	if err := db.Model(&models.SKUPlatformContent{}).Count(&contentCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.SKUPlatformContentRevision{}).Count(&revisionCount).Error; err != nil {
		t.Fatal(err)
	}
	var stored models.AITextResult
	if err := db.First(&stored, results[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if contentCount != 0 || revisionCount != 0 || stored.AppliedAt != nil {
		t.Fatalf("partial bilingual apply persisted: contents=%d revisions=%d applied_at=%v", contentCount, revisionCount, stored.AppliedAt)
	}
}

func TestTextResultOwnershipRejectionPreviewAndHistory(t *testing.T) {
	service, db, job, item, results := prepareTextResultService(t)
	if _, err := service.Approve(t.Context(), "00000000-0000-4000-8000-000000000000", item.PublicID, results[0].PublicID, 10); !errors.Is(err, ErrTextResultNotFound) {
		t.Fatalf("cross-job error=%v", err)
	}
	if _, err := service.Preview(t.Context(), job.PublicID, item.PublicID, results[0].PublicID); !errors.Is(err, ErrTextResultApprovalRequired) {
		t.Fatalf("preview before approval=%v", err)
	}
	if _, err := service.Reject(t.Context(), job.PublicID, item.PublicID, results[0].PublicID, 12); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(t.Context(), job.PublicID, item.PublicID, results[0].PublicID, 12); !errors.Is(err, ErrTextResultLifecycleConflict) {
		t.Fatalf("approve rejected error=%v", err)
	}
	var sku models.SKU
	if err := db.First(&sku, job.SKUID).Error; err != nil {
		t.Fatal(err)
	}
	history, err := service.GetPlatformContent(t.Context(), sku.PublicID, job.TargetPlatform, job.Locale)
	if err != nil || history.Content != nil || len(history.Revisions) != 0 {
		t.Fatalf("empty history=%#v err=%v", history, err)
	}
}

func TestEditingApprovedTextResultInvalidatesApproval(t *testing.T) {
	service, db, job, item, results := prepareTextResultService(t)
	if _, err := service.Approve(t.Context(), job.PublicID, item.PublicID, results[0].PublicID, 10); err != nil {
		t.Fatal(err)
	}
	edited := json.RawMessage(`{"title":"Needs fresh approval","keywords":["fresh"],"source_fields":["product.name"]}`)
	document, err := service.Edit(t.Context(), job.PublicID, item.PublicID, results[0].PublicID, 11, edited)
	if err != nil {
		t.Fatal(err)
	}
	if document.State != models.AITextResultCandidate || document.Effective || document.ApprovedAt != nil {
		t.Fatalf("edited approved result retained approval: %#v", document)
	}
	secondEdit := json.RawMessage(`{"title":"Second audited edit","keywords":["second"],"source_fields":["product.name"]}`)
	if _, err := service.Edit(t.Context(), job.PublicID, item.PublicID, results[0].PublicID, 12, secondEdit); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(t.Context(), job.PublicID, item.PublicID, results[0].PublicID, 11); !errors.Is(err, ErrTextResultApprovalRequired) {
		t.Fatalf("apply after edit error=%v", err)
	}
	var storedItem models.AIJobItem
	if err := db.First(&storedItem, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedItem.EffectiveApprovedResultID != nil {
		t.Fatalf("effective approval was not cleared: %#v", storedItem)
	}
	var audits []models.AIAuditEvent
	if err := db.Where("entity_public_id = ? AND event_type = ?", results[0].PublicID, "ai_text_result.edited").Order("id").Find(&audits).Error; err != nil || len(audits) != 2 {
		t.Fatalf("edit audits=%#v err=%v", audits, err)
	}
	var firstAudit, secondAudit struct {
		Before json.RawMessage `json:"before"`
		After  json.RawMessage `json:"after"`
	}
	if json.Unmarshal(audits[0].MetadataJSON, &firstAudit) != nil || json.Unmarshal(audits[1].MetadataJSON, &secondAudit) != nil || string(firstAudit.After) != string(edited) || string(secondAudit.Before) != string(edited) || string(secondAudit.After) != string(secondEdit) {
		t.Fatalf("audit chain first=%s second=%s", audits[0].MetadataJSON, audits[1].MetadataJSON)
	}
}

func TestValidateEditedTextResultHonorsPublishedTextConstraints(t *testing.T) {
	constraints := json.RawMessage(`{"required_fields":["brand"],"forbidden_terms":["best"],"keyword_policy":"natural"}`)
	snapshot, err := json.Marshal(SlotFacts{Kind: models.AIContentSlotTitle, Constraints: constraints})
	if err != nil {
		t.Fatal(err)
	}
	item := models.AIJobItem{SlotSnapshotJSON: snapshot}
	jobSnapshot, err := json.Marshal(ProductSnapshotV1{Product: ProductFacts{Brand: "Acme"}})
	if err != nil {
		t.Fatal(err)
	}
	job := models.AIJob{InputSnapshotJSON: jobSnapshot}
	valid := json.RawMessage(`{"title":"Acme product title","keywords":["brand","product"],"source_fields":["product.brand"]}`)
	if err := validateEditedTextResult(job, item, models.AIContentSlotTitle, valid); err != nil {
		t.Fatalf("valid candidate rejected: %v", err)
	}
	for name, candidate := range map[string]json.RawMessage{
		"forbidden term":   json.RawMessage(`{"title":"Best Acme product title","keywords":["brand"],"source_fields":["product.brand"]}`),
		"required field":   json.RawMessage(`{"title":"Product title","keywords":["brand"],"source_fields":["product.brand"]}`),
		"natural keywords": json.RawMessage(`{"title":"Acme product title","keywords":["brand"," Brand "],"source_fields":["product.brand"]}`),
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateEditedTextResult(job, item, models.AIContentSlotTitle, candidate); !errors.Is(err, ErrTextResultInvalid) {
				t.Fatalf("constraint violation error=%v", err)
			}
		})
	}
}

func TestParseTextConstraintRulesRejectsUnsupportedRules(t *testing.T) {
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"made_up_rule":true}`),
		json.RawMessage(`{"required_fields":["unknown_field"]}`),
		json.RawMessage(`{"keyword_policy":"stuffed"}`),
	} {
		if _, err := parseTextConstraintRules(raw, models.AIContentSlotTitle); err == nil {
			t.Fatalf("unsupported constraints accepted: %s", raw)
		}
	}
}
