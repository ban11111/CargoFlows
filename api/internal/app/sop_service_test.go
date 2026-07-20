package app

import (
	"context"
	"errors"
	"testing"

	"cargoflows/api/internal/models"
	"cargoflows/api/internal/sop"
	"gorm.io/gorm"
)

func seedSOPCategoryAndUser(t *testing.T, db *gorm.DB) (models.Category, models.User) {
	t.Helper()
	category := models.Category{Name: "手机壳-" + t.Name(), NameEN: "Phone Case"}
	if err := db.Create(&category).Error; err != nil {
		t.Fatal(err)
	}
	user := models.User{
		Name: "SOP Author", Email: t.Name() + "@example.com", PasswordHash: "test",
		Role: models.RoleAdmin, Status: "active",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	return category, user
}

func createTestSOP(t *testing.T, service *SOPService, category models.Category, user models.User) *CreatedSOP {
	t.Helper()
	created, err := service.Create(context.Background(), CreateSOPInput{
		CategoryID: category.ID, CreatedByID: user.ID,
		NameZH: "手机壳拍摄", NameEN: "Phone Case Capture",
	})
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func TestSOPServiceCreateUpdateAndPublish(t *testing.T) {
	db := newTestDB(t)
	category, user := seedSOPCategoryAndUser(t, db)
	service := NewSOPService(db)
	created := createTestSOP(t, service, category, user)

	if created.SOP.PublicID == "" || created.Version.VersionNumber != 1 || len(created.Version.Views) != 1 {
		t.Fatalf("unexpected aggregate: %#v", created)
	}
	ref := created.Version.Views[0]
	if ref.Role != models.SOPViewReferenceFront || ref.Sequence != 1 || !ref.Required {
		t.Fatalf("invalid ref: %#v", ref)
	}

	updated, err := service.UpdateVersion(context.Background(), created.Version.PublicID, UpdateVersionInput{
		NameZH: "更新名称", NameEN: "Updated Name", DescriptionZH: "中文说明", DescriptionEN: "English description",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.NameEN != "Updated Name" || updated.DescriptionZH != "中文说明" {
		t.Fatalf("metadata not updated: %#v", updated)
	}

	published, err := service.Publish(context.Background(), created.Version.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if published.Status != models.SOPVersionPublished || published.PublishedAt == nil {
		t.Fatalf("version not published: %#v", published)
	}
	if _, err := service.AddView(context.Background(), created.Version.PublicID, AddViewInput{PresetKey: "back"}); !errors.Is(err, ErrVersionImmutable) {
		t.Fatalf("add after publish error = %v", err)
	}
	if _, err := service.UpdateVersion(context.Background(), created.Version.PublicID, UpdateVersionInput{NameZH: "x", NameEN: "x"}); !errors.Is(err, ErrVersionImmutable) {
		t.Fatalf("metadata update after publish error = %v", err)
	}
}

func TestSOPServiceViewMutationLockAndReorder(t *testing.T) {
	db := newTestDB(t)
	category, user := seedSOPCategoryAndUser(t, db)
	service := NewSOPService(db)
	created := createTestSOP(t, service, category, user)
	ctx := context.Background()
	ref := created.Version.Views[0]

	if err := service.DeleteView(ctx, created.Version.PublicID, ref.PublicID); !errors.Is(err, ErrReferenceLocked) {
		t.Fatalf("delete reference error = %v", err)
	}
	if _, err := service.UpdateView(ctx, created.Version.PublicID, ref.PublicID, UpdateViewInput{}); !errors.Is(err, ErrReferenceLocked) {
		t.Fatalf("update reference error = %v", err)
	}

	back, err := service.AddView(ctx, created.Version.PublicID, AddViewInput{PresetKey: "back"})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := service.AddView(ctx, created.Version.PublicID, AddViewInput{Custom: &sop.ViewInput{
		Role: models.SOPViewCapture, Kind: models.SOPViewDetail,
		NameZH: "细节", NameEN: "Detail", Required: false,
		CameraPosition: sop.Vector3{0, 0, 5}, ImageUp: sop.Vector3{2, 0, 0}, Target: sop.Vector3{0.1, 0.2, 0.3},
		Composition: models.Composition{FrameOccupancy: .8, AspectRatio: "1:1", AllowRotationCorrection: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if detail.CameraPositionZ != 1 || detail.ImageUpX != 1 {
		t.Fatalf("pose was not canonicalized: %#v", detail)
	}

	before, err := service.GetVersion(ctx, created.Version.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Reorder(ctx, created.Version.PublicID, []string{ref.PublicID, back.PublicID, back.PublicID}); err == nil {
		t.Fatal("duplicate reorder unexpectedly succeeded")
	}
	if err := service.Reorder(ctx, created.Version.PublicID, []string{ref.PublicID, back.PublicID}); err == nil {
		t.Fatal("missing reorder unexpectedly succeeded")
	}
	afterInvalid, err := service.GetVersion(ctx, created.Version.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	for i := range before.Views {
		if before.Views[i].PublicID != afterInvalid.Views[i].PublicID || before.Views[i].Sequence != afterInvalid.Views[i].Sequence {
			t.Fatalf("invalid reorder changed views: before=%#v after=%#v", before.Views, afterInvalid.Views)
		}
	}
	if err := service.Reorder(ctx, created.Version.PublicID, []string{ref.PublicID, detail.PublicID, back.PublicID}); err != nil {
		t.Fatal(err)
	}
	reordered, err := service.GetVersion(ctx, created.Version.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if reordered.Views[1].PublicID != detail.PublicID || reordered.Views[2].PublicID != back.PublicID {
		t.Fatalf("unexpected order: %#v", reordered.Views)
	}

	updated, err := service.UpdateView(ctx, created.Version.PublicID, detail.PublicID, UpdateViewInput{
		NameZH: "特写", NameEN: "Close-up", InstructionZH: "说明", InstructionEN: "Instruction", Required: true,
		CameraPosition: sop.Vector3{0, 3, 0}, ImageUp: sop.Vector3{2, 0, 0}, Target: sop.Vector3{.2, .1, 0},
		Composition: models.Composition{FrameOccupancy: .9, AspectRatio: "4:3", AllowRotationCorrection: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.NameEN != "Close-up" || updated.CameraPositionY != 1 || !updated.Required {
		t.Fatalf("view not updated: %#v", updated)
	}
	if err := service.DeleteView(ctx, created.Version.PublicID, back.PublicID); err != nil {
		t.Fatal(err)
	}
	remaining, err := service.GetVersion(ctx, created.Version.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining.Views) != 2 || remaining.Views[1].Sequence != 2 {
		t.Fatalf("delete did not compact sequences: %#v", remaining.Views)
	}
}

func TestSOPServiceReferenceImages(t *testing.T) {
	db := newTestDB(t)
	category, user := seedSOPCategoryAndUser(t, db)
	service := NewSOPService(db)
	created := createTestSOP(t, service, category, user)
	ctx := context.Background()
	view := created.Version.Views[0]

	first, err := service.AddReferenceImage(ctx, created.Version.PublicID, view.PublicID, ReferenceImageInput{ObjectKey: "one.jpg", ThumbnailURL: "/one", CaptionEN: "One"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.AddReferenceImage(ctx, created.Version.PublicID, view.PublicID, ReferenceImageInput{ObjectKey: "two.jpg", ThumbnailURL: "/two", CaptionEN: "Two"})
	if err != nil {
		t.Fatal(err)
	}
	if first.SortOrder != 1 || second.SortOrder != 2 {
		t.Fatalf("sort orders are not service controlled: %#v %#v", first, second)
	}
	if err := service.ReorderReferenceImages(ctx, created.Version.PublicID, view.PublicID, []string{second.PublicID, second.PublicID}); err == nil {
		t.Fatal("duplicate image reorder unexpectedly succeeded")
	}
	if err := service.ReorderReferenceImages(ctx, created.Version.PublicID, view.PublicID, []string{second.PublicID, first.PublicID}); err != nil {
		t.Fatal(err)
	}
	version, err := service.GetVersion(ctx, created.Version.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if version.Views[0].ReferenceImages[0].PublicID != second.PublicID || version.Views[0].ReferenceImages[0].SortOrder != 1 {
		t.Fatalf("images not reordered: %#v", version.Views[0].ReferenceImages)
	}
	if err := service.DeleteReferenceImage(ctx, created.Version.PublicID, view.PublicID, second.PublicID); err != nil {
		t.Fatal(err)
	}
	version, err = service.GetVersion(ctx, created.Version.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if len(version.Views[0].ReferenceImages) != 1 || version.Views[0].ReferenceImages[0].SortOrder != 1 {
		t.Fatalf("image delete did not compact order: %#v", version.Views[0].ReferenceImages)
	}
}

func TestSOPServiceCopySingleDraftArchiveAndList(t *testing.T) {
	db := newTestDB(t)
	category, user := seedSOPCategoryAndUser(t, db)
	service := NewSOPService(db)
	created := createTestSOP(t, service, category, user)
	ctx := context.Background()
	if _, err := service.AddView(ctx, created.Version.PublicID, AddViewInput{PresetKey: "back"}); err != nil {
		t.Fatal(err)
	}
	sourceImage, err := service.AddReferenceImage(ctx, created.Version.PublicID, created.Version.Views[0].PublicID, ReferenceImageInput{
		ObjectKey: "reference/source.jpg", ThumbnailURL: "/reference/source.jpg", CaptionEN: "Source reference",
	})
	if err != nil {
		t.Fatal(err)
	}
	published, err := service.Publish(ctx, created.Version.PublicID)
	if err != nil {
		t.Fatal(err)
	}

	copied, err := service.CopyVersion(ctx, created.SOP.PublicID, published.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if copied.VersionNumber != 2 || copied.Status != models.SOPVersionDraft || len(copied.Views) != len(published.Views) {
		t.Fatalf("unexpected copy: %#v", copied)
	}
	for i := range copied.Views {
		if copied.Views[i].PublicID == published.Views[i].PublicID {
			t.Fatalf("copied view reused UUID at index %d", i)
		}
	}
	if len(copied.Views[0].ReferenceImages) != 1 {
		t.Fatalf("copied reference images = %#v", copied.Views[0].ReferenceImages)
	}
	copiedImage := copied.Views[0].ReferenceImages[0]
	if copiedImage.PublicID == sourceImage.PublicID {
		t.Fatal("copied reference-image relationship reused UUID")
	}
	if copiedImage.ObjectKey != sourceImage.ObjectKey {
		t.Fatalf("copied object key = %q, want %q", copiedImage.ObjectKey, sourceImage.ObjectKey)
	}
	if _, err := service.CopyVersion(ctx, created.SOP.PublicID, published.PublicID); !errors.Is(err, ErrDraftExists) {
		t.Fatalf("second copy error = %v", err)
	}

	if err := service.Archive(ctx, published.PublicID); err != nil {
		t.Fatal(err)
	}
	listed, err := service.List(ctx, category.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("archived version remains selectable: %#v", listed)
	}
	var persisted models.SOPVersion
	if err := db.Where("id = ?", published.ID).First(&persisted).Error; err != nil {
		t.Fatalf("archived row was not preserved: %v", err)
	}
	if persisted.Status != models.SOPVersionArchived {
		t.Fatalf("status = %q", persisted.Status)
	}
	if err := service.Restore(ctx, published.PublicID); err != nil {
		t.Fatal(err)
	}
	listed, err = service.List(ctx, category.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || len(listed[0].Versions) != 1 || listed[0].Versions[0].PublicID != published.PublicID || listed[0].Versions[0].Status != models.SOPVersionPublished {
		t.Fatalf("restored version is not selectable: %#v", listed)
	}
	if err := service.Restore(ctx, published.PublicID); !errors.Is(err, ErrVersionImmutable) {
		t.Fatalf("second restore error = %v", err)
	}
}

func TestSOPServiceCopyRejectsArchivedSource(t *testing.T) {
	db := newTestDB(t)
	category, user := seedSOPCategoryAndUser(t, db)
	service := NewSOPService(db)
	created := createTestSOP(t, service, category, user)
	ctx := context.Background()
	published, err := service.Publish(ctx, created.Version.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Archive(ctx, published.PublicID); err != nil {
		t.Fatal(err)
	}

	if _, err := service.CopyVersion(ctx, created.SOP.PublicID, published.PublicID); !errors.Is(err, ErrSourceVersionNotPublished) {
		t.Fatalf("copy archived source error = %v", err)
	}
	var draftCount int64
	if err := db.Model(&models.SOPVersion{}).Where("capture_sop_id = ? AND status = ?", created.SOP.ID, models.SOPVersionDraft).Count(&draftCount).Error; err != nil {
		t.Fatal(err)
	}
	if draftCount != 0 {
		t.Fatalf("copy failure created %d drafts", draftCount)
	}
}

func TestSOPServicePublishRejectsUnknownViewEnumsTransactionally(t *testing.T) {
	db := newTestDB(t)
	category, user := seedSOPCategoryAndUser(t, db)
	service := NewSOPService(db)
	created := createTestSOP(t, service, category, user)
	ctx := context.Background()
	if _, err := service.AddView(ctx, created.Version.PublicID, AddViewInput{Custom: &sop.ViewInput{
		Role: models.SOPViewRole("sideways"), Kind: models.SOPViewKind("panorama"),
		NameZH: "无效", NameEN: "Invalid",
		CameraPosition: sop.Vector3{0, 0, 1}, ImageUp: sop.Vector3{1, 0, 0},
		Composition: models.Composition{FrameOccupancy: 0.85, AspectRatio: "1:1"},
	}}); err != nil {
		t.Fatal(err)
	}

	_, err := service.Publish(ctx, created.Version.PublicID)
	var validationErr *SOPValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("publish error = %v", err)
	}
	codes := make(map[string]bool, len(validationErr.Errors))
	for _, item := range validationErr.Errors {
		codes[item.Code] = true
	}
	if !codes["view_role_invalid"] || !codes["view_kind_invalid"] {
		t.Fatalf("publication errors = %#v", validationErr.Errors)
	}
	stored, err := service.GetVersion(ctx, created.Version.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != models.SOPVersionDraft || stored.PublishedAt != nil {
		t.Fatalf("failed publish mutated version: %#v", stored)
	}
}

func TestSOPServicePublishValidationIsTransactional(t *testing.T) {
	db := newTestDB(t)
	category, user := seedSOPCategoryAndUser(t, db)
	service := NewSOPService(db)
	created := createTestSOP(t, service, category, user)
	ctx := context.Background()
	if _, err := service.UpdateVersion(ctx, created.Version.PublicID, UpdateVersionInput{NameZH: "", NameEN: ""}); err != nil {
		t.Fatal(err)
	}

	validationErrors, err := service.Validate(ctx, created.Version.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if len(validationErrors) == 0 {
		t.Fatal("Validate returned no errors")
	}
	if _, err := service.Publish(ctx, created.Version.PublicID); err == nil {
		t.Fatal("invalid version was published")
	}
	stored, err := service.GetVersion(ctx, created.Version.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != models.SOPVersionDraft || stored.PublishedAt != nil {
		t.Fatalf("failed publish mutated version: %#v", stored)
	}
}
