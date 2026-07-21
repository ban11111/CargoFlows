package ai

import (
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"cargoflows/api/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func templateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.AIContentTemplate{}, &models.AIContentTemplateVersion{}, &models.AIContentSlot{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func validTemplateInput() CreateTemplateInput {
	return CreateTemplateInput{
		NameZH: "Lazada 详情", NameEN: "Lazada Detail", TargetPlatform: "lazada", CreatedByID: 1,
		Version: UpdateTemplateVersionInput{
			DefaultLocale: "zh-CN", PromptCompilerVersion: "v1", PlatformPrompt: "Create content for {{product.name}}.",
			Slots: []SlotInput{{
				SlotKey: "hero", Kind: "image", NameZH: "主图", NameEN: "Hero", Sequence: 1,
				PromptFragment:   "Create a faithful image of {{sku.code}}.",
				Constraints:      json.RawMessage(`{"required_views":["reference_front"]}`),
				GenerationConfig: json.RawMessage(`{"size":"1024x1024","candidate_count":2}`),
				LayoutConfig:     json.RawMessage(`{"text_safe_area":{"x":0.08,"y":0.08,"width":0.84,"height":0.28}}`),
			}},
		},
	}
}

func TestTemplateCreateGetListAndNormalizesSlots(t *testing.T) {
	service := NewTemplateService(templateTestDB(t))
	input := validTemplateInput()
	input.Version.Slots = append(input.Version.Slots, SlotInput{
		SlotKey: " title ", Kind: " title ", NameZH: " 标题 ", NameEN: " Title ",
		PromptFragment: " Write {{product.name}} ", Constraints: json.RawMessage(` { } `),
		GenerationConfig: json.RawMessage(`{"candidate_count":1}`), LayoutConfig: json.RawMessage(`{}`),
	})
	input.Version.Slots[0].Sequence = 0

	created, err := service.Create(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if created.Template.PublicID == "" || created.Version.PublicID == "" || created.Version.Status != models.AITemplateDraft {
		t.Fatalf("unexpected created template: %#v", created)
	}
	if created.Template.Status != models.AIContentTemplateActive || created.Version.DraftGuard == nil {
		t.Fatalf("unexpected lifecycle guards: %#v", created)
	}
	got, err := service.Get(t.Context(), created.Template.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Versions) != 1 || len(got.Versions[0].Slots) != 2 {
		t.Fatalf("unexpected loaded template: %#v", got)
	}
	for i, slot := range got.Versions[0].Slots {
		if slot.Sequence != i+1 {
			t.Fatalf("slot %d sequence = %d", i, slot.Sequence)
		}
	}
	if got.Versions[0].Slots[1].SlotKey != "title" || got.Versions[0].Slots[1].NameEN != "Title" {
		t.Fatalf("slot was not normalized: %#v", got.Versions[0].Slots[1])
	}
	listed, err := service.List(t.Context(), true)
	if err != nil || len(listed) != 1 || listed[0].PublicID != created.Template.PublicID {
		t.Fatalf("list = %#v, err = %v", listed, err)
	}
}

func TestPublishReturnsAllValidationIssuesAndLeavesDraft(t *testing.T) {
	service := NewTemplateService(templateTestDB(t))
	input := validTemplateInput()
	input.NameEN = ""
	input.Version.PlatformPrompt = "Use {{unknown.value}}"
	input.Version.Slots[0].NameZH = ""
	input.Version.Slots[0].PromptFragment = ""
	input.Version.Slots[0].GenerationConfig = json.RawMessage(`{"size":"999x999","candidate_count":5}`)
	input.Version.Slots[0].LayoutConfig = json.RawMessage(`{"text_safe_area":{"x":0.9,"y":0.1,"width":0.2,"height":0.3}}`)
	created, err := service.Create(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	issues, err := service.Publish(t.Context(), created.Version.PublicID, 9)
	if err != nil {
		t.Fatal(err)
	}
	assertIssueCodes(t, issues, "name_en_required", "slot_name_zh_required", "prompt_required", "template_variable_unknown", "candidate_count_invalid", "image_size_invalid", "safe_area_invalid")

	got, err := service.Get(t.Context(), created.Template.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != models.AIContentTemplateActive || got.Versions[0].Status != models.AITemplateDraft || got.Versions[0].PublishedAt != nil {
		t.Fatalf("invalid publication changed lifecycle: %#v", got)
	}
}

func TestPublishRejectsMalformedImageRequiredViews(t *testing.T) {
	service := NewTemplateService(templateTestDB(t))
	input := validTemplateInput()
	input.Version.Slots[0].Constraints = json.RawMessage(`{"required_views":null}`)
	created, err := service.Create(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	issues, err := service.Publish(t.Context(), created.Version.PublicID, 9)
	if err != nil {
		t.Fatal(err)
	}
	assertIssueCodes(t, issues, "required_views_invalid")
}

func TestPublishValidatesGenerationAllowLists(t *testing.T) {
	service := NewTemplateService(templateTestDB(t))
	input := validTemplateInput()
	input.Version.Slots[0].GenerationConfig = json.RawMessage(`{"size":"1024x1024","allowed_candidate_count":[0,2,2],"allowed_sizes":["999x999"],"allowed_qualities":["ultra"],"allowed_styles":["", "` + strings.Repeat("x", 81) + `"]}`)
	created, err := service.Create(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	issues, err := service.Publish(t.Context(), created.Version.PublicID, 9)
	if err != nil {
		t.Fatal(err)
	}
	assertIssueCodes(t, issues, "allowed_candidate_count_invalid", "allowed_sizes_invalid", "allowed_qualities_invalid", "allowed_styles_invalid")
	wantPaths := map[string]string{"allowed_candidate_count_invalid": "slots[0].generation_config.allowed_candidate_count", "allowed_sizes_invalid": "slots[0].generation_config.allowed_sizes", "allowed_qualities_invalid": "slots[0].generation_config.allowed_qualities", "allowed_styles_invalid": "slots[0].generation_config.allowed_styles"}
	for _, issue := range issues {
		if want, ok := wantPaths[issue.Code]; ok && issue.Path != want {
			t.Errorf("%s path = %q, want %q", issue.Code, issue.Path, want)
		}
	}
}

func TestPublishRequiresDefaultImageStyleInAllowList(t *testing.T) {
	service := NewTemplateService(templateTestDB(t))
	input := validTemplateInput()
	input.Version.Slots[0].GenerationConfig = json.RawMessage(`{"size":"1024x1024","style":"premium_dark","allowed_styles":["clean_white_background"]}`)
	created, err := service.Create(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	issues, err := service.Publish(t.Context(), created.Version.PublicID, 9)
	if err != nil {
		t.Fatal(err)
	}
	assertIssueCodes(t, issues, "default_style_not_allowed")
}

func TestPublishRejectsNonBooleanUserExtraPromptPermission(t *testing.T) {
	service := NewTemplateService(templateTestDB(t))
	input := validTemplateInput()
	input.Version.Slots[0].GenerationConfig = json.RawMessage(`{"size":"1024x1024","allow_user_extra_prompt":"yes"}`)
	created, err := service.Create(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	issues, err := service.Publish(t.Context(), created.Version.PublicID, 9)
	if err != nil {
		t.Fatal(err)
	}
	assertIssueCodes(t, issues, "allow_user_extra_prompt_invalid")
	for _, issue := range issues {
		if issue.Code == "allow_user_extra_prompt_invalid" && issue.Path != "slots[0].generation_config.allow_user_extra_prompt" {
			t.Fatalf("issue path = %q", issue.Path)
		}
	}
}

func TestUpdateDraftPersistsDuplicateKeysAndPublishReturnsAllIssues(t *testing.T) {
	service := NewTemplateService(templateTestDB(t))
	created, err := service.Create(t.Context(), validTemplateInput())
	if err != nil {
		t.Fatal(err)
	}
	update := validTemplateInput().Version
	update.NameZH = "Lazada 详情"
	update.NameEN = ""
	update.Slots = []SlotInput{
		{SlotKey: "hero", Kind: "image", NameZH: "主图", NameEN: "Hero", PromptFragment: "Create {{product.name}}", Constraints: json.RawMessage(`{}`), GenerationConfig: json.RawMessage(`{"size":"1024x1024"}`), LayoutConfig: json.RawMessage(`{}`)},
		{SlotKey: "hero", Kind: "image", NameZH: "", NameEN: "Hero duplicate", PromptFragment: "", Constraints: json.RawMessage(`{}`), GenerationConfig: json.RawMessage(`{"size":"1024x1024"}`), LayoutConfig: json.RawMessage(`{}`)},
	}
	updated, err := service.UpdateDraft(t.Context(), created.Version.PublicID, update)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Slots) != 2 || updated.Slots[0].SlotKey != "hero" || updated.Slots[1].SlotKey != "hero" {
		t.Fatalf("duplicate draft slots were not persisted: %#v", updated.Slots)
	}
	issues, err := service.Publish(t.Context(), created.Version.PublicID, 9)
	if err != nil {
		t.Fatal(err)
	}
	assertIssueCodes(t, issues, "name_en_required", "slot_key_duplicate", "slot_name_zh_required", "prompt_required")
	got, err := service.Get(t.Context(), created.Template.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != models.AIContentTemplateActive || got.Versions[0].Status != models.AITemplateDraft || got.Versions[0].DraftGuard == nil {
		t.Fatalf("invalid publish changed draft state: %#v", got)
	}
}

func TestValidateTemplateVersionReportsDuplicateKeysKindsSequencesAndJSON(t *testing.T) {
	version := models.AIContentTemplateVersion{DefaultLocale: "zh-CN", PromptCompilerVersion: "v1", PlatformPrompt: "ok"}
	slots := []models.AIContentSlot{
		{SlotKey: "hero", Kind: "video", NameZH: "主图", NameEN: "Hero", Sequence: 1, PromptFragment: "ok", ConstraintsJSON: []byte(`[]`), GenerationConfigJSON: []byte(`{`), LayoutConfigJSON: []byte(`{}`)},
		{SlotKey: "hero", Kind: models.AIContentSlotImage, NameZH: "主图 2", NameEN: "Hero 2", Sequence: 3, PromptFragment: "ok", ConstraintsJSON: []byte(`{}`), GenerationConfigJSON: []byte(`{}`), LayoutConfigJSON: []byte(`{}`)},
	}
	issues := ValidateTemplateVersion(version, slots)
	assertIssueCodes(t, issues, "slot_key_duplicate", "slot_kind_invalid", "slot_sequence_invalid", "constraints_object_required", "generation_config_object_required")
}

func TestTemplateVariablesUseExactV1Allowlist(t *testing.T) {
	accepted := []string{
		"locale", "target_platform", "candidate_count", "product.name", "product.brand", "product.category",
		"product.description", "sku.code", "sku.color", "sku.size", "sku.compatible_device_model", "sku.platform_title", "sop.name_zh",
		"sop.name_en", "sop.required_views", "style.name", "style.description", "style.instructions",
	}
	for _, variable := range accepted {
		t.Run("accept_"+variable, func(t *testing.T) {
			if issues := validatePrompt("Use {{"+variable+"}}", "prompt", true); len(issues) != 0 {
				t.Fatalf("documented variable %q rejected: %#v", variable, issues)
			}
		})
	}
	for _, variable := range []string{"product.password_hash", "sku.nonexistent", "unknown.value", "secrets.api_key"} {
		t.Run("reject_"+variable, func(t *testing.T) {
			assertIssueCodes(t, validatePrompt("Use {{"+variable+"}}", "prompt", true), "template_variable_unknown")
		})
	}
}

func TestValidateTemplateVersionRejectsNonBooleanCompatibleDeviceRequirement(t *testing.T) {
	version := models.AIContentTemplateVersion{DefaultLocale: "zh-CN", PromptCompilerVersion: "v1", PlatformPrompt: "ok"}
	slot := models.AIContentSlot{SlotKey: "installed", Kind: models.AIContentSlotImage, NameZH: "装机", NameEN: "Installed", Sequence: 1, PromptFragment: "Use {{sku.compatible_device_model}}", ConstraintsJSON: []byte(`{"requires_compatible_device_model":"yes"}`), GenerationConfigJSON: []byte(`{"size":"1024x1024"}`), LayoutConfigJSON: []byte(`{}`)}
	assertIssueCodes(t, ValidateTemplateVersion(version, []models.AIContentSlot{slot}), "requires_compatible_device_model_invalid")
}

func TestPublishFreezesVersionCopyCreatesFreshDraftAndArchiveRecalculatesParent(t *testing.T) {
	db := templateTestDB(t)
	service := NewTemplateService(db)
	created, err := service.Create(t.Context(), validTemplateInput())
	if err != nil {
		t.Fatal(err)
	}
	issues, err := service.Publish(t.Context(), created.Version.PublicID, 7)
	if err != nil || len(issues) != 0 {
		t.Fatalf("publish issues = %#v, err = %v", issues, err)
	}
	firstPublished, err := service.Get(t.Context(), created.Template.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if firstPublished.Status != models.AIContentTemplateActive || firstPublished.Versions[0].DraftGuard != nil {
		t.Fatalf("publish changed logical status or retained guard: %#v", firstPublished)
	}
	if _, err := service.UpdateDraft(t.Context(), created.Version.PublicID, validTemplateInput().Version); !errors.Is(err, ErrTemplateVersionImmutable) {
		t.Fatalf("update published error = %v", err)
	}

	copied, err := service.CopyVersion(t.Context(), created.Template.PublicID, created.Version.PublicID, 8)
	if err != nil {
		t.Fatal(err)
	}
	if copied.PublicID == created.Version.PublicID || copied.VersionNumber != 2 || copied.Status != models.AITemplateDraft || len(copied.Slots) != 1 || copied.Slots[0].PublicID == created.Version.Slots[0].PublicID {
		t.Fatalf("unexpected copied version: %#v", copied)
	}
	if _, err := service.CopyVersion(t.Context(), created.Template.PublicID, created.Version.PublicID, 8); !errors.Is(err, ErrTemplateDraftExists) {
		t.Fatalf("second copy error = %v", err)
	}
	issues, err = service.Publish(t.Context(), copied.PublicID, 8)
	if err != nil || len(issues) != 0 {
		t.Fatalf("publish copy issues = %#v, err = %v", issues, err)
	}
	if err := service.Archive(t.Context(), created.Version.PublicID); err != nil {
		t.Fatal(err)
	}
	afterFirstArchive, err := service.Get(t.Context(), created.Template.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if afterFirstArchive.Status != models.AIContentTemplateActive || afterFirstArchive.Versions[0].Status != models.AITemplateArchived || afterFirstArchive.Versions[0].ArchivedAt == nil || afterFirstArchive.Versions[1].Status != models.AITemplatePublished {
		t.Fatalf("first archive states = %#v", afterFirstArchive)
	}
	selectable, err := service.List(t.Context(), false)
	if err != nil || len(selectable) != 1 || len(selectable[0].Versions) != 1 || selectable[0].Versions[0].PublicID != copied.PublicID {
		t.Fatalf("selectable after first archive = %#v, err = %v", selectable, err)
	}
	if err := service.Archive(t.Context(), copied.PublicID); err != nil {
		t.Fatal(err)
	}
	afterLastArchive, err := service.Get(t.Context(), created.Template.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if afterLastArchive.Status != models.AIContentTemplateArchived || afterLastArchive.Versions[1].Status != models.AITemplateArchived || afterLastArchive.Versions[1].ArchivedAt == nil {
		t.Fatalf("last archive states = %#v", afterLastArchive)
	}
	selectable, err = service.List(t.Context(), false)
	if err != nil || len(selectable) != 0 {
		t.Fatalf("selectable after last archive = %#v, err = %v", selectable, err)
	}
	if err := service.Restore(t.Context(), created.Version.PublicID); err != nil {
		t.Fatal(err)
	}
	restored, err := service.Get(t.Context(), created.Template.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Status != models.AIContentTemplateActive || restored.Versions[0].Status != models.AITemplatePublished || restored.Versions[0].ArchivedAt != nil {
		t.Fatalf("restored states = %#v", restored)
	}
	selectable, err = service.List(t.Context(), false)
	if err != nil || len(selectable) != 1 || len(selectable[0].Versions) != 1 || selectable[0].Versions[0].PublicID != created.Version.PublicID {
		t.Fatalf("selectable after restore = %#v, err = %v", selectable, err)
	}
	if err := service.Restore(t.Context(), created.Version.PublicID); !errors.Is(err, ErrTemplateVersionImmutable) {
		t.Fatalf("second restore error = %v", err)
	}
}

func TestDatabaseRejectsSecondDraftForLogicalTemplate(t *testing.T) {
	db := templateTestDB(t)
	created, err := NewTemplateService(db).Create(t.Context(), validTemplateInput())
	if err != nil {
		t.Fatal(err)
	}
	guard := templateDraftGuard
	second := models.AIContentTemplateVersion{
		PublicID: "00000000-0000-0000-0000-000000000002", AIContentTemplateID: created.Template.ID,
		VersionNumber: 2, Status: models.AITemplateDraft, DraftGuard: &guard, DefaultLocale: "zh-CN",
		PromptCompilerVersion: "v1", CreatedByID: 2,
	}
	if err := db.Create(&second).Error; err == nil {
		t.Fatal("database accepted a second draft for one logical template")
	}
}

func TestDatabaseRejectsDraftWithoutGuard(t *testing.T) {
	db := templateTestDB(t)
	created, err := NewTemplateService(db).Create(t.Context(), validTemplateInput())
	if err != nil {
		t.Fatal(err)
	}
	result := db.Exec(`INSERT INTO ai_content_template_versions
		(public_id, ai_content_template_id, version_number, status, default_locale, prompt_compiler_version, platform_prompt, created_by_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		"00000000-0000-0000-0000-000000000003", created.Template.ID, 2, models.AITemplateDraft, "zh-CN", "v1", "", 2)
	if result.Error == nil || !strings.Contains(result.Error.Error(), "chk_ai_template_draft_guard") {
		t.Fatalf("unguarded draft error = %v, want draft-guard check constraint", result.Error)
	}
}

func TestArchiveResolvesIdentityOutsideTransactionThenLocksParentFirst(t *testing.T) {
	db := templateTestDB(t)
	service := NewTemplateService(db)
	created, err := service.Create(t.Context(), validTemplateInput())
	if err != nil {
		t.Fatal(err)
	}
	if issues, err := service.Publish(t.Context(), created.Version.PublicID, 1); err != nil || len(issues) != 0 {
		t.Fatalf("publish issues=%#v err=%v", issues, err)
	}

	var mu sync.Mutex
	var events []string
	const queryCallback = "test:observe-template-archive-reads"
	const updateCallback = "test:observe-template-version-update"
	if err := db.Callback().Query().Before("gorm:query").Register(queryCallback, func(tx *gorm.DB) {
		_, inTransaction := tx.Statement.ConnPool.(*sql.Tx)
		_, locked := tx.Statement.Clauses["FOR"]
		event := ""
		switch tx.Statement.Table {
		case "ai_content_template_versions":
			switch {
			case !inTransaction:
				event = "identity_outside"
			case locked:
				event = "lock_version"
			default:
				event = "read_version_in_transaction"
			}
		case "ai_content_templates":
			if inTransaction && locked {
				event = "lock_parent"
			}
		}
		if event != "" {
			mu.Lock()
			events = append(events, event)
			mu.Unlock()
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register(updateCallback, func(tx *gorm.DB) {
		if tx.Statement.Table == "ai_content_template_versions" {
			mu.Lock()
			events = append(events, "update_version")
			mu.Unlock()
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove(queryCallback)
		_ = db.Callback().Update().Remove(updateCallback)
	})

	if err := service.Archive(t.Context(), created.Version.PublicID); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	identityIndex, parentLockIndex, versionLockIndex, updateIndex := -1, -1, -1, -1
	for index, event := range events {
		if event == "identity_outside" && identityIndex == -1 {
			identityIndex = index
		}
		if event == "lock_parent" && parentLockIndex == -1 {
			parentLockIndex = index
		}
		if event == "lock_version" && versionLockIndex == -1 {
			versionLockIndex = index
		}
		if event == "update_version" && updateIndex == -1 {
			updateIndex = index
		}
	}
	if identityIndex == -1 || parentLockIndex == -1 || versionLockIndex == -1 || updateIndex == -1 ||
		identityIndex >= parentLockIndex || parentLockIndex >= versionLockIndex || versionLockIndex >= updateIndex {
		t.Fatalf("archive orchestration events = %v, want outside identity -> parent lock -> version lock -> update", events)
	}
	for index, event := range events {
		if index < parentLockIndex && event == "read_version_in_transaction" {
			t.Fatalf("archive established a pre-lock transaction snapshot: %v", events)
		}
	}
}

func TestUpdateDraftReplacesSlotsAndRejectsMissingVersion(t *testing.T) {
	service := NewTemplateService(templateTestDB(t))
	created, err := service.Create(t.Context(), validTemplateInput())
	if err != nil {
		t.Fatal(err)
	}
	update := validTemplateInput().Version
	update.Slots = []SlotInput{{
		SlotKey: "seo", Kind: "seo_description", NameZH: "SEO 描述", NameEN: "SEO description",
		PromptFragment: "Write for {{product.name}}", Constraints: json.RawMessage(`{}`), GenerationConfig: json.RawMessage(`{"candidate_count":4}`), LayoutConfig: json.RawMessage(`{}`),
	}}
	updated, err := service.UpdateDraft(t.Context(), created.Version.PublicID, update)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Slots) != 1 || updated.Slots[0].SlotKey != "seo" || updated.Slots[0].Sequence != 1 {
		t.Fatalf("unexpected update: %#v", updated)
	}
	if _, err := service.UpdateDraft(t.Context(), "00000000-0000-0000-0000-000000000000", update); !errors.Is(err, ErrTemplateVersionNotFound) {
		t.Fatalf("missing update error = %v", err)
	}
}

func assertIssueCodes(t *testing.T, issues []ValidationIssue, want ...string) {
	t.Helper()
	got := make([]string, 0, len(issues))
	for _, issue := range issues {
		got = append(got, issue.Code)
	}
	sort.Strings(got)
	sort.Strings(want)
	for _, code := range want {
		if !containsString(got, code) {
			t.Fatalf("issue codes = %v, want code %q (issues: %#v)", got, code, issues)
		}
	}
}

func containsString(values []string, value string) bool {
	return reflect.ValueOf(values).Len() > 0 && sort.SearchStrings(values, value) < len(values) && values[sort.SearchStrings(values, value)] == value
}
