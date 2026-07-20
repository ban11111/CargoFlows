package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"cargoflow/api/internal/models"
	"cargoflow/api/internal/sop"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrVersionImmutable          = errors.New("SOP version is immutable")
	ErrDraftExists               = errors.New("an SOP draft already exists")
	ErrReferenceLocked           = errors.New("reference-front view is locked")
	ErrVersionNotFound           = errors.New("SOP version not found")
	ErrCaptureSOPNotFound        = errors.New("capture SOP not found")
	ErrCategoryNotFound          = errors.New("category not found")
	ErrSourceVersionNotPublished = errors.New("source SOP version is not published")
	ErrStaleSOPVersion           = errors.New("SOP version was changed by another editor")
)

type sopRevisionContextKey struct{}

func withSOPRevision(ctx context.Context, revision time.Time) context.Context {
	return context.WithValue(ctx, sopRevisionContextKey{}, revision)
}

type SOPValidationError struct {
	Errors []sop.ValidationError
}

func (e *SOPValidationError) Error() string { return "SOP validation failed" }

type SOPService struct {
	db *gorm.DB
}

func NewSOPService(db *gorm.DB) *SOPService { return &SOPService{db: db} }

type CreateSOPInput struct {
	CategoryID, CreatedByID                      uint
	NameZH, NameEN, DescriptionZH, DescriptionEN string
}

type UpdateVersionInput struct {
	NameZH, NameEN, DescriptionZH, DescriptionEN string
}

type AddViewInput struct {
	PresetKey string
	Custom    *sop.ViewInput
}

type UpdateViewInput struct {
	Role                                         models.SOPViewRole
	ViewKind                                     models.SOPViewKind
	NameZH, NameEN, InstructionZH, InstructionEN string
	Required                                     bool
	AllowMultiple                                bool
	CameraPosition, ImageUp, Target              sop.Vector3
	Composition                                  models.Composition
}

type ReferenceImageInput struct {
	PublicID, ObjectKey, ThumbnailURL, CaptionZH, CaptionEN string
	SortOrder                                               int
}

type CreatedSOP struct {
	SOP     models.CaptureSOP
	Version models.SOPVersion
}

func (s *SOPService) Create(ctx context.Context, input CreateSOPInput) (*CreatedSOP, error) {
	var created CreatedSOP
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var category models.Category
		if err := tx.Select("id").First(&category, input.CategoryID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrCategoryNotFound
		} else if err != nil {
			return err
		}
		created.SOP = models.CaptureSOP{
			PublicID: uuid.NewString(), CategoryID: input.CategoryID, CreatedByID: input.CreatedByID,
		}
		if err := tx.Create(&created.SOP).Error; err != nil {
			return err
		}
		created.Version = models.SOPVersion{
			PublicID: uuid.NewString(), CaptureSOPID: created.SOP.ID, VersionNumber: 1,
			SchemaVersion: "1.0", NameZH: input.NameZH, NameEN: input.NameEN,
			DescriptionZH: input.DescriptionZH, DescriptionEN: input.DescriptionEN,
			Status: models.SOPVersionDraft, CoordinateSystem: "pcs_object_v1",
		}
		if err := tx.Create(&created.Version).Error; err != nil {
			return err
		}
		preset, ok := sop.PresetByKey("reference_front")
		if !ok {
			return errors.New("reference-front preset is unavailable")
		}
		view, err := newViewFromInput(created.Version.ID, 1, "reference_front", preset)
		if err != nil {
			return err
		}
		if err := tx.Create(&view).Error; err != nil {
			return err
		}
		created.Version.Views = []models.SOPView{view}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &created, nil
}

