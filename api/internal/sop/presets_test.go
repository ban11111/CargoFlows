package sop

import (
	"testing"

	"cargoflows/api/internal/models"
)

func TestPresetCatalog(t *testing.T) {
	cases := []struct {
		key      string
		camera   Vector3
		up       Vector3
		required bool
	}{
		{"reference_front", Vector3{0, 0, 1}, Vector3{1, 0, 0}, true},
		{"back", Vector3{0, 0, -1}, Vector3{1, 0, 0}, true},
		{"left", Vector3{0, 1, 0}, Vector3{1, 0, 0}, true},
		{"bottom", Vector3{-1, 0, 0}, Vector3{0, 1, 0}, true},
		{"right", Vector3{0, -1, 0}, Vector3{-1, 0, 0}, true},
		{"top", Vector3{1, 0, 0}, Vector3{0, -1, 0}, true},
		{"detail_label", Vector3{0, 0, 1}, Vector3{1, 0, 0}, false},
		{"packaging_front", Vector3{0, 0, 1}, Vector3{1, 0, 0}, false},
		{"supplemental_info", Vector3{0, 0, 1}, Vector3{1, 0, 0}, false},
	}
	for _, tc := range cases {
		got, ok := PresetByKey(tc.key)
		if !ok {
			t.Fatalf("missing preset %s", tc.key)
		}
		if got.CameraPosition != tc.camera || got.ImageUp != tc.up || got.Required != tc.required {
			t.Fatalf("preset %s = %#v", tc.key, got)
		}
	}

	packaging, _ := PresetByKey("packaging_front")
	if packaging.NameZH != "包装正面" || packaging.NameEN != "Packaging Front" || packaging.Kind != models.SOPViewStandard {
		t.Fatalf("invalid packaging preset: %#v", packaging)
	}
	supplemental, _ := PresetByKey("supplemental_info")
	if supplemental.NameZH != "补充信息图片" || supplemental.NameEN != "Supplemental Product Information" || supplemental.Kind != models.SOPViewDetail || !supplemental.AllowMultiple || supplemental.Required {
		t.Fatalf("invalid supplemental preset: %#v", supplemental)
	}
}

func TestPresetByKeyReturnsIndependentValues(t *testing.T) {
	first, ok := PresetByKey("packaging_front")
	if !ok {
		t.Fatal("missing packaging_front preset")
	}
	first.NameEN = "Changed"
	first.Composition.FrameOccupancy = 0.1

	second, _ := PresetByKey("packaging_front")
	if second.NameEN != "Packaging Front" || second.Composition.FrameOccupancy != 0.85 {
		t.Fatalf("preset catalog was mutated: %#v", second)
	}
	if _, ok := PresetByKey("missing"); ok {
		t.Fatal("unknown preset unexpectedly found")
	}
}
