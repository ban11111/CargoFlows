package app

import (
	"encoding/json"
	"net/http"
	"testing"

	"cargoflows/api/internal/models"
	"github.com/google/uuid"
)

func TestSOPMutationsRequireManagerRole(t *testing.T) {
	for _, role := range []models.Role{models.RoleViewer, models.RolePhotographer} {
		t.Run(string(role), func(t *testing.T) {
			db := newTestDB(t)
			category, _ := seedSOPCategoryAndUser(t, db)
			user := models.User{Name: string(role), Email: string(role) + "@example.com", PasswordHash: "test", Role: role, Status: "active"}
			if err := db.Create(&user).Error; err != nil {
				t.Fatal(err)
			}
			server, token := authenticatedSOPRouter(t, db, user)
			defer server.Close()

			response := sopRequest(t, server, token, http.MethodPost, "/api/v1/capture-sops", `{"category_id":`+jsonNumber(category.ID)+`,"name":{"zh-CN":"禁止创建","en":"Forbidden"}}`)
			defer response.Body.Close()
			assertSOPAuthError(t, response, http.StatusForbidden, "forbidden")

			var count int64
			if err := db.Model(&models.CaptureSOP{}).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("unauthorized mutation created %d SOPs", count)
			}
		})
	}
}

func TestOperatorCanManageSOPs(t *testing.T) {
	db := newTestDB(t)
	category, _ := seedSOPCategoryAndUser(t, db)
	operator := models.User{Name: "Operator", Email: "operator@example.com", PasswordHash: "test", Role: models.RoleOperator, Status: "active"}
	if err := db.Create(&operator).Error; err != nil {
		t.Fatal(err)
	}
	server, token := authenticatedSOPRouter(t, db, operator)
	defer server.Close()

	response := sopRequest(t, server, token, http.MethodPost, "/api/v1/capture-sops", `{"category_id":`+jsonNumber(category.ID)+`,"name":{"zh-CN":"运营创建","en":"Operator Created"}}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusCreated)
	}
}

func TestNonManagerCannotReadDraftLifecycleData(t *testing.T) {
	db := newTestDB(t)
	category, manager := seedSOPCategoryAndUser(t, db)
	service := NewSOPService(db)
	created := createTestSOP(t, service, category, manager)
	viewer := models.User{Name: "Viewer", Email: "draft-viewer@example.com", PasswordHash: "test", Role: models.RoleViewer, Status: "active"}
	if err := db.Create(&viewer).Error; err != nil {
		t.Fatal(err)
	}
	server, token := authenticatedSOPRouter(t, db, viewer)
	defer server.Close()

	includeAll := sopRequest(t, server, token, http.MethodGet, "/api/v1/capture-sops?include_all=true", "")
	defer includeAll.Body.Close()
	assertSOPAuthError(t, includeAll, http.StatusForbidden, "forbidden")

	version := sopRequest(t, server, token, http.MethodGet, "/api/v1/sop-versions/"+created.Version.PublicID, "")
	defer version.Body.Close()
	assertSOPAuthError(t, version, http.StatusNotFound, "not_found")

	parent := sopRequest(t, server, token, http.MethodGet, "/api/v1/capture-sops/"+created.SOP.PublicID, "")
	defer parent.Body.Close()
	assertSOPAuthError(t, parent, http.StatusNotFound, "not_found")

	if _, err := service.Publish(t.Context(), created.Version.PublicID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/api/v1/capture-sops",
		"/api/v1/capture-sops/" + created.SOP.PublicID,
		"/api/v1/sop-versions/" + created.Version.PublicID,
	} {
		response := sopRequest(t, server, token, http.MethodGet, path, "")
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			t.Fatalf("published read %s status = %d, want %d", path, response.StatusCode, http.StatusOK)
		}
		response.Body.Close()
	}
}

func TestAllSOPMutationRoutesRejectViewerBeforeResourceLookup(t *testing.T) {
	db := newTestDB(t)
	_, _ = seedSOPCategoryAndUser(t, db)
	viewer := models.User{Name: "Viewer", Email: "route-viewer@example.com", PasswordHash: "test", Role: models.RoleViewer, Status: "active"}
	if err := db.Create(&viewer).Error; err != nil {
		t.Fatal(err)
	}
	server, token := authenticatedSOPRouter(t, db, viewer)
	defer server.Close()
	id := uuid.NewString()
	cases := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/capture-sops"},
		{http.MethodPatch, "/api/v1/sop-versions/" + id},
		{http.MethodPost, "/api/v1/sop-versions/" + id + "/views"},
		{http.MethodPatch, "/api/v1/sop-versions/" + id + "/views/" + id},
		{http.MethodDelete, "/api/v1/sop-versions/" + id + "/views/" + id},
		{http.MethodPut, "/api/v1/sop-versions/" + id + "/view-order"},
		{http.MethodPost, "/api/v1/sop-versions/" + id + "/validate"},
		{http.MethodPost, "/api/v1/sop-versions/" + id + "/publish"},
		{http.MethodPost, "/api/v1/capture-sops/" + id + "/versions"},
		{http.MethodPost, "/api/v1/sop-versions/" + id + "/archive"},
		{http.MethodPost, "/api/v1/sop-versions/" + id + "/restore"},
		{http.MethodPost, "/api/v1/sop-versions/" + id + "/views/" + id + "/reference-images/upload-url"},
		{http.MethodPost, "/api/v1/sop-versions/" + id + "/views/" + id + "/reference-images"},
		{http.MethodDelete, "/api/v1/sop-versions/" + id + "/views/" + id + "/reference-images/" + id},
		{http.MethodPut, "/api/v1/sop-versions/" + id + "/views/" + id + "/reference-image-order"},
	}
	for _, tc := range cases {
		response := sopRequest(t, server, token, tc.method, tc.path, `{}`)
		assertSOPAuthError(t, response, http.StatusForbidden, "forbidden")
		response.Body.Close()
	}
}

func assertSOPAuthError(t *testing.T, response *http.Response, status int, code string) {
	t.Helper()
	if response.StatusCode != status {
		t.Fatalf("status = %d, want %d", response.StatusCode, status)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Code != code {
		t.Fatalf("code = %q, want %q", body.Code, code)
	}
}
