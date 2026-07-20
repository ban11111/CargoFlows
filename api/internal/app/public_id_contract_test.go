package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cargoflows/api/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestSKURoutesUsePublicUUIDAndSafeDTOs(t *testing.T) {
	db := newTestDB(t)
	category := models.Category{Name: "Public catalog", NameEN: "Public catalog"}
	if err := db.Create(&category).Error; err != nil {
		t.Fatal(err)
	}
	product := models.Product{CategoryID: category.ID, Name: "Public product", Brand: "CargoFlows", Category: category.Name}
	if err := db.Create(&product).Error; err != nil {
		t.Fatal(err)
	}
	sku := models.SKU{ProductID: product.ID, Code: "PUBLIC-ID-SKU", Stock: 5, Status: "active"}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatal(err)
	}
	server := &Server{db: db}

	numeric := performSKUHandlerRequest(t, server.getSKU, http.MethodGet, fmt.Sprintf("/skus/%d", sku.ID), fmt.Sprint(sku.ID), "")
	if numeric.Code != http.StatusBadRequest {
		t.Fatalf("numeric internal SKU ID = %d %s", numeric.Code, numeric.Body.String())
	}

	response := performSKUHandlerRequest(t, server.getSKU, http.MethodGet, "/skus/"+sku.PublicID, sku.PublicID, "")
	if response.Code != http.StatusOK {
		t.Fatalf("public UUID SKU = %d %s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["public_id"] != sku.PublicID {
		t.Fatalf("public_id = %#v", body["public_id"])
	}
	for _, forbidden := range []string{"id", "product_id"} {
		if _, exists := body[forbidden]; exists {
			t.Fatalf("SKU DTO exposed %q: %s", forbidden, response.Body.String())
		}
	}
	if nested, ok := body["product"].(map[string]any); !ok {
		t.Fatalf("missing product DTO: %s", response.Body.String())
	} else if _, exists := nested["id"]; exists {
		t.Fatalf("product DTO exposed internal ID: %s", response.Body.String())
	}

	adjustment := performSKUHandlerRequest(t, server.createInventoryAdjustment, http.MethodPost, "/skus/"+sku.PublicID+"/inventory-adjustments", sku.PublicID, `{"quantity_delta":2,"reason":"restock"}`)
	if adjustment.Code != http.StatusCreated {
		t.Fatalf("public UUID inventory adjustment = %d %s", adjustment.Code, adjustment.Body.String())
	}
	for _, forbidden := range []string{fmt.Sprintf(`"sku_id":%d`, sku.ID), `"operator_id"`} {
		if strings.Contains(adjustment.Body.String(), forbidden) {
			t.Fatalf("inventory DTO leaked %q: %s", forbidden, adjustment.Body.String())
		}
	}
	if !strings.Contains(adjustment.Body.String(), `"sku_id":"`+sku.PublicID+`"`) {
		t.Fatalf("inventory DTO missing public SKU UUID: %s", adjustment.Body.String())
	}

	inUse := performSKUHandlerRequest(t, server.deleteSKU, http.MethodDelete, "/skus/"+sku.PublicID, sku.PublicID, "")
	if inUse.Code != http.StatusConflict || !strings.Contains(inUse.Body.String(), `"code":"sku_in_use"`) {
		t.Fatalf("referenced SKU delete = %d %s", inUse.Code, inUse.Body.String())
	}
	deletableProduct := models.Product{CategoryID: category.ID, Name: "Disposable product", Category: category.Name}
	if err := db.Create(&deletableProduct).Error; err != nil {
		t.Fatal(err)
	}
	deletable := models.SKU{ProductID: deletableProduct.ID, Code: "DELETE-ME", Status: "draft"}
	if err := db.Create(&deletable).Error; err != nil {
		t.Fatal(err)
	}
	deleted := performSKUHandlerRequest(t, server.deleteSKU, http.MethodDelete, "/skus/"+deletable.PublicID, deletable.PublicID, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("unreferenced SKU delete = %d %s", deleted.Code, deleted.Body.String())
	}
	var skuCount, productCount int64
	db.Model(&models.SKU{}).Where("id = ?", deletable.ID).Count(&skuCount)
	db.Model(&models.Product{}).Where("id = ?", deletableProduct.ID).Count(&productCount)
	if skuCount != 0 || productCount != 0 {
		t.Fatalf("delete left SKU/product rows: sku=%d product=%d", skuCount, productCount)
	}
}

