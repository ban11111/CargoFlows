package app

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cargoflow/api/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type assetAccessFixture struct {
	router                       *gin.Engine
	db                           *gorm.DB
	photographerA, photographerB models.User
	operator, viewer             models.User
	assetA, assetB               models.Asset
}

func TestAssetReviewRoutesEnforceRoleAndOwnership(t *testing.T) {
	fixture := newAssetAccessFixture(t)

	for _, path := range []string{"/api/v1/assets/review", "/api/v1/assets/review/hierarchy"} {
		response := fixture.request(t, fixture.photographerA, http.MethodGet, path, "")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), fixture.assetA.PublicID) || strings.Contains(response.Body.String(), fixture.assetB.PublicID) {
			t.Fatalf("photographer-scoped %s = %d %s", path, response.Code, response.Body.String())
		}
		viewer := fixture.request(t, fixture.viewer, http.MethodGet, path, "")
		if viewer.Code != http.StatusForbidden {
			t.Fatalf("viewer %s = %d %s, want 403", path, viewer.Code, viewer.Body.String())
		}
	}

	owned := fixture.request(t, fixture.photographerA, http.MethodGet, "/api/v1/assets/"+fixture.assetA.PublicID+"/media", "")
	if owned.Code != http.StatusOK {
		t.Fatalf("photographer own media = %d %s", owned.Code, owned.Body.String())
	}
	other := fixture.request(t, fixture.photographerA, http.MethodGet, "/api/v1/assets/"+fixture.assetB.PublicID+"/media", "")
	if other.Code != http.StatusNotFound {
		t.Fatalf("photographer other media = %d %s, want 404", other.Code, other.Body.String())
	}
	reviewer := fixture.request(t, fixture.operator, http.MethodGet, "/api/v1/assets/"+fixture.assetB.PublicID+"/media", "")
	if reviewer.Code != http.StatusOK {
		t.Fatalf("operator media = %d %s", reviewer.Code, reviewer.Body.String())
	}
	viewer := fixture.request(t, fixture.viewer, http.MethodGet, "/api/v1/assets/"+fixture.assetA.PublicID+"/media", "")
	if viewer.Code != http.StatusForbidden {
		t.Fatalf("viewer media = %d %s, want 403", viewer.Code, viewer.Body.String())
	}

	photographerReview := fixture.request(t, fixture.photographerA, http.MethodPatch, "/api/v1/assets/"+fixture.assetA.PublicID+"/review", `{"status":"approved"}`)
	if photographerReview.Code != http.StatusForbidden {
		t.Fatalf("photographer review = %d %s, want 403", photographerReview.Code, photographerReview.Body.String())
	}
	approved := fixture.request(t, fixture.operator, http.MethodPatch, "/api/v1/assets/"+fixture.assetB.PublicID+"/review", `{"status":"approved"}`)
	if approved.Code != http.StatusOK {
		t.Fatalf("operator review = %d %s", approved.Code, approved.Body.String())
	}
	var audit models.AssetReview
	if err := fixture.db.Where("asset_id = ?", fixture.assetB.ID).First(&audit).Error; err != nil || audit.ReviewerID != fixture.operator.ID || audit.Status != "approved" {
		t.Fatalf("review audit = %#v, err = %v", audit, err)
	}
}

func TestAssetReviewListFiltersByPublicSKUID(t *testing.T) {
	fixture := newAssetAccessFixture(t)
	var sku models.SKU
	if err := fixture.db.First(&sku, fixture.assetA.SKUID).Error; err != nil {
		t.Fatal(err)
	}

	filtered := fixture.request(t, fixture.operator, http.MethodGet, "/api/v1/assets/review?sku_id="+sku.PublicID, "")
	if filtered.Code != http.StatusOK || !strings.Contains(filtered.Body.String(), fixture.assetA.PublicID) || !strings.Contains(filtered.Body.String(), fixture.assetB.PublicID) {
		t.Fatalf("filtered assets = %d %s", filtered.Code, filtered.Body.String())
	}
	if !strings.Contains(filtered.Body.String(), `"sop_view_id"`) || !strings.Contains(filtered.Body.String(), `"sop_view_key":"reference_front"`) {
		t.Fatalf("filtered assets omit stable SOP view identity: %s", filtered.Body.String())
	}
	missing := fixture.request(t, fixture.operator, http.MethodGet, "/api/v1/assets/review?sku_id="+uuid.NewString(), "")
	if missing.Code != http.StatusOK || strings.Contains(missing.Body.String(), fixture.assetA.PublicID) || strings.Contains(missing.Body.String(), fixture.assetB.PublicID) {
		t.Fatalf("missing SKU filter = %d %s", missing.Code, missing.Body.String())
	}

	invalid := fixture.request(t, fixture.operator, http.MethodGet, "/api/v1/assets/review?sku_id=not-a-uuid", "")
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid sku filter = %d %s, want 400", invalid.Code, invalid.Body.String())
	}
}