func (s *SOPService) List(ctx context.Context, categoryID uint, includeAll bool) ([]models.CaptureSOP, error) {
	var result []models.CaptureSOP
	db := s.db.WithContext(ctx).Model(&models.CaptureSOP{})
	if includeAll {
		db = db.Preload("Versions", func(db *gorm.DB) *gorm.DB { return db.Order("version_number ASC") })
	} else {
		db = db.
			Joins("JOIN sop_versions selectable_versions ON selectable_versions.capture_sop_id = capture_sops.id AND selectable_versions.status = ?", models.SOPVersionPublished).
			Distinct("capture_sops.*").
			Preload("Versions", "status = ?", models.SOPVersionPublished)
	}
	db = db.
		Preload("Versions.Views", func(db *gorm.DB) *gorm.DB { return db.Order("sequence ASC") }).
		Preload("Versions.Views.ReferenceImages", func(db *gorm.DB) *gorm.DB { return db.Order("sort_order ASC") }).
		Order("capture_sops.id ASC")
	if categoryID != 0 {
		db = db.Where("capture_sops.category_id = ?", categoryID)
	}
	if err := db.Find(&result).Error; err != nil {
		return nil, err
	}
	return result, nil
}

func (s *SOPService) Get(ctx context.Context, publicID string) (*models.CaptureSOP, error) {
	var result models.CaptureSOP
	err := s.db.WithContext(ctx).
		Preload("Versions", func(db *gorm.DB) *gorm.DB { return db.Order("version_number ASC") }).
		Preload("Versions.Views", func(db *gorm.DB) *gorm.DB { return db.Order("sequence ASC") }).
		Preload("Versions.Views.ReferenceImages", func(db *gorm.DB) *gorm.DB { return db.Order("sort_order ASC") }).
		Where("public_id = ?", publicID).First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCaptureSOPNotFound
	}
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *SOPService) GetVersion(ctx context.Context, publicID string) (*models.SOPVersion, error) {
	return getVersion(s.db.WithContext(ctx), publicID)
}

func (s *SOPService) RequireDraftView(ctx context.Context, versionPublicID, viewPublicID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, _, err := draftVersionAndView(tx, versionPublicID, viewPublicID)
		return err
	})
}

func (s *SOPService) UpdateVersion(ctx context.Context, publicID string, input UpdateVersionInput) (*models.SOPVersion, error) {
	var version *models.SOPVersion
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		version, err = getVersionRecord(tx, publicID)
		if err != nil {
			return err
		}
		if err := requireDraft(*version); err != nil {
			return err
		}
		if err := requireSOPRevision(ctx, *version); err != nil {
			return err
		}
		updates := map[string]any{
			"name_zh": input.NameZH, "name_en": input.NameEN,
			"description_zh": input.DescriptionZH, "description_en": input.DescriptionEN,
			"updated_at": nextSOPRevision(version.UpdatedAt),
		}
		if err := tx.Model(version).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(version, version.ID).Error
	})
	return version, err
}

func (s *SOPService) AddView(ctx context.Context, versionPublicID string, input AddViewInput) (*models.SOPView, error) {
	var added models.SOPView
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		version, err := getVersionRecord(tx, versionPublicID)
		if err != nil {
			return err
		}
		if err := requireDraft(*version); err != nil {
			return err
		}
		if err := requireSOPRevision(ctx, *version); err != nil {
			return err
		}
		viewInput, presetKey, err := resolveViewInput(input)
		if err != nil {
			return err
		}
		if viewInput.Role == models.SOPViewReferenceFront {
			return ErrReferenceLocked
		}
		var sequence int
		if err := tx.Model(&models.SOPView{}).Where("sop_version_id = ?", version.ID).Select("COALESCE(MAX(sequence), 0)").Scan(&sequence).Error; err != nil {
			return err
		}
		added, err = newViewFromInput(version.ID, sequence+1, presetKey, viewInput)
		if err != nil {
			return err
		}
		if err := tx.Create(&added).Error; err != nil {
			return err
		}
		return touchSOPVersion(tx, version)
	})
	if err != nil {
		return nil, err
	}
	return &added, nil
}

