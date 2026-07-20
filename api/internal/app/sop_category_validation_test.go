package app

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"cargoflows/api/internal/models"
)

func TestSOPServiceCreateRejectsMissingCategoryWithoutWrites(t *testing.T) {
	db := newTestDB(t)
	_, user := seedSOPCategoryAndUser(t, db)

	_, err := NewSOPService(db).Create(context.Background(), CreateSOPInput{
		CategoryID: 999999, CreatedByID: user.ID, NameZH: "不存在", NameEN: "Missing",
	})
	if !errors.Is(err, ErrCategoryNotFound) {
		t.Fatalf("error = %v, want ErrCategoryNotFound", err)
	}
	var sopCount, versionCount, viewCount int64
	db.Model(&models.CaptureSOP{}).Count(&sopCount)
	db.Model(&models.SOPVersion{}).Count(&versionCount)
	db.Model(&models.SOPView{}).Count(&viewCount)
	if sopCount != 0 || versionCount != 0 || viewCount != 0 {
		t.Fatalf("unexpected writes: sop=%d version=%d view=%d", sopCount, versionCount, viewCount)
	}
}

func TestCreateCaptureSOPReturnsNotFoundForMissingCategory(t *testing.T) {
	db := newTestDB(t)
	_, manager := seedSOPCategoryAndUser(t, db)
	server, token := authenticatedSOPRouter(t, db, manager)
	defer server.Close()

	response := sopRequest(t, server, token, http.MethodPost, "/api/v1/capture-sops", `{"category_id":999999,"name":{"zh-CN":"不存在","en":"Missing"}}`)
	defer response.Body.Close()
	assertSOPAuthError(t, response, http.StatusNotFound, "category_not_found")

	var count int64
	if err := db.Model(&models.CaptureSOP{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("missing category created %d SOPs", count)
	}
}
