package app

import (
	"encoding/json"
	"net/http"
	"testing"

	"cargoflow/api/internal/models"
	"cargoflow/api/internal/sop"
)

func TestModelFamilyRoutesEnforceRolesAndExposeOnlyPublicIDs(t *testing.T) {
	db := newTestDB(t)
	admin := models.User{Name: "Admin", Email: "model-family-admin@example.test", PasswordHash: "test", Role: models.RoleAdmin, Status: "active"}
	operator := models.User{Name: "Operator", Email: "model-family-operator@example.test", PasswordHash: "test", Role: models.RoleOperator, Status: "active"}
	viewer := models.User{Name: "Viewer", Email: "model-family-viewer@example.test", PasswordHash: "test", Role: models.RoleViewer, Status: "active"}
	photographer := models.User{Name: "Photographer", Email: "model-family-photographer@example.test", PasswordHash: "test", Role: models.RolePhotographer, Status: "active"}
	for _, user := range []*models.User{&admin, &operator, &viewer, &photographer} {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}
	product := models.Product{Name: "Family test", Category: "test"}
	if err := db.Create(&product).Error; err != nil {
		t.Fatal(err)
	}
	sku := models.SKU{ProductID: product.ID, Code: "MODEL-FAMILY-HTTP"}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatal(err)
	}
	server, adminToken := authenticatedSOPRouter(t, db, admin)
	defer server.Close()
	operatorServer, operatorToken := authenticatedSOPRouter(t, db, operator)
	defer operatorServer.Close()
	viewerServer, viewerToken := authenticatedSOPRouter(t, db, viewer)
	defer viewerServer.Close()
	photographerServer, photographerToken := authenticatedSOPRouter(t, db, photographer)
	defer photographerServer.Close()
	body := `{"brand":"CargoFlow","name_zh":"测试型号","name_en":"Test model","model_code":"HTTP-MODEL","common_structure":{"schema":"model_family_common_structure_v1","invariants":["housing"]},"variation_dimensions":["color"]}`
	for _, token := range []string{viewerToken, photographerToken, operatorToken} {
		response := sopRequest(t, server, token, http.MethodPost, "/api/v1/model-families", body)
		defer response.Body.Close()
		assertHTTPErrorResponse(t, response, http.StatusForbidden, "forbidden")
	}
	created := sopRequest(t, server, adminToken, http.MethodPost, "/api/v1/model-families", body)
	defer created.Body.Close()
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", created.StatusCode)
	}
	var family struct {
		PublicID string `json:"public_id"`
		ID       any    `json:"id"`
		Members  []any  `json:"members"`
	}
	if err := json.NewDecoder(created.Body).Decode(&family); err != nil {
		t.Fatal(err)
	}
	if family.PublicID == "" || family.ID != nil {
		t.Fatalf("unsafe family response = %#v", family)
	}
	response := sopRequest(t, server, operatorToken, http.MethodPost, "/api/v1/model-families/"+family.PublicID+"/members", `{"sku_id":"`+sku.PublicID+`"}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("operator add status = %d", response.StatusCode)
	}
	var member struct {
		PublicID string `json:"public_id"`
		SKUID    string `json:"sku_id"`
		ID       any    `json:"id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&member); err != nil {
		t.Fatal(err)
	}
	if member.PublicID == "" || member.SKUID != sku.PublicID || member.ID != nil {
		t.Fatalf("unsafe member response = %#v", member)
	}
	read := sopRequest(t, server, viewerToken, http.MethodGet, "/api/v1/model-families/"+family.PublicID, "")
	defer read.Body.Close()
	if read.StatusCode != http.StatusOK {
		t.Fatalf("viewer read status = %d", read.StatusCode)
	}
	var readFamily map[string]any
	if err := json.NewDecoder(read.Body).Decode(&readFamily); err != nil {
		t.Fatal(err)
	}
	if _, found := readFamily["id"]; found {
		t.Fatalf("family read leaked internal id: %#v", readFamily)
	}
	members, ok := readFamily["members"].([]any)
	if !ok || len(members) != 1 {
		t.Fatalf("viewer family membership = %#v", readFamily)
	}
	if _, found := members[0].(map[string]any)["id"]; found {
		t.Fatalf("member read leaked internal id: %#v", members[0])
	}
	malformed := sopRequest(t, server, adminToken, http.MethodGet, "/api/v1/model-families/not-a-uuid", "")
	defer malformed.Body.Close()
	assertHTTPErrorResponse(t, malformed, http.StatusBadRequest, "invalid_request")
	removed := sopRequest(t, server, operatorToken, http.MethodDelete, "/api/v1/model-families/"+family.PublicID+"/members/"+member.PublicID, "")
	defer removed.Body.Close()
	if removed.StatusCode != http.StatusNoContent {
		t.Fatalf("remove status = %d", removed.StatusCode)
	}
}

