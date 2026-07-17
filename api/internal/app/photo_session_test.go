package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"cargoflow/api/internal/ai"
	"cargoflow/api/internal/config"
	"cargoflow/api/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type fakeAssetStorage struct {
	exists        bool
	err           error
	source        []byte
	lastObjectKey string
}

var defaultFakeAssetImage = func() []byte {
	var output bytes.Buffer
	if err := jpeg.Encode(&output, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil); err != nil {
		panic(err)
	}
	return output.Bytes()
}()

type assetUploadTicketResponse struct {
	ObjectKey       string `json:"object_key"`
	CompletionToken string `json:"completion_token"`
}

func (s *fakeAssetStorage) createUploadURL(_ context.Context, objectKey string) (string, string, error) {
	s.lastObjectKey = objectKey
	return "https://upload.example.test/" + objectKey, s.assetURL(objectKey), s.err
}

func (s *fakeAssetStorage) assetURL(objectKey string) string {
	return "https://assets.example.test/cargoflow/" + objectKey
}

func (s *fakeAssetStorage) objectExists(_ context.Context, _ string) (bool, error) {
	return s.exists, s.err
}

func (s *fakeAssetStorage) ReadSource(_ context.Context, _ string) (ai.ImageInput, error) {
	if s.err != nil {
		return ai.ImageInput{}, s.err
	}
	if len(s.source) == 0 {
		return ai.ImageInput{Bytes: defaultFakeAssetImage}, nil
	}
	return ai.ImageInput{Bytes: s.source}, nil
}

