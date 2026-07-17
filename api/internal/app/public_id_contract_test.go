package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cargoflow/api/internal/models"
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
	product := models.Product{CategoryID: category.ID, Name: "Public product", Brand: "CargoFlow", Category: category.Name}
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
