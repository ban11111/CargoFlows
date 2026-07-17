package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"cargoflow/api/internal/ai"
	"cargoflow/api/internal/config"
	"cargoflow/api/internal/database"
	"cargoflow/api/internal/models"
	"cargoflow/api/internal/secrets"
	"github.com/gin-gonic/gin"
	"github.com/goccy/go-yaml"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type handlerVerifier struct {
	authenticated bool
}

func (f *handlerVerifier) Verify(context.Context, string) (ai.ProviderVerification, error) {
	return ai.ProviderVerification{Authenticated: f.authenticated}, nil
}

func authenticatedAIRouter(t *testing.T, db *gorm.DB, verifier ai.ProviderVerifier) (*ginTestServer, models.User, models.User) {
	t.Helper()
	admin := models.User{Name: "AI Admin", Email: "ai-admin@example.test", PasswordHash: "unused", Role: models.RoleAdmin}
	operator := models.User{Name: "AI Operator", Email: "ai-operator@example.test", PasswordHash: "unused", Role: models.RoleOperator}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&operator).Error; err != nil {
		t.Fatal(err)
	}
	box, err := secrets.NewAESGCM(bytes.Repeat([]byte{0x61}, 32))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{JWTSecret: "ai-handler-test-secret", MinIOEndpoint: "127.0.0.1:9000", MinIOPublicEndpoint: "127.0.0.1:9000", MinIOAccessKey: "test", MinIOSecretKey: "test", MinIOBucket: "test"}
	router := NewRouterWithAIDependencies(cfg, db, AIDependencies{
		ProviderSettings: ai.NewProviderSettingsService(db, box, verifier),
		Templates:        ai.NewTemplateService(db),
	})
	return &ginTestServer{handler: router, jwtSecret: cfg.JWTSecret}, admin, operator
}

type ginTestServer struct {
	handler   http.Handler
	jwtSecret string
}

func (s *ginTestServer) token(t *testing.T, user models.User) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": user.ID}).SignedString([]byte(s.jwtSecret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func aiRequest(t *testing.T, server *ginTestServer, token, method, path, body string) *httptest.ResponseRecorder {
	return aiRequestWithIdempotency(t, server, token, method, path, body, uuid.NewString())
}

func aiRequestWithIdempotency(t *testing.T, server *ginTestServer, token, method, path, body, key string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	response := httptest.NewRecorder()
	server.handler.ServeHTTP(response, req)
	return response
}

