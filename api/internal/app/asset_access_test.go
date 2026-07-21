package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cargoflows/api/internal/models"
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
		operator := fixture.request(t, fixture.photographerA, http.MethodGet, path, "")
		if operator.Code != http.StatusForbidden {
			t.Fatalf("operator %s = %d %s, want 403", path, operator.Code, operator.Body.String())
		}
		manager := fixture.request(t, fixture.operator, http.MethodGet, path, "")
		if manager.Code != http.StatusOK || !strings.Contains(manager.Body.String(), fixture.assetA.PublicID) || !strings.Contains(manager.Body.String(), fixture.assetB.PublicID) {
			t.Fatalf("admin %s = %d %s", path, manager.Code, manager.Body.String())
		}
	}

	other := fixture.request(t, fixture.photographerA, http.MethodGet, "/api/v1/assets/"+fixture.assetB.PublicID+"/media", "")
	if other.Code != http.StatusOK {
		t.Fatalf("operator media = %d %s", other.Code, other.Body.String())
	}
	reviewer := fixture.request(t, fixture.operator, http.MethodGet, "/api/v1/assets/"+fixture.assetB.PublicID+"/media", "")
	if reviewer.Code != http.StatusOK {
		t.Fatalf("operator media = %d %s", reviewer.Code, reviewer.Body.String())
	}
	photographerReview := fixture.request(t, fixture.photographerA, http.MethodPatch, "/api/v1/assets/"+fixture.assetA.PublicID+"/review", `{"status":"approved"}`)
	if photographerReview.Code != http.StatusForbidden {
		t.Fatalf("photographer review = %d %s, want 403", photographerReview.Code, photographerReview.Body.String())
	}
	approved := fixture.request(t, fixture.operator, http.MethodPatch, "/api/v1/assets/"+fixture.assetB.PublicID+"/review", `{"status":"approved"}`)
	if approved.Code != http.StatusOK {
		t.Fatalf("admin review = %d %s", approved.Code, approved.Body.String())
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

func TestAssetReviewSKUQueuePaginatesFiltersAndPrioritizesPending(t *testing.T) {
	fixture := newAssetAccessFixture(t)
	var pendingSKU models.SKU
	if err := fixture.db.Preload("Product.CatalogCategory").First(&pendingSKU, fixture.assetA.SKUID).Error; err != nil {
		t.Fatal(err)
	}
	category := models.Category{Name: "Archived accessories", NameEN: "Archived accessories"}
	if err := fixture.db.Create(&category).Error; err != nil {
		t.Fatal(err)
	}
	product := models.Product{CategoryID: category.ID, Name: "Reviewed product", Category: category.Name}
	if err := fixture.db.Create(&product).Error; err != nil {
		t.Fatal(err)
	}
	reviewedSKU := models.SKU{ProductID: product.ID, Code: "ZZZ-REVIEWED"}
	if err := fixture.db.Create(&reviewedSKU).Error; err != nil {
		t.Fatal(err)
	}
	reviewedAsset := models.Asset{SKUID: reviewedSKU.ID, PhotoSessionID: fixture.assetA.PhotoSessionID, SOPViewID: fixture.assetA.SOPViewID, ObjectKey: "reviewed.jpg", OriginalURL: "private://reviewed.jpg", ReviewStatus: "approved", MIMEType: "image/jpeg", CapturedAt: time.Now().Add(time.Hour)}
	if err := fixture.db.Create(&reviewedAsset).Error; err != nil {
		t.Fatal(err)
	}

	response := fixture.request(t, fixture.operator, http.MethodGet, "/api/v1/assets/review/skus?page=1&page_size=1", "")
	if response.Code != http.StatusOK {
		t.Fatalf("queue = %d %s", response.Code, response.Body.String())
	}
	var queue struct {
		Data []struct {
			PublicID string            `json:"public_id"`
			Counts   assetReviewCounts `json:"counts"`
			Cover    *assetReviewCover `json:"cover_asset"`
		} `json:"data"`
		Pagination paginationDTO `json:"pagination"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &queue); err != nil {
		t.Fatal(err)
	}
	if len(queue.Data) != 1 || queue.Data[0].PublicID != pendingSKU.PublicID || queue.Data[0].Counts.Pending != 2 || queue.Data[0].Cover == nil || queue.Pagination.Total != 2 || queue.Pagination.TotalPages != 2 {
		t.Fatalf("unexpected queue: %#v body=%s", queue, response.Body.String())
	}

	for name, expected := range map[string]struct {
		path string
		id   string
	}{
		"status":   {path: "/api/v1/assets/review/skus?status=approved", id: reviewedSKU.PublicID},
		"query":    {path: "/api/v1/assets/review/skus?q=ZZZ", id: reviewedSKU.PublicID},
		"category": {path: fmt.Sprintf("/api/v1/assets/review/skus?category_id=%d", category.ID), id: reviewedSKU.PublicID},
	} {
		t.Run(name, func(t *testing.T) {
			filtered := fixture.request(t, fixture.operator, http.MethodGet, expected.path, "")
			if filtered.Code != http.StatusOK || !strings.Contains(filtered.Body.String(), expected.id) || strings.Contains(filtered.Body.String(), pendingSKU.PublicID) {
				t.Fatalf("filtered queue = %d %s", filtered.Code, filtered.Body.String())
			}
		})
	}

	detail := fixture.request(t, fixture.operator, http.MethodGet, "/api/v1/assets/review/skus/"+pendingSKU.PublicID, "")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"pending":2`) || !strings.Contains(detail.Body.String(), pendingSKU.Code) {
		t.Fatalf("detail = %d %s", detail.Code, detail.Body.String())
	}
	pagedAssets := fixture.request(t, fixture.operator, http.MethodGet, "/api/v1/assets/review?sku_id="+pendingSKU.PublicID+"&status=pending&page=1&page_size=1", "")
	if pagedAssets.Code != http.StatusOK || !strings.Contains(pagedAssets.Body.String(), `"total":2`) {
		t.Fatalf("paged assets = %d %s", pagedAssets.Code, pagedAssets.Body.String())
	}
	legacyAssets := fixture.request(t, fixture.operator, http.MethodGet, "/api/v1/assets/review?sku_id="+pendingSKU.PublicID, "")
	if legacyAssets.Code != http.StatusOK || strings.Contains(legacyAssets.Body.String(), `"pagination"`) {
		t.Fatalf("legacy response changed = %d %s", legacyAssets.Code, legacyAssets.Body.String())
	}
}

func TestAssetReviewQueueRejectsInvalidFiltersAndPermissions(t *testing.T) {
	fixture := newAssetAccessFixture(t)
	for _, path := range []string{
		"/api/v1/assets/review/skus?page=0",
		"/api/v1/assets/review/skus?page_size=101",
		"/api/v1/assets/review/skus?category_id=bad",
		"/api/v1/assets/review/skus?status=unknown",
		"/api/v1/assets/review/skus/not-a-uuid",
	} {
		response := fixture.request(t, fixture.operator, http.MethodGet, path, "")
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d %s, want 400", path, response.Code, response.Body.String())
		}
	}
	for _, path := range []string{"/api/v1/assets/review/skus", "/api/v1/assets/review/skus/" + uuid.NewString()} {
		response := fixture.request(t, fixture.viewer, http.MethodGet, path, "")
		if response.Code != http.StatusForbidden {
			t.Fatalf("viewer %s = %d, want 403", path, response.Code)
		}
	}
}

