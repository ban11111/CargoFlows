package ai

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"cargoflow/api/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type aiJobFixture struct {
	SKU              models.SKU
	OtherSKU         models.SKU
	PublishedVersion models.AIContentTemplateVersion
	DraftVersion     models.AIContentTemplateVersion
	ApprovedAsset    models.Asset
	OtherAsset       models.Asset
	Operator         models.User
}

func seedAIJobFixture(t *testing.T) (*gorm.DB, aiJobFixture) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.User{}, &models.Category{}, &models.Tag{}, &models.Product{}, &models.SKU{},
		&models.CaptureSOP{}, &models.SOPVersion{}, &models.SOPView{}, &models.PhotoSession{}, &models.Asset{},
		&models.AIContentTemplate{}, &models.AIContentTemplateVersion{}, &models.AIContentSlot{}, &models.AIJob{}, &models.AIJobItem{}, &models.AIAuditEvent{},
	); err != nil {
		t.Fatal(err)
	}
	operator := models.User{Name: "Operator", Email: uuid.NewString() + "@example.test", PasswordHash: "do-not-snapshot", Role: models.RoleOperator, Status: "active"}
	category := models.Category{Name: "手机壳", NameEN: "Phone cases"}
	if err := db.Create(&operator).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&category).Error; err != nil {
		t.Fatal(err)
	}
	product := models.Product{CategoryID: category.ID, Name: "透明手机壳", Brand: "CargoFlow", Category: category.Name, Description: "轻薄透明保护壳"}
	if err := db.Create(&product).Error; err != nil {
		t.Fatal(err)
	}
	tags := []models.Tag{{Name: "透明-" + uuid.NewString()}, {Name: "轻薄-" + uuid.NewString()}}
	if err := db.Create(&tags).Error; err != nil {
		t.Fatal(err)
	}
	sku := models.SKU{ProductID: product.ID, Code: "CASE-17-PRO", Color: "透明", Size: "iPhone 17 Pro", Barcode: "secret-ish-barcode", Stock: 99, LowStockThreshold: 5, PlatformTitle: "透明手机壳", SellingPoints: "轻薄;防刮", Status: "active", Tags: []models.Tag{tags[1], tags[0]}}
	otherSKU := models.SKU{ProductID: product.ID, Code: "CASE-17-AIR", Color: "透明", Size: "iPhone 17 Air", Status: "active"}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&otherSKU).Error; err != nil {
		t.Fatal(err)
	}

	sop := models.CaptureSOP{PublicID: uuid.NewString(), CategoryID: category.ID, CreatedByID: operator.ID}
	if err := db.Create(&sop).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	sopVersion := models.SOPVersion{PublicID: uuid.NewString(), CaptureSOPID: sop.ID, VersionNumber: 2, SchemaVersion: "1.0", NameZH: "手机壳 SOP", NameEN: "Phone case SOP", DescriptionZH: "标准拍摄", DescriptionEN: "Standard capture", Status: models.SOPVersionPublished, CoordinateSystem: "pcs_object_v1", PublishedAt: &now}
	if err := db.Create(&sopVersion).Error; err != nil {
		t.Fatal(err)
	}
	front := models.SOPView{PublicID: uuid.NewString(), SOPVersionID: sopVersion.ID, Sequence: 1, Role: models.SOPViewReferenceFront, ViewKind: models.SOPViewStandard, PresetKey: "reference_front", NameZH: "正面", NameEN: "Front", InstructionZH: "正面拍摄", InstructionEN: "Front capture", Required: true, CameraPositionZ: 1, ImageUpX: 1, Composition: models.Composition{FrameOccupancy: .85, AspectRatio: "1:1", AllowRotationCorrection: true}}
	if err := db.Create(&front).Error; err != nil {
		t.Fatal(err)
	}
	session := models.PhotoSession{PublicID: uuid.NewString(), Code: "PS-" + uuid.NewString(), SKUID: sku.ID, SOPVersionID: sopVersion.ID, PhotographerID: operator.ID, Status: "completed"}
	otherSession := models.PhotoSession{PublicID: uuid.NewString(), Code: "PS-" + uuid.NewString(), SKUID: otherSKU.ID, SOPVersionID: sopVersion.ID, PhotographerID: operator.ID, Status: "completed"}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&otherSession).Error; err != nil {
		t.Fatal(err)
	}
	approved := models.Asset{SKUID: sku.ID, PhotoSessionID: session.ID, SOPViewID: front.ID, ObjectKey: "approved/" + uuid.NewString() + ".jpg", OriginalURL: "https://assets.example.test/approved.jpg", ThumbnailURL: "https://assets.example.test/approved-thumb.jpg", ReviewStatus: "approved", CapturedAt: now}
	otherAsset := models.Asset{SKUID: otherSKU.ID, PhotoSessionID: otherSession.ID, SOPViewID: front.ID, ObjectKey: "other/" + uuid.NewString() + ".jpg", OriginalURL: "https://assets.example.test/other.jpg", ReviewStatus: "approved", CapturedAt: now}
	if err := db.Create(&approved).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&otherAsset).Error; err != nil {
		t.Fatal(err)
	}

	template := models.AIContentTemplate{PublicID: uuid.NewString(), NameZH: "Lazada 套图", NameEN: "Lazada set", TargetPlatform: "lazada", Status: models.AIContentTemplateActive, CreatedByID: operator.ID}
	if err := db.Create(&template).Error; err != nil {
		t.Fatal(err)
	}
	published := models.AIContentTemplateVersion{PublicID: uuid.NewString(), AIContentTemplateID: template.ID, VersionNumber: 1, Status: models.AITemplatePublished, DefaultLocale: "zh-CN", PromptCompilerVersion: "v1", PlatformPrompt: "Lazada content", CreatedByID: operator.ID, PublishedByID: &operator.ID, PublishedAt: &now}
	draftGuard := "draft"
	draft := models.AIContentTemplateVersion{PublicID: uuid.NewString(), AIContentTemplateID: template.ID, VersionNumber: 2, Status: models.AITemplateDraft, DraftGuard: &draftGuard, DefaultLocale: "zh-CN", PromptCompilerVersion: "v1", PlatformPrompt: "draft", CreatedByID: operator.ID}
	if err := db.Create(&published).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&draft).Error; err != nil {
		t.Fatal(err)
	}
	slots := []models.AIContentSlot{
		{PublicID: uuid.NewString(), AIContentTemplateVersionID: published.ID, SlotKey: "hero", Kind: models.AIContentSlotImage, NameZH: "主图", NameEN: "Hero", Sequence: 2, Optional: true, DefaultSelected: true, PromptFragment: "hero", ConstraintsJSON: []byte(`{"required_views":["reference_front"]}`), GenerationConfigJSON: []byte(`{"size":"1024x1024"}`), LayoutConfigJSON: []byte(`{}`)},
		{PublicID: uuid.NewString(), AIContentTemplateVersionID: published.ID, SlotKey: "title", Kind: models.AIContentSlotTitle, NameZH: "标题", NameEN: "Title", Sequence: 1, Optional: true, DefaultSelected: true, PromptFragment: "title", ConstraintsJSON: []byte(`{}`), GenerationConfigJSON: []byte(`{"candidate_count":3,"allowed_candidate_count":[1,3],"allow_user_extra_prompt":true}`), LayoutConfigJSON: []byte(`{}`)},
		{PublicID: uuid.NewString(), AIContentTemplateVersionID: published.ID, SlotKey: "seo", Kind: models.AIContentSlotSEODescription, NameZH: "搜索描述", NameEN: "SEO description", Sequence: 3, Optional: true, PromptFragment: "seo", ConstraintsJSON: []byte(`{}`), GenerationConfigJSON: []byte(`{"candidate_count":1}`), LayoutConfigJSON: []byte(`{}`)},
	}
	if err := db.Create(&slots).Error; err != nil {
		t.Fatal(err)
	}
	return db, aiJobFixture{SKU: sku, OtherSKU: otherSKU, PublishedVersion: published, DraftVersion: draft, ApprovedAsset: approved, OtherAsset: otherAsset, Operator: operator}
}

