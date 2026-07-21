package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cargoflows/api/internal/models"
	"github.com/gin-gonic/gin"
)

func TestAIReferenceSignedUploadCreatesImmutableNormalizedItem(t *testing.T) {
	db := newTestDB(t)
	category, administrator := seedSOPCategoryAndUser(t, db)
	sop := models.AIReferenceSOP{CategoryID: category.ID, CreatedByID: administrator.ID}
	if err := db.Create(&sop).Error; err != nil {
		t.Fatal(err)
	}
	version := models.AIReferenceSOPVersion{AIReferenceSOPID: sop.ID, VersionNumber: 1, NameZH: "套机参考", NameEN: "Fitted reference", Status: models.SOPVersionDraft}
	if err := db.Create(&version).Error; err != nil {
		t.Fatal(err)
	}
	storage := &fakeAssetStorage{objects: map[string][]byte{}}
	server := &Server{db: db, cfg: testAssetConfig(), storage: storage}

	ticketResponse := httptest.NewRecorder()
	ticketContext, _ := gin.CreateTestContext(ticketResponse)
	ticketContext.Params = gin.Params{{Key: "version_id", Value: version.PublicID}}
	ticketContext.Request = httptest.NewRequest(http.MethodPost, "/items/upload-url", strings.NewReader(`{"purpose":"usage_effect","file_name":"reference.jpg","content_type":"image/jpeg"}`))
	ticketContext.Request.Header.Set("Content-Type", "application/json")
	ticketContext.Set("user", administrator)
	server.createAIReferenceItemUploadURL(ticketContext)
	if ticketResponse.Code != http.StatusOK {
		t.Fatalf("ticket status = %d: %s", ticketResponse.Code, ticketResponse.Body.String())
	}
	var ticket struct {
		CompletionToken string `json:"completion_token"`
		Image           struct {
			UploadURL string `json:"upload_url"`
		} `json:"image"`
	}
	if err := json.Unmarshal(ticketResponse.Body.Bytes(), &ticket); err != nil || !isUUID(ticket.CompletionToken) || ticket.Image.UploadURL == "" {
		t.Fatalf("invalid ticket: %#v, err=%v", ticket, err)
	}
	var upload models.AIReferenceUpload
	if err := db.Where("public_id=?", ticket.CompletionToken).First(&upload).Error; err != nil {
		t.Fatal(err)
	}
	storage.objects[upload.TemporaryKey] = append([]byte(nil), defaultFakeAssetImage...)

	completionPayload := `{"completion_token":"` + ticket.CompletionToken + `","caption_zh":"比例参考","caption_en":"Proportion reference","allowed_guidance_zh":"比例和视角","allowed_guidance_en":"Proportion and angle","forbidden_guidance_zh":"品牌和兼容性","forbidden_guidance_en":"Brand and compatibility","source_name":"Competitor","rights_confirmed":true}`
	completeResponse := httptest.NewRecorder()
	completeContext, _ := gin.CreateTestContext(completeResponse)
	completeContext.Params = ticketContext.Params
	completeContext.Request = httptest.NewRequest(http.MethodPost, "/items/complete", strings.NewReader(completionPayload))
	completeContext.Request.Header.Set("Content-Type", "application/json")
	completeContext.Set("user", administrator)
	server.completeAIReferenceItemUpload(completeContext)
	if completeResponse.Code != http.StatusCreated {
		t.Fatalf("completion status = %d: %s", completeResponse.Code, completeResponse.Body.String())
	}
	var item models.AIReferenceItem
	if err := db.First(&item).Error; err != nil {
		t.Fatal(err)
	}
	if item.Purpose != models.AIReferenceUsageEffect || item.MIMEType != "image/png" || len(item.SHA256) != 64 || item.ObjectKey == upload.TemporaryKey || item.ThumbnailObjectKey == "" {
		t.Fatalf("unexpected normalized item: %#v", item)
	}
	if _, exists := storage.objects[upload.TemporaryKey]; exists {
		t.Fatal("temporary upload was not deleted after completion")
	}

	replayResponse := httptest.NewRecorder()
	replayContext, _ := gin.CreateTestContext(replayResponse)
	replayContext.Params = ticketContext.Params
	replayContext.Request = httptest.NewRequest(http.MethodPost, "/items/complete", strings.NewReader(completionPayload))
	replayContext.Request.Header.Set("Content-Type", "application/json")
	replayContext.Set("user", administrator)
	server.completeAIReferenceItemUpload(replayContext)
	if replayResponse.Code == http.StatusCreated {
		t.Fatal("one-time completion token was accepted twice")
	}
}