func assetImageFixture(t *testing.T, width, height int, encode func(*bytes.Buffer, image.Image) error) []byte {
	t.Helper()
	var output bytes.Buffer
	if err := encode(&output, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestCreatePhotoSessionRequiresPublishedVersion(t *testing.T) {
	db := newTestDB(t)
	version := createCaptureVersionFixture(t, db, models.SOPVersionDraft, "44444444-4444-4444-8444-444444444444")
	sku := createSKUForSOPVersion(t, db, version, "PUBLISH-SKU")

	response := performCreatePhotoSession(t, db, sku.PublicID, version.PublicID)
	assertErrorResponse(t, response, http.StatusConflict, "version_not_published")

	if err := db.Model(&version).Update("status", models.SOPVersionPublished).Error; err != nil {
		t.Fatal(err)
	}
	response = performCreatePhotoSession(t, db, sku.PublicID, version.PublicID)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, response.Code, response.Body.String())
	}
	var body struct {
		PublicID     string `json:"public_id"`
		SOPVersionID string `json:"sop_version_id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(body.PublicID); err != nil {
		t.Fatalf("expected session UUID, got %q: %v", body.PublicID, err)
	}
	if body.SOPVersionID != version.PublicID {
		t.Fatalf("expected version UUID %q, got %q", version.PublicID, body.SOPVersionID)
	}
}

func TestCreatePhotoSessionRejectsNumericInternalSKUIdentifier(t *testing.T) {
	db := newTestDB(t)
	version := createCaptureVersionFixture(t, db, models.SOPVersionPublished, "41111111-1111-4111-8111-111111111111")
	sku := createSKUForSOPVersion(t, db, version, "PUBLIC-SKU-ONLY")

	response := performCreatePhotoSession(t, db, fmt.Sprint(sku.ID), version.PublicID)
	assertErrorResponse(t, response, http.StatusBadRequest, "invalid_request")
	assertPhotoSessionCount(t, db, 0)
}

func TestCreateAssetUploadURLRejectsHEICBeforeSigningAndNeverReturnsObjectLocator(t *testing.T) {
	db := newTestDB(t)
	version := createCaptureVersionFixture(t, db, models.SOPVersionPublished, "42111111-1111-4111-8111-111111111111")
	view := createCaptureViewFixture(t, db, version.ID, "43111111-1111-4111-8111-111111111111", "正面", "Front")
	session := createPhotoSessionFixture(t, db, version.ID, "44111111-1111-4111-8111-111111111111")
	storage := &fakeAssetStorage{exists: true}
	server := &Server{db: db, cfg: testAssetConfig(), storage: storage}

	rejected := performCreateAssetUploadURLWithContentType(t, server, session.PublicID, view.PublicID, session.PhotographerID, "capture.heic", "image/heic")
	assertErrorResponse(t, rejected, http.StatusBadRequest, "invalid_request")
	if storage.lastObjectKey != "" {
		t.Fatalf("unsupported format reached object storage signing: %q", storage.lastObjectKey)
	}

	accepted := performCreateAssetUploadURLWithContentType(t, server, session.PublicID, view.PublicID, session.PhotographerID, "capture.jpg", "image/jpeg")
	if accepted.Code != http.StatusOK {
		t.Fatalf("JPEG upload ticket = %d %s", accepted.Code, accepted.Body.String())
	}
	for _, forbidden := range []string{`"object_key"`, `"asset_url"`} {
		if strings.Contains(accepted.Body.String(), forbidden) {
			t.Fatalf("upload response leaked %q: %s", forbidden, accepted.Body.String())
		}
	}
}

func TestCreatePhotoSessionRejectsArchivedVersion(t *testing.T) {
	db := newTestDB(t)
	version := createCaptureVersionFixture(t, db, models.SOPVersionArchived, "55555555-5555-4555-8555-555555555555")
	sku := createSKUForSOPVersion(t, db, version, "ARCHIVED-SKU")
	response := performCreatePhotoSession(t, db, sku.PublicID, version.PublicID)
	assertErrorResponse(t, response, http.StatusConflict, "version_not_published")
}

func TestCreatePhotoSessionRejectsMissingSKUWithoutWriting(t *testing.T) {
	db := newTestDB(t)
	version := createCaptureVersionFixture(t, db, models.SOPVersionPublished, "13131313-1313-4313-8313-131313131313")

	response := performCreatePhotoSession(t, db, "99999999-9999-4999-8999-999999999999", version.PublicID)
	assertErrorResponse(t, response, http.StatusNotFound, "sku_not_found")
	assertPhotoSessionCount(t, db, 0)
}

func TestCreatePhotoSessionRejectsSKUCaptureSOPCategoryMismatchWithoutWriting(t *testing.T) {
	db := newTestDB(t)
	version := createCaptureVersionFixture(t, db, models.SOPVersionPublished, "14141414-1414-4414-8414-141414141414")
	otherCategory := models.Category{Name: "Other Category", NameEN: "Other Category"}
	if err := db.Create(&otherCategory).Error; err != nil {
		t.Fatal(err)
	}
	sku := createSKUFixture(t, db, otherCategory.ID, "OTHER-SKU")

	response := performCreatePhotoSession(t, db, sku.PublicID, version.PublicID)
	assertErrorResponse(t, response, http.StatusConflict, "sku_sop_category_mismatch")
	assertPhotoSessionCount(t, db, 0)
}

func TestCompleteAssetRejectsViewFromAnotherVersion(t *testing.T) {
	db := newTestDB(t)
	v1 := createCaptureVersionFixture(t, db, models.SOPVersionPublished, "66666666-6666-4666-8666-666666666666")
	v2 := createCaptureVersionFixture(t, db, models.SOPVersionPublished, "77777777-7777-4777-8777-777777777777")
	view := createCaptureViewFixture(t, db, v2.ID, "88888888-8888-4888-8888-888888888888", "背面", "Back")
	session := createPhotoSessionFixture(t, db, v1.ID, "99999999-9999-4999-8999-999999999999")

	response := performCompleteAsset(t, db, session.PublicID, view.PublicID)
	assertErrorResponse(t, response, http.StatusConflict, "view_version_mismatch")

	var count int64
	if err := db.Model(&models.Asset{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected no asset on mismatch, got %d", count)
	}
}

func TestCompleteAssetRejectsUserWhoDoesNotOwnPhotoSession(t *testing.T) {
	db := newTestDB(t)
	version := createCaptureVersionFixture(t, db, models.SOPVersionPublished, "21212121-2121-4212-8212-212121212121")
	view := createCaptureViewFixture(t, db, version.ID, "22222222-2222-4222-8222-222222222222", "正面", "Front")
	session := createPhotoSessionFixture(t, db, version.ID, "23232323-2323-4232-8232-232323232323")

	response := performCompleteAssetAsUser(t, db, session.PublicID, view.PublicID, session.PhotographerID+1)
	assertErrorResponse(t, response, http.StatusForbidden, "photo_session_forbidden")

	var count int64
	if err := db.Model(&models.Asset{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected no asset for a different user, got %d", count)
	}
}

func TestCompleteAssetRejectsMalformedCapturedAtWithoutWriting(t *testing.T) {
	db := newTestDB(t)
	version := createCaptureVersionFixture(t, db, models.SOPVersionPublished, "15151515-1515-4515-8515-151515151515")
	view := createCaptureViewFixture(t, db, version.ID, "16161616-1616-4616-8616-161616161616", "正面", "Front")
	session := createPhotoSessionFixture(t, db, version.ID, "17171717-1717-4717-8717-171717171717")

	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodPost, "/assets/complete", strings.NewReader(fmt.Sprintf(
		`{"photo_session_id":%q,"sop_view_id":%q,"completion_token":"invalid","captured_at":"not-a-date"}`,
		session.PublicID, view.PublicID,
	)))
	context.Request.Header.Set("Content-Type", "application/json")
	(&Server{db: db}).completeAssetUpload(context)

	assertErrorResponse(t, response, http.StatusBadRequest, "invalid_request")
	var count int64
	if err := db.Model(&models.Asset{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected no assets, got %d", count)
	}
}

func TestCreateUploadURLRejectsViewFromAnotherVersionBeforeStorage(t *testing.T) {
	db := newTestDB(t)
	v1 := createCaptureVersionFixture(t, db, models.SOPVersionPublished, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	v2 := createCaptureVersionFixture(t, db, models.SOPVersionPublished, "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	view := createCaptureViewFixture(t, db, v2.ID, "cccccccc-cccc-4ccc-8ccc-cccccccccccc", "背面", "Back")
	session := createPhotoSessionFixture(t, db, v1.ID, "dddddddd-dddd-4ddd-8ddd-dddddddddddd")

	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodPost, "/assets/upload-url", strings.NewReader(fmt.Sprintf(
		`{"file_name":"capture.jpg","content_type":"image/jpeg","photo_session_id":%q,"sop_view_id":%q}`,
		session.PublicID, view.PublicID,
	)))
	context.Request.Header.Set("Content-Type", "application/json")
	context.Set("user", models.User{ID: session.PhotographerID, Role: models.RolePhotographer})
	server := &Server{db: db}
	server.createUploadURL(context)

	assertErrorResponse(t, response, http.StatusConflict, "view_version_mismatch")
}

func TestCreateUploadURLRejectsUserWhoDoesNotOwnPhotoSession(t *testing.T) {
	db := newTestDB(t)
	version := createCaptureVersionFixture(t, db, models.SOPVersionPublished, "24242424-2424-4242-8242-242424242424")
	view := createCaptureViewFixture(t, db, version.ID, "25252525-2525-4252-8252-252525252525", "正面", "Front")
	session := createPhotoSessionFixture(t, db, version.ID, "26262626-2626-4262-8262-262626262626")

	response := performCreateAssetUploadURL(t, &Server{db: db, cfg: testAssetConfig(), storage: &fakeAssetStorage{exists: true}}, session.PublicID, view.PublicID, session.PhotographerID+1, "capture.jpg")
	assertErrorResponse(t, response, http.StatusForbidden, "photo_session_forbidden")
}

func TestCompleteAssetUsesSignedTicketAndServerDerivedURL(t *testing.T) {
	db := newTestDB(t)
	version := createCaptureVersionFixture(t, db, models.SOPVersionPublished, "27272727-2727-4272-8272-272727272727")
	view := createCaptureViewFixture(t, db, version.ID, "28282828-2828-4282-8282-282828282828", "正面", "Front")
	session := createPhotoSessionFixture(t, db, version.ID, "29292929-2929-4292-8292-292929292929")
	storage := &fakeAssetStorage{exists: true}
	server := &Server{db: db, cfg: testAssetConfig(), storage: storage}
	ticket := createAssetUploadTicket(t, server, session, view, "../../fake/name.jpg")

	if !strings.HasPrefix(ticket.ObjectKey, "photo-sessions/"+session.PublicID+"/views/"+view.PublicID+"/") {
		t.Fatalf("object key escaped its session/view scope: %q", ticket.ObjectKey)
	}
	if strings.Contains(ticket.ObjectKey, "..") || strings.Contains(ticket.ObjectKey, "fake") || !strings.HasSuffix(ticket.ObjectKey, ".jpg") {
		t.Fatalf("object key must use a server-generated basename: %q", ticket.ObjectKey)
	}

	response := performCompleteAssetWithTicket(t, server, session, view, ticket.CompletionToken, "")
	if response.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", response.Code, response.Body.String())
	}
	var receipt completedAssetResponse
	if err := json.Unmarshal(response.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.PublicID == "" || receipt.MediaURL != "/api/v1/assets/"+receipt.PublicID+"/media" {
		t.Fatalf("asset response exposed an unsafe media locator: %#v", receipt)
	}
}

func TestCompleteAssetPersistsValidatedAssetMetadata(t *testing.T) {
	db := newTestDB(t)
	version := createCaptureVersionFixture(t, db, models.SOPVersionPublished, "26262626-2626-4262-8262-262626262627")
	view := createCaptureViewFixture(t, db, version.ID, "27272727-2727-4272-8272-272727272728", "正面", "Front")
	session := createPhotoSessionFixture(t, db, version.ID, "28282828-2828-4282-8282-282828282829")
	source := assetImageFixture(t, 2, 3, func(output *bytes.Buffer, value image.Image) error { return jpeg.Encode(output, value, nil) })
	server := &Server{db: db, cfg: testAssetConfig(), storage: &fakeAssetStorage{exists: true, source: source}}
	ticket := createAssetUploadTicket(t, server, session, view, "capture.jpg")

	response := performCompleteAssetWithTicket(t, server, session, view, ticket.CompletionToken, "")
	if response.Code != http.StatusCreated {
		t.Fatalf("expected validated asset completion, got %d: %s", response.Code, response.Body.String())
	}
	var asset models.Asset
	if err := db.Where("object_key = ?", ticket.ObjectKey).First(&asset).Error; err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(source)
	if asset.MIMEType != "image/jpeg" || asset.Width != 2 || asset.Height != 3 || asset.ByteCount != int64(len(source)) || asset.SHA256 != fmt.Sprintf("%x", hash) {
		t.Fatalf("persisted asset metadata = %#v", asset)
	}
}

func TestCompleteAssetRejectsMismatchedDeclaredContentType(t *testing.T) {
	db := newTestDB(t)
	version := createCaptureVersionFixture(t, db, models.SOPVersionPublished, "29292929-2929-4292-8292-292929292930")
	view := createCaptureViewFixture(t, db, version.ID, "30303030-3030-4303-8303-303030303031", "正面", "Front")
	session := createPhotoSessionFixture(t, db, version.ID, "31313131-3131-4313-8313-313131313132")
	source := assetImageFixture(t, 1, 1, func(output *bytes.Buffer, value image.Image) error { return png.Encode(output, value) })
	server := &Server{db: db, cfg: testAssetConfig(), storage: &fakeAssetStorage{exists: true, source: source}}
	ticket := createAssetUploadTicket(t, server, session, view, "capture.jpg")

	response := performCompleteAssetWithTicket(t, server, session, view, ticket.CompletionToken, "")
	assertErrorResponse(t, response, http.StatusBadRequest, "invalid_uploaded_image")
	var count int64
	if err := db.Model(&models.Asset{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("invalid image must not create an asset: count=%d err=%v", count, err)
	}
}

func TestCompleteAssetIsIdempotentForRepeatedTicket(t *testing.T) {
	db := newTestDB(t)
	version := createCaptureVersionFixture(t, db, models.SOPVersionPublished, "41414141-4141-4414-8414-414141414141")
	view := createCaptureViewFixture(t, db, version.ID, "42424242-4242-4424-8424-424242424242", "正面", "Front")
	session := createPhotoSessionFixture(t, db, version.ID, "43434343-4343-4434-8434-434343434343")
	server := &Server{db: db, cfg: testAssetConfig(), storage: &fakeAssetStorage{exists: true}}
	ticket := createAssetUploadTicket(t, server, session, view, "capture.jpg")

	first := performCompleteAssetWithTicket(t, server, session, view, ticket.CompletionToken, "2026-07-16T12:00:00Z")
	second := performCompleteAssetWithTicket(t, server, session, view, ticket.CompletionToken, "2026-07-16T13:00:00Z")
	if first.Code != http.StatusCreated || second.Code != http.StatusOK {
		t.Fatalf("expected first completion 201 and replay 200, got %d and %d: first=%s second=%s", first.Code, second.Code, first.Body.String(), second.Body.String())
	}
	var firstAsset, secondAsset completedAssetResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstAsset); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondAsset); err != nil {
		t.Fatal(err)
	}
	if firstAsset.PublicID != secondAsset.PublicID || !firstAsset.CapturedAt.Equal(secondAsset.CapturedAt) {
		t.Fatalf("replay must return the originally created asset: first=%#v second=%#v", firstAsset, secondAsset)
	}
	var count int64
	if err := db.Model(&models.Asset{}).Where("object_key = ?", ticket.ObjectKey).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("expected exactly one asset for ticket, count=%d err=%v", count, err)
	}
}

func TestCompleteAssetIsIdempotentForConcurrentTicketConsumption(t *testing.T) {
	db := newTestDB(t)
	version := createCaptureVersionFixture(t, db, models.SOPVersionPublished, "44444444-4141-4414-8414-414141414141")
	view := createCaptureViewFixture(t, db, version.ID, "45454545-4242-4424-8424-424242424242", "正面", "Front")
	session := createPhotoSessionFixture(t, db, version.ID, "46464646-4343-4434-8434-434343434343")
	server := &Server{db: db, cfg: testAssetConfig(), storage: &fakeAssetStorage{exists: true}}
	ticket := createAssetUploadTicket(t, server, session, view, "capture.jpg")

	start := make(chan struct{})
	responses := make(chan *httptest.ResponseRecorder, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			responses <- performCompleteAssetWithTicket(t, server, session, view, ticket.CompletionToken, "")
		}()
	}
	close(start)
	workers.Wait()
	close(responses)

	ids := make([]string, 0, 2)
	statuses := map[int]int{}
	for response := range responses {
		statuses[response.Code]++
		if response.Code != http.StatusCreated && response.Code != http.StatusOK {
			t.Fatalf("expected concurrent completion to succeed idempotently, got %d: %s", response.Code, response.Body.String())
		}
		var asset completedAssetResponse
		if err := json.Unmarshal(response.Body.Bytes(), &asset); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, asset.PublicID)
	}
	if statuses[http.StatusCreated] != 1 || statuses[http.StatusOK] != 1 || len(ids) != 2 || ids[0] != ids[1] {
		t.Fatalf("expected one create and one replay for the same asset, statuses=%v ids=%v", statuses, ids)
	}
	var count int64
	if err := db.Model(&models.Asset{}).Where("object_key = ?", ticket.ObjectKey).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("expected exactly one concurrent asset, count=%d err=%v", count, err)
	}
}

func TestAssetObjectKeyCannotBeRebound(t *testing.T) {
	db := newTestDB(t)
	first := models.Asset{SKUID: 1, PhotoSessionID: 1, SOPViewID: 1, ObjectKey: "photo-sessions/one/views/front/capture.jpg", OriginalURL: "https://assets.example.test/one.jpg", ReviewStatus: "pending", CapturedAt: time.Now()}
	second := models.Asset{SKUID: 2, PhotoSessionID: 2, SOPViewID: 2, ObjectKey: first.ObjectKey, OriginalURL: "https://assets.example.test/two.jpg", ReviewStatus: "pending", CapturedAt: time.Now()}
	if err := db.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&second).Error; err == nil {
		t.Fatal("expected an object key to be permanently bound to one asset")
	}
	var count int64
	if err := db.Model(&models.Asset{}).Where("object_key = ?", first.ObjectKey).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("expected exactly one binding for object key, count=%d err=%v", count, err)
	}
}

func TestCompleteAssetRejectsForgedTicketAndClientSuppliedURL(t *testing.T) {
	db := newTestDB(t)
	version := createCaptureVersionFixture(t, db, models.SOPVersionPublished, "30303030-3030-4303-8303-303030303030")
	view := createCaptureViewFixture(t, db, version.ID, "31313131-3131-4313-8313-313131313131", "正面", "Front")
	session := createPhotoSessionFixture(t, db, version.ID, "32323232-3232-4323-8323-323232323232")
	server := &Server{db: db, cfg: testAssetConfig(), storage: &fakeAssetStorage{exists: true}}
	ticket := createAssetUploadTicket(t, server, session, view, "capture.jpg")

	for name, test := range map[string]struct {
		body string
		code string
	}{
		"tampered ticket": {fmt.Sprintf(`{"photo_session_id":%q,"sop_view_id":%q,"completion_token":%q}`, session.PublicID, view.PublicID, ticket.CompletionToken+"x"), "invalid_upload_ticket"},
		"client URL":      {fmt.Sprintf(`{"photo_session_id":%q,"sop_view_id":%q,"completion_token":%q,"original_url":"https://evil.test/fake.jpg"}`, session.PublicID, view.PublicID, ticket.CompletionToken), "invalid_request"},
		"client key":      {fmt.Sprintf(`{"photo_session_id":%q,"sop_view_id":%q,"completion_token":%q,"object_key":"photo-sessions/%s/views/%s/fake.jpg"}`, session.PublicID, view.PublicID, ticket.CompletionToken, session.PublicID, view.PublicID), "invalid_request"},
	} {
		t.Run(name, func(t *testing.T) {
			response := performAssetCompleteBody(t, server, session.PhotographerID, test.body)
			assertErrorResponse(t, response, http.StatusBadRequest, test.code)
		})
	}
	var count int64
	if err := db.Model(&models.Asset{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("expected no forged assets, count=%d err=%v", count, err)
	}
}

func TestCompleteAssetRejectsTicketForAnotherBinding(t *testing.T) {
	db := newTestDB(t)
	version := createCaptureVersionFixture(t, db, models.SOPVersionPublished, "33333333-3333-4333-8333-333333333333")
	view1 := createCaptureViewFixture(t, db, version.ID, "34343434-3434-4343-8343-343434343434", "正面", "Front")
	view2 := models.SOPView{PublicID: "35353535-3535-4353-8353-353535353535", SOPVersionID: version.ID, Sequence: 2, Role: models.SOPViewCapture, ViewKind: models.SOPViewDetail, NameZH: "背面", NameEN: "Back", CameraPositionZ: -1, ImageUpX: 1, Composition: models.Composition{FrameOccupancy: .85, AspectRatio: "1:1", AllowRotationCorrection: true}}
	if err := db.Create(&view2).Error; err != nil {
		t.Fatal(err)
	}
	session := createPhotoSessionFixture(t, db, version.ID, "36363636-3636-4363-8363-363636363636")
	server := &Server{db: db, cfg: testAssetConfig(), storage: &fakeAssetStorage{exists: true}}
	ticket := createAssetUploadTicket(t, server, session, view1, "capture.jpg")

	response := performCompleteAssetWithTicket(t, server, session, view2, ticket.CompletionToken, "")
	assertErrorResponse(t, response, http.StatusBadRequest, "invalid_upload_ticket")
}

func TestCompleteAssetRejectsMissingUploadedObject(t *testing.T) {
	db := newTestDB(t)
	version := createCaptureVersionFixture(t, db, models.SOPVersionPublished, "37373737-3737-4373-8373-373737373737")
	view := createCaptureViewFixture(t, db, version.ID, "38383838-3838-4383-8383-383838383838", "正面", "Front")
	session := createPhotoSessionFixture(t, db, version.ID, "39393939-3939-4393-8393-393939393939")
	storage := &fakeAssetStorage{exists: false}
	server := &Server{db: db, cfg: testAssetConfig(), storage: storage}
	ticket := createAssetUploadTicket(t, server, session, view, "capture.jpg")

	response := performCompleteAssetWithTicket(t, server, session, view, ticket.CompletionToken, "")
	assertErrorResponse(t, response, http.StatusConflict, "upload_not_found")
}

func TestAssetReviewHierarchyReturnsLocalizedViewName(t *testing.T) {
	db := newTestDB(t)
	category := models.Category{Name: "手机壳", NameEN: "Phone Case"}
	if err := db.Create(&category).Error; err != nil {
		t.Fatal(err)
	}
	product := models.Product{CategoryID: category.ID, Name: "透明手机壳", Category: category.Name}
	if err := db.Create(&product).Error; err != nil {
		t.Fatal(err)
	}
	sku := models.SKU{ProductID: product.ID, Code: "TEST-SKU"}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatal(err)
	}
	version := createCaptureVersionFixture(t, db, models.SOPVersionPublished, "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee")
	view := createCaptureViewFixture(t, db, version.ID, "ffffffff-ffff-4fff-8fff-ffffffffffff", "背面", "Back")
	session := createPhotoSessionFixture(t, db, version.ID, "12121212-1212-4212-8212-121212121212")
	asset := models.Asset{SKUID: sku.ID, PhotoSessionID: session.ID, SOPViewID: view.ID, ObjectKey: "capture.jpg", OriginalURL: "http://example.test/capture.jpg", ReviewStatus: "pending"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodGet, "/assets/review/hierarchy", nil)
	(&Server{db: db}).listAssetReviewHierarchy(context)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
	var body struct {
		Data []struct {
			SKUs []struct {
				Assets []struct {
					SOPViewKey  string `json:"sop_view_key"`
					SOPViewName struct {
						ZHCN string `json:"zh-CN"`
						EN   string `json:"en"`
					} `json:"sop_view_name"`
				} `json:"assets"`
			} `json:"skus"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	name := body.Data[0].SKUs[0].Assets[0].SOPViewName
	if name.ZHCN != "背面" || name.EN != "Back" {
		t.Fatalf("unexpected localized name: %#v", name)
	}
	if body.Data[0].SKUs[0].Assets[0].SOPViewKey != "reference_front" {
		t.Fatalf("unexpected SOP view key: %s", response.Body.String())
	}

	response = httptest.NewRecorder()
	context, _ = gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodGet, "/assets/review", nil)
	(&Server{db: db}).listAssetsForReview(context)
	var listBody struct {
		Data []struct {
			SOPViewName struct {
				ZHCN string `json:"zh-CN"`
				EN   string `json:"en"`
			} `json:"sop_view_name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &listBody); err != nil {
		t.Fatal(err)
	}
	if len(listBody.Data) != 1 || listBody.Data[0].SOPViewName.ZHCN != "背面" || listBody.Data[0].SOPViewName.EN != "Back" {
		t.Fatalf("unexpected localized review-list response: %s", response.Body.String())
	}
}

func createCaptureVersionFixture(t *testing.T, db *gorm.DB, status models.SOPVersionStatus, publicID string) models.SOPVersion {
	t.Helper()
	category := models.Category{Name: "Category " + publicID, NameEN: "Category " + publicID}
	if err := db.Create(&category).Error; err != nil {
		t.Fatal(err)
	}
	parent := models.CaptureSOP{PublicID: uuid.NewString(), CategoryID: category.ID, CreatedByID: 1}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	version := models.SOPVersion{PublicID: publicID, CaptureSOPID: parent.ID, VersionNumber: 1, SchemaVersion: "1.0", NameZH: "拍摄规范", NameEN: "Capture SOP", Status: status, CoordinateSystem: "pcs_object_v1"}
	if err := db.Create(&version).Error; err != nil {
		t.Fatal(err)
	}
	return version
}

func createCaptureViewFixture(t *testing.T, db *gorm.DB, versionID uint, publicID, nameZH, nameEN string) models.SOPView {
	t.Helper()
	view := models.SOPView{PublicID: publicID, SOPVersionID: versionID, Sequence: 1, Role: models.SOPViewReferenceFront, ViewKind: models.SOPViewStandard, PresetKey: "reference_front", NameZH: nameZH, NameEN: nameEN, Required: true, CameraPositionZ: 1, ImageUpX: 1, Composition: models.Composition{FrameOccupancy: .85, AspectRatio: "1:1", AllowRotationCorrection: true}}
	if err := db.Create(&view).Error; err != nil {
		t.Fatal(err)
	}
	return view
}

func createPhotoSessionFixture(t *testing.T, db *gorm.DB, versionID uint, publicID string) models.PhotoSession {
	t.Helper()
	var sku models.SKU
	if err := db.First(&sku, 42).Error; err != nil {
		sku = models.SKU{ID: 42, ProductID: 1, Code: "PHOTO-SESSION-SKU"}
		if err := db.Create(&sku).Error; err != nil {
			t.Fatal(err)
		}
	}
	session := models.PhotoSession{PublicID: publicID, Code: "PS-" + publicID, SKUID: 42, SOPVersionID: versionID, PhotographerID: 7, Status: "in_progress"}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	return session
}

func createSKUFixture(t *testing.T, db *gorm.DB, categoryID uint, code string) models.SKU {
	t.Helper()
	product := models.Product{CategoryID: categoryID, Name: code + " Product"}
	if err := db.Create(&product).Error; err != nil {
		t.Fatal(err)
	}
	sku := models.SKU{ProductID: product.ID, Code: code}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatal(err)
	}
	return sku
}

func createSKUForSOPVersion(t *testing.T, db *gorm.DB, version models.SOPVersion, code string) models.SKU {
	t.Helper()
	var parent models.CaptureSOP
	if err := db.First(&parent, version.CaptureSOPID).Error; err != nil {
		t.Fatal(err)
	}
	return createSKUFixture(t, db, parent.CategoryID, code)
}

func assertPhotoSessionCount(t *testing.T, db *gorm.DB, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&models.PhotoSession{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("expected %d photo sessions, got %d", want, count)
	}
}

func performCompleteAsset(t *testing.T, db *gorm.DB, sessionID, viewID string) *httptest.ResponseRecorder {
	t.Helper()
	return performCompleteAssetAsUser(t, db, sessionID, viewID, 7)
}

func performCompleteAssetAsUser(t *testing.T, db *gorm.DB, sessionID, viewID string, userID uint) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodPost, "/assets/complete", strings.NewReader(fmt.Sprintf(
		`{"photo_session_id":%q,"sop_view_id":%q,"completion_token":"invalid"}`,
		sessionID, viewID,
	)))
	context.Request.Header.Set("Content-Type", "application/json")
	context.Set("user", models.User{ID: userID, Role: models.RolePhotographer})
	(&Server{db: db}).completeAssetUpload(context)
	return response
}

func testAssetConfig() config.Config {
	return config.Config{JWTSecret: "asset-upload-test-secret"}
}

func performCreateAssetUploadURL(t *testing.T, server *Server, sessionID, viewID string, userID uint, fileName string) *httptest.ResponseRecorder {
	return performCreateAssetUploadURLWithContentType(t, server, sessionID, viewID, userID, fileName, "image/jpeg")
}

func performCreateAssetUploadURLWithContentType(t *testing.T, server *Server, sessionID, viewID string, userID uint, fileName, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodPost, "/assets/upload-url", strings.NewReader(fmt.Sprintf(
		`{"file_name":%q,"content_type":%q,"photo_session_id":%q,"sop_view_id":%q}`,
		fileName, contentType, sessionID, viewID,
	)))
	context.Request.Header.Set("Content-Type", "application/json")
	context.Set("user", models.User{ID: userID, Role: models.RolePhotographer})
	server.createUploadURL(context)
	return response
}

func createAssetUploadTicket(t *testing.T, server *Server, session models.PhotoSession, view models.SOPView, fileName string) assetUploadTicketResponse {
	t.Helper()
	response := performCreateAssetUploadURL(t, server, session.PublicID, view.PublicID, session.PhotographerID, fileName)
	if response.Code != http.StatusOK {
		t.Fatalf("expected upload ticket, got %d: %s", response.Code, response.Body.String())
	}
	var ticket assetUploadTicketResponse
	if err := json.Unmarshal(response.Body.Bytes(), &ticket); err != nil {
		t.Fatal(err)
	}
	if ticket.CompletionToken == "" {
		t.Fatalf("incomplete upload ticket: %s", response.Body.String())
	}
	if storage, ok := server.storage.(*fakeAssetStorage); ok {
		ticket.ObjectKey = storage.lastObjectKey
	}
	if ticket.ObjectKey == "" {
		t.Fatal("test storage did not observe the signed object key")
	}
	return ticket
}

func performCompleteAssetWithTicket(t *testing.T, server *Server, session models.PhotoSession, view models.SOPView, completionToken, capturedAt string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"photo_session_id":%q,"sop_view_id":%q,"completion_token":%q`, session.PublicID, view.PublicID, completionToken)
	if capturedAt != "" {
		body += fmt.Sprintf(`,"captured_at":%q`, capturedAt)
	}
	body += "}"
	return performAssetCompleteBody(t, server, session.PhotographerID, body)
}

func performAssetCompleteBody(t *testing.T, server *Server, userID uint, body string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodPost, "/assets/complete", strings.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	context.Set("user", models.User{ID: userID, Role: models.RolePhotographer})
	server.completeAssetUpload(context)
	return response
}

func assertErrorResponse(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("expected status %d, got %d: %s", status, response.Code, response.Body.String())
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != code {
		t.Fatalf("expected code %q, got %q: %s", code, body.Code, response.Body.String())
	}
}