func TestCreateJobSnapshotsOnlyWhitelistedFactsAndSelectedSlots(t *testing.T) {
	db, fixture := seedAIJobFixture(t)
	job, err := NewJobService(db).Create(t.Context(), CreateJobInput{
		SKUID: fixture.SKU.PublicID, TemplateVersionPublicID: fixture.PublishedVersion.PublicID,
		SelectedSlotKeys: []string{"hero", "title"}, SelectedAssetIDs: []string{fixture.ApprovedAsset.PublicID, fixture.ApprovedAsset.PublicID},
		Locale: "zh-CN", CreatedByID: fixture.Operator.ID,
		IdempotencyKey: "job-test-whitelist",
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.PublicID == "" || job.Status != models.AIJobQueued || len(job.Items) != 2 {
		t.Fatalf("unexpected job: %#v", job)
	}
	if got := []string{job.Items[0].SlotKey, job.Items[1].SlotKey}; !reflect.DeepEqual(got, []string{"title", "hero"}) {
		t.Fatalf("item order = %v", got)
	}
	for _, item := range job.Items {
		want := []string{}
		if item.Kind == models.AIContentSlotImage {
			want = []string{fixture.ApprovedAsset.PublicID}
		}
		if !reflect.DeepEqual(item.SelectedInputAssetIDs, want) {
			t.Fatalf("%s selected assets = %v", item.SlotKey, item.SelectedInputAssetIDs)
		}
	}
	snapshotText := string(job.InputSnapshot)
	for _, forbidden := range []string{"low_stock_threshold", `"stock"`, `"status"`, "password_hash", "created_by_id", "barcode", "object_key", "original_url", "thumbnail_url", `"id":`} {
		if strings.Contains(snapshotText, forbidden) {
			t.Fatalf("snapshot contains %q: %s", forbidden, snapshotText)
		}
	}
	var snapshot ProductSnapshotV1
	if err := json.Unmarshal(job.InputSnapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Schema != ProductSnapshotSchemaV1 || snapshot.Product.Category.NameEN != "Phone cases" || snapshot.SOP.CoordinateSystem != "pcs_object_v1" || len(snapshot.SelectedAssets) != 1 || len(snapshot.Template.SelectedSlots) != 2 || !snapshot.Template.SelectedSlots[0].DefaultSelected {
		t.Fatalf("incomplete snapshot: %#v", snapshot)
	}
	if job.SKUID != fixture.SKU.PublicID || snapshot.SKU.PublicID != fixture.SKU.PublicID || snapshot.SelectedAssets[0].PublicID != fixture.ApprovedAsset.PublicID {
		t.Fatalf("public identity contract was not preserved: job=%#v snapshot=%#v", job, snapshot)
	}
	if snapshot.SelectedAssets[0].View.CameraPositionDirection.Z != 1 || snapshot.SelectedAssets[0].View.Instruction.EN != "Front capture" || snapshot.SelectedAssets[0].View.Composition.AspectRatio != "1:1" {
		t.Fatalf("asset-specific view was not snapshotted: %#v", snapshot.SelectedAssets[0].View)
	}
}

func TestSupplementalAssetsAreInformationOnlyAndCannotSatisfyImageViews(t *testing.T) {
	db, fixture := seedAIJobFixture(t)
	var front models.SOPView
	if err := db.First(&front, fixture.ApprovedAsset.SOPViewID).Error; err != nil {
		t.Fatal(err)
	}
	supplementalView := models.SOPView{
		PublicID: uuid.NewString(), SOPVersionID: front.SOPVersionID, Sequence: 2,
		Role: models.SOPViewCapture, ViewKind: models.SOPViewDetail, PresetKey: "supplemental_info",
		NameZH: "补充信息图片", NameEN: "Supplemental Product Information", Required: false, AllowMultiple: true,
		CameraPositionZ: 1, ImageUpX: 1, Composition: models.Composition{FrameOccupancy: .95, AspectRatio: "4:5", AllowRotationCorrection: true},
	}
	if err := db.Create(&supplementalView).Error; err != nil {
		t.Fatal(err)
	}
	supplemental := models.Asset{
		PublicID: uuid.NewString(), SKUID: fixture.SKU.ID, PhotoSessionID: fixture.ApprovedAsset.PhotoSessionID, SOPViewID: supplementalView.ID,
		ObjectKey: "approved/" + uuid.NewString() + ".jpg", OriginalURL: "private://supplemental", ReviewStatus: "approved", CapturedAt: time.Now().UTC(),
	}
	if err := db.Create(&supplemental).Error; err != nil {
		t.Fatal(err)
	}

	job, err := NewJobService(db).Create(t.Context(), CreateJobInput{
		SKUID: fixture.SKU.PublicID, TemplateVersionPublicID: fixture.PublishedVersion.PublicID,
		SelectedSlotKeys: []string{"title"}, SelectedAssetIDs: []string{supplemental.PublicID}, Locale: "zh-CN",
		CreatedByID: fixture.Operator.ID, IdempotencyKey: "job-supplemental-text",
	})
	if err != nil {
		t.Fatal(err)
	}
	var snapshot ProductSnapshotV1
	if err := json.Unmarshal(job.InputSnapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.SelectedAssets) != 1 || snapshot.SelectedAssets[0].SourceType != AssetSourceProductInformation || snapshot.SelectedAssets[0].View.PresetKey != "supplemental_info" {
		t.Fatalf("supplemental asset semantics = %#v", snapshot.SelectedAssets)
	}
	if len(job.Items) != 1 || !reflect.DeepEqual(job.Items[0].SelectedInputAssetIDs, []string{supplemental.PublicID}) {
		t.Fatalf("text item supplemental inputs = %#v", job.Items)
	}

	_, err = NewJobService(db).Create(t.Context(), CreateJobInput{
		SKUID: fixture.SKU.PublicID, TemplateVersionPublicID: fixture.PublishedVersion.PublicID,
		SelectedSlotKeys: []string{"hero"}, SelectedAssetIDs: []string{supplemental.PublicID}, Locale: "zh-CN",
		CreatedByID: fixture.Operator.ID, IdempotencyKey: "job-supplemental-image-only",
	})
	if !errors.Is(err, ErrAssetNotEligible) {
		t.Fatalf("image-only supplemental error = %v", err)
	}
}

func TestCreateJobAllowsTextOnlyWithoutAssets(t *testing.T) {
	db, fixture := seedAIJobFixture(t)
	job, err := NewJobService(db).Create(t.Context(), CreateJobInput{SKUID: fixture.SKU.PublicID, TemplateVersionPublicID: fixture.PublishedVersion.PublicID, SelectedSlotKeys: []string{"seo", "title"}, Locale: "en", CreatedByID: fixture.Operator.ID, IdempotencyKey: "job-test-text-only"})
	if err != nil {
		t.Fatal(err)
	}
	if len(job.Items) != 2 || len(job.Items[0].SelectedInputAssetIDs) != 0 {
		t.Fatalf("text-only job = %#v", job)
	}
}

func TestCreateJobRejectsInvalidTemplateSlotsAndAssetsWithoutWriting(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(aiJobFixture) CreateJobInput
		want   error
	}{
		{"draft template", func(f aiJobFixture) CreateJobInput {
			return CreateJobInput{SKUID: f.SKU.PublicID, TemplateVersionPublicID: f.DraftVersion.PublicID, SelectedSlotKeys: []string{"title"}}
		}, ErrTemplateVersionNotPublished},
		{"empty slots", func(f aiJobFixture) CreateJobInput {
			return CreateJobInput{SKUID: f.SKU.PublicID, TemplateVersionPublicID: f.PublishedVersion.PublicID}
		}, ErrSlotSelectionInvalid},
		{"duplicate slot", func(f aiJobFixture) CreateJobInput {
			return CreateJobInput{SKUID: f.SKU.PublicID, TemplateVersionPublicID: f.PublishedVersion.PublicID, SelectedSlotKeys: []string{"title", "title"}}
		}, ErrSlotSelectionInvalid},
		{"unknown slot", func(f aiJobFixture) CreateJobInput {
			return CreateJobInput{SKUID: f.SKU.PublicID, TemplateVersionPublicID: f.PublishedVersion.PublicID, SelectedSlotKeys: []string{"missing"}}
		}, ErrSlotSelectionInvalid},
		{"cross sku asset", func(f aiJobFixture) CreateJobInput {
			return CreateJobInput{SKUID: f.SKU.PublicID, TemplateVersionPublicID: f.PublishedVersion.PublicID, SelectedSlotKeys: []string{"hero"}, SelectedAssetIDs: []string{f.OtherAsset.PublicID}}
		}, ErrAssetNotEligible},
		{"invalid asset id", func(f aiJobFixture) CreateJobInput {
			return CreateJobInput{SKUID: f.SKU.PublicID, TemplateVersionPublicID: f.PublishedVersion.PublicID, SelectedSlotKeys: []string{"title"}, SelectedAssetIDs: []string{"not-a-uuid"}}
		}, ErrAssetIDInvalid},
		{"image without required asset", func(f aiJobFixture) CreateJobInput {
			return CreateJobInput{SKUID: f.SKU.PublicID, TemplateVersionPublicID: f.PublishedVersion.PublicID, SelectedSlotKeys: []string{"hero"}}
		}, ErrAssetNotEligible},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db, fixture := seedAIJobFixture(t)
			input := tc.mutate(fixture)
			input.IdempotencyKey = "job-invalid-" + strings.ReplaceAll(tc.name, " ", "-")
			if input.Locale == "" {
				input.Locale = "zh-CN"
			}
			if _, err := NewJobService(db).Create(t.Context(), input); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			var jobs, items int64
			db.Model(&models.AIJob{}).Count(&jobs)
			db.Model(&models.AIJobItem{}).Count(&items)
			if jobs != 0 || items != 0 {
				t.Fatalf("partial writes: jobs=%d items=%d", jobs, items)
			}
		})
	}
}