func (s *SOPService) UpdateView(ctx context.Context, versionPublicID, viewPublicID string, input UpdateViewInput) (*models.SOPView, error) {
	var view models.SOPView
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		version, err := getVersionRecord(tx, versionPublicID)
		if err != nil {
			return err
		}
		if err := requireDraft(*version); err != nil {
			return err
		}
		if err := requireSOPRevision(ctx, *version); err != nil {
			return err
		}
		if err := tx.Where("sop_version_id = ? AND public_id = ?", version.ID, viewPublicID).First(&view).Error; err != nil {
			return err
		}
		role, kind := input.Role, input.ViewKind
		if role == "" {
			role = view.Role
		}
		if kind == "" {
			kind = view.ViewKind
		}
		if view.Role == models.SOPViewReferenceFront && (role != models.SOPViewReferenceFront || kind != models.SOPViewStandard) {
			return ErrReferenceLocked
		}
		if view.Role != models.SOPViewReferenceFront && role != models.SOPViewCapture {
			return errors.New("capture view role must remain capture")
		}
		if kind != models.SOPViewStandard && kind != models.SOPViewDetail {
			return errors.New("view_kind must be standard or detail")
		}
		pose, err := sop.CanonicalizePose(input.CameraPosition, input.ImageUp)
		if err != nil {
			if view.Role == models.SOPViewReferenceFront {
				return ErrReferenceLocked
			}
			return err
		}
		if view.Role == models.SOPViewReferenceFront &&
			(!input.Required || pose.CameraPosition != (sop.Vector3{0, 0, 1}) || pose.ImageUp != (sop.Vector3{1, 0, 0}) || input.Target != (sop.Vector3{})) {
			return ErrReferenceLocked
		}
		updates := viewUpdateMap(input, pose)
		updates["role"] = role
		updates["view_kind"] = kind
		compositionJSON, err := json.Marshal(input.Composition)
		if err != nil {
			return err
		}
		updates["composition"] = string(compositionJSON)
		if err := tx.Model(&view).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.First(&view, view.ID).Error; err != nil {
			return err
		}
		return touchSOPVersion(tx, version)
	})
	if err != nil {
		return nil, err
	}
	return &view, nil
}

func (s *SOPService) DeleteView(ctx context.Context, versionPublicID, viewPublicID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		version, err := getVersionRecord(tx, versionPublicID)
		if err != nil {
			return err
		}
		if err := requireDraft(*version); err != nil {
			return err
		}
		if err := requireSOPRevision(ctx, *version); err != nil {
			return err
		}
		var view models.SOPView
		if err := tx.Where("sop_version_id = ? AND public_id = ?", version.ID, viewPublicID).First(&view).Error; err != nil {
			return err
		}
		if view.Role == models.SOPViewReferenceFront {
			return ErrReferenceLocked
		}
		if err := tx.Where("sop_view_id = ?", view.ID).Delete(&models.SOPViewReferenceImage{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&view).Error; err != nil {
			return err
		}
		var remaining []models.SOPView
		if err := tx.Where("sop_version_id = ?", version.ID).Order("sequence ASC").Find(&remaining).Error; err != nil {
			return err
		}
		if err := applyViewOrder(tx, version.ID, remaining); err != nil {
			return err
		}
		return touchSOPVersion(tx, version)
	})
}

func (s *SOPService) Reorder(ctx context.Context, versionPublicID string, orderedViewIDs []string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		version, err := getVersionRecord(tx, versionPublicID)
		if err != nil {
			return err
		}
		if err := requireDraft(*version); err != nil {
			return err
		}
		if err := requireSOPRevision(ctx, *version); err != nil {
			return err
		}
		var views []models.SOPView
		if err := tx.Where("sop_version_id = ?", version.ID).Find(&views).Error; err != nil {
			return err
		}
		ordered, err := orderViews(views, orderedViewIDs)
		if err != nil {
			return err
		}
		if len(ordered) == 0 || ordered[0].Role != models.SOPViewReferenceFront {
			return ErrReferenceLocked
		}
		if err := applyViewOrder(tx, version.ID, ordered); err != nil {
			return err
		}
		return touchSOPVersion(tx, version)
	})
}

func (s *SOPService) Validate(ctx context.Context, versionPublicID string) ([]sop.ValidationError, error) {
	version, err := getVersion(s.db.WithContext(ctx), versionPublicID)
	if err != nil {
		return nil, err
	}
	return sop.ValidateVersion(*version), nil
}