func TestOpenAISettingIsAdminOnlyAndNeverEchoesKey(t *testing.T) {
	db := newTestDB(t)
	server, admin, operator := authenticatedAIRouter(t, db, &handlerVerifier{authenticated: true})
	secret := "sk-proj-secret-value-ABCD"
	response := aiRequest(t, server, server.token(t, admin), http.MethodPut, "/api/v1/settings/openai", `{"api_key":"`+secret+`"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("PUT status/body = %d %s", response.Code, response.Body.String())
	}
	for _, forbidden := range []string{secret, "secret-value", "encrypted_api_key", "encryption_nonce"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("PUT leaked %q: %s", forbidden, response.Body.String())
		}
	}

	get := aiRequest(t, server, server.token(t, admin), http.MethodGet, "/api/v1/settings/openai", "")
	if get.Code != http.StatusOK || strings.Contains(get.Body.String(), secret) {
		t.Fatalf("GET status/body = %d %s", get.Code, get.Body.String())
	}
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		body := ""
		if method == http.MethodPut {
			body = `{"api_key":"sk-proj-operator-secret-WXYZ"}`
		}
		forbidden := aiRequest(t, server, server.token(t, operator), method, "/api/v1/settings/openai", body)
		if forbidden.Code != http.StatusForbidden {
			t.Fatalf("operator %s status/body = %d %s", method, forbidden.Code, forbidden.Body.String())
		}
	}

	disabled := aiRequest(t, server, server.token(t, admin), http.MethodDelete, "/api/v1/settings/openai", "")
	if disabled.Code != http.StatusOK || !strings.Contains(disabled.Body.String(), `"status":"disabled"`) {
		t.Fatalf("DELETE status/body = %d %s", disabled.Code, disabled.Body.String())
	}
}

func TestAIContentTemplateAdminLifecycleUsesPublicDTOs(t *testing.T) {
	db := newTestDB(t)
	server, admin, operator := authenticatedAIRouter(t, db, &handlerVerifier{authenticated: true})
	adminToken := server.token(t, admin)

	createBody := `{
		"name_zh":"Lazada 标准套图","name_en":"Lazada Standard Set","target_platform":"lazada",
		"default_locale":"zh-CN","prompt_compiler_version":"v1","platform_prompt":"Use {{product.brand}} accurately.",
		"slots":[{"slot_key":"hero","kind":"image","name_zh":"主图","name_en":"Hero","description_zh":"","description_en":"","sequence":1,"optional":false,"default_selected":true,"prompt_fragment":"Create a faithful hero image.","constraints":{},"generation_config":{"size":"1024x1024","candidate_count":1},"layout_config":{}}]
	}`
	created := aiRequest(t, server, adminToken, http.MethodPost, "/api/v1/ai-content-templates", createBody)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status/body = %d %s", created.Code, created.Body.String())
	}
	var aggregate struct {
		PublicID string `json:"public_id"`
		Versions []struct {
			PublicID string `json:"public_id"`
			Status   string `json:"status"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &aggregate); err != nil {
		t.Fatal(err)
	}
	if aggregate.PublicID == "" || len(aggregate.Versions) != 1 || aggregate.Versions[0].PublicID == "" {
		t.Fatalf("incomplete create DTO: %s", created.Body.String())
	}
	assertNoAIInternalFields(t, created.Body.Bytes())
	versionID := aggregate.Versions[0].PublicID
	updated := aiRequest(t, server, adminToken, http.MethodPatch, "/api/v1/ai-content-template-versions/"+versionID, createBody)
	if updated.Code != http.StatusOK {
		t.Fatalf("draft PATCH status/body = %d %s", updated.Code, updated.Body.String())
	}
	assertNoAIInternalFields(t, updated.Body.Bytes())

	for _, path := range []string{"/api/v1/ai-content-templates?include_all=true", "/api/v1/ai-content-templates/" + aggregate.PublicID} {
		response := aiRequest(t, server, adminToken, http.MethodGet, path, "")
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status/body = %d %s", path, response.Code, response.Body.String())
		}
		assertNoAIInternalFields(t, response.Body.Bytes())
	}

	validated := aiRequest(t, server, adminToken, http.MethodPost, "/api/v1/ai-content-template-versions/"+versionID+"/validate", "")
	if validated.Code != http.StatusOK || !strings.Contains(validated.Body.String(), `"code":"template_valid"`) {
		t.Fatalf("validate status/body = %d %s", validated.Code, validated.Body.String())
	}
	published := aiRequest(t, server, adminToken, http.MethodPost, "/api/v1/ai-content-template-versions/"+versionID+"/publish", "")
	if published.Code != http.StatusOK || !strings.Contains(published.Body.String(), `"status":"published"`) {
		t.Fatalf("publish status/body = %d %s", published.Code, published.Body.String())
	}
	assertNoAIInternalFields(t, published.Body.Bytes())

	immutable := aiRequest(t, server, adminToken, http.MethodPatch, "/api/v1/ai-content-template-versions/"+versionID, createBody)
	if immutable.Code != http.StatusConflict {
		t.Fatalf("published PATCH status/body = %d %s", immutable.Code, immutable.Body.String())
	}

	copied := aiRequest(t, server, adminToken, http.MethodPost, "/api/v1/ai-content-templates/"+aggregate.PublicID+"/versions", `{"source_version_id":"`+versionID+`"}`)
	if copied.Code != http.StatusCreated || !strings.Contains(copied.Body.String(), `"status":"draft"`) {
		t.Fatalf("copy status/body = %d %s", copied.Code, copied.Body.String())
	}
	assertNoAIInternalFields(t, copied.Body.Bytes())

	archived := aiRequest(t, server, adminToken, http.MethodPost, "/api/v1/ai-content-template-versions/"+versionID+"/archive", "")
	if archived.Code != http.StatusOK || !strings.Contains(archived.Body.String(), `"status":"archived"`) {
		t.Fatalf("archive status/body = %d %s", archived.Code, archived.Body.String())
	}
	assertNoAIInternalFields(t, archived.Body.Bytes())

	forbidden := aiRequest(t, server, server.token(t, operator), http.MethodPost, "/api/v1/ai-content-templates", createBody)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("operator create status/body = %d %s", forbidden.Code, forbidden.Body.String())
	}
}