func TestCreateJobRollsBackWhenItemInsertFails(t *testing.T) {
	db, fixture := seedAIJobFixture(t)
	callbackName := "test:fail_ai_job_item_insert"
	if err := db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*models.AIJobItem); ok {
			tx.AddError(errors.New("forced item insert failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callbackName) })
	_, err := NewJobService(db).Create(t.Context(), CreateJobInput{SKUID: fixture.SKU.PublicID, TemplateVersionPublicID: fixture.PublishedVersion.PublicID, SelectedSlotKeys: []string{"title"}, Locale: "zh-CN", CreatedByID: fixture.Operator.ID, IdempotencyKey: "job-test-rollback"})
	if err == nil {
		t.Fatal("expected failure")
	}
	var jobs int64
	db.Model(&models.AIJob{}).Count(&jobs)
	if jobs != 0 {
		t.Fatalf("job was not rolled back: %d", jobs)
	}
}

func TestCreateJobIsIdempotentAuditedAndRejectsKeyReuse(t *testing.T) {
	db, fixture := seedAIJobFixture(t)
	service := NewJobService(db)
	input := CreateJobInput{SKUID: fixture.SKU.PublicID, TemplateVersionPublicID: fixture.PublishedVersion.PublicID, SelectedSlotKeys: []string{"title"}, SelectedAssetIDs: []string{}, Locale: "zh-CN", CreatedByID: fixture.Operator.ID, IdempotencyKey: "job-idempotency-0001", UserPreference: "  clean premium layout  ", GenerationOverrides: map[string]GenerationOverride{"title": {CandidateCount: intPointer(3)}}}
	first, err := service.Create(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Create(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if first.PublicID != second.PublicID {
		t.Fatalf("replay created another job: %s != %s", first.PublicID, second.PublicID)
	}
	var jobs, items, events int64
	db.Model(&models.AIJob{}).Count(&jobs)
	db.Model(&models.AIJobItem{}).Count(&items)
	db.Model(&models.AIAuditEvent{}).Count(&events)
	if jobs != 1 || items != 1 || events != 1 {
		t.Fatalf("counts jobs/items/events = %d/%d/%d", jobs, items, events)
	}
	var audit models.AIAuditEvent
	if err := db.First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	if audit.EventType != "ai_job.created" || audit.ActorID == nil || *audit.ActorID != fixture.Operator.ID || audit.AIJobID == nil || strings.Contains(string(audit.MetadataJSON), "premium") {
		t.Fatalf("unsafe/incomplete audit event: %#v %s", audit, audit.MetadataJSON)
	}
	input.Locale = "en"
	if _, err := service.Create(t.Context(), input); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("key reuse error = %v", err)
	}
	var snapshot ProductSnapshotV1
	if err := json.Unmarshal(first.InputSnapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.UserPreference != "clean premium layout" || snapshot.GenerationOverrides["title"].CandidateCount == nil {
		t.Fatalf("missing immutable preference/override: %#v", snapshot)
	}
	normalized, hash, err := normalizeCreateJobInput(CreateJobInput{SKUID: fixture.SKU.PublicID, TemplateVersionPublicID: fixture.PublishedVersion.PublicID, SelectedSlotKeys: []string{"title"}, SelectedAssetIDs: []string{}, Locale: "zh-CN", CreatedByID: fixture.Operator.ID, IdempotencyKey: "job-idempotency-0001", UserPreference: "  clean premium layout  ", GenerationOverrides: map[string]GenerationOverride{"title": {CandidateCount: intPointer(3)}}})
	if err != nil {
		t.Fatal(err)
	}
	if recovered, ok, err := service.recoverIdempotentCreate(t.Context(), normalized, hash); err != nil || !ok || recovered.PublicID != first.PublicID {
		t.Fatalf("unique-race recovery = %#v/%v/%v", recovered, ok, err)
	}
	if _, ok, err := service.recoverIdempotentCreate(t.Context(), normalized, strings.Repeat("0", 64)); !ok || !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("unique-race mismatch = %v/%v", ok, err)
	}
}

func TestCreateJobRejectsInvalidRuntimeConfiguration(t *testing.T) {
	db, fixture := seedAIJobFixture(t)
	service := NewJobService(db)
	base := CreateJobInput{SKUID: fixture.SKU.PublicID, TemplateVersionPublicID: fixture.PublishedVersion.PublicID, SelectedSlotKeys: []string{"title"}, Locale: "zh-CN", CreatedByID: fixture.Operator.ID, IdempotencyKey: "job-runtime-invalid"}
	base.IdempotencyKey = ""
	if _, err := service.Create(t.Context(), base); !errors.Is(err, ErrIdempotencyKeyInvalid) {
		t.Fatalf("idempotency key error = %v", err)
	}
	base.IdempotencyKey = "job-runtime-preference"
	base.UserPreference = strings.Repeat("图", 1001)
	if _, err := service.Create(t.Context(), base); !errors.Is(err, ErrUserPreferenceInvalid) {
		t.Fatalf("preference error = %v", err)
	}
	base.UserPreference = ""
	base.Locale = " ZH-cn "
	normalized, _, err := normalizeCreateJobInput(base)
	if err != nil || normalized.Locale != "zh-CN" {
		t.Fatalf("locale normalization = %q/%v", normalized.Locale, err)
	}
	base.Locale = strings.Repeat("x", 33)
	if _, err := service.Create(t.Context(), base); !errors.Is(err, ErrLocaleInvalid) {
		t.Fatalf("locale error = %v", err)
	}
	base.Locale = "zh-CN"
	base.IdempotencyKey = "job-runtime-override"
	base.GenerationOverrides = map[string]GenerationOverride{"title": {CandidateCount: intPointer(9)}}
	if _, err := service.Create(t.Context(), base); !errors.Is(err, ErrGenerationOverrideInvalid) {
		t.Fatalf("override error = %v", err)
	}
	update := db.Model(&models.AIContentSlot{}).Where(&models.AIContentSlot{AIContentTemplateVersionID: fixture.PublishedVersion.ID, SlotKey: "hero"}).Update("constraints_json", []byte(`{"required_views":null}`))
	if update.Error != nil || update.RowsAffected != 1 {
		t.Fatalf("malformed fixture update rows/error = %d/%v", update.RowsAffected, update.Error)
	}
	base.IdempotencyKey = "job-runtime-malformed"
	base.GenerationOverrides = nil
	base.SelectedSlotKeys = []string{"hero"}
	base.SelectedAssetIDs = []string{fixture.ApprovedAsset.PublicID}
	if _, err := service.Create(t.Context(), base); !errors.Is(err, ErrPublishedTemplateConfigInvalid) {
		t.Fatalf("malformed config error = %v", err)
	}
}

func TestCreateJobRequiresEverySelectedSlotToAllowUserPreference(t *testing.T) {
	db, fixture := seedAIJobFixture(t)
	_, err := NewJobService(db).Create(t.Context(), CreateJobInput{SKUID: fixture.SKU.PublicID, TemplateVersionPublicID: fixture.PublishedVersion.PublicID, SelectedSlotKeys: []string{"title", "seo"}, Locale: "zh-CN", CreatedByID: fixture.Operator.ID, IdempotencyKey: "job-mixed-preference", UserPreference: "minimal premium"})
	if !errors.Is(err, ErrUserPreferenceNotAllowed) {
		t.Fatalf("mixed-slot preference error = %v", err)
	}
}

func TestCreateJobTreatsLegacyNonBooleanPreferencePermissionAsBrokenConfig(t *testing.T) {
	db, fixture := seedAIJobFixture(t)
	update := db.Model(&models.AIContentSlot{}).Where(&models.AIContentSlot{AIContentTemplateVersionID: fixture.PublishedVersion.ID, SlotKey: "title"}).Update("generation_config_json", []byte(`{"allow_user_extra_prompt":"yes"}`))
	if update.Error != nil || update.RowsAffected != 1 {
		t.Fatal(update.Error)
	}
	_, err := NewJobService(db).Create(t.Context(), CreateJobInput{SKUID: fixture.SKU.PublicID, TemplateVersionPublicID: fixture.PublishedVersion.PublicID, SelectedSlotKeys: []string{"title"}, Locale: "zh-CN", CreatedByID: fixture.Operator.ID, IdempotencyKey: "job-broken-preference", UserPreference: "minimal"})
	if !errors.Is(err, ErrPublishedTemplateConfigInvalid) {
		t.Fatalf("legacy permission error = %v", err)
	}
}

func TestCreateJobCanonicalizesTemplateUUIDForIdempotency(t *testing.T) {
	db, fixture := seedAIJobFixture(t)
	service := NewJobService(db)
	input := CreateJobInput{SKUID: strings.ToUpper(fixture.SKU.PublicID), TemplateVersionPublicID: strings.ToUpper(fixture.PublishedVersion.PublicID), SelectedSlotKeys: []string{"title"}, Locale: "zh-CN", CreatedByID: fixture.Operator.ID, IdempotencyKey: "job-uuid-canonical"}
	first, err := service.Create(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	input.TemplateVersionPublicID = strings.ToLower(input.TemplateVersionPublicID)
	second, err := service.Create(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if first.PublicID != second.PublicID {
		t.Fatalf("UUID spelling created duplicate %s/%s", first.PublicID, second.PublicID)
	}
	input.IdempotencyKey = "job-invalid-uuid"
	input.TemplateVersionPublicID = "not-a-uuid"
	if _, err := service.Create(t.Context(), input); !errors.Is(err, ErrTemplateVersionIDInvalid) {
		t.Fatalf("invalid UUID error = %v", err)
	}
}

func TestCreateJobDefensivelyBoundsLegacyAllowedOverrideValues(t *testing.T) {
	tests := []struct {
		name, config string
		override     GenerationOverride
	}{{"candidate", `{"allowed_candidate_count":[5]}`, GenerationOverride{CandidateCount: intPointer(5)}}, {"size", `{"allowed_sizes":["999x999"]}`, GenerationOverride{Size: stringPointer("999x999")}}, {"quality", `{"allowed_qualities":["ultra"]}`, GenerationOverride{Quality: stringPointer("ultra")}}, {"style", `{"allowed_styles":["` + strings.Repeat("x", 81) + `"]}`, GenerationOverride{Style: stringPointer(strings.Repeat("x", 81))}}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db, fixture := seedAIJobFixture(t)
			update := db.Model(&models.AIContentSlot{}).Where(&models.AIContentSlot{AIContentTemplateVersionID: fixture.PublishedVersion.ID, SlotKey: "title"}).Update("generation_config_json", []byte(tc.config))
			if update.Error != nil || update.RowsAffected != 1 {
				t.Fatal(update.Error)
			}
			_, err := NewJobService(db).Create(t.Context(), CreateJobInput{SKUID: fixture.SKU.PublicID, TemplateVersionPublicID: fixture.PublishedVersion.PublicID, SelectedSlotKeys: []string{"title"}, Locale: "zh-CN", CreatedByID: fixture.Operator.ID, IdempotencyKey: "job-bound-" + tc.name, GenerationOverrides: map[string]GenerationOverride{"title": tc.override}})
			if !errors.Is(err, ErrPublishedTemplateConfigInvalid) && !errors.Is(err, ErrGenerationOverrideInvalid) {
				t.Fatalf("legacy boundary error = %v", err)
			}
		})
	}
}

func stringPointer(value string) *string { return &value }

func intPointer(value int) *int { return &value }

func TestCreateJobSelectsMostRecentlyPublishedCategorySOPAcrossLogicalSOPs(t *testing.T) {
	db, fixture := seedAIJobFixture(t)
	old := time.Now().UTC().Add(-24 * time.Hour)
	if err := db.Model(&models.SOPVersion{}).Where("status = ?", models.SOPVersionPublished).Update("published_at", old).Error; err != nil {
		t.Fatal(err)
	}
	var product models.Product
	if err := db.First(&product, fixture.SKU.ProductID).Error; err != nil {
		t.Fatal(err)
	}
	parent := models.CaptureSOP{PublicID: uuid.NewString(), CategoryID: product.CategoryID, CreatedByID: fixture.Operator.ID}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	recent := time.Now().UTC()
	version := models.SOPVersion{PublicID: uuid.NewString(), CaptureSOPID: parent.ID, VersionNumber: 1, SchemaVersion: "1.0", NameZH: "新 SOP", NameEN: "New SOP", DescriptionZH: "新", DescriptionEN: "New", Status: models.SOPVersionPublished, CoordinateSystem: "pcs_object_v1", PublishedAt: &recent}
	if err := db.Create(&version).Error; err != nil {
		t.Fatal(err)
	}
	view := models.SOPView{PublicID: uuid.NewString(), SOPVersionID: version.ID, Sequence: 1, Role: models.SOPViewReferenceFront, ViewKind: models.SOPViewStandard, PresetKey: "reference_front", NameZH: "新正面", NameEN: "New front", InstructionZH: "新", InstructionEN: "New", CameraPositionZ: 1, ImageUpX: 1, Composition: models.Composition{AspectRatio: "1:1"}}
	if err := db.Create(&view).Error; err != nil {
		t.Fatal(err)
	}
	job, err := NewJobService(db).Create(t.Context(), CreateJobInput{SKUID: fixture.SKU.PublicID, TemplateVersionPublicID: fixture.PublishedVersion.PublicID, SelectedSlotKeys: []string{"title"}, Locale: "zh-CN", CreatedByID: fixture.Operator.ID, IdempotencyKey: "job-latest-sop"})
	if err != nil {
		t.Fatal(err)
	}
	var snapshot ProductSnapshotV1
	if err := json.Unmarshal(job.InputSnapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.SOP.VersionPublicID != version.PublicID {
		t.Fatalf("selected SOP %s, want newer publication %s", snapshot.SOP.VersionPublicID, version.PublicID)
	}
}

func TestCreateJobRequestsUpdateLocksForEligibilityRows(t *testing.T) {
	db, fixture := seedAIJobFixture(t)
	locked := map[string]bool{}
	name := "test:observe-ai-job-locks"
	if err := db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
		value, ok := tx.Statement.Clauses["FOR"]
		if !ok {
			return
		}
		locking, ok := value.Expression.(clause.Locking)
		if !ok || locking.Strength != "UPDATE" || tx.Statement.Schema == nil {
			return
		}
		locked[tx.Statement.Schema.Name] = true
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(name) })
	_, err := NewJobService(db).Create(t.Context(), CreateJobInput{SKUID: fixture.SKU.PublicID, TemplateVersionPublicID: fixture.PublishedVersion.PublicID, SelectedSlotKeys: []string{"hero"}, SelectedAssetIDs: []string{fixture.ApprovedAsset.PublicID}, Locale: "zh-CN", CreatedByID: fixture.Operator.ID, IdempotencyKey: "job-locking-test"})
	if err != nil {
		t.Fatal(err)
	}
	for _, schema := range []string{"AIContentTemplateVersion", "Asset", "SOPVersion"} {
		if !locked[schema] {
			t.Errorf("missing UPDATE lock request for %s; observed %v", schema, locked)
		}
	}
}