func TestReviewAssetUsesStrictValidatedTransactionalInput(t *testing.T) {
	fixture := newAssetAccessFixture(t)
	path := "/api/v1/assets/" + fixture.assetA.PublicID + "/review"
	for name, body := range map[string]string{
		"unknown field":  `{"status":"approved","unexpected":true}`,
		"invalid status": `{"status":"draft"}`,
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

func TestReviewAssetCanRestorePendingWithAudit(t *testing.T) {
	fixture := newAssetAccessFixture(t)
	path := "/api/v1/assets/" + fixture.assetA.PublicID + "/review"
	approved := fixture.request(t, fixture.operator, http.MethodPatch, path, `{"status":"approved"}`)
	if approved.Code != http.StatusOK {
		t.Fatalf("approve = %d %s", approved.Code, approved.Body.String())
	}
	restored := fixture.request(t, fixture.operator, http.MethodPatch, path, `{"status":"pending","reason":"undo"}`)
	if restored.Code != http.StatusOK || !strings.Contains(restored.Body.String(), `"review_status":"pending"`) {
		t.Fatalf("restore = %d %s", restored.Code, restored.Body.String())
	}
	var audits []models.AssetReview
	if err := fixture.db.Where("asset_id = ?", fixture.assetA.ID).Order("id ASC").Find(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if len(audits) != 2 || audits[0].Status != "approved" || audits[1].Status != "pending" || audits[1].Reason != "undo" {
		t.Fatalf("unexpected audit trail: %#v", audits)
	}
}

func newAssetAccessFixture(t *testing.T) assetAccessFixture {
	t.Helper()
	db := newTestDB(t)
	users := []models.User{
		{Name: "Operator A", Email: uuid.NewString() + "@example.test", PasswordHash: "unused", Role: models.RoleOperator, Status: "active"},
		{Name: "Operator B", Email: uuid.NewString() + "@example.test", PasswordHash: "unused", Role: models.RoleOperator, Status: "active"},
		{Name: "Admin", Email: uuid.NewString() + "@example.test", PasswordHash: "unused", Role: models.RoleAdmin, Status: "active"},
		{Name: "Operator C", Email: uuid.NewString() + "@example.test", PasswordHash: "unused", Role: models.RoleOperator, Status: "active"},
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