func (s *SOPService) Publish(ctx context.Context, versionPublicID string) (*models.SOPVersion, error) {
	var published *models.SOPVersion
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		version, err := getVersion(tx.Clauses(clause.Locking{Strength: "UPDATE"}), versionPublicID)
		if err != nil {
			return err
		}
		if err := requireDraft(*version); err != nil {
			return err
		}
		if err := requireSOPRevision(ctx, *version); err != nil {
			return err
		}
		if validationErrors := sop.ValidateVersion(*version); len(validationErrors) != 0 {
			return &SOPValidationError{Errors: validationErrors}
		}
		now := time.Now()
		revision := nextSOPRevision(version.UpdatedAt)
		if err := tx.Model(version).Updates(map[string]any{"status": models.SOPVersionPublished, "published_at": &now, "updated_at": revision}).Error; err != nil {
			return err
		}
		version.Status = models.SOPVersionPublished
		version.PublishedAt = &now
		version.UpdatedAt = revision
		published = version
		return nil
	})
	return published, err
}

func (s *SOPService) CopyVersion(ctx context.Context, sopPublicID, sourceVersionPublicID string) (*models.SOPVersion, error) {
	var copied *models.SOPVersion
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var parent models.CaptureSOP
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("public_id = ?", sopPublicID).First(&parent).Error; err != nil {
			return err
		}
		source, err := getVersion(tx.Clauses(clause.Locking{Strength: "UPDATE"}), sourceVersionPublicID)
		if err != nil {
			return err
		}
		if source.CaptureSOPID != parent.ID {
			return ErrVersionNotFound
		}
		if source.Status != models.SOPVersionPublished {
			return ErrSourceVersionNotPublished
		}
		var draftCount int64
		if err := tx.Model(&models.SOPVersion{}).Where("capture_sop_id = ? AND status = ?", parent.ID, models.SOPVersionDraft).Count(&draftCount).Error; err != nil {
			return err
		}
		if draftCount != 0 {
			return ErrDraftExists
		}
		var maxVersion int
		if err := tx.Model(&models.SOPVersion{}).Where("capture_sop_id = ?", parent.ID).Select("COALESCE(MAX(version_number), 0)").Scan(&maxVersion).Error; err != nil {
			return err
		}
		copyRecord := models.SOPVersion{
			PublicID: uuid.NewString(), CaptureSOPID: parent.ID, VersionNumber: maxVersion + 1,
			SchemaVersion: source.SchemaVersion, NameZH: source.NameZH, NameEN: source.NameEN,
			DescriptionZH: source.DescriptionZH, DescriptionEN: source.DescriptionEN,
			Status: models.SOPVersionDraft, CoordinateSystem: source.CoordinateSystem,
			CopiedFromVersionID: &source.ID,
		}
		if err := tx.Create(&copyRecord).Error; err != nil {
			return err
		}
		for _, sourceView := range source.Views {
			view := cloneView(sourceView, copyRecord.ID)
			if err := tx.Create(&view).Error; err != nil {
				return err
			}
			for _, sourceImage := range sourceView.ReferenceImages {
				image := models.SOPViewReferenceImage{
					PublicID: uuid.NewString(), SOPViewID: view.ID, ObjectKey: sourceImage.ObjectKey,
					ThumbnailURL: sourceImage.ThumbnailURL, SortOrder: sourceImage.SortOrder,
					CaptionZH: sourceImage.CaptionZH, CaptionEN: sourceImage.CaptionEN,
				}
				if err := tx.Create(&image).Error; err != nil {
					return err
				}
			}
		}
		copied, err = getVersion(tx, copyRecord.PublicID)
		return err
	})
	return copied, err
}

func (s *SOPService) Archive(ctx context.Context, versionPublicID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		version, err := getVersionRecord(tx, versionPublicID)
		if err != nil {
			return err
		}
		if version.Status != models.SOPVersionPublished {
			return ErrVersionImmutable
		}
		return tx.Model(version).Update("status", models.SOPVersionArchived).Error
	})
}