func TestAssetMediaRequiresAuthenticationAndSetsSafeHeaders(t *testing.T) {
	db := newTestDB(t)
	user := models.User{Name: "Media reviewer", Email: uuid.NewString() + "@example.test", PasswordHash: "unused", Role: models.RoleOperator, Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	asset := models.Asset{SKUID: 1, ObjectKey: "photo-sessions/session/views/front/image.jpg", OriginalURL: "https://must-not-be-used.invalid/image.jpg", ReviewStatus: "pending", MIMEType: "image/jpeg", Width: 1, Height: 1, ByteCount: int64(len(defaultFakeAssetImage)), SHA256: strings.Repeat("a", 64)}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	cfg := testAssetConfig()
	server := &Server{db: db, cfg: cfg, storage: &fakeAssetStorage{exists: true, source: defaultFakeAssetImage}}
	router := gin.New()
	protected := router.Group("/api/v1")
	protected.Use(server.requireAuth())
	registerExistingRoutes(protected, server)

	unauthorized := httptest.NewRecorder()
	router.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/assets/"+asset.PublicID+"/media", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated media = %d %s", unauthorized.Code, unauthorized.Body.String())
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": user.ID}).SignedString([]byte(cfg.JWTSecret))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/assets/"+asset.PublicID+"/media", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), defaultFakeAssetImage) {
		t.Fatalf("authenticated media = %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(response.Header().Get("Cache-Control"), "private") {
		t.Fatalf("unsafe media headers: %#v", response.Header())
	}
}

func TestHTTPRouteParamsRejectNonCanonicalAndNilUUIDs(t *testing.T) {
	db := newTestDB(t)
	user := models.User{Name: "Strict UUID", Email: uuid.NewString() + "@example.test", PasswordHash: "unused", Role: models.RoleOperator, Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	cfg := testAssetConfig()
	server := &Server{db: db, cfg: cfg}
	router := gin.New()
	protected := router.Group("/api/v1")
	protected.Use(server.requireAuth())
	registerExistingRoutes(protected, server)
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": user.ID}).SignedString([]byte(cfg.JWTSecret))
	if err != nil {
		t.Fatal(err)
	}
	canonical := "abcdefab-cdef-4abc-8def-abcdefabcdef"
	for name, value := range map[string]string{
		"uppercase":    strings.ToUpper(canonical),
		"unhyphenated": strings.ReplaceAll(canonical, "-", ""),
		"nil":          uuid.Nil.String(),
	} {
		t.Run(name, func(t *testing.T) {
			if isUUID(value) {
				t.Fatalf("isUUID(%q) accepted a non-canonical or nil UUID", value)
			}
			request := httptest.NewRequest(http.MethodGet, "/api/v1/capture-sops/"+value, nil)
			request.Header.Set("Authorization", "Bearer "+token)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("SOP route UUID %q = %d, want 400: %s", value, response.Code, response.Body.String())
			}
		})
	}
}

