package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"cargoflow/api/internal/ai"
	"cargoflow/api/internal/config"
	"cargoflow/api/internal/models"
	"cargoflow/api/internal/secrets"
	"github.com/goccy/go-yaml"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
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
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
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

	forbidden := aiRequest(t, server, server.token(t, operator), http.MethodPost, "/api/v1/ai-content-templates", createBody)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("operator create status/body = %d %s", forbidden.Code, forbidden.Body.String())
	}
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
}

func assertNoAIInternalFields(t *testing.T, body []byte) {
	t.Helper()
	for _, field := range [][]byte{[]byte(`"id"`), []byte("created_by_id"), []byte("published_by_id"), []byte("ai_content_template_id"), []byte("draft_guard"), []byte("constraints_json")} {
		if bytes.Contains(body, field) {
			t.Fatalf("response contains internal field %s: %s", field, body)
		}
	}
}
