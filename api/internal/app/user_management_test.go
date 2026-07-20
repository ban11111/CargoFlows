package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cargoflows/api/internal/models"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type userManagementFixture struct {
	db       *gorm.DB
	router   http.Handler
	secret   string
	super    models.User
	admin    models.User
	operator models.User
}

func newUserManagementFixture(t *testing.T) userManagementFixture {
	t.Helper()
	db := newTestDB(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("existing-password-123"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	users := []models.User{
		{Name: "Owner", Email: "owner@example.test", PasswordHash: string(hash), Role: models.RoleSuperAdmin, Status: "active"},
		{Name: "Admin", Email: "admin@example.test", PasswordHash: string(hash), Role: models.RoleAdmin, Status: "active"},
		{Name: "Operator", Email: "operator@example.test", PasswordHash: string(hash), Role: models.RoleOperator, Status: "active"},
	}
	for index := range users {
		if err := db.Create(&users[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	cfg := testAssetConfig()
	cfg.MinIOEndpoint = "127.0.0.1:9000"
	cfg.MinIOPublicEndpoint = "127.0.0.1:9000"
	cfg.MinIOAccessKey = "test"
	cfg.MinIOSecretKey = "test"
	cfg.MinIOBucket = "test-assets"
	cfg.MinIOAIBucket = "test-ai"
	return userManagementFixture{db: db, router: NewRouter(cfg, db), secret: cfg.JWTSecret, super: users[0], admin: users[1], operator: users[2]}
}

func (fixture userManagementFixture) token(t *testing.T, user models.User) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": user.ID, "session_version": user.SessionVersion}).SignedString([]byte(fixture.secret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func (fixture userManagementFixture) request(t *testing.T, user models.User, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Authorization", "Bearer "+fixture.token(t, user))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	return response
}

func TestUserManagementCreatesNormalizesAndProtectsAccounts(t *testing.T) {
	fixture := newUserManagementFixture(t)
	denied := fixture.request(t, fixture.operator, http.MethodGet, "/api/v1/users", "")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("operator users = %d %s", denied.Code, denied.Body.String())
	}

	created := fixture.request(t, fixture.admin, http.MethodPost, "/api/v1/users", `{"name":"New User","email":"  NEW@Example.Test ","role":"operator","password":"temporary-password-123"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Code, created.Body.String())
	}
	var body userDTO
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Email != "new@example.test" || body.Role != models.RoleOperator || !body.MustChangePassword || body.PublicID == "" || body.LastSeenAt != nil {
		t.Fatalf("created user = %#v", body)
	}
	duplicate := fixture.request(t, fixture.admin, http.MethodPost, "/api/v1/users", `{"name":"Again","email":"new@example.test","role":"admin","password":"temporary-password-123"}`)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate = %d %s", duplicate.Code, duplicate.Body.String())
	}
	illegal := fixture.request(t, fixture.admin, http.MethodPost, "/api/v1/users", `{"name":"Owner 2","email":"owner2@example.test","role":"super_admin","password":"temporary-password-123"}`)
	if illegal.Code != http.StatusUnprocessableEntity {
		t.Fatalf("illegal role = %d %s", illegal.Code, illegal.Body.String())
	}

	protected := fixture.request(t, fixture.super, http.MethodPatch, "/api/v1/users/"+fixture.super.PublicID, `{"status":"disabled"}`)
	if protected.Code != http.StatusForbidden {
		t.Fatalf("owner mutation = %d %s", protected.Code, protected.Body.String())
	}
	self := fixture.request(t, fixture.admin, http.MethodPatch, "/api/v1/users/"+fixture.admin.PublicID, `{"role":"operator"}`)
	if self.Code != http.StatusForbidden {
		t.Fatalf("self mutation = %d %s", self.Code, self.Body.String())
	}

	oldOperatorToken := fixture.token(t, fixture.operator)
	disabled := fixture.request(t, fixture.admin, http.MethodPatch, "/api/v1/users/"+fixture.operator.PublicID, `{"status":"disabled"}`)
	if disabled.Code != http.StatusOK {
		t.Fatalf("disable = %d %s", disabled.Code, disabled.Body.String())
	}
	if enabled := fixture.request(t, fixture.admin, http.MethodPatch, "/api/v1/users/"+fixture.operator.PublicID, `{"status":"active"}`); enabled.Code != http.StatusOK {
		t.Fatalf("enable = %d %s", enabled.Code, enabled.Body.String())
	}
	activeDelete := fixture.request(t, fixture.admin, http.MethodDelete, "/api/v1/users/"+fixture.operator.PublicID, "")
	if activeDelete.Code != http.StatusConflict || !bytes.Contains(activeDelete.Body.Bytes(), []byte("user_must_be_disabled")) {
		t.Fatalf("delete active = %d %s", activeDelete.Code, activeDelete.Body.String())
	}
	oldRequest := httptest.NewRequest(http.MethodGet, "/api/v1/tags", nil)
	oldRequest.Header.Set("Authorization", "Bearer "+oldOperatorToken)
	oldResponse := httptest.NewRecorder()
	fixture.router.ServeHTTP(oldResponse, oldRequest)
	if oldResponse.Code != http.StatusUnauthorized {
		t.Fatalf("session survived disable/enable = %d %s", oldResponse.Code, oldResponse.Body.String())
	}
	if disabledAgain := fixture.request(t, fixture.admin, http.MethodPatch, "/api/v1/users/"+fixture.operator.PublicID, `{"status":"disabled"}`); disabledAgain.Code != http.StatusOK {
		t.Fatalf("disable before delete = %d %s", disabledAgain.Code, disabledAgain.Body.String())
	}
	deleted := fixture.request(t, fixture.admin, http.MethodDelete, "/api/v1/users/"+fixture.operator.PublicID, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete disabled = %d %s", deleted.Code, deleted.Body.String())
	}
	var deletedUser models.User
	if err := fixture.db.Unscoped().Where("id = ?", fixture.operator.ID).First(&deletedUser).Error; err != nil || !deletedUser.DeletedAt.Valid {
		t.Fatalf("soft-deleted user = %#v, err = %v", deletedUser, err)
	}
	if visible := fixture.request(t, fixture.admin, http.MethodGet, "/api/v1/users", ""); visible.Code != http.StatusOK || bytes.Contains(visible.Body.Bytes(), []byte("operator@example.test")) {
		t.Fatalf("list after delete = %d %s", visible.Code, visible.Body.String())
	}
	ownerDelete := fixture.request(t, fixture.super, http.MethodDelete, "/api/v1/users/"+fixture.super.PublicID, "")
	if ownerDelete.Code != http.StatusForbidden {
		t.Fatalf("delete owner = %d %s", ownerDelete.Code, ownerDelete.Body.String())
	}
}

func TestTemporaryPasswordRequiresChangeAndRevokesOldSession(t *testing.T) {
	fixture := newUserManagementFixture(t)
	oldToken := fixture.token(t, fixture.operator)
	reset := fixture.request(t, fixture.admin, http.MethodPut, "/api/v1/users/"+fixture.operator.PublicID+"/password", `{"password":"temporary-password-456"}`)
	if reset.Code != http.StatusOK {
		t.Fatalf("reset = %d %s", reset.Code, reset.Body.String())
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/tags", nil)
	request.Header.Set("Authorization", "Bearer "+oldToken)
	response := httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("old session = %d %s", response.Code, response.Body.String())
	}

	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"email":"operator@example.test","password":"temporary-password-456"}`))
	login.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	fixture.router.ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login = %d %s", loginResponse.Code, loginResponse.Body.String())
	}
	var auth struct {
		Token string  `json:"token"`
		User  userDTO `json:"user"`
	}
	if err := json.Unmarshal(loginResponse.Body.Bytes(), &auth); err != nil || !auth.User.MustChangePassword {
		t.Fatalf("login body = %s, err = %v", loginResponse.Body.String(), err)
	}

	business := httptest.NewRequest(http.MethodGet, "/api/v1/tags", nil)
	business.Header.Set("Authorization", "Bearer "+auth.Token)
	businessResponse := httptest.NewRecorder()
	fixture.router.ServeHTTP(businessResponse, business)
	if businessResponse.Code != http.StatusForbidden || !bytes.Contains(businessResponse.Body.Bytes(), []byte("password_change_required")) {
		t.Fatalf("business before change = %d %s", businessResponse.Code, businessResponse.Body.String())
	}

	change := httptest.NewRequest(http.MethodPost, "/api/v1/auth/change-password", bytes.NewBufferString(`{"current_password":"temporary-password-456","new_password":"personal-password-789"}`))
	change.Header.Set("Authorization", "Bearer "+auth.Token)
	change.Header.Set("Content-Type", "application/json")
	changeResponse := httptest.NewRecorder()
	fixture.router.ServeHTTP(changeResponse, change)
	if changeResponse.Code != http.StatusOK {
		t.Fatalf("change = %d %s", changeResponse.Code, changeResponse.Body.String())
	}
}

func TestThreeTierPermissionMatrix(t *testing.T) {
	fixture := newUserManagementFixture(t)
	checks := []struct {
		name       string
		user       models.User
		path       string
		wantStatus int
	}{
		{name: "admin cannot manage OpenAI", user: fixture.admin, path: "/api/v1/settings/openai", wantStatus: http.StatusForbidden},
		{name: "operator cannot manage users", user: fixture.operator, path: "/api/v1/users", wantStatus: http.StatusForbidden},
		{name: "operator cannot open review hierarchy", user: fixture.operator, path: "/api/v1/assets/review/hierarchy", wantStatus: http.StatusForbidden},
		{name: "admin can list users", user: fixture.admin, path: "/api/v1/users", wantStatus: http.StatusOK},
		{name: "admin can manage templates", user: fixture.admin, path: "/api/v1/ai-content-templates?include_all=true", wantStatus: http.StatusOK},
		{name: "operator retains SOP operations", user: fixture.operator, path: "/api/v1/capture-sops?include_all=true", wantStatus: http.StatusOK},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			response := fixture.request(t, check.user, http.MethodGet, check.path, "")
			if response.Code != check.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, check.wantStatus, response.Body.String())
			}
		})
	}
}
