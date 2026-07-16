package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cargoflow/api/internal/config"
	"cargoflow/api/internal/models"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func authenticatedSOPRouter(t *testing.T, db *gorm.DB, user models.User) (*httptest.Server, string) {
	t.Helper()
	cfg := config.Config{JWTSecret: "sop-handler-test-secret", MinIOEndpoint: "127.0.0.1:9000", MinIOPublicEndpoint: "127.0.0.1:9000", MinIOAccessKey: "test", MinIOSecretKey: "test", MinIOBucket: "test"}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": user.ID}).SignedString([]byte(cfg.JWTSecret))
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(NewRouter(cfg, db)), token
}

func sopRequest(t *testing.T, server *httptest.Server, token, method, path, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func TestCreateCaptureSOPReturnsReferenceVersionDocument(t *testing.T) {
	db := newTestDB(t)
	category, user := seedSOPCategoryAndUser(t, db)
	server, token := authenticatedSOPRouter(t, db, user)
	defer server.Close()

	response := sopRequest(t, server, token, http.MethodPost, "/api/v1/capture-sops", `{"category_id":`+jsonNumber(category.ID)+`,"name":{"zh-CN":"手机壳拍摄","en":"Phone Case Capture"}}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var document map[string]any
	if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(document["public_id"].(string)); err != nil {
		t.Fatalf("public_id is not UUID: %v", document["public_id"])
	}
	if document["version_number"] != float64(1) || document["status"] != "draft" {
		t.Fatalf("unexpected lifecycle: %#v", document)
	}
	coordinate := document["coordinate_system"].(map[string]any)
	if coordinate["id"] != "pcs_object_v1" || coordinate["handedness"] != "right_handed" {
		t.Fatalf("unexpected coordinate system: %#v", coordinate)
	}
	views := document["views"].([]any)
	view := views[0].(map[string]any)
	if len(views) != 1 || view["role"] != "reference_front" || view["required"] != true {
		t.Fatalf("unexpected reference front: %#v", views)
	}
}

func TestPublishReturnsAllValidationErrors(t *testing.T) {
	db := newTestDB(t)
	category, user := seedSOPCategoryAndUser(t, db)
	created := createTestSOP(t, NewSOPService(db), category, user)
	if err := db.Model(&models.SOPVersion{}).Where("id = ?", created.Version.ID).Updates(map[string]any{"name_zh": "", "name_en": ""}).Error; err != nil {
		t.Fatal(err)
	}
	server, token := authenticatedSOPRouter(t, db, user)
	defer server.Close()
	response := sopRequest(t, server, token, http.MethodPost, "/api/v1/sop-versions/"+created.Version.PublicID+"/publish", `{}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var validation struct {
		Code   string `json:"code"`
		Errors []struct {
			Code, Path string
			Message    map[string]string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(response.Body).Decode(&validation); err != nil {
		t.Fatal(err)
	}
	if validation.Code != "sop_validation_failed" || len(validation.Errors) < 2 {
		t.Fatalf("unexpected validation response: %#v", validation)
	}
	for _, item := range validation.Errors {
		if item.Code == "" || item.Path == "" || item.Message["zh-CN"] == "" || item.Message["en"] == "" {
			t.Fatalf("unstructured validation item: %#v", item)
		}
	}
}

func TestPublishedVersionRejectsPatch(t *testing.T) {
	db := newTestDB(t)
	category, user := seedSOPCategoryAndUser(t, db)
	service := NewSOPService(db)
	created := createTestSOP(t, service, category, user)
	if _, err := service.Publish(t.Context(), created.Version.PublicID); err != nil {
		t.Fatal(err)
	}
	server, token := authenticatedSOPRouter(t, db, user)
	defer server.Close()
	response := sopRequest(t, server, token, http.MethodPatch, "/api/v1/sop-versions/"+created.Version.PublicID, `{"name":{"zh-CN":"改名","en":"Renamed"},"description":{"zh-CN":"","en":""}}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var failure map[string]any
	if err := json.NewDecoder(response.Body).Decode(&failure); err != nil {
		t.Fatal(err)
	}
	if failure["code"] != "version_immutable" {
		t.Fatalf("failure = %#v", failure)
	}
}

func TestSOPMutationsRejectUnknownFieldsAndUnsafeReferenceKeys(t *testing.T) {
	db := newTestDB(t)
	category, user := seedSOPCategoryAndUser(t, db)
	created := createTestSOP(t, NewSOPService(db), category, user)
	server, token := authenticatedSOPRouter(t, db, user)
	defer server.Close()

	unknown := sopRequest(t, server, token, http.MethodPatch, "/api/v1/sop-versions/"+created.Version.PublicID, `{"name":{"zh-CN":"名称","en":"Name"},"description":{"zh-CN":"","en":""},"unexpected":true}`)
	defer unknown.Body.Close()
	if unknown.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown-field status = %d", unknown.StatusCode)
	}

	viewID := created.Version.Views[0].PublicID
	unsafe := sopRequest(t, server, token, http.MethodPost, "/api/v1/sop-versions/"+created.Version.PublicID+"/views/"+viewID+"/reference-images", `{"object_key":"sop-references/another-version/another-view/file.jpg","thumbnail_url":"/thumb.jpg","caption":{"zh-CN":"","en":""}}`)
	defer unsafe.Body.Close()
	if unsafe.StatusCode != http.StatusBadRequest {
		t.Fatalf("unsafe-key status = %d", unsafe.StatusCode)
	}
}

func TestUpdateViewPersistsKindAndRejectsReferenceRoleMutation(t *testing.T) {
	db := newTestDB(t)
	category, user := seedSOPCategoryAndUser(t, db)
	service := NewSOPService(db)
	created := createTestSOP(t, service, category, user)
	back, err := service.AddView(t.Context(), created.Version.PublicID, AddViewInput{PresetKey: "back"})
	if err != nil {
		t.Fatal(err)
	}
	server, token := authenticatedSOPRouter(t, db, user)
	defer server.Close()
	body := `{"role":"capture","view_kind":"detail","name":{"zh-CN":"细节","en":"Detail"},"instruction":{"zh-CN":"","en":""},"required":false,"pose":{"space":"object","camera_position_direction":[0,0,-1],"image_up_direction":[1,0,0],"target":[0.1,0,0]},"composition":{"frame_occupancy":0.8,"aspect_ratio":"1:1","allow_rotation_correction":true,"allow_mirror":false}}`
	response := sopRequest(t, server, token, http.MethodPatch, "/api/v1/sop-versions/"+created.Version.PublicID+"/views/"+back.PublicID, body)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	version, err := service.GetVersion(t.Context(), created.Version.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if version.Views[1].ViewKind != models.SOPViewDetail {
		t.Fatalf("kind = %q", version.Views[1].ViewKind)
	}
	lockedBody := `{"role":"capture","view_kind":"standard","name":{"zh-CN":"正面","en":"Front"},"instruction":{"zh-CN":"","en":""},"required":true,"pose":{"space":"object","camera_position_direction":[0,0,1],"image_up_direction":[1,0,0],"target":[0,0,0]},"composition":{"frame_occupancy":0.85,"aspect_ratio":"1:1","allow_rotation_correction":true,"allow_mirror":false}}`
	locked := sopRequest(t, server, token, http.MethodPatch, "/api/v1/sop-versions/"+created.Version.PublicID+"/views/"+created.Version.Views[0].PublicID, lockedBody)
	defer locked.Body.Close()
	if locked.StatusCode != http.StatusConflict {
		t.Fatalf("reference role mutation status = %d", locked.StatusCode)
	}
}

func TestListCaptureSOPsWithoutCategoryReturnsPublishedSOPs(t *testing.T) {
	db := newTestDB(t)
	category, user := seedSOPCategoryAndUser(t, db)
	service := NewSOPService(db)
	created := createTestSOP(t, service, category, user)
	if _, err := service.Publish(t.Context(), created.Version.PublicID); err != nil {
		t.Fatal(err)
	}
	server, token := authenticatedSOPRouter(t, db, user)
	defer server.Close()
	response := sopRequest(t, server, token, http.MethodGet, "/api/v1/capture-sops", "")
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var result struct {
		Data []captureSOPSummaryDTO `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Data) != 1 || result.Data[0].PublicID != created.SOP.PublicID {
		t.Fatalf("data = %#v", result.Data)
	}
}

func TestListCaptureSOPsRejectsExplicitZeroCategory(t *testing.T) {
	db := newTestDB(t)
	_, user := seedSOPCategoryAndUser(t, db)
	server, token := authenticatedSOPRouter(t, db, user)
	defer server.Close()
	response := sopRequest(t, server, token, http.MethodGet, "/api/v1/capture-sops?category_id=0", "")
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", response.StatusCode)
	}
}

func TestPublishedVersionRejectsAllReferenceImageMutations(t *testing.T) {
	db := newTestDB(t)
	category, user := seedSOPCategoryAndUser(t, db)
	service := NewSOPService(db)
	created := createTestSOP(t, service, category, user)
	view := created.Version.Views[0]
	prefix := "sop-references/" + created.Version.PublicID + "/" + view.PublicID + "/"
	image, err := service.AddReferenceImage(t.Context(), created.Version.PublicID, view.PublicID, ReferenceImageInput{ObjectKey: prefix + "existing.jpg", ThumbnailURL: "/existing.jpg"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Publish(t.Context(), created.Version.PublicID); err != nil {
		t.Fatal(err)
	}
	server, token := authenticatedSOPRouter(t, db, user)
	defer server.Close()

	cases := []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/sop-versions/" + created.Version.PublicID + "/views/" + view.PublicID + "/reference-images/upload-url", `{"file_name":"next.jpg","content_type":"image/jpeg"}`},
		{http.MethodPost, "/api/v1/sop-versions/" + created.Version.PublicID + "/views/" + view.PublicID + "/reference-images", `{"object_key":"` + prefix + `next.jpg","thumbnail_url":"/next.jpg","caption":{"zh-CN":"","en":""}}`},
		{http.MethodDelete, "/api/v1/sop-versions/" + created.Version.PublicID + "/views/" + view.PublicID + "/reference-images/" + image.PublicID, ""},
		{http.MethodPut, "/api/v1/sop-versions/" + created.Version.PublicID + "/views/" + view.PublicID + "/reference-image-order", `{"public_ids":["` + image.PublicID + `"]}`},
	}
	for _, tc := range cases {
		response := sopRequest(t, server, token, tc.method, tc.path, tc.body)
		response.Body.Close()
		if response.StatusCode != http.StatusConflict {
			t.Errorf("%s %s status = %d", tc.method, tc.path, response.StatusCode)
		}
	}
}

func TestVersionPatchRejectsMissingRequiredShapeWithoutMutation(t *testing.T) {
	cases := []struct{ name, body string }{
		{"null", `null`},
		{"empty object", `{}`},
		{"missing name", `{"description":{"zh-CN":"说明","en":"Description"}}`},
		{"missing description", `{"name":{"zh-CN":"名称","en":"Name"}}`},
		{"missing Chinese name", `{"name":{"en":"Name"},"description":{"zh-CN":"说明","en":"Description"}}`},
		{"missing English description", `{"name":{"zh-CN":"名称","en":"Name"},"description":{"zh-CN":"说明"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestDB(t)
			category, user := seedSOPCategoryAndUser(t, db)
			service := NewSOPService(db)
			created := createTestSOP(t, service, category, user)
			server, token := authenticatedSOPRouter(t, db, user)
			defer server.Close()

			response := sopRequest(t, server, token, http.MethodPatch, "/api/v1/sop-versions/"+created.Version.PublicID, tc.body)
			response.Body.Close()
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d", response.StatusCode)
			}
			persisted, err := service.GetVersion(t.Context(), created.Version.PublicID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.NameZH != created.Version.NameZH || persisted.NameEN != created.Version.NameEN || persisted.DescriptionZH != created.Version.DescriptionZH || persisted.DescriptionEN != created.Version.DescriptionEN {
				t.Fatalf("invalid patch mutated version: %#v", persisted)
			}
		})
	}
}

func TestMutationBodiesEnforceNestedRequiredFields(t *testing.T) {
	db := newTestDB(t)
	category, user := seedSOPCategoryAndUser(t, db)
	service := NewSOPService(db)
	created := createTestSOP(t, service, category, user)
	server, token := authenticatedSOPRouter(t, db, user)
	defer server.Close()
	versionPath := "/api/v1/sop-versions/" + created.Version.PublicID
	viewPath := versionPath + "/views/" + created.Version.Views[0].PublicID
	prefix := "sop-references/" + created.Version.PublicID + "/" + created.Version.Views[0].PublicID + "/"

	cases := []struct{ name, method, path, body string }{
		{"partial create description", http.MethodPost, "/api/v1/capture-sops", `{"category_id":` + jsonNumber(category.ID) + `,"name":{"zh-CN":"新建","en":"New"},"description":{"en":"Only English"}}`},
		{"custom view missing allow_mirror", http.MethodPost, versionPath + "/views", `{"custom":{"role":"capture","view_kind":"standard","name":{"zh-CN":"背面","en":"Back"},"instruction":{"zh-CN":"","en":""},"required":true,"pose":{"space":"object","camera_position_direction":[0,0,-1],"image_up_direction":[1,0,0],"target":[0,0,0]},"composition":{"frame_occupancy":0.8,"aspect_ratio":"1:1","allow_rotation_correction":true}}}`},
		{"view patch missing required", http.MethodPatch, viewPath, `{"role":"reference_front","view_kind":"standard","name":{"zh-CN":"正面","en":"Front"}}`},
		{"view reorder missing IDs", http.MethodPut, versionPath + "/view-order", `{}`},
		{"copy missing source", http.MethodPost, "/api/v1/capture-sops/" + created.SOP.PublicID + "/versions", `{}`},
		{"upload missing content type", http.MethodPost, viewPath + "/reference-images/upload-url", `{"file_name":"example.jpg"}`},
		{"reference missing thumbnail", http.MethodPost, viewPath + "/reference-images", `{"object_key":"` + prefix + `example.jpg","caption":{"zh-CN":"","en":""}}`},
		{"reference missing caption language", http.MethodPost, viewPath + "/reference-images", `{"object_key":"` + prefix + `example.jpg","thumbnail_url":"/thumb.jpg","caption":{"zh-CN":"示例"}}`},
		{"reference reorder missing IDs", http.MethodPut, viewPath + "/reference-image-order", `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := sopRequest(t, server, token, tc.method, tc.path, tc.body)
			response.Body.Close()
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d", response.StatusCode)
			}
		})
	}
	version, err := service.GetVersion(t.Context(), created.Version.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if len(version.Views) != 1 || len(version.Views[0].ReferenceImages) != 0 {
		t.Fatalf("invalid requests mutated aggregate: %#v", version)
	}
}

func TestCustomAndUpdateViewsRejectNonThreeElementVectorsWithoutMutation(t *testing.T) {
	lengths := []int{0, 2, 4}
	for _, endpoint := range []string{"create", "update"} {
		for _, field := range []string{"camera_position_direction", "image_up_direction", "target"} {
			for _, length := range lengths {
				name := endpoint + "/" + field + "/length-" + jsonNumber(uint(length))
				t.Run(name, func(t *testing.T) {
					db := newTestDB(t)
					category, user := seedSOPCategoryAndUser(t, db)
					service := NewSOPService(db)
					created := createTestSOP(t, service, category, user)
					back, err := service.AddView(t.Context(), created.Version.PublicID, AddViewInput{PresetKey: "back"})
					if err != nil {
						t.Fatal(err)
					}
					server, token := authenticatedSOPRouter(t, db, user)
					defer server.Close()

					view := validViewRequestMap()
					pose := view["pose"].(map[string]any)
					pose[field] = make([]float64, length)
					payload := any(view)
					path := "/api/v1/sop-versions/" + created.Version.PublicID + "/views/" + back.PublicID
					method := http.MethodPatch
					if endpoint == "create" {
						payload = map[string]any{"custom": view}
						path = "/api/v1/sop-versions/" + created.Version.PublicID + "/views"
						method = http.MethodPost
					}
					response := sopRequest(t, server, token, method, path, string(mustJSON(payload)))
					response.Body.Close()
					if response.StatusCode != http.StatusBadRequest {
						t.Fatalf("status = %d", response.StatusCode)
					}
					version, err := service.GetVersion(t.Context(), created.Version.PublicID)
					if err != nil {
						t.Fatal(err)
					}
					if len(version.Views) != 2 || version.Views[1].PublicID != back.PublicID || version.Views[1].NameEN != "Back" || version.Views[1].TargetX != 0 {
						t.Fatalf("invalid vector mutated aggregate: %#v", version.Views)
					}
				})
			}
		}
	}
}

func TestReferenceImageExplicitInvalidSortOrderDoesNotAppend(t *testing.T) {
	for _, sortOrder := range []int{0, -1} {
		t.Run(jsonNumber(uint(sortOrder+1)), func(t *testing.T) {
			db := newTestDB(t)
			category, user := seedSOPCategoryAndUser(t, db)
			service := NewSOPService(db)
			created := createTestSOP(t, service, category, user)
			view := created.Version.Views[0]
			server, token := authenticatedSOPRouter(t, db, user)
			defer server.Close()
			prefix := "sop-references/" + created.Version.PublicID + "/" + view.PublicID + "/"
			body := map[string]any{"object_key": prefix + "example.jpg", "thumbnail_url": "/example.jpg", "caption": map[string]any{"zh-CN": "", "en": ""}, "sort_order": sortOrder}
			response := sopRequest(t, server, token, http.MethodPost, "/api/v1/sop-versions/"+created.Version.PublicID+"/views/"+view.PublicID+"/reference-images", string(mustJSON(body)))
			response.Body.Close()
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d", response.StatusCode)
			}
			version, err := service.GetVersion(t.Context(), created.Version.PublicID)
			if err != nil {
				t.Fatal(err)
			}
			if len(version.Views[0].ReferenceImages) != 0 {
				t.Fatalf("invalid sort order appended image: %#v", version.Views[0].ReferenceImages)
			}
		})
	}
}

func TestCustomViewRejectsOutOfRangeFrameOccupancyWithoutMutation(t *testing.T) {
	for _, occupancy := range []float64{0, 1.1} {
		t.Run(string(mustJSON(occupancy)), func(t *testing.T) {
			db := newTestDB(t)
			category, user := seedSOPCategoryAndUser(t, db)
			service := NewSOPService(db)
			created := createTestSOP(t, service, category, user)
			server, token := authenticatedSOPRouter(t, db, user)
			defer server.Close()
			view := validViewRequestMap()
			view["composition"].(map[string]any)["frame_occupancy"] = occupancy
			response := sopRequest(t, server, token, http.MethodPost, "/api/v1/sop-versions/"+created.Version.PublicID+"/views", string(mustJSON(map[string]any{"custom": view})))
			response.Body.Close()
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d", response.StatusCode)
			}
			version, err := service.GetVersion(t.Context(), created.Version.PublicID)
			if err != nil {
				t.Fatal(err)
			}
			if len(version.Views) != 1 {
				t.Fatalf("invalid occupancy added view: %#v", version.Views)
			}
		})
	}
}

func validViewRequestMap() map[string]any {
	return map[string]any{
		"role": "capture", "view_kind": "detail", "name": map[string]any{"zh-CN": "细节", "en": "Detail"},
		"instruction": map[string]any{"zh-CN": "", "en": ""}, "required": false,
		"pose":        map[string]any{"space": "object", "camera_position_direction": []float64{0, 0, 1}, "image_up_direction": []float64{1, 0, 0}, "target": []float64{0, 0, 0}},
		"composition": map[string]any{"frame_occupancy": .8, "aspect_ratio": "1:1", "allow_rotation_correction": true, "allow_mirror": false},
	}
}

func TestThreeElementVectorsRoundTripAndOmittedSortOrderAppends(t *testing.T) {
	db := newTestDB(t)
	category, user := seedSOPCategoryAndUser(t, db)
	service := NewSOPService(db)
	created := createTestSOP(t, service, category, user)
	server, token := authenticatedSOPRouter(t, db, user)
	defer server.Close()
	viewRequest := validViewRequestMap()
	viewRequest["pose"].(map[string]any)["target"] = []float64{.1, .2, .3}
	createdView := sopRequest(t, server, token, http.MethodPost, "/api/v1/sop-versions/"+created.Version.PublicID+"/views", string(mustJSON(map[string]any{"custom": viewRequest})))
	createdView.Body.Close()
	if createdView.StatusCode != http.StatusCreated {
		t.Fatalf("view status = %d", createdView.StatusCode)
	}
	version, err := service.GetVersion(t.Context(), created.Version.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if len(version.Views) != 2 || version.Views[1].TargetX != .1 || version.Views[1].TargetY != .2 || version.Views[1].TargetZ != .3 {
		t.Fatalf("vectors did not round trip: %#v", version.Views)
	}

	view := version.Views[1]
	prefix := "sop-references/" + created.Version.PublicID + "/" + view.PublicID + "/"
	imageBody := map[string]any{"object_key": prefix + "example.jpg", "thumbnail_url": "/example.jpg", "caption": map[string]any{"zh-CN": "", "en": ""}}
	createdImage := sopRequest(t, server, token, http.MethodPost, "/api/v1/sop-versions/"+created.Version.PublicID+"/views/"+view.PublicID+"/reference-images", string(mustJSON(imageBody)))
	createdImage.Body.Close()
	if createdImage.StatusCode != http.StatusCreated {
		t.Fatalf("image status = %d", createdImage.StatusCode)
	}
	version, err = service.GetVersion(t.Context(), created.Version.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if len(version.Views[1].ReferenceImages) != 1 || version.Views[1].ReferenceImages[0].SortOrder != 1 {
		t.Fatalf("omitted sort_order did not append: %#v", version.Views[1].ReferenceImages)
	}
}

func jsonNumber(value uint) string {
	return strings.TrimSpace(string(mustJSON(value)))
}

func mustJSON(value any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}
