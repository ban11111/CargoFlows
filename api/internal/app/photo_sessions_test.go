package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cargoflow/api/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestCreatePhotoSessionLocksVersionBeforeValidatingPublishedStatus(t *testing.T) {
	db := newTestDB(t)
	version := createTestSOPVersion(t, db, "12111111-1111-4111-8111-111111111111")
	sku := createSKUForSOPVersion(t, db, version, "SESSION-LOCK")

	var versionQueryObserved, updateLockObserved bool
	callbackName := "test:observe-version-lock"
	if err := db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "sop_versions" {
			return
		}
		versionQueryObserved = true
		lockingClause, ok := tx.Statement.Clauses["FOR"]
		if !ok {
			return
		}
		locking, ok := lockingClause.Expression.(clause.Locking)
		updateLockObserved = ok && locking.Strength == "UPDATE"
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	response := performCreatePhotoSession(t, db, sku.ID, version.PublicID)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, response.Code, response.Body.String())
	}
	if !versionQueryObserved {
		t.Fatal("expected SOP version query to be observed")
	}
	if !updateLockObserved {
		t.Fatal("expected SOP version to be selected with an UPDATE lock")
	}
}

func TestCreatePhotoSessionResolvesSOPVersionPublicID(t *testing.T) {
	db := newTestDB(t)
	version := createTestSOPVersion(t, db, "22222222-2222-4222-8222-222222222222")
	sku := createSKUForSOPVersion(t, db, version, "SESSION-RESOLVE")

	response := performCreatePhotoSession(t, db, sku.ID, version.PublicID)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, response.Code, response.Body.String())
	}

	var session models.PhotoSession
	if err := db.First(&session).Error; err != nil {
		t.Fatal(err)
	}
	if session.SOPVersionID != version.ID {
		t.Fatalf("expected SOP version ID %d, got %d", version.ID, session.SOPVersionID)
	}
}

func TestCreatePhotoSessionAssignsUniquePublicIDs(t *testing.T) {
	db := newTestDB(t)
	version := createTestSOPVersion(t, db, "33333333-3333-4333-8333-333333333333")

	for _, sku := range []models.SKU{createSKUForSOPVersion(t, db, version, "SESSION-ONE"), createSKUForSOPVersion(t, db, version, "SESSION-TWO")} {
		response := performCreatePhotoSession(t, db, sku.ID, version.PublicID)
		if response.Code != http.StatusCreated {
			t.Fatalf("expected status %d for SKU %d, got %d: %s", http.StatusCreated, sku.ID, response.Code, response.Body.String())
		}
	}

	var sessions []models.PhotoSession
	if err := db.Order("id ASC").Find(&sessions).Error; err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	if sessions[0].PublicID == "" || sessions[1].PublicID == "" {
		t.Fatal("expected non-empty public IDs")
	}
	for _, session := range sessions {
		if _, err := uuid.Parse(session.PublicID); err != nil {
			t.Fatalf("expected valid UUID public ID, got %q: %v", session.PublicID, err)
		}
	}
	if sessions[0].PublicID == sessions[1].PublicID {
		t.Fatalf("expected unique public IDs, both were %q", sessions[0].PublicID)
	}
}

func createTestSOPVersion(t *testing.T, db *gorm.DB, publicID string) models.SOPVersion {
	t.Helper()
	category := models.Category{Name: "Test Category", NameEN: "Test Category"}
	if err := db.Create(&category).Error; err != nil {
		t.Fatal(err)
	}
	captureSOP := models.CaptureSOP{PublicID: "11111111-1111-4111-8111-111111111111", CategoryID: category.ID, CreatedByID: 1}
	if err := db.Create(&captureSOP).Error; err != nil {
		t.Fatal(err)
	}
	version := models.SOPVersion{
		PublicID:         publicID,
		CaptureSOPID:     captureSOP.ID,
		VersionNumber:    1,
		SchemaVersion:    "1.0",
		NameZH:           "测试版本",
		NameEN:           "Test Version",
		DescriptionZH:    "测试描述",
		DescriptionEN:    "Test description",
		Status:           models.SOPVersionPublished,
		CoordinateSystem: "pcs_object_v1",
	}
	if err := db.Create(&version).Error; err != nil {
		t.Fatal(err)
	}
	return version
}

func performCreatePhotoSession(t *testing.T, db *gorm.DB, skuID uint, sopVersionPublicID string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodPost, "/photo-sessions", strings.NewReader(fmt.Sprintf(
		`{"sku_id":%d,"sop_version_id":%q}`,
		skuID,
		sopVersionPublicID,
	)))
	context.Request.Header.Set("Content-Type", "application/json")
	context.Set("user", models.User{ID: 7})

	server := &Server{db: db}
	server.createPhotoSession(context)
	return response
}