func TestAIContentTemplateDraftPreservesIncompleteAndArbitraryJSONUntilPublish(t *testing.T) {
	db := newTestDB(t)
	server, admin, _ := authenticatedAIRouter(t, db, &handlerVerifier{authenticated: true})
	created := aiRequest(t, server, server.token(t, admin), http.MethodPost, "/api/v1/ai-content-templates", `{
		"slots":[{"constraints":42,"generation_config":["draft"],"layout_config":null}]
	}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create incomplete draft status/body = %d %s", created.Code, created.Body.String())
	}
	var document struct {
		Versions []struct {
			PublicID string `json:"public_id"`
			Slots    []struct {
				Constraints      any `json:"constraints"`
				GenerationConfig any `json:"generation_config"`
				LayoutConfig     any `json:"layout_config"`
			} `json:"slots"`
		} `json:"versions"`
	}
	if err := json.NewDecoder(created.Body).Decode(&document); err != nil {
		t.Fatal(err)
	}
	if len(document.Versions) != 1 || len(document.Versions[0].Slots) != 1 || document.Versions[0].Slots[0].Constraints != float64(42) {
		t.Fatalf("scalar configuration was not preserved: %#v", document)
	}
	if values, ok := document.Versions[0].Slots[0].GenerationConfig.([]any); !ok || len(values) != 1 || values[0] != "draft" {
		t.Fatalf("array configuration was not preserved: %#v", document.Versions[0].Slots[0].GenerationConfig)
	}
	if document.Versions[0].Slots[0].LayoutConfig != nil {
		t.Fatalf("null configuration was not preserved: %#v", document.Versions[0].Slots[0].LayoutConfig)
	}
	publish := aiRequest(t, server, server.token(t, admin), http.MethodPost, "/api/v1/ai-content-template-versions/"+document.Versions[0].PublicID+"/publish", "")
	if publish.Code != http.StatusUnprocessableEntity {
		t.Fatalf("publish incomplete draft status/body = %d %s", publish.Code, publish.Body.String())
	}
	for _, code := range []string{"constraints_object_required", "generation_config_object_required", "layout_config_object_required"} {
		if !strings.Contains(publish.Body.String(), code) {
			t.Errorf("publish response missing %s: %s", code, publish.Body.String())
		}
	}
}

func TestAIRouterConstructorsValidateProductionMasterKey(t *testing.T) {
	db := newTestDB(t)
	base := config.Config{AppEnv: "production", JWTSecret: "test", MinIOEndpoint: "127.0.0.1:9000", MinIOPublicEndpoint: "127.0.0.1:9000", MinIOAccessKey: "test", MinIOSecretKey: "test", MinIOBucket: "test"}
	constructors := map[string]func(config.Config){
		"default": func(cfg config.Config) { NewRouter(cfg, db) },
		"injected": func(cfg config.Config) {
			NewRouterWithAIDependencies(cfg, db, AIDependencies{Templates: ai.NewTemplateService(db)})
		},
	}
	for name, construct := range constructors {
		for _, tc := range []struct {
			name string
			key  string
		}{{name: "missing"}, {name: "invalid_base64", key: "%%%"}, {name: "wrong_length", key: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 31))}} {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				cfg := base
				cfg.SecretsMasterKey = tc.key
				assertPanicsContaining(t, "CARGOFLOW_SECRETS_MASTER_KEY", func() { construct(cfg) })
			})
		}
	}

	nonProduction := base
	nonProduction.AppEnv = "test"
	if router := NewRouterWithAIDependencies(nonProduction, db, AIDependencies{Templates: ai.NewTemplateService(db)}); router == nil {
		t.Fatal("non-production injected router is nil")
	}
}

func TestOpenAISettingHTTPFlowNeverLogsCredentialOrEncryptedMaterial(t *testing.T) {
	var logs bytes.Buffer
	gormLogger := logger.New(log.New(&logs, "", 0), logger.Config{
		SlowThreshold:        time.Second,
		LogLevel:             logger.Info,
		ParameterizedQueries: true,
	})
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: gormLogger})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	previousWriter, previousErrorWriter := gin.DefaultWriter, gin.DefaultErrorWriter
	gin.DefaultWriter, gin.DefaultErrorWriter = &logs, &logs
	t.Cleanup(func() { gin.DefaultWriter, gin.DefaultErrorWriter = previousWriter, previousErrorWriter })

	server, admin, _ := authenticatedAIRouter(t, db, &handlerVerifier{authenticated: true})
	const apiKey = "sk-proj-http-log-secret-ABCD"
	response := aiRequest(t, server, server.token(t, admin), http.MethodPut, "/api/v1/settings/openai", `{"api_key":"`+apiKey+`"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("PUT status/body = %d %s", response.Code, response.Body.String())
	}
	var row models.OpenAIProviderSetting
	if err := db.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	for label, value := range map[string]string{
		"plaintext":         apiKey,
		"ciphertext base64": base64.StdEncoding.EncodeToString(row.EncryptedAPIKey),
		"ciphertext hex":    hex.EncodeToString(row.EncryptedAPIKey),
		"ciphertext bytes":  fmt.Sprint(row.EncryptedAPIKey),
		"nonce base64":      base64.StdEncoding.EncodeToString(row.EncryptionNonce),
		"nonce hex":         hex.EncodeToString(row.EncryptionNonce),
		"nonce bytes":       fmt.Sprint(row.EncryptionNonce),
	} {
		if value != "" && strings.Contains(logs.String(), value) {
			t.Errorf("logs contain %s: %s", label, logs.String())
		}
	}
}

func assertPanicsContaining(t *testing.T, expected string, fn func()) {
	t.Helper()
	defer func() {
		value := recover()
		if value == nil || !strings.Contains(fmt.Sprint(value), expected) {
			t.Fatalf("panic = %v, want text containing %q", value, expected)
		}
	}()
	fn()
}

func TestAIContentTemplatePublishReturnsStructuredValidationIssues(t *testing.T) {
	db := newTestDB(t)
	server, admin, _ := authenticatedAIRouter(t, db, &handlerVerifier{authenticated: true})
	created := aiRequest(t, server, server.token(t, admin), http.MethodPost, "/api/v1/ai-content-templates", `{"name_zh":"","name_en":"","target_platform":"lazada","slots":[]}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create invalid draft status/body = %d %s", created.Code, created.Body.String())
	}
	var value struct {
		Versions []struct {
			PublicID string `json:"public_id"`
		} `json:"versions"`
	}
	if err := json.NewDecoder(created.Body).Decode(&value); err != nil {
		t.Fatal(err)
	}
	response := aiRequest(t, server, server.token(t, admin), http.MethodPost, "/api/v1/ai-content-template-versions/"+value.Versions[0].PublicID+"/publish", "")
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), `"code":"template_validation_failed"`) || !strings.Contains(response.Body.String(), `"issues"`) {
		t.Fatalf("publish status/body = %d %s", response.Code, response.Body.String())
	}
}

func TestAIJobEndpointsUseTypedArraysUUIDsAndSafeDTOs(t *testing.T) {
	db := newTestDB(t)
	server, _, operator := authenticatedAIRouter(t, db, &handlerVerifier{authenticated: true})
	category := models.Category{Name: "AI Jobs", NameEN: "AI Jobs"}
	if err := db.Create(&category).Error; err != nil {
		t.Fatal(err)
	}
	product := models.Product{CategoryID: category.ID, Name: "Bottle", Brand: "CargoFlow", Category: category.Name, Description: "A bottle"}
	if err := db.Create(&product).Error; err != nil {
		t.Fatal(err)
	}
	sku := models.SKU{ProductID: product.ID, Code: "BOTTLE-ONE", Color: "Blue", Size: "500ml", Stock: 42, LowStockThreshold: 3, Status: "active"}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatal(err)
	}
	captureSOP := models.CaptureSOP{PublicID: uuid.NewString(), CategoryID: category.ID, CreatedByID: operator.ID}
	if err := db.Create(&captureSOP).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	sopVersion := models.SOPVersion{PublicID: uuid.NewString(), CaptureSOPID: captureSOP.ID, VersionNumber: 1, SchemaVersion: "1.0", NameZH: "瓶子", NameEN: "Bottle", DescriptionZH: "拍摄", DescriptionEN: "Capture", Status: models.SOPVersionPublished, CoordinateSystem: "pcs_object_v1", PublishedAt: &now}
	if err := db.Create(&sopVersion).Error; err != nil {
		t.Fatal(err)
	}
	view := models.SOPView{PublicID: uuid.NewString(), SOPVersionID: sopVersion.ID, Sequence: 1, Role: models.SOPViewReferenceFront, ViewKind: models.SOPViewStandard, PresetKey: "reference_front", NameZH: "正面", NameEN: "Front", InstructionZH: "拍摄", InstructionEN: "Capture", Required: true, CameraPositionZ: 1, ImageUpX: 1, Composition: models.Composition{FrameOccupancy: .8, AspectRatio: "1:1"}}
	if err := db.Create(&view).Error; err != nil {
		t.Fatal(err)
	}
	template := models.AIContentTemplate{PublicID: uuid.NewString(), NameZH: "Lazada", NameEN: "Lazada", TargetPlatform: "lazada", Status: models.AIContentTemplateActive, CreatedByID: operator.ID}
	if err := db.Create(&template).Error; err != nil {
		t.Fatal(err)
	}
	version := models.AIContentTemplateVersion{PublicID: uuid.NewString(), AIContentTemplateID: template.ID, VersionNumber: 1, Status: models.AITemplatePublished, DefaultLocale: "zh-CN", PromptCompilerVersion: "v1", PlatformPrompt: "Lazada", CreatedByID: operator.ID, PublishedByID: &operator.ID, PublishedAt: &now}
	if err := db.Create(&version).Error; err != nil {
		t.Fatal(err)
	}
	slot := models.AIContentSlot{PublicID: uuid.NewString(), AIContentTemplateVersionID: version.ID, SlotKey: "title", Kind: models.AIContentSlotTitle, NameZH: "标题", NameEN: "Title", Sequence: 1, Optional: true, PromptFragment: "title", ConstraintsJSON: []byte(`{}`), GenerationConfigJSON: []byte(`{}`), LayoutConfigJSON: []byte(`{}`)}
	if err := db.Create(&slot).Error; err != nil {
		t.Fatal(err)
	}
	token := server.token(t, operator)
	createBody := fmt.Sprintf(`{"sku_id":%d,"template_version_id":%q,"selected_slot_keys":["title"],"selected_asset_ids":[],"locale":"zh-CN"}`, sku.ID, version.PublicID)
	created := aiRequestWithIdempotency(t, server, token, http.MethodPost, "/api/v1/ai-jobs", createBody, "http-job-idem-0001")
	if created.Code != http.StatusCreated {
		t.Fatalf("POST status/body = %d %s", created.Code, created.Body.String())
	}
	var job struct {
		PublicID string `json:"public_id"`
		Items    []struct {
			PublicID string `json:"public_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if !isUUID(job.PublicID) || len(job.Items) != 1 || !isUUID(job.Items[0].PublicID) {
		t.Fatalf("invalid public job DTO: %s", created.Body.String())
	}
	replayed := aiRequestWithIdempotency(t, server, token, http.MethodPost, "/api/v1/ai-jobs", createBody, "http-job-idem-0001")
	if replayed.Code != http.StatusOK || !strings.Contains(replayed.Body.String(), job.PublicID) {
		t.Fatalf("replay = %d %s", replayed.Code, replayed.Body.String())
	}
	conflictBody := strings.Replace(createBody, `"locale":"zh-CN"`, `"locale":"en"`, 1)
	conflict := aiRequestWithIdempotency(t, server, token, http.MethodPost, "/api/v1/ai-jobs", conflictBody, "http-job-idem-0001")
	if conflict.Code != http.StatusConflict {
		t.Fatalf("idempotency conflict = %d %s", conflict.Code, conflict.Body.String())
	}
	missingKey := aiRequestWithIdempotency(t, server, token, http.MethodPost, "/api/v1/ai-jobs", createBody, "")
	if missingKey.Code != http.StatusBadRequest {
		t.Fatalf("missing idempotency key = %d %s", missingKey.Code, missingKey.Body.String())
	}
	longLocaleBody := strings.Replace(createBody, `"locale":"zh-CN"`, `"locale":"`+strings.Repeat("x", 33)+`"`, 1)
	longLocale := aiRequestWithIdempotency(t, server, token, http.MethodPost, "/api/v1/ai-jobs", longLocaleBody, "http-job-locale-0001")
	if longLocale.Code != http.StatusBadRequest {
		t.Fatalf("long locale = %d %s", longLocale.Code, longLocale.Body.String())
	}
	assertNoAIInternalFields(t, created.Body.Bytes())
	for _, path := range []string{"/api/v1/ai-jobs", "/api/v1/ai-jobs/" + job.PublicID} {
		response := aiRequest(t, server, token, http.MethodGet, path, "")
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s = %d %s", path, response.Code, response.Body.String())
		}
		assertNoAIInternalFields(t, response.Body.Bytes())
	}
	legacy := aiRequest(t, server, token, http.MethodPost, "/api/v1/ai-jobs", fmt.Sprintf(`{"sku_id":%d,"target_platform":"lazada","input_asset_ids":"1,2"}`, sku.ID))
	if legacy.Code != http.StatusBadRequest {
		t.Fatalf("legacy payload = %d %s", legacy.Code, legacy.Body.String())
	}
	missingArray := aiRequest(t, server, token, http.MethodPost, "/api/v1/ai-jobs", fmt.Sprintf(`{"sku_id":%d,"template_version_id":%q,"selected_slot_keys":["title"],"locale":"zh-CN"}`, sku.ID, version.PublicID))
	if missingArray.Code != http.StatusBadRequest {
		t.Fatalf("missing selected_asset_ids array = %d %s", missingArray.Code, missingArray.Body.String())
	}
	invalidUUID := aiRequest(t, server, token, http.MethodGet, "/api/v1/ai-jobs/not-a-uuid", "")
	if invalidUUID.Code != http.StatusBadRequest {
		t.Fatalf("invalid UUID = %d %s", invalidUUID.Code, invalidUUID.Body.String())
	}
	photographer := models.User{Name: "AI Photographer", Email: uuid.NewString() + "@example.test", PasswordHash: "unused", Role: models.RolePhotographer, Status: "active"}
	if err := db.Create(&photographer).Error; err != nil {
		t.Fatal(err)
	}
	photographerToken := server.token(t, photographer)
	for _, request := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/ai-jobs", ""},
		{http.MethodGet, "/api/v1/ai-jobs/" + job.PublicID, ""},
		{http.MethodPost, "/api/v1/ai-jobs", fmt.Sprintf(`{"sku_id":%d,"template_version_id":%q,"selected_slot_keys":["title"],"selected_asset_ids":[],"locale":"zh-CN"}`, sku.ID, version.PublicID)},
	} {
		response := aiRequest(t, server, photographerToken, request.method, request.path, request.body)
		if response.Code != http.StatusForbidden {
			t.Fatalf("photographer %s %s = %d %s", request.method, request.path, response.Code, response.Body.String())
		}
	}
}

