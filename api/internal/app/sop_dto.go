package app

import (
	"sort"
	"time"

	"cargoflows/api/internal/models"
	"cargoflows/api/internal/sop"
)

type localizedTextDTO struct {
	ZHCN string `json:"zh-CN"`
	EN   string `json:"en"`
}

type poseDTO struct {
	Space                   string      `json:"space"`
	CameraPositionDirection sop.Vector3 `json:"camera_position_direction"`
	ImageUpDirection        sop.Vector3 `json:"image_up_direction"`
	Target                  sop.Vector3 `json:"target"`
}

type coordinateSystemDTO struct {
	ID         string            `json:"id"`
	Handedness string            `json:"handedness"`
	Origin     string            `json:"origin"`
	Unit       string            `json:"unit"`
	Axes       map[string]string `json:"axes"`
}

type referenceImageDTO struct {
	PublicID     string           `json:"public_id"`
	ThumbnailURL string           `json:"thumbnail_url"`
	SortOrder    int              `json:"sort_order"`
	Caption      localizedTextDTO `json:"caption"`
	CreatedAt    time.Time        `json:"created_at"`
}

type sopViewDTO struct {
	PublicID        string              `json:"public_id"`
	Sequence        int                 `json:"sequence"`
	Role            models.SOPViewRole  `json:"role"`
	ViewKind        models.SOPViewKind  `json:"view_kind"`
	PresetKey       string              `json:"preset_key,omitempty"`
	Name            localizedTextDTO    `json:"name"`
	Instruction     localizedTextDTO    `json:"instruction"`
	Required        bool                `json:"required"`
	AllowMultiple   bool                `json:"allow_multiple"`
	Pose            poseDTO             `json:"pose"`
	Composition     models.Composition  `json:"composition"`
	ReferenceImages []referenceImageDTO `json:"reference_images"`
}

type sopVersionDTO struct {
	SchemaVersion    string                  `json:"schema_version"`
	PublicID         string                  `json:"public_id"`
	SOPPublicID      string                  `json:"sop_public_id"`
	VersionNumber    int                     `json:"version_number"`
	Status           models.SOPVersionStatus `json:"status"`
	Name             localizedTextDTO        `json:"name"`
	Description      localizedTextDTO        `json:"description"`
	CoordinateSystem coordinateSystemDTO     `json:"coordinate_system"`
	PublishedAt      *time.Time              `json:"published_at"`
	CreatedAt        time.Time               `json:"created_at"`
	UpdatedAt        time.Time               `json:"updated_at"`
	Views            []sopViewDTO            `json:"views"`
}

type captureSOPSummaryDTO struct {
	PublicID   string          `json:"public_id"`
	CategoryID uint            `json:"category_id"`
	Versions   []sopVersionDTO `json:"versions"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

func versionDTOFromModel(version models.SOPVersion, sopPublicID string) sopVersionDTO {
	views := append([]models.SOPView(nil), version.Views...)
	sort.SliceStable(views, func(i, j int) bool { return views[i].Sequence < views[j].Sequence })
	viewDTOs := make([]sopViewDTO, 0, len(views))
	for _, view := range views {
		images := append([]models.SOPViewReferenceImage(nil), view.ReferenceImages...)
		sort.SliceStable(images, func(i, j int) bool { return images[i].SortOrder < images[j].SortOrder })
		imageDTOs := make([]referenceImageDTO, 0, len(images))
		for _, image := range images {
			imageDTOs = append(imageDTOs, referenceImageDTO{PublicID: image.PublicID, ThumbnailURL: "/api/v1/sop-reference-images/" + image.PublicID + "/media", SortOrder: image.SortOrder, Caption: localizedTextDTO{ZHCN: image.CaptionZH, EN: image.CaptionEN}, CreatedAt: image.CreatedAt})
		}
		viewDTOs = append(viewDTOs, sopViewDTO{
			PublicID: view.PublicID, Sequence: view.Sequence, Role: view.Role, ViewKind: view.ViewKind, PresetKey: view.PresetKey,
			Name: localizedTextDTO{ZHCN: view.NameZH, EN: view.NameEN}, Instruction: localizedTextDTO{ZHCN: view.InstructionZH, EN: view.InstructionEN}, Required: view.Required, AllowMultiple: view.AllowMultiple,
			Pose:        poseDTO{Space: "object", CameraPositionDirection: sop.Vector3{view.CameraPositionX, view.CameraPositionY, view.CameraPositionZ}, ImageUpDirection: sop.Vector3{view.ImageUpX, view.ImageUpY, view.ImageUpZ}, Target: sop.Vector3{view.TargetX, view.TargetY, view.TargetZ}},
			Composition: view.Composition, ReferenceImages: imageDTOs,
		})
	}
	return sopVersionDTO{
		SchemaVersion: version.SchemaVersion, PublicID: version.PublicID, SOPPublicID: sopPublicID, VersionNumber: version.VersionNumber, Status: version.Status,
		Name: localizedTextDTO{ZHCN: version.NameZH, EN: version.NameEN}, Description: localizedTextDTO{ZHCN: version.DescriptionZH, EN: version.DescriptionEN},
		CoordinateSystem: coordinateSystemDTO{ID: "pcs_object_v1", Handedness: "right_handed", Origin: "bounding_box_center", Unit: "normalized", Axes: map[string]string{"x_positive": "object_top", "y_positive": "object_left", "z_positive": "object_front"}},
		PublishedAt:      version.PublishedAt, CreatedAt: version.CreatedAt, UpdatedAt: version.UpdatedAt, Views: viewDTOs,
	}
}

func summaryDTOFromModel(value models.CaptureSOP) captureSOPSummaryDTO {
	versions := append([]models.SOPVersion(nil), value.Versions...)
	sort.SliceStable(versions, func(i, j int) bool { return versions[i].VersionNumber < versions[j].VersionNumber })
	dtos := make([]sopVersionDTO, 0, len(versions))
	for _, version := range versions {
		dtos = append(dtos, versionDTOFromModel(version, value.PublicID))
	}
	return captureSOPSummaryDTO{PublicID: value.PublicID, CategoryID: value.CategoryID, Versions: dtos, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}
