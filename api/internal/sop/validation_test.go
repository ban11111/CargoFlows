package sop

import (
	"math"
	"testing"

	"cargoflow/api/internal/models"
)

func TestValidateVersionReportsAllErrors(t *testing.T) {
	version := models.SOPVersion{
		SchemaVersion: "1.0", CoordinateSystem: "pcs_object_v1", Status: models.SOPVersionDraft,
		NameZH: "", NameEN: "Example",
		Views: []models.SOPView{{
			Sequence: 2, Role: models.SOPViewCapture, ViewKind: models.SOPViewStandard,
			NameZH: "", NameEN: "Broken", Required: true,
			CameraPositionZ: 1, ImageUpZ: 1,
			TargetX:     0.2,
			Composition: models.Composition{FrameOccupancy: 1.2, AspectRatio: "1:1"},
		}},
	}
	errors := ValidateVersion(version)
	codes := validationCodes(errors)
	for _, code := range []string{"name_zh_required", "reference_front_count", "sequence_invalid", "pose_vectors_parallel", "standard_target_not_origin", "frame_occupancy_invalid"} {
		if !codes[code] {
			t.Errorf("missing %s in %#v", code, errors)
		}
	}
}

func TestValidateVersionAcceptsValidVersion(t *testing.T) {
	version := validVersion()
	if errors := ValidateVersion(version); len(errors) != 0 {
		t.Fatalf("valid version returned errors: %#v", errors)
	}
}

func TestValidateVersionRejectsUnsupportedSchemaAndCoordinateSystem(t *testing.T) {
	cases := []struct {
		name             string
		schemaVersion    string
		coordinateSystem string
	}{
		{name: "empty", schemaVersion: "", coordinateSystem: ""},
		{name: "wrong", schemaVersion: "2.0", coordinateSystem: "world_v1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			version := validVersion()
			version.SchemaVersion = tc.schemaVersion
			version.CoordinateSystem = tc.coordinateSystem

			errors := ValidateVersion(version)
			assertValidationError(t, errors, "schema_version_invalid", "schema_version")
			assertValidationError(t, errors, "coordinate_system_invalid", "coordinate_system.id")
		})
	}
}

func TestValidateVersionEnforcesReferenceFront(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*models.SOPView)
	}{
		{"sequence", func(view *models.SOPView) { view.Sequence = 2 }},
		{"kind", func(view *models.SOPView) { view.ViewKind = models.SOPViewDetail }},
		{"required", func(view *models.SOPView) { view.Required = false }},
		{"camera", func(view *models.SOPView) { view.CameraPositionZ = -1 }},
		{"image up", func(view *models.SOPView) { view.ImageUpX = -1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			version := validVersion()
			tc.mutate(&version.Views[0])
			if !validationCodes(ValidateVersion(version))["reference_front_invalid"] {
				t.Fatalf("missing reference_front_invalid")
			}
		})
	}
}

func TestValidateVersionChecksEveryViewField(t *testing.T) {
	version := validVersion()
	version.NameEN = " "
	version.Views = append(version.Views, models.SOPView{
		Sequence: 2, Role: models.SOPViewCapture, ViewKind: models.SOPViewDetail,
		NameZH: " ", NameEN: "",
		CameraPositionX: math.NaN(), ImageUpX: 1,
		TargetX:     0.6,
		Composition: models.Composition{FrameOccupancy: 0, AspectRatio: "2:0", AllowMirror: true},
	})

	errors := ValidateVersion(version)
	codes := validationCodes(errors)
	for _, code := range []string{
		"name_en_required", "view_name_zh_required", "view_name_en_required",
		"pose_vector_non_finite", "detail_target_out_of_bounds", "frame_occupancy_invalid",
		"aspect_ratio_invalid", "allow_mirror_invalid",
	} {
		if !codes[code] {
			t.Errorf("missing %s in %#v", code, errors)
		}
	}
}

func TestValidateVersionDetectsZeroPoseVectorAndDuplicateSequence(t *testing.T) {
	version := validVersion()
	version.Views = append(version.Views, models.SOPView{
		Sequence: 1, Role: models.SOPViewCapture, ViewKind: models.SOPViewStandard,
		NameZH: "背面", NameEN: "Back",
		ImageUpX:    1,
		Composition: models.Composition{FrameOccupancy: 1, AspectRatio: "4:5"},
	})

	errors := ValidateVersion(version)
	codes := validationCodes(errors)
	for _, code := range []string{"sequence_invalid", "pose_vector_zero"} {
		if !codes[code] {
			t.Errorf("missing %s in %#v", code, errors)
		}
	}
}

func TestValidateVersionRejectsPoseThatCanonicalizesToZero(t *testing.T) {
	version := validVersion()
	version.Views = append(version.Views, models.SOPView{
		Sequence: 2, Role: models.SOPViewCapture, ViewKind: models.SOPViewStandard,
		NameZH: "大数值", NameEN: "Large values",
		CameraPositionX: math.MaxFloat64, ImageUpY: 1,
		Composition: models.Composition{FrameOccupancy: 1, AspectRatio: "1:1"},
	})

	errors := ValidateVersion(version)
	if !validationCodes(errors)["pose_vector_invalid"] {
		t.Fatalf("missing pose_vector_invalid in %#v", errors)
	}
}

func TestValidationErrorsHaveStablePathAndLocalizedMessage(t *testing.T) {
	version := validVersion()
	version.Views[0].Composition.AllowMirror = true

	errors := ValidateVersion(version)
	for _, item := range errors {
		if item.Code == "allow_mirror_invalid" {
			if item.Path != "views[0].composition.allow_mirror" {
				t.Fatalf("path = %q", item.Path)
			}
			if item.Message.ZHCN == "" || item.Message.EN == "" {
				t.Fatalf("message is not localized: %#v", item.Message)
			}
			return
		}
	}
	t.Fatalf("missing allow_mirror_invalid in %#v", errors)
}

func validVersion() models.SOPVersion {
	return models.SOPVersion{
		SchemaVersion: "1.0", CoordinateSystem: "pcs_object_v1", Status: models.SOPVersionDraft,
		NameZH: "示例", NameEN: "Example",
		Views: []models.SOPView{{
			Sequence: 1, Role: models.SOPViewReferenceFront, ViewKind: models.SOPViewStandard,
			PresetKey: "reference_front", NameZH: "正面", NameEN: "Front", Required: true,
			CameraPositionZ: 1, ImageUpX: 1,
			Composition: models.Composition{FrameOccupancy: 0.85, AspectRatio: "1:1", AllowRotationCorrection: true},
		}},
	}
}

func validationCodes(errors []ValidationError) map[string]bool {
	codes := make(map[string]bool, len(errors))
	for _, item := range errors {
		codes[item.Code] = true
	}
	return codes
}

func assertValidationError(t *testing.T, errors []ValidationError, code, path string) {
	t.Helper()
	for _, item := range errors {
		if item.Code != code {
			continue
		}
		if item.Path != path {
			t.Fatalf("%s path = %q, want %q", code, item.Path, path)
		}
		if item.Message.ZHCN == "" || item.Message.EN == "" {
			t.Fatalf("%s message is not bilingual: %#v", code, item.Message)
		}
		return
	}
	t.Fatalf("missing %s in %#v", code, errors)
}
