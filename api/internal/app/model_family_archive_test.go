package app

import (
	"net/http"
	"testing"

	"cargoflow/api/internal/models"
	"cargoflow/api/internal/sop"
)

func TestArchivedModelFamilyPatchIsRejectedWithoutNewAudit(t *testing.T) {
	db := newTestDB(t)
	admin := models.User{Name: "Archive admin", Email: "model-family-archive@example.test", PasswordHash: "test", Role: models.RoleAdmin, Status: "active"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	service := sop.NewModelFamilyService(db)
	family, err := service.Create(t.Context(), sop.CreateModelFamilyInput{Brand: "CargoFlow", NameZH: "归档测试", NameEN: "Archive test", ModelCode: "MODEL-ARCHIVE-HTTP", CommonStructure: []byte(`{"schema":"model_family_common_structure_v1","invariants":["housing"]}`), VariationDimensions: []string{"color"}, CreatedByID: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	archived := models.ModelFamilyArchived
	if _, err := service.Update(t.Context(), family.PublicID, sop.UpdateModelFamilyInput{Status: &archived, UpdatedByID: admin.ID}); err != nil {
		t.Fatal(err)
	}
	var before int64
	if err := db.Model(&models.AIAuditEvent{}).Count(&before).Error; err != nil {
		t.Fatal(err)
	}
	server, token := authenticatedSOPRouter(t, db, admin)
	defer server.Close()
	response := sopRequest(t, server, token, http.MethodPatch, "/api/v1/model-families/"+family.PublicID, `{"status":"archived"}`)
	defer response.Body.Close()
	assertHTTPErrorResponse(t, response, http.StatusConflict, "lifecycle_conflict")
	var after int64
	if err := db.Model(&models.AIAuditEvent{}).Count(&after).Error; err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("HTTP repeat archive wrote audit: before=%d after=%d", before, after)
	}
}
