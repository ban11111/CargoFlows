package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cargoflows/api/internal/models"
	"github.com/gin-gonic/gin"
)

func TestSOPReferenceUploadUsesOneTimeOpaqueTicketAndFinalObject(t *testing.T) {
	db := newTestDB(t)
	category, manager := seedSOPCategoryAndUser(t, db)
	created := createTestSOP(t, NewSOPService(db), category, manager)
	view := created.Version.Views[0]
	storage := &fakeAssetStorage{objects: make(map[string][]byte)}
	server := &Server{db: db, cfg: testAssetConfig(), storage: storage}

	uploadResponse := httptest.NewRecorder()
	uploadContext, _ := gin.CreateTestContext(uploadResponse)
	uploadContext.Params = gin.Params{{Key: "version_id", Value: created.Version.PublicID}, {Key: "view_id", Value: view.PublicID}}
	uploadContext.Request = httptest.NewRequest(http.MethodPost, "/reference-images/upload-url", strings.NewReader(`{"file_name":"reference.jpg","content_type":"image/jpeg"}`))
	uploadContext.Request.Header.Set("Content-Type", "application/json")
	uploadContext.Set("user", manager)
	server.createSOPReferenceUploadURL(uploadContext)
	if uploadResponse.Code != http.StatusOK {
		t.Fatalf("reference upload ticket = %d %s", uploadResponse.Code, uploadResponse.Body.String())
	}
	if strings.Contains(uploadResponse.Body.String(), "object_key") || strings.Contains(uploadResponse.Body.String(), "asset_url") {
		t.Fatalf("reference upload leaked object locator: %s", uploadResponse.Body.String())
	}
	var envelope struct {
		CompletionToken string `json:"completion_token"`
	}
	if err := json.Unmarshal(uploadResponse.Body.Bytes(), &envelope); err != nil || !isUUID(envelope.CompletionToken) {
		t.Fatalf("opaque completion ticket = %#v, err=%v", envelope, err)
	}
	if storage.lastObjectKey == "" || !strings.HasPrefix(storage.lastObjectKey, "sop-reference-uploads/") {
		t.Fatalf("server did not hold a temporary reference key: %q", storage.lastObjectKey)
	}
	storage.objects[storage.lastObjectKey] = defaultFakeAssetImage

	completeResponse := httptest.NewRecorder()
	completeContext, _ := gin.CreateTestContext(completeResponse)
	completeContext.Params = uploadContext.Params
	completeContext.Request = httptest.NewRequest(http.MethodPost, "/reference-images", strings.NewReader(`{"completion_token":"`+envelope.CompletionToken+`","caption":{"zh-CN":"参考","en":"Reference"}}`))
	completeContext.Request.Header.Set("Content-Type", "application/json")
	completeContext.Request.Header.Set("X-SOP-Version-Updated-At", created.Version.UpdatedAt.Format(time.RFC3339Nano))
	completeContext.Set("user", manager)
	server.addSOPReferenceImage(completeContext)
	if completeResponse.Code != http.StatusCreated {
		t.Fatalf("reference completion = %d %s", completeResponse.Code, completeResponse.Body.String())
	}
	for _, forbidden := range []string{"object_key", "asset_url", "thumbnail_url\":\"http"} {
		if strings.Contains(completeResponse.Body.String(), forbidden) {
			t.Fatalf("reference DTO leaked %q: %s", forbidden, completeResponse.Body.String())
		}
	}
	var image models.SOPViewReferenceImage
	if err := db.First(&image).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(image.ObjectKey, "sop-references/final/") || image.ObjectKey == storage.lastObjectKey {
		t.Fatalf("reference image retained mutable key %q", image.ObjectKey)
	}
	if _, exists := storage.objects[storage.lastObjectKey]; exists {
		t.Fatalf("temporary reference object was not removed")
	}
	overwrite := append([]byte(nil), defaultFakeAssetImage...)
	overwrite[len(overwrite)-1] ^= 0x01
	storage.objects[storage.lastObjectKey] = overwrite // An old presigned PUT may still target the temporary key.
	media := httptest.NewRecorder()
	mediaContext, _ := gin.CreateTestContext(media)
	mediaContext.Params = gin.Params{{Key: "image_id", Value: image.PublicID}}
	mediaContext.Request = httptest.NewRequest(http.MethodGet, "/sop-reference-images/"+image.PublicID+"/media", nil)
	mediaContext.Set("user", manager)
	server.sopReferenceMedia(mediaContext)
	if media.Code != http.StatusOK || !bytes.Equal(media.Body.Bytes(), defaultFakeAssetImage) {
		t.Fatalf("final reference changed after temporary overwrite: status=%d bytes=%d", media.Code, media.Body.Len())
	}

	replay := httptest.NewRecorder()
	replayContext, _ := gin.CreateTestContext(replay)
	replayContext.Params = uploadContext.Params
	replayContext.Request = httptest.NewRequest(http.MethodPost, "/reference-images", strings.NewReader(`{"completion_token":"`+envelope.CompletionToken+`","caption":{"zh-CN":"参考","en":"Reference"}}`))
	replayContext.Request.Header.Set("Content-Type", "application/json")
	replayContext.Request.Header.Set("X-SOP-Version-Updated-At", created.Version.UpdatedAt.Format(time.RFC3339Nano))
	replayContext.Set("user", manager)
	server.addSOPReferenceImage(replayContext)
	if replay.Code != http.StatusBadRequest {
		t.Fatalf("replayed reference ticket = %d %s", replay.Code, replay.Body.String())
	}
}