func TestReviewAssetUsesStrictValidatedTransactionalInput(t *testing.T) {
	fixture := newAssetAccessFixture(t)
	path := "/api/v1/assets/" + fixture.assetA.PublicID + "/review"
	for name, body := range map[string]string{
		"unknown field":  `{"status":"approved","unexpected":true}`,
		"invalid status": `{"status":"pending"}`,
	} {
		t.Run(name, func(t *testing.T) {
			response := fixture.request(t, fixture.operator, http.MethodPatch, path, body)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", response.Code, response.Body.String())
			}
		})
	}

	callbackName := "test:fail_asset_review_audit"
	if err := fixture.db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "AssetReview" {
			tx.AddError(errors.New("audit unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer fixture.db.Callback().Create().Remove(callbackName)
	response := fixture.request(t, fixture.operator, http.MethodPatch, path, `{"status":"rejected","reason":"blurred"}`)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("failed audit review = %d %s, want 500", response.Code, response.Body.String())
	}
	var persisted models.Asset
	if err := fixture.db.First(&persisted, fixture.assetA.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.ReviewStatus != "pending" {
		t.Fatalf("asset status committed without audit = %q", persisted.ReviewStatus)
	}
	var count int64
	if err := fixture.db.Model(&models.AssetReview{}).Where("asset_id = ?", fixture.assetA.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed transaction audit count = %d, err = %v", count, err)
	}
}

func newAssetAccessFixture(t *testing.T) assetAccessFixture {
	t.Helper()
	db := newTestDB(t)
	users := []models.User{
		{Name: "Photographer A", Email: uuid.NewString() + "@example.test", PasswordHash: "unused", Role: models.RolePhotographer, Status: "active"},
		{Name: "Photographer B", Email: uuid.NewString() + "@example.test", PasswordHash: "unused", Role: models.RolePhotographer, Status: "active"},
		{Name: "Operator", Email: uuid.NewString() + "@example.test", PasswordHash: "unused", Role: models.RoleOperator, Status: "active"},
		{Name: "Viewer", Email: uuid.NewString() + "@example.test", PasswordHash: "unused", Role: models.RoleViewer, Status: "active"},
	}
	for index := range users {
		if err := db.Create(&users[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	category := models.Category{Name: "ACL " + uuid.NewString(), NameEN: "ACL"}
	if err := db.Create(&category).Error; err != nil {
		t.Fatal(err)
	}
	product := models.Product{CategoryID: category.ID, Name: "ACL product", Category: category.Name}
	if err := db.Create(&product).Error; err != nil {
		t.Fatal(err)
	}
	sku := models.SKU{ProductID: product.ID, Code: "ACL-" + uuid.NewString()}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatal(err)
	}
	sop := models.CaptureSOP{PublicID: uuid.NewString(), CategoryID: category.ID, CreatedByID: users[2].ID}
	if err := db.Create(&sop).Error; err != nil {
		t.Fatal(err)
	}
	version := models.SOPVersion{PublicID: uuid.NewString(), CaptureSOPID: sop.ID, VersionNumber: 1, SchemaVersion: "1.0", NameZH: "ACL SOP", NameEN: "ACL SOP", Status: models.SOPVersionPublished, CoordinateSystem: "pcs_object_v1"}
	if err := db.Create(&version).Error; err != nil {
		t.Fatal(err)
	}
	view := models.SOPView{PublicID: uuid.NewString(), SOPVersionID: version.ID, Sequence: 1, Role: models.SOPViewReferenceFront, ViewKind: models.SOPViewStandard, PresetKey: "reference_front", NameZH: "正面", NameEN: "Front", Required: true, CameraPositionZ: 1, ImageUpX: 1, Composition: models.Composition{FrameOccupancy: .85, AspectRatio: "1:1", AllowRotationCorrection: true}}
	if err := db.Create(&view).Error; err != nil {
		t.Fatal(err)
	}
	sessions := []models.PhotoSession{
		{PublicID: uuid.NewString(), Code: "ACL-A-" + uuid.NewString(), SKUID: sku.ID, SOPVersionID: version.ID, PhotographerID: users[0].ID, Status: "in_progress"},
		{PublicID: uuid.NewString(), Code: "ACL-B-" + uuid.NewString(), SKUID: sku.ID, SOPVersionID: version.ID, PhotographerID: users[1].ID, Status: "in_progress"},
	}
	for index := range sessions {
		if err := db.Create(&sessions[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	assets := []models.Asset{
		{SKUID: sku.ID, PhotoSessionID: sessions[0].ID, SOPViewID: view.ID, ObjectKey: "acl/a.jpg", OriginalURL: "private://acl/a.jpg", ReviewStatus: "pending", MIMEType: "image/jpeg"},
		{SKUID: sku.ID, PhotoSessionID: sessions[1].ID, SOPViewID: view.ID, ObjectKey: "acl/b.jpg", OriginalURL: "private://acl/b.jpg", ReviewStatus: "pending", MIMEType: "image/jpeg"},
	}
	for index := range assets {
		if err := db.Create(&assets[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	cfg := testAssetConfig()
	server := &Server{db: db, cfg: cfg, storage: &fakeAssetStorage{source: defaultFakeAssetImage}}
	router := gin.New()
	protected := router.Group("/api/v1")
	protected.Use(server.requireAuth())
	registerExistingRoutes(protected, server)
	return assetAccessFixture{router: router, db: db, photographerA: users[0], photographerB: users[1], operator: users[2], viewer: users[3], assetA: assets[0], assetB: assets[1]}
}

func (fixture assetAccessFixture) request(t *testing.T, user models.User, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": user.ID}).SignedString([]byte(testAssetConfig().JWTSecret))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	return response
}