func TestVariantIdentityRoutesEnforceMutationRolesAndPublishReadOnlyFacts(t *testing.T) {
	db := newTestDB(t)
	admin := models.User{Name: "Admin", Email: "variant-admin@example.test", PasswordHash: "test", Role: models.RoleAdmin, Status: "active"}
	operator := models.User{Name: "Operator", Email: "variant-operator@example.test", PasswordHash: "test", Role: models.RoleOperator, Status: "active"}
	viewer := models.User{Name: "Viewer", Email: "variant-viewer@example.test", PasswordHash: "test", Role: models.RoleViewer, Status: "active"}
	for _, user := range []*models.User{&admin, &operator, &viewer} {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}
	product := models.Product{Name: "Variant API product", Category: "test"}
	if err := db.Create(&product).Error; err != nil {
		t.Fatal(err)
	}
	target := models.SKU{ProductID: product.ID, Code: "VARIANT-HTTP"}
	if err := db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	family, err := sop.NewModelFamilyService(db).Create(t.Context(), sop.CreateModelFamilyInput{Brand: "CargoFlow", NameZH: "同款", NameEN: "Same", ModelCode: "VARIANT-HTTP", CommonStructure: json.RawMessage(`{"schema":"model_family_common_structure_v1","invariants":["housing"]}`), VariationDimensions: []string{"color"}, CreatedByID: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sop.NewModelFamilyService(db).AddMember(t.Context(), family.PublicID, target.PublicID, operator.ID); err != nil {
		t.Fatal(err)
	}
	server, adminToken := authenticatedSOPRouter(t, db, admin)
	defer server.Close()
	_, operatorToken := authenticatedSOPRouter(t, db, operator)
	_, viewerToken := authenticatedSOPRouter(t, db, viewer)
	body := `{"identity":{"schema":"variant_identity_v1","colors":[{"key":"body","name":"blue","value":"#123ABC"}],"material":"","finish":"","texture":"","labels":[],"ports":[],"controls":[],"accessories":[],"packaging":[],"other":[],"must_prove_with_target_assets":[]},"regions":[]}`
	denied := sopRequest(t, server, viewerToken, http.MethodPost, "/api/v1/skus/"+target.PublicID+"/variant-identity/versions", body)
	defer denied.Body.Close()
	assertHTTPErrorResponse(t, denied, http.StatusForbidden, "forbidden")
	created := sopRequest(t, server, operatorToken, http.MethodPost, "/api/v1/skus/"+target.PublicID+"/variant-identity/versions", body)
	defer created.Body.Close()
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", created.StatusCode)
	}
	var version struct {
		PublicID string `json:"public_id"`
		ID       any    `json:"id"`
	}
	if err := json.NewDecoder(created.Body).Decode(&version); err != nil {
		t.Fatal(err)
	}
	if version.PublicID == "" || version.ID != nil {
		t.Fatalf("unsafe version = %#v", version)
	}
	validated := sopRequest(t, server, operatorToken, http.MethodPost, "/api/v1/variant-identity-versions/"+version.PublicID+"/validate", "")
	defer validated.Body.Close()
	if validated.StatusCode != http.StatusOK {
		t.Fatalf("validate status = %d", validated.StatusCode)
	}
	published := sopRequest(t, server, adminToken, http.MethodPost, "/api/v1/variant-identity-versions/"+version.PublicID+"/publish", "")
	defer published.Body.Close()
	if published.StatusCode != http.StatusOK {
		t.Fatalf("publish status = %d", published.StatusCode)
	}
	read := sopRequest(t, server, viewerToken, http.MethodGet, "/api/v1/skus/"+target.PublicID+"/variant-identity", "")
	defer read.Body.Close()
	if read.StatusCode != http.StatusOK {
		t.Fatalf("viewer read status = %d", read.StatusCode)
	}
	var response map[string]any
	if err := json.NewDecoder(read.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	for _, unsafe := range []string{"id", "object_key", "asset_id", "published_by_id"} {
		if _, found := response[unsafe]; found {
			t.Fatalf("read leaked %s: %#v", unsafe, response)
		}
	}
	immutable := sopRequest(t, server, operatorToken, http.MethodPatch, "/api/v1/variant-identity-versions/"+version.PublicID, body)
	defer immutable.Body.Close()
	assertHTTPErrorResponse(t, immutable, http.StatusConflict, "version_immutable")
}