func (s *SOPService) AddReferenceImage(ctx context.Context, versionPublicID, viewPublicID string, input ReferenceImageInput) (*models.SOPViewReferenceImage, error) {
	var added models.SOPViewReferenceImage
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		version, view, err := draftVersionAndView(tx, versionPublicID, viewPublicID)
		if err != nil {
			return err
		}
		if err := requireSOPRevision(ctx, *version); err != nil {
			return err
		}
		var images []models.SOPViewReferenceImage
		if err := tx.Where("sop_view_id = ?", view.ID).Order("sort_order ASC").Find(&images).Error; err != nil {
			return err
		}
		position := input.SortOrder
		if position == 0 {
			position = len(images) + 1
		}
		if position < 1 || position > len(images)+1 {
			return fmt.Errorf("reference image sort order must be between 1 and %d", len(images)+1)
		}
		if len(images) != 0 {
			if err := offsetReferenceImageOrder(tx, view.ID); err != nil {
				return err
			}
			for index := range images {
				order := index + 1
				if order >= position {
					order++
				}
				if err := tx.Model(&models.SOPViewReferenceImage{}).Where("id = ?", images[index].ID).Update("sort_order", order).Error; err != nil {
					return err
				}
			}
		}
		added = models.SOPViewReferenceImage{
			PublicID: input.PublicID, SOPViewID: view.ID, ObjectKey: input.ObjectKey,
			ThumbnailURL: input.ThumbnailURL, SortOrder: position, CaptionZH: input.CaptionZH, CaptionEN: input.CaptionEN,
		}
		if added.PublicID == "" {
			added.PublicID = uuid.NewString()
		}
		if err := tx.Create(&added).Error; err != nil {
			return err
		}
		return touchSOPVersion(tx, version)
	})
	if err != nil {
		return nil, err
	}
	return &added, nil
}

func (s *SOPService) DeleteReferenceImage(ctx context.Context, versionPublicID, viewPublicID, imagePublicID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		version, view, err := draftVersionAndView(tx, versionPublicID, viewPublicID)
		if err != nil {
			return err
		}
		if err := requireSOPRevision(ctx, *version); err != nil {
			return err
		}
		var image models.SOPViewReferenceImage
		if err := tx.Where("sop_view_id = ? AND public_id = ?", view.ID, imagePublicID).First(&image).Error; err != nil {
			return err
		}
		if err := tx.Delete(&image).Error; err != nil {
			return err
		}
		var remaining []models.SOPViewReferenceImage
		if err := tx.Where("sop_view_id = ?", view.ID).Order("sort_order ASC").Find(&remaining).Error; err != nil {
			return err
		}
		if err := applyReferenceImageOrder(tx, view.ID, remaining); err != nil {
			return err
		}
		return touchSOPVersion(tx, version)
	})
}

func (s *SOPService) ReorderReferenceImages(ctx context.Context, versionPublicID, viewPublicID string, orderedImageIDs []string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		version, view, err := draftVersionAndView(tx, versionPublicID, viewPublicID)
		if err != nil {
			return err
		}
		if err := requireSOPRevision(ctx, *version); err != nil {
			return err
		}
		var images []models.SOPViewReferenceImage
		if err := tx.Where("sop_view_id = ?", view.ID).Find(&images).Error; err != nil {
			return err
		}
		ordered, err := orderReferenceImages(images, orderedImageIDs)
		if err != nil {
			return err
		}
		if err := applyReferenceImageOrder(tx, view.ID, ordered); err != nil {
			return err
		}
		return touchSOPVersion(tx, version)
	})
}

