package models_test

import (
	"encoding/json"
	"strings"
	"testing"

	"cargoflow/api/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestVariantIdentityDefaults(t *testing.T) {
	version := models.VariantIdentityManifestVersion{}
	if err := version.BeforeCreate(nil); err != nil {
		t.Fatal(err)
	}
	if string(version.IdentityJSON) != `{}` {
		t.Fatalf("identity = %s", version.IdentityJSON)
	}
	if version.Status != models.VariantManifestDraft {
		t.Fatalf("status = %s", version.Status)
	}
	if version.DraftGuard == nil || *version.DraftGuard != "draft" {
		t.Fatalf("draft guard = %#v", version.DraftGuard)
	}
}

func TestModelFamilyDefaultsGenerateOpaquePublicIDs(t *testing.T) {
	family := models.ModelFamily{}
	member := models.ModelFamilyMember{}
	manifest := models.VariantIdentityManifest{}
	region := models.VariantDifferenceRegion{}
	sku := models.SKU{}
	asset := models.Asset{}
	for _, value := range []interface{ BeforeCreate(*gorm.DB) error }{&family, &member, &manifest, &region, &sku, &asset} {
		if err := value.BeforeCreate(nil); err != nil {
			t.Fatal(err)
		}
	}
	for name, publicID := range map[string]string{
		"family": family.PublicID, "member": member.PublicID, "manifest": manifest.PublicID,
		"region": region.PublicID, "sku": sku.PublicID, "asset": asset.PublicID,
	} {
		parsed, err := uuid.Parse(publicID)
		if err != nil || parsed == uuid.Nil {
			t.Fatalf("%s public ID = %q, err = %v", name, publicID, err)
		}
	}
	if string(family.CommonStructureJSON) != `{}` || string(family.VariationDimensionsJSON) != `[]` || family.Status != models.ModelFamilyActive {
		t.Fatalf("family defaults = %#v", family)
	}
	if member.ActiveGuard == nil || *member.ActiveGuard != "active" {
		t.Fatalf("member active guard = %#v", member.ActiveGuard)
	}
	if string(region.ShapeJSON) != `{}` || string(region.ForbiddenInheritanceJSON) != `[]` || string(region.RequiredViewKeysJSON) != `[]` {
		t.Fatalf("region defaults = %#v", region)
	}
}

func TestEveryNewPublicIDHookRejectsCallerSuppliedNonUUIDs(t *testing.T) {
	constructors := map[string]func() interface{ BeforeCreate(*gorm.DB) error }{
		"family": func() interface{ BeforeCreate(*gorm.DB) error } { return &models.ModelFamily{PublicID: "not-a-uuid"} },
		"member": func() interface{ BeforeCreate(*gorm.DB) error } {
			return &models.ModelFamilyMember{PublicID: "not-a-uuid"}
		},
		"manifest": func() interface{ BeforeCreate(*gorm.DB) error } {
			return &models.VariantIdentityManifest{PublicID: "not-a-uuid"}
		},
		"version": func() interface{ BeforeCreate(*gorm.DB) error } {
			return &models.VariantIdentityManifestVersion{PublicID: "not-a-uuid"}
		},
		"region": func() interface{ BeforeCreate(*gorm.DB) error } {
			return &models.VariantDifferenceRegion{PublicID: "not-a-uuid"}
		},
		"sku":   func() interface{ BeforeCreate(*gorm.DB) error } { return &models.SKU{PublicID: "not-a-uuid"} },
		"asset": func() interface{ BeforeCreate(*gorm.DB) error } { return &models.Asset{PublicID: "not-a-uuid"} },
	}
	for name, constructor := range constructors {
		t.Run(name, func(t *testing.T) {
			if err := constructor().BeforeCreate(nil); err == nil {
				t.Fatal("caller-supplied non-UUID public ID was accepted")
			}
		})
	}
}

func TestVariantDifferenceRegionSerializesNormalizedGeometryAndExactViewKeys(t *testing.T) {
	region := models.VariantDifferenceRegion{
		PublicID:                         "region-public-id",
		VariantIdentityManifestVersionID: 99,
		Key:                              "right_ports",
		DifferenceKind:                   models.DifferenceKindPorts,
		Strictness:                       models.DifferenceRegionExact,
		ShapeJSON:                        []byte(`{"kind":"rectangle","x":0.1,"y":0.2,"width":0.3,"height":0.4}`),
		ForbiddenInheritanceJSON:         []byte(`["ports","labels"]`),
		RequiredViewKeysJSON:             []byte(`["right_ports"]`),
	}
	if !strings.Contains(string(region.ShapeJSON), `"x":0.1`) || !strings.Contains(string(region.ShapeJSON), `"height":0.4`) {
		t.Fatalf("shape is not normalized geometry: %s", region.ShapeJSON)
	}
	if string(region.RequiredViewKeysJSON) != `["right_ports"]` {
		t.Fatalf("exact region required view keys = %s", region.RequiredViewKeysJSON)
	}
	encoded, err := json.Marshal(region)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"id"`, "variant_identity_manifest_version_id"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("serialized region leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestVariantIdentityModelsDoNotSerializeInternalIDsOrPublishedActor(t *testing.T) {
	publishedBy := uint(17)
	version := models.VariantIdentityManifestVersion{ID: 7, PublicID: "version-public-id", VariantIdentityManifestID: 8, PublishedByID: &publishedBy}
	encoded, err := json.Marshal(version)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"id"`, "variant_identity_manifest_id", "published_by_id"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("serialized version leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestVariantIdentityJSONFieldsSerializeAsJSONValues(t *testing.T) {
	values := []struct {
		name  string
		value any
		want  []string
	}{
		{name: "family", value: models.ModelFamily{CommonStructureJSON: []byte(`{"body":"shared"}`), VariationDimensionsJSON: []byte(`["color"]`)}, want: []string{`"common_structure":{"body":"shared"}`, `"variation_dimensions":["color"]`}},
		{name: "manifest version", value: models.VariantIdentityManifestVersion{IdentityJSON: []byte(`{"color":"blue"}`)}, want: []string{`"identity":{"color":"blue"}`}},
		{name: "difference region", value: models.VariantDifferenceRegion{ShapeJSON: []byte(`{"kind":"rectangle"}`), ForbiddenInheritanceJSON: []byte(`["ports"]`), RequiredViewKeysJSON: []byte(`["right_ports"]`)}, want: []string{`"shape":{"kind":"rectangle"}`, `"forbidden_inheritance":["ports"]`, `"required_view_keys":["right_ports"]`}},
	}
	for _, test := range values {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.value)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range test.want {
				if !strings.Contains(string(encoded), want) {
					t.Fatalf("serialized JSON = %s, want fragment %s", encoded, want)
				}
			}
		})
	}
}
