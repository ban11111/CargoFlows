package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cargoflow/api/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestCreatePhotoSessionRequiresPublishedVersion(t *testing.T) {
	db := newTestDB(t)
	version := createCaptureVersionFixture(t, db, models.SOPVersionDraft, "44444444-4444-4444-8444-444444444444")

	response := performCreatePhotoSession(t, db, 42, version.PublicID)
	assertErrorResponse(t, response, http.StatusConflict, "version_not_published")

	if err := db.Model(&version).Update("status", models.SOPVersionPublished).Error; err != nil {
		t.Fatal(err)
	}
	response = performCreatePhotoSession(t, db, 42, version.PublicID)
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

func TestCreatePhotoSessionRejectsArchivedVersion(t *testing.T) {
	db := newTestDB(t)
	version := createCaptureVersionFixture(t, db, models.SOPVersionArchived, "55555555-5555-4555-8555-555555555555")
	response := performCreatePhotoSession(t, db, 42, version.PublicID)
	assertErrorResponse(t, response, http.StatusConflict, "version_not_published")
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
	server := &Server{db: db}
	server.createUploadURL(context)

	assertErrorResponse(t, response, http.StatusConflict, "view_version_mismatch")
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
	session := models.PhotoSession{PublicID: publicID, Code: "PS-" + publicID, SKUID: 42, SOPVersionID: versionID, PhotographerID: 7, Status: "in_progress"}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	return session
}

func performCompleteAsset(t *testing.T, db *gorm.DB, sessionID, viewID string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodPost, "/assets/complete", strings.NewReader(fmt.Sprintf(
		`{"photo_session_id":%q,"sop_view_id":%q,"object_key":"capture.jpg","original_url":"http://example.test/capture.jpg"}`,
		sessionID, viewID,
	)))
	context.Request.Header.Set("Content-Type", "application/json")
	(&Server{db: db}).completeAssetUpload(context)
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