func getVersion(db *gorm.DB, publicID string) (*models.SOPVersion, error) {
	var version models.SOPVersion
	err := db.Preload("Views", func(db *gorm.DB) *gorm.DB { return db.Order("sequence ASC") }).
		Preload("Views.ReferenceImages", func(db *gorm.DB) *gorm.DB { return db.Order("sort_order ASC") }).
		Where("public_id = ?", publicID).First(&version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrVersionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &version, nil
}

func getVersionRecord(db *gorm.DB, publicID string) (*models.SOPVersion, error) {
	var version models.SOPVersion
	err := db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("public_id = ?", publicID).First(&version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrVersionNotFound
	}
	return &version, err
}

func requireDraft(version models.SOPVersion) error {
	if version.Status != models.SOPVersionDraft {
		return ErrVersionImmutable
	}
	return nil
}

func requireSOPRevision(ctx context.Context, version models.SOPVersion) error {
	expected, ok := ctx.Value(sopRevisionContextKey{}).(time.Time)
	if ok && !version.UpdatedAt.Equal(expected) {
		return ErrStaleSOPVersion
	}
	return nil
}

func nextSOPRevision(previous time.Time) time.Time {
	next := time.Now().UTC()
	// MySQL timestamps are stored with millisecond precision in this project.
	// Advancing by at least one millisecond keeps the token distinct even for
	// two mutations handled inside the same clock tick.
	minimum := previous.Add(time.Millisecond)
	if next.Before(minimum) {
		return minimum
	}
	return next
}

func touchSOPVersion(tx *gorm.DB, version *models.SOPVersion) error {
	next := nextSOPRevision(version.UpdatedAt)
	if err := tx.Model(version).UpdateColumn("updated_at", next).Error; err != nil {
		return err
	}
	version.UpdatedAt = next
	return nil
}

func resolveViewInput(input AddViewInput) (sop.ViewInput, string, error) {
	if input.PresetKey != "" && input.Custom != nil {
		return sop.ViewInput{}, "", errors.New("provide either a preset or a custom view")
	}
	if input.PresetKey != "" {
		preset, ok := sop.PresetByKey(input.PresetKey)
		if !ok {
			return sop.ViewInput{}, "", fmt.Errorf("unknown SOP preset %q", input.PresetKey)
		}
		return preset, input.PresetKey, nil
	}
	if input.Custom == nil {
		return sop.ViewInput{}, "", errors.New("a preset or custom view is required")
	}
	return *input.Custom, "", nil
}

func newViewFromInput(versionID uint, sequence int, presetKey string, input sop.ViewInput) (models.SOPView, error) {
	pose, err := sop.CanonicalizePose(input.CameraPosition, input.ImageUp)
	if err != nil {
		return models.SOPView{}, err
	}
	return models.SOPView{
		PublicID: uuid.NewString(), SOPVersionID: versionID, Sequence: sequence,
		Role: input.Role, ViewKind: input.Kind, PresetKey: presetKey,
		NameZH: input.NameZH, NameEN: input.NameEN, InstructionZH: input.InstructionZH, InstructionEN: input.InstructionEN,
		Required: input.Required, AllowMultiple: input.AllowMultiple,
		CameraPositionX: pose.CameraPosition[0], CameraPositionY: pose.CameraPosition[1], CameraPositionZ: pose.CameraPosition[2],
		ImageUpX: pose.ImageUp[0], ImageUpY: pose.ImageUp[1], ImageUpZ: pose.ImageUp[2],
		TargetX: input.Target[0], TargetY: input.Target[1], TargetZ: input.Target[2], Composition: input.Composition,
	}, nil
}

func viewUpdateMap(input UpdateViewInput, pose sop.CanonicalPose) map[string]any {
	return map[string]any{
		"name_zh": input.NameZH, "name_en": input.NameEN,
		"instruction_zh": input.InstructionZH, "instruction_en": input.InstructionEN, "required": input.Required, "allow_multiple": input.AllowMultiple,
		"camera_position_x": pose.CameraPosition[0], "camera_position_y": pose.CameraPosition[1], "camera_position_z": pose.CameraPosition[2],
		"image_up_x": pose.ImageUp[0], "image_up_y": pose.ImageUp[1], "image_up_z": pose.ImageUp[2],
		"target_x": input.Target[0], "target_y": input.Target[1], "target_z": input.Target[2],
	}
}

func orderViews(views []models.SOPView, orderedIDs []string) ([]models.SOPView, error) {
	if len(orderedIDs) != len(views) {
		return nil, errors.New("view order must contain every view exactly once")
	}
	byID := make(map[string]models.SOPView, len(views))
	for _, view := range views {
		byID[view.PublicID] = view
	}
	ordered := make([]models.SOPView, 0, len(views))
	seen := make(map[string]struct{}, len(views))
	for _, id := range orderedIDs {
		view, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("view %q does not belong to this version", id)
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, fmt.Errorf("view %q appears more than once", id)
		}
		seen[id] = struct{}{}
		ordered = append(ordered, view)
	}
	return ordered, nil
}

