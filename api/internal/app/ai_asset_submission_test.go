package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cargoflows/api/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestSelectedAIImageSubmissionIsPendingAndIdempotent(t *testing.T) {
	db := newTestDB(t)
	user := models.User{Name: "Reviewer", Email: uuid.NewString() + "@example.test", PasswordHash: "x", Role: models.RoleAdmin, Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	category := models.Category{Name: "Cases"}
	db.Create(&category)
	product := models.Product{Name: "Case", CategoryID: category.ID}
	db.Create(&product)
	sku := models.SKU{ProductID: product.ID, Code: "CASE-A", Status: "active"}
	db.Create(&sku)
	template := models.AIContentTemplate{PublicID: uuid.NewString(), NameZH: "图", NameEN: "Image", TargetPlatform: "test", Status: models.AIContentTemplateActive, CreatedByID: user.ID}
	db.Create(&template)
	version := models.AIContentTemplateVersion{PublicID: uuid.NewString(), AIContentTemplateID: template.ID, VersionNumber: 1, Status: models.AITemplatePublished, DefaultLocale: "zh-CN", PromptCompilerVersion: "image-v1", PlatformPrompt: "test", CreatedByID: user.ID}
	db.Create(&version)
	slot := models.AIContentSlot{PublicID: uuid.NewString(), AIContentTemplateVersionID: version.ID, SlotKey: "hero", Kind: models.AIContentSlotImage, NameZH: "主图", NameEN: "Hero", Sequence: 1, PromptFragment: "hero", ConstraintsJSON: []byte(`{}`), GenerationConfigJSON: []byte(`{}`), LayoutConfigJSON: []byte(`{}`)}
	db.Create(&slot)
	job := models.AIJob{PublicID: uuid.NewString(), SKUID: sku.ID, AIContentTemplateVersionID: version.ID, TargetPlatform: "test", Locale: "zh-CN", Status: models.AIJobCompleted, SnapshotSchema: "cargoflows_product_generation_v1", InputSnapshotJSON: []byte(`{}`), CreatedByID: user.ID}
	db.Create(&job)
	item := models.AIJobItem{PublicID: uuid.NewString(), AIJobID: job.ID, AIContentSlotID: slot.ID, SlotKey: "hero", Kind: models.AIContentSlotImage, Status: models.AIJobItemCompleted, SlotSnapshotJSON: []byte(`{}`), SelectedInputAssetIDsJSON: []byte(`[]`)}
	db.Create(&item)
	thread := models.AIImageThread{PublicID: uuid.NewString(), AIJobItemID: item.ID}
	db.Create(&thread)
	turn := models.AIImageTurn{PublicID: uuid.NewString(), AIImageThreadID: thread.ID, Sequence: 1, Operation: models.AIExecutionGenerate, ActorID: user.ID, Status: models.AIImageTurnCompleted}
	db.Create(&turn)
	execution := models.AIExecution{PublicID: uuid.NewString(), AIJobItemID: item.ID, AIImageTurnID: &turn.ID, Operation: models.AIExecutionGenerate, Status: models.AIExecutionCompleted, AttemptNumber: 1, Model: "gpt-image-2", RequestedModel: "gpt-image-2", APIMode: "images", NormalizedInputJSON: []byte(`{}`), OrderedInputListJSON: []byte(`[]`), RequestConfigJSON: []byte(`{}`), ProviderOutputJSON: []byte(`{}`)}
	db.Create(&execution)
	result := models.AIImageResult{PublicID: uuid.NewString(), AIImageTurnID: turn.ID, AIExecutionID: execution.ID, CandidateIndex: 1, ObjectKey: "generated/result.png", MIMEType: "image/png", Width: 1, Height: 1, ByteCount: 10, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	db.Create(&result)
	db.Model(&thread).Update("selected_result_id", result.ID)
	storage := &fakeAssetStorage{objects: map[string][]byte{"generated/result.png": defaultFakeAssetImage}}
	server := &Server{db: db, storage: storage}
	call := func() int {
		rec := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(rec)
		context.Request = httptest.NewRequest(http.MethodPost, "/", nil)
		context.Params = gin.Params{{Key: "job_id", Value: job.PublicID}, {Key: "item_id", Value: item.PublicID}, {Key: "result_id", Value: result.PublicID}}
		context.Set("user", user)
		server.submitAIImageResultToAssets(context)
		var body map[string]any
		if json.Unmarshal(rec.Body.Bytes(), &body) != nil || body["origin_type"] != "ai_generated" {
			t.Fatalf("response %d %s", rec.Code, rec.Body.String())
		}
		return rec.Code
	}
	if status := call(); status != http.StatusCreated {
		t.Fatalf("first status=%d", status)
	}
	if status := call(); status != http.StatusOK {
		t.Fatalf("replay status=%d", status)
	}
	var count int64
	db.Model(&models.Asset{}).Where("source_ai_image_result_id=?", result.ID).Count(&count)
	if count != 1 {
		t.Fatalf("assets=%d", count)
	}
	var asset models.Asset
	db.Where("source_ai_image_result_id=?", result.ID).First(&asset)
	if asset.ReviewStatus != "pending" || asset.SKUID != sku.ID {
		t.Fatalf("asset=%#v", asset)
	}
}