func TestAIOpenAPIHasExactAdminPathsAndNeverExposesCredentialMaterial(t *testing.T) {
	contents, err := os.ReadFile("../../openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(contents, &document); err != nil {
		t.Fatal(err)
	}
	paths := document["paths"].(map[string]any)
	expected := map[string]map[string][]string{
		"/settings/openai": {
			"get":    {"200", "401", "403", "500", "503"},
			"put":    {"200", "400", "401", "403", "422", "500", "503"},
			"delete": {"200", "401", "403", "404", "500", "503"},
		},
		"/ai-content-templates": {
			"get":  {"200", "400", "401", "403", "500"},
			"post": {"201", "400", "401", "403", "500"},
		},
		"/ai-content-templates/{template_id}":                 {"get": {"200", "400", "401", "403", "404", "500"}},
		"/ai-content-templates/{template_id}/versions":        {"post": {"201", "400", "401", "403", "404", "409", "500"}},
		"/ai-content-template-versions/{version_id}":          {"patch": {"200", "400", "401", "403", "404", "409", "500"}},
		"/ai-content-template-versions/{version_id}/validate": {"post": {"200", "400", "401", "403", "404", "500"}},
		"/ai-content-template-versions/{version_id}/publish":  {"post": {"200", "400", "401", "403", "404", "409", "422", "500"}},
		"/ai-content-template-versions/{version_id}/archive":  {"post": {"200", "400", "401", "403", "404", "409", "500"}},
	}
	for path, methods := range expected {
		pathItem, ok := paths[path].(map[string]any)
		if !ok {
			t.Errorf("missing path %s", path)
			continue
		}
		for method, statuses := range methods {
			operation, ok := pathItem[method].(map[string]any)
			if !ok {
				t.Errorf("missing operation %s %s", method, path)
				continue
			}
			responses := operation["responses"].(map[string]any)
			for _, status := range statuses {
				if _, ok := responses[status]; !ok {
					t.Errorf("%s %s missing response %s", method, path, status)
				}
			}
		}
	}

	schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
	for _, name := range []string{"OpenAISetting", "OpenAISettingRequest", "AIContentTemplate", "AIContentTemplateVersion", "AIContentSlot", "AIContentTemplateMutationRequest", "AITemplateValidationResponse"} {
		if _, ok := schemas[name]; !ok {
			t.Errorf("missing schema %s", name)
		}
	}
	setting, ok := schemas["OpenAISetting"].(map[string]any)
	if ok {
		properties := setting["properties"].(map[string]any)
		for _, forbidden := range []string{"api_key", "encrypted_api_key", "encryption_nonce", "encryption_key_version"} {
			if _, exists := properties[forbidden]; exists {
				t.Errorf("OpenAISetting exposes %s", forbidden)
			}
		}
	}
	for _, name := range []string{"AIContentTemplateMutationRequest", "AIContentSlotMutation"} {
		schema := schemas[name].(map[string]any)
		if required, exists := schema["required"]; exists {
			t.Errorf("%s incorrectly requires invalid-draft fields: %#v", name, required)
		}
	}
	for _, name := range []string{"AIContentSlotMutation", "AIContentSlot"} {
		properties := schemas[name].(map[string]any)["properties"].(map[string]any)
		for _, field := range []string{"constraints", "generation_config", "layout_config"} {
			if schema := properties[field].(map[string]any); len(schema) != 0 {
				t.Errorf("%s.%s must accept any valid draft JSON, got %#v", name, field, schema)
			}
		}
	}
	responses := document["components"].(map[string]any)["responses"].(map[string]any)
	serviceUnavailable := responses["ServiceUnavailable"].(map[string]any)
	if description := serviceUnavailable["description"].(string); strings.Contains(strings.ToLower(description), "object storage") {
		t.Errorf("shared ServiceUnavailable description is storage-specific: %q", description)
	}
}

func assertNoAIInternalFields(t *testing.T, body []byte) {
	t.Helper()
	for _, field := range [][]byte{[]byte(`"id"`), []byte("created_by_id"), []byte("published_by_id"), []byte("ai_content_template_id"), []byte("draft_guard"), []byte("constraints_json"), []byte("idempotency_key"), []byte("request_sha256")} {
		if bytes.Contains(body, field) {
			t.Fatalf("response contains internal field %s: %s", field, body)
		}
	}
}