func applyViewOrder(tx *gorm.DB, versionID uint, ordered []models.SOPView) error {
	if len(ordered) == 0 {
		return nil
	}
	if err := tx.Model(&models.SOPView{}).Where("sop_version_id = ?", versionID).Update("sequence", gorm.Expr("sequence + ?", 1000000)).Error; err != nil {
		return err
	}
	for index := range ordered {
		if err := tx.Model(&models.SOPView{}).Where("id = ?", ordered[index].ID).Update("sequence", index+1).Error; err != nil {
			return err
		}
	}
	return nil
}

func cloneView(source models.SOPView, versionID uint) models.SOPView {
	return models.SOPView{
		PublicID: uuid.NewString(), SOPVersionID: versionID, Sequence: source.Sequence,
		Role: source.Role, ViewKind: source.ViewKind, PresetKey: source.PresetKey,
		NameZH: source.NameZH, NameEN: source.NameEN, InstructionZH: source.InstructionZH, InstructionEN: source.InstructionEN,
		Required: source.Required, AllowMultiple: source.AllowMultiple,
		CameraPositionX: source.CameraPositionX, CameraPositionY: source.CameraPositionY, CameraPositionZ: source.CameraPositionZ,
		ImageUpX: source.ImageUpX, ImageUpY: source.ImageUpY, ImageUpZ: source.ImageUpZ,
		TargetX: source.TargetX, TargetY: source.TargetY, TargetZ: source.TargetZ, Composition: source.Composition,
	}
}

func draftVersionAndView(tx *gorm.DB, versionPublicID, viewPublicID string) (*models.SOPVersion, *models.SOPView, error) {
	version, err := getVersionRecord(tx, versionPublicID)
	if err != nil {
		return nil, nil, err
	}
	if err := requireDraft(*version); err != nil {
		return nil, nil, err
	}
	var view models.SOPView
	if err := tx.Where("sop_version_id = ? AND public_id = ?", version.ID, viewPublicID).First(&view).Error; err != nil {
		return nil, nil, err
	}
	return version, &view, nil
}

func orderReferenceImages(images []models.SOPViewReferenceImage, orderedIDs []string) ([]models.SOPViewReferenceImage, error) {
	if len(orderedIDs) != len(images) {
		return nil, errors.New("reference-image order must contain every image exactly once")
	}
	byID := make(map[string]models.SOPViewReferenceImage, len(images))
	for _, image := range images {
		byID[image.PublicID] = image
	}
	ordered := make([]models.SOPViewReferenceImage, 0, len(images))
	seen := make(map[string]struct{}, len(images))
	for _, id := range orderedIDs {
		image, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("reference image %q does not belong to this view", id)
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, fmt.Errorf("reference image %q appears more than once", id)
		}
		seen[id] = struct{}{}
		ordered = append(ordered, image)
	}
	return ordered, nil
}

func offsetReferenceImageOrder(tx *gorm.DB, viewID uint) error {
	return tx.Model(&models.SOPViewReferenceImage{}).Where("sop_view_id = ?", viewID).
		Update("sort_order", gorm.Expr("sort_order + ?", 1000000)).Error
}

func applyReferenceImageOrder(tx *gorm.DB, viewID uint, ordered []models.SOPViewReferenceImage) error {
	if len(ordered) == 0 {
		return nil
	}
	if err := offsetReferenceImageOrder(tx, viewID); err != nil {
		return err
	}
	for index := range ordered {
		if err := tx.Model(&models.SOPViewReferenceImage{}).Where("id = ?", ordered[index].ID).Update("sort_order", index+1).Error; err != nil {
			return err
		}
	}
	return nil
}