func TestSOPReferenceMediaUsesAuthenticatedPrivateEndpoint(t *testing.T) {
	db := newTestDB(t)
	manager := models.User{Name: "SOP manager", Email: uuid.NewString() + "@example.test", PasswordHash: "unused", Role: models.RoleOperator, Status: "active"}
	viewer := models.User{Name: "SOP viewer", Email: uuid.NewString() + "@example.test", PasswordHash: "unused", Role: models.RoleViewer, Status: "active"}
	for _, user := range []*models.User{&manager, &viewer} {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}
	category := models.Category{Name: "Private reference " + uuid.NewString(), NameEN: "Private reference"}
	if err := db.Create(&category).Error; err != nil {
		t.Fatal(err)
	}
	sop := models.CaptureSOP{PublicID: uuid.NewString(), CategoryID: category.ID, CreatedByID: manager.ID}
	if err := db.Create(&sop).Error; err != nil {
		t.Fatal(err)
	}
	version := models.SOPVersion{PublicID: uuid.NewString(), CaptureSOPID: sop.ID, VersionNumber: 1, SchemaVersion: "1.0", NameZH: "参考图", NameEN: "Reference", Status: models.SOPVersionDraft, CoordinateSystem: "pcs_object_v1"}
	if err := db.Create(&version).Error; err != nil {
		t.Fatal(err)
	}
	view := models.SOPView{PublicID: uuid.NewString(), SOPVersionID: version.ID, Sequence: 1, Role: models.SOPViewReferenceFront, ViewKind: models.SOPViewStandard, PresetKey: "reference_front", NameZH: "正面", NameEN: "Front", Required: true, CameraPositionZ: 1, ImageUpX: 1, Composition: models.Composition{FrameOccupancy: .85, AspectRatio: "1:1", AllowRotationCorrection: true}}
	if err := db.Create(&view).Error; err != nil {
		t.Fatal(err)
	}
	image := models.SOPViewReferenceImage{PublicID: uuid.NewString(), SOPViewID: view.ID, ObjectKey: "sop-references/private.jpg", ThumbnailURL: "http://minio.invalid/source/private.jpg", SortOrder: 1}
	if err := db.Create(&image).Error; err != nil {
		t.Fatal(err)
	}

	document := versionDTOFromModel(models.SOPVersion{Views: []models.SOPView{{ReferenceImages: []models.SOPViewReferenceImage{image}}}}, sop.PublicID)
	wantMediaURL := "/api/v1/sop-reference-images/" + image.PublicID + "/media"
	if got := document.Views[0].ReferenceImages[0].ThumbnailURL; got != wantMediaURL {
		t.Fatalf("reference DTO thumbnail URL = %q, want %q", got, wantMediaURL)
	}

	cfg := testAssetConfig()
	server := &Server{db: db, cfg: cfg, storage: &fakeAssetStorage{source: defaultFakeAssetImage}}
	router := gin.New()
	protected := router.Group("/api/v1")
	protected.Use(server.requireAuth())
	registerExistingRoutes(protected, server)
	request := func(user *models.User) *httptest.ResponseRecorder {
		t.Helper()
		httpRequest := httptest.NewRequest(http.MethodGet, wantMediaURL, nil)
		if user != nil {
			token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": user.ID}).SignedString([]byte(cfg.JWTSecret))
			if err != nil {
				t.Fatal(err)
			}
			httpRequest.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httpRequest)
		return response
	}
	if response := request(nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous reference media = %d %s", response.Code, response.Body.String())
	}
	if response := request(&manager); response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), defaultFakeAssetImage) {
		t.Fatalf("manager draft reference media = %d %s", response.Code, response.Body.String())
	}
	if response := request(&viewer); response.Code != http.StatusNotFound {
		t.Fatalf("viewer draft reference media = %d %s, want 404", response.Code, response.Body.String())
	}
	if err := db.Model(&version).Update("status", models.SOPVersionPublished).Error; err != nil {
		t.Fatal(err)
	}
	if response := request(&viewer); response.Code != http.StatusOK {
		t.Fatalf("viewer published reference media = %d %s", response.Code, response.Body.String())
	}
}

func performSKUHandlerRequest(t *testing.T, handler gin.HandlerFunc, method, path, skuID, body string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Params = gin.Params{{Key: "id", Value: skuID}, {Key: "sku_id", Value: skuID}}
	context.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	context.Set("user", models.User{ID: 77, Role: models.RoleOperator})
	handler(context)
	return response
}
