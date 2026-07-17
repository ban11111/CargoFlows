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
		&models.AIContentTemplate{}, &models.AIContentTemplateVersion{}, &models.AIContentSlot{}, &models.AIJob{}, &models.AIJobItem{},
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
		{PublicID: uuid.NewString(), AIContentTemplateVersionID: published.ID, SlotKey: "title", Kind: models.AIContentSlotTitle, NameZH: "标题", NameEN: "Title", Sequence: 1, Optional: true, DefaultSelected: true, PromptFragment: "title", ConstraintsJSON: []byte(`{}`), GenerationConfigJSON: []byte(`{"candidate_count":3}`), LayoutConfigJSON: []byte(`{}`)},
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
		SKUID: fixture.SKU.ID, TemplateVersionPublicID: fixture.PublishedVersion.PublicID,
		SelectedSlotKeys: []string{"hero", "title"}, SelectedAssetIDs: []uint{fixture.ApprovedAsset.ID, fixture.ApprovedAsset.ID},
		Locale: "zh-CN", CreatedByID: fixture.Operator.ID,
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
		want := []uint{}
		if item.Kind == models.AIContentSlotImage {
			want = []uint{fixture.ApprovedAsset.ID}
		}
		if !reflect.DeepEqual(item.SelectedInputAssetIDs, want) {
			t.Fatalf("%s selected assets = %v", item.SlotKey, item.SelectedInputAssetIDs)
		}
	}
	snapshotText := string(job.InputSnapshot)
	for _, forbidden := range []string{"low_stock_threshold", `"stock"`, `"status"`, "password_hash", "created_by_id", "barcode"} {
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
}

func TestCreateJobAllowsTextOnlyWithoutAssets(t *testing.T) {
	db, fixture := seedAIJobFixture(t)
	job, err := NewJobService(db).Create(t.Context(), CreateJobInput{SKUID: fixture.SKU.ID, TemplateVersionPublicID: fixture.PublishedVersion.PublicID, SelectedSlotKeys: []string{"seo", "title"}, Locale: "en", CreatedByID: fixture.Operator.ID})
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
			return CreateJobInput{SKUID: f.SKU.ID, TemplateVersionPublicID: f.DraftVersion.PublicID, SelectedSlotKeys: []string{"title"}}
		}, ErrTemplateVersionNotPublished},
		{"empty slots", func(f aiJobFixture) CreateJobInput {
			return CreateJobInput{SKUID: f.SKU.ID, TemplateVersionPublicID: f.PublishedVersion.PublicID}
		}, ErrSlotSelectionInvalid},
		{"duplicate slot", func(f aiJobFixture) CreateJobInput {
			return CreateJobInput{SKUID: f.SKU.ID, TemplateVersionPublicID: f.PublishedVersion.PublicID, SelectedSlotKeys: []string{"title", "title"}}
		}, ErrSlotSelectionInvalid},
		{"unknown slot", func(f aiJobFixture) CreateJobInput {
			return CreateJobInput{SKUID: f.SKU.ID, TemplateVersionPublicID: f.PublishedVersion.PublicID, SelectedSlotKeys: []string{"missing"}}
		}, ErrSlotSelectionInvalid},
		{"cross sku asset", func(f aiJobFixture) CreateJobInput {
			return CreateJobInput{SKUID: f.SKU.ID, TemplateVersionPublicID: f.PublishedVersion.PublicID, SelectedSlotKeys: []string{"hero"}, SelectedAssetIDs: []uint{f.OtherAsset.ID}}
		}, ErrAssetNotEligible},
		{"zero asset id", func(f aiJobFixture) CreateJobInput {
			return CreateJobInput{SKUID: f.SKU.ID, TemplateVersionPublicID: f.PublishedVersion.PublicID, SelectedSlotKeys: []string{"title"}, SelectedAssetIDs: []uint{0}}
		}, ErrAssetNotEligible},
		{"image without required asset", func(f aiJobFixture) CreateJobInput {
			return CreateJobInput{SKUID: f.SKU.ID, TemplateVersionPublicID: f.PublishedVersion.PublicID, SelectedSlotKeys: []string{"hero"}}
		}, ErrAssetNotEligible},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db, fixture := seedAIJobFixture(t)
			if _, err := NewJobService(db).Create(t.Context(), tc.mutate(fixture)); !errors.Is(err, tc.want) {
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
	_, err := NewJobService(db).Create(t.Context(), CreateJobInput{SKUID: fixture.SKU.ID, TemplateVersionPublicID: fixture.PublishedVersion.PublicID, SelectedSlotKeys: []string{"title"}, CreatedByID: fixture.Operator.ID})
	if err == nil {
		t.Fatal("expected failure")
	}
	var jobs int64
	db.Model(&models.AIJob{}).Count(&jobs)
	if jobs != 0 {
		t.Fatalf("job was not rolled back: %d", jobs)
	}
}
