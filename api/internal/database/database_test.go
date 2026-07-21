package database

import (
	"bytes"
	"context"
	"log"
	"reflect"
	"strings"
	"testing"
	"time"

	"cargoflows/api/internal/models"
	"cargoflows/api/internal/sop"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type oldAIContentTemplate struct {
	ID             uint `gorm:"primaryKey"`
	PublicID       string
	NameZH         string
	NameEN         string
	TargetPlatform string
	Status         string
	CreatedByID    uint
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type oldUser struct {
	ID           uint `gorm:"primaryKey"`
	Name         string
	Email        string `gorm:"uniqueIndex"`
	PasswordHash string
	Role         string
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (oldUser) TableName() string { return "users" }

func TestBackfillBrandsGroupsNamesCaseInsensitivelyAndIsIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Brand{}, &models.Product{}); err != nil {
		t.Fatal(err)
	}
	products := []models.Product{{Name: "One", Brand: " CargoFlows "}, {Name: "Two", Brand: "cargoflows"}, {Name: "Unbranded", Brand: " "}}
	for index := range products {
		if err := db.Create(&products[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := backfillBrands(db); err != nil {
		t.Fatal(err)
	}
	if err := backfillBrands(db); err != nil {
		t.Fatal(err)
	}
	var brands int64
	if err := db.Model(&models.Brand{}).Count(&brands).Error; err != nil || brands != 1 {
		t.Fatalf("brand count = %d, %v", brands, err)
	}
	var linked int64
	if err := db.Model(&models.Product{}).Where("brand_id IS NOT NULL").Count(&linked).Error; err != nil || linked != 2 {
		t.Fatalf("linked product count = %d, %v", linked, err)
	}
	var blank models.Product
	if err := db.Where("name = ?", "Unbranded").First(&blank).Error; err != nil || blank.BrandID != nil {
		t.Fatalf("blank brand was linked: %#v, %v", blank.BrandID, err)
	}
}

func TestMigrationSeedsAIWorkerConcurrencyDefaultsIdempotently(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := migrateSchema(db); err != nil {
		t.Fatalf("second migration: %v", err)
	}
	var settings []models.AIWorkerSetting
	if err := db.Find(&settings).Error; err != nil {
		t.Fatal(err)
	}
	if len(settings) != 1 || settings[0].ID != 1 || settings[0].MaxWorkersPerJob != 3 || settings[0].MaxWorkersGlobal != 9 {
		t.Fatalf("worker settings = %#v", settings)
	}
}

func TestUserMigrationPromotesOneOwnerAndCollapsesLegacyRoles(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&oldUser{}); err != nil {
		t.Fatal(err)
	}
	legacy := []oldUser{
		{Name: "First admin", Email: "first@example.test", PasswordHash: "hash-one", Role: "admin", Status: "active"},
		{Name: "Second admin", Email: "second@example.test", PasswordHash: "hash-two", Role: "admin", Status: "active"},
		{Name: "Photographer", Email: "photo@example.test", PasswordHash: "hash-three", Role: "photographer", Status: "active"},
		{Name: "Viewer", Email: "viewer@example.test", PasswordHash: "hash-four", Role: "viewer", Status: "active"},
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateUserSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := migrateUserSchema(db); err != nil {
		t.Fatalf("second migration: %v", err)
	}
	var users []models.User
	if err := db.Order("id ASC").Find(&users).Error; err != nil {
		t.Fatal(err)
	}
	if users[0].Role != models.RoleSuperAdmin || users[1].Role != models.RoleAdmin || users[2].Role != models.RoleOperator || users[3].Role != models.RoleOperator {
		t.Fatalf("migrated roles = %q, %q, %q, %q", users[0].Role, users[1].Role, users[2].Role, users[3].Role)
	}
	for index, user := range users {
		if user.PublicID == "" || user.SessionVersion != 1 || user.MustChangePassword || user.PasswordHash != legacy[index].PasswordHash {
			t.Fatalf("migrated user %d = %#v", index, user)
		}
	}
	columns, err := db.Migrator().ColumnTypes(&models.User{})
	if err != nil {
		t.Fatal(err)
	}
	for _, column := range columns {
		if column.Name() == "last_seen_at" {
			nullable, ok := column.Nullable()
			if !ok || !nullable {
				t.Fatalf("last_seen_at nullable = %v, known = %v", nullable, ok)
			}
			return
		}
	}
	t.Fatal("last_seen_at column not found")
}

func TestUserMigrationRenamesLegacyAdministratorEmail(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&oldUser{}); err != nil {
		t.Fatal(err)
	}
	legacy := oldUser{
		Name:         "Administrator",
		Email:        "admin@" + "cargo" + "flow.local",
		PasswordHash: "unchanged-hash",
		Role:         "admin",
		Status:       "active",
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateUserSchema(db); err != nil {
		t.Fatal(err)
	}
	var migrated models.User
	if err := db.First(&migrated, legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if migrated.Email != "admin@cargoflows.cc" || migrated.PasswordHash != legacy.PasswordHash || migrated.ID != legacy.ID {
		t.Fatalf("migrated administrator = %#v", migrated)
	}
}

func (oldAIContentTemplate) TableName() string { return "ai_content_templates" }

type oldAIContentTemplateVersion struct {
	ID                    uint `gorm:"primaryKey"`
	PublicID              string
	AIContentTemplateID   uint `gorm:"uniqueIndex:idx_ai_template_version"`
	VersionNumber         int  `gorm:"uniqueIndex:idx_ai_template_version"`
	Status                string
	DefaultLocale         string
	PromptCompilerVersion string
	PlatformPrompt        string
	CreatedByID           uint
	PublishedAt           *time.Time
	ArchivedAt            *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

func (oldAIContentTemplateVersion) TableName() string { return "ai_content_template_versions" }

type oldAIContentSlot struct {
	ID                         uint `gorm:"primaryKey"`
	PublicID                   string
	AIContentTemplateVersionID uint   `gorm:"uniqueIndex:idx_ai_slot_key"`
	SlotKey                    string `gorm:"uniqueIndex:idx_ai_slot_key"`
	Kind                       string
	NameZH                     string
	NameEN                     string
	Sequence                   int
	PromptFragment             string
	ConstraintsJSON            []byte
	GenerationConfigJSON       []byte
	LayoutConfigJSON           []byte
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

func (oldAIContentSlot) TableName() string { return "ai_content_slots" }

func TestProductionLoggerRemovesSecretQueryParameters(t *testing.T) {
	var output bytes.Buffer
	configured := newProductionLogger(log.New(&output, "", 0))
	filter, ok := configured.(interface {
		ParamsFilter(context.Context, string, ...interface{}) (string, []interface{})
	})
	if !ok {
		t.Fatal("production logger does not expose a parameter filter")
	}

	const ciphertext = "ciphertext-secret-value"
	const nonce = "nonce-secret-value"
	sql, params := filter.ParamsFilter(t.Context(), "UPDATE open_ai_provider_settings SET encrypted_api_key=?, encryption_nonce=?", ciphertext, nonce)
	if len(params) != 0 {
		t.Fatalf("filtered params = %#v, want none", params)
	}
	configured.Warn(t.Context(), "query: %s params: %v", sql, params)
	if got := output.String(); strings.Contains(got, ciphertext) || strings.Contains(got, nonce) {
		t.Fatalf("logger emitted secret query parameters: %q", got)
	}
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestMigrateAddsAllowMultipleWithFalseDefault(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var defaultValue string
	if err := db.Raw(`SELECT dflt_value FROM pragma_table_info('sop_views') WHERE name = 'allow_multiple'`).Scan(&defaultValue).Error; err != nil {
		t.Fatal(err)
	}
	if defaultValue != "false" && defaultValue != "0" {
		t.Fatalf("allow_multiple default = %q, want false", defaultValue)
	}
}

func TestMigrateCreatesAIFoundationTables(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	for _, model := range []any{&models.OpenAIProviderSetting{}, &models.AIContentTemplate{}, &models.AIContentTemplateVersion{}, &models.AIContentSlot{}, &models.AIJob{}, &models.AIJobItem{}, &models.AIExecution{}, &models.AIAuditEvent{}, &models.AIUsageLedger{}} {
		if !db.Migrator().HasTable(model) {
			t.Fatalf("missing table for %T", model)
		}
	}
	for _, column := range []string{"l0_policy_version", "l1_product_context_version", "l2_template_version_public_id", "l3_content_slot_public_id", "normalized_input_json", "ordered_input_list_json"} {
		if !db.Migrator().HasColumn(&models.AIExecution{}, column) {
			t.Fatalf("missing AI execution provenance column %q", column)
		}
	}
	if db.Migrator().HasColumn(&models.AIJob{}, "input_asset_ids") {
		t.Fatal("legacy input_asset_ids compatibility field must not be persisted")
	}
	for _, column := range []string{"idempotency_key", "request_sha256"} {
		if !db.Migrator().HasColumn(&models.AIJob{}, column) {
			t.Fatalf("missing AI job column %q", column)
		}
	}
	for _, column := range []string{"created_by_snapshot_json", "model_snapshot_json"} {
		if !db.Migrator().HasColumn(&models.AIJob{}, column) {
			t.Fatalf("missing AI job audit column %q", column)
		}
	}
	for _, column := range []string{"requested_model", "actual_model", "api_mode", "failure_code"} {
		if !db.Migrator().HasColumn(&models.AIExecution{}, column) {
			t.Fatalf("missing AI execution audit column %q", column)
		}
	}
	if !db.Migrator().HasIndex(&models.AIJob{}, "idx_ai_job_actor_idempotency") {
		t.Fatal("missing actor-scoped AI job idempotency index")
	}
	if !db.Migrator().HasColumn(&models.AIContentTemplateVersion{}, "draft_guard") {
		t.Fatal("missing AI template version draft guard")
	}
	if !db.Migrator().HasIndex(&models.AIContentTemplateVersion{}, "idx_ai_template_draft_guard") {
		t.Fatal("missing unique AI template draft guard index")
	}
}

func TestMigrateBackfillsLegacyImageModelIntoCompatibleAPIMode(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	setting := models.OpenAIProviderSetting{Provider: "openai", EncryptedAPIKey: []byte("sealed"), EncryptionNonce: []byte("nonce"), EncryptionKeyVersion: "v1", KeyFingerprint: "TEST", Status: "active", TextModel: "gpt-5.6", ImageModel: "gpt-image-2", CreatedByID: 1, UpdatedByID: 1}
	if err := db.Create(&setting).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&setting).Updates(map[string]any{"image_api_mode": "", "image_responses_model": "", "image_generation_model": ""}).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&setting, setting.ID).Error; err != nil {
		t.Fatal(err)
	}
	if setting.ImageAPIMode != "images" || setting.ImageGenerationModel != "gpt-image-2" || setting.ImageResponsesModel != "gpt-5.6" {
		t.Fatalf("direct-image migration = %#v", setting)
	}
	if err := db.Model(&setting).Updates(map[string]any{"image_model": "gpt-5.6", "image_api_mode": "", "image_responses_model": "", "image_generation_model": ""}).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&setting, setting.ID).Error; err != nil {
		t.Fatal(err)
	}
	if setting.ImageAPIMode != "responses" || setting.ImageResponsesModel != "gpt-5.6" || setting.ImageGenerationModel != "gpt-image-2" {
		t.Fatalf("Responses migration = %#v", setting)
	}
}

func TestMigrateBackfillsLegacyBlankSOPViewPublicIDs(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	views := []models.SOPView{
		{PublicID: uuid.NewString(), SOPVersionID: 1, Sequence: 1, Role: models.SOPViewReferenceFront, ViewKind: models.SOPViewStandard, PresetKey: "reference_front", NameZH: "正面", NameEN: "Front", Required: true},
		{PublicID: uuid.NewString(), SOPVersionID: 1, Sequence: 2, Role: models.SOPViewCapture, ViewKind: models.SOPViewStandard, PresetKey: "back", NameZH: "背面", NameEN: "Back"},
	}
	if err := db.Create(&views).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropIndex(&models.SOPView{}, "idx_sop_views_public_id"); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.SOPView{}).Where("id IN ?", []uint{views[0].ID, views[1].ID}).Update("public_id", "").Error; err != nil {
		t.Fatal(err)
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("legacy migration failed: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("repeated migration failed: %v", err)
	}

	var migrated []models.SOPView
	if err := db.Where("id IN ?", []uint{views[0].ID, views[1].ID}).Order("id").Find(&migrated).Error; err != nil {
		t.Fatal(err)
	}
	if len(migrated) != 2 || migrated[0].PublicID == migrated[1].PublicID {
		t.Fatalf("migrated public IDs are not unique: %#v", migrated)
	}
	for _, view := range migrated {
		parsed, err := uuid.Parse(view.PublicID)
		if err != nil || parsed == uuid.Nil {
			t.Fatalf("invalid migrated public ID %q: %v", view.PublicID, err)
		}
	}
}

func TestRunWithMigrationLockHoldsDedicatedLockAroundNormalMigration(t *testing.T) {
	locked := false
	order := make([]string, 0, 3)
	err := runWithMigrationLock(func() (func() error, error) {
		locked = true
		order = append(order, "lock")
		return func() error {
			if !locked {
				t.Fatal("lock released twice")
			}
			locked = false
			order = append(order, "unlock")
			return nil
		}, nil
	}, func() error {
		if !locked {
			t.Fatal("migration ran without advisory lock")
		}
		order = append(order, "migrate")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(order, ","); got != "lock,migrate,unlock" {
		t.Fatalf("order = %q", got)
	}
}

func TestMigrateAIJobIdempotencyIndexIsActorScopedAndLegacySafe(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	key := "job-database-idem"
	newJob := func(public string, actor uint, key *string) models.AIJob {
		return models.AIJob{PublicID: public, SKUID: 1, AIContentTemplateVersionID: 1, TargetPlatform: "lazada", Locale: "zh-CN", Status: models.AIJobQueued, SnapshotSchema: "v1", InputSnapshotJSON: []byte(`{}`), CreatedByID: actor, IdempotencyKey: key, RequestSHA256: strings.Repeat("a", 64)}
	}
	if err := db.Create(&[]models.AIJob{newJob("job-legacy-1", 1, nil), newJob("job-legacy-2", 1, nil)}).Error; err != nil {
		t.Fatalf("multiple legacy null keys: %v", err)
	}
	if err := db.Create(&[]models.AIJob{newJob("job-key-1", 1, &key), newJob("job-key-2", 2, &key)}).Error; err != nil {
		t.Fatalf("same key different actors: %v", err)
	}
	duplicate := newJob("job-key-duplicate", 1, &key)
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("same actor/key must be unique")
	}
}

func TestMigrateUpgradesLegacyAITemplateLifecycleAndIndexes(t *testing.T) {
	db := openTestDB(t)
	if err := db.AutoMigrate(&oldAIContentTemplate{}, &oldAIContentTemplateVersion{}, &oldAIContentSlot{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	parents := []oldAIContentTemplate{
		{ID: 1, PublicID: "template-active", NameZH: "活动", NameEN: "Active", TargetPlatform: "lazada", Status: "published", CreatedByID: 1, CreatedAt: now, UpdatedAt: now},
		{ID: 2, PublicID: "template-archived", NameZH: "归档", NameEN: "Archived", TargetPlatform: "lazada", Status: "draft", CreatedByID: 1, CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&parents).Error; err != nil {
		t.Fatal(err)
	}
	versions := []oldAIContentTemplateVersion{
		{ID: 1, PublicID: "v1-published", AIContentTemplateID: 1, VersionNumber: 1, Status: "published", DefaultLocale: "zh-CN", PromptCompilerVersion: "v1", CreatedByID: 1, CreatedAt: now, UpdatedAt: now},
		{ID: 2, PublicID: "v2-old-draft", AIContentTemplateID: 1, VersionNumber: 2, Status: "draft", DefaultLocale: "zh-CN", PromptCompilerVersion: "v1", CreatedByID: 1, CreatedAt: now, UpdatedAt: now},
		{ID: 3, PublicID: "v3-kept-draft", AIContentTemplateID: 1, VersionNumber: 3, Status: "draft", DefaultLocale: "zh-CN", PromptCompilerVersion: "v1", CreatedByID: 1, CreatedAt: now, UpdatedAt: now},
		{ID: 4, PublicID: "v1-archived", AIContentTemplateID: 2, VersionNumber: 1, Status: "archived", DefaultLocale: "zh-CN", PromptCompilerVersion: "v1", CreatedByID: 1, ArchivedAt: &now, CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&versions).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&oldAIContentSlot{ID: 1, PublicID: "slot-1", AIContentTemplateVersionID: 1, SlotKey: "hero", Kind: "image", NameZH: "主图", NameEN: "Hero", Sequence: 1, ConstraintsJSON: []byte(`{}`), GenerationConfigJSON: []byte(`{}`), LayoutConfigJSON: []byte(`{}`), CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}

	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("repeated migration failed: %v", err)
	}
	var migratedParents []models.AIContentTemplate
	if err := db.Order("id").Find(&migratedParents).Error; err != nil {
		t.Fatal(err)
	}
	if migratedParents[0].Status != models.AIContentTemplateActive || migratedParents[1].Status != models.AIContentTemplateArchived {
		t.Fatalf("parent statuses = %q, %q", migratedParents[0].Status, migratedParents[1].Status)
	}
	var migratedVersions []models.AIContentTemplateVersion
	if err := db.Order("id").Find(&migratedVersions).Error; err != nil {
		t.Fatal(err)
	}
	if migratedVersions[1].Status != models.AITemplateArchived || migratedVersions[1].DraftGuard != nil || migratedVersions[1].ArchivedAt == nil {
		t.Fatalf("older draft was not safely archived: %#v", migratedVersions[1])
	}
	if migratedVersions[2].Status != models.AITemplateDraft || migratedVersions[2].DraftGuard == nil || *migratedVersions[2].DraftGuard != "draft" {
		t.Fatalf("latest draft was not guarded: %#v", migratedVersions[2])
	}
	duplicate := models.AIContentSlot{PublicID: "slot-duplicate", AIContentTemplateVersionID: 1, SlotKey: "hero", Kind: models.AIContentSlotImage, NameZH: "重复", NameEN: "Duplicate", Sequence: 2, ConstraintsJSON: []byte(`{}`), GenerationConfigJSON: []byte(`{}`), LayoutConfigJSON: []byte(`{}`)}
	if err := db.Create(&duplicate).Error; err != nil {
		t.Fatalf("non-unique migrated slot index rejected duplicate: %v", err)
	}
	result := db.Exec(`INSERT INTO ai_content_template_versions
		(public_id, ai_content_template_id, version_number, status, default_locale, prompt_compiler_version, platform_prompt, created_by_id, created_at, updated_at)
		VALUES (?, ?, ?, 'draft', 'zh-CN', 'v1', '', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, "unguarded", 2, 2)
	if result.Error == nil {
		t.Fatal("migrated database accepted an unguarded draft")
	}
}

func TestArchiveDuplicateLegacyDraftsMySQLSQLShape(t *testing.T) {
	sql := archiveDuplicateLegacyDraftsSQL("mysql")
	for _, fragment := range []string{
		"UPDATE ai_content_template_versions AS older",
		"JOIN ai_content_template_versions AS newer",
		"newer.ai_content_template_id = older.ai_content_template_id",
		"newer.version_number > older.version_number",
		"newer.id > older.id",
		"WHERE older.status = 'draft'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("MySQL duplicate-draft cleanup SQL missing %q:\n%s", fragment, sql)
		}
	}
}

func TestSeedCreatesPublishedPhoneCaseCaptureSOPFromExactPresets(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := Seed(db); err != nil {
		t.Fatal(err)
	}

	var version models.SOPVersion
	if err := db.Preload("Views", func(tx *gorm.DB) *gorm.DB { return tx.Order("sequence ASC") }).Where("status = ?", models.SOPVersionPublished).First(&version).Error; err != nil {
		t.Fatal(err)
	}
	if version.SchemaVersion != "1.0" || version.CoordinateSystem != "pcs_object_v1" {
		t.Fatalf("unexpected schema metadata: %#v", version)
	}
	if _, err := uuid.Parse(version.PublicID); err != nil {
		t.Fatalf("invalid version UUID: %v", err)
	}
	wantKeys := []string{"reference_front", "back", "left", "bottom", "right", "top", "detail_label", "packaging_front"}
	if len(version.Views) != len(wantKeys) {
		t.Fatalf("expected %d views, got %d", len(wantKeys), len(version.Views))
	}
	for index, key := range wantKeys {
		view := version.Views[index]
		if view.PresetKey != key || view.Sequence != index+1 {
			t.Fatalf("view %d: expected %q at sequence %d, got %#v", index, key, index+1, view)
		}
		if _, err := uuid.Parse(view.PublicID); err != nil {
			t.Fatalf("view %q has invalid UUID: %v", key, err)
		}
		preset, ok := sop.PresetByKey(key)
		if !ok {
			t.Fatalf("preset %q is missing", key)
		}
		pose, err := sop.CanonicalizePose(preset.CameraPosition, preset.ImageUp)
		if err != nil {
			t.Fatal(err)
		}
		if view.Role != preset.Role || view.ViewKind != preset.Kind || view.NameZH != preset.NameZH || view.NameEN != preset.NameEN || view.InstructionZH != preset.InstructionZH || view.InstructionEN != preset.InstructionEN || view.Required != preset.Required || view.AllowMultiple != preset.AllowMultiple || view.Composition != preset.Composition {
			t.Fatalf("view %q metadata drifted from preset", key)
		}
		if got := []float64{view.CameraPositionX, view.CameraPositionY, view.CameraPositionZ}; !reflect.DeepEqual(got, pose.CameraPosition[:]) {
			t.Fatalf("view %q camera position: got %v want %v", key, got, pose.CameraPosition)
		}
		if got := []float64{view.ImageUpX, view.ImageUpY, view.ImageUpZ}; !reflect.DeepEqual(got, pose.ImageUp[:]) {
			t.Fatalf("view %q image up: got %v want %v", key, got, pose.ImageUp)
		}
		if got := []float64{view.TargetX, view.TargetY, view.TargetZ}; !reflect.DeepEqual(got, preset.Target[:]) {
			t.Fatalf("view %q target: got %v want %v", key, got, preset.Target)
		}
	}
	if !version.Views[0].Required || version.Views[6].Required || version.Views[7].Required {
		t.Fatal("seed required flags do not match presets")
	}
}
