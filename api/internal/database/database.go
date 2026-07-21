package database

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"cargoflows/api/internal/models"
	"cargoflows/api/internal/sop"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func Open(dsn string) (*gorm.DB, error) {
	return gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger:                                   newProductionLogger(log.New(os.Stdout, "\r\n", log.LstdFlags)),
		DisableForeignKeyConstraintWhenMigrating: true,
	})
}

func newProductionLogger(writer logger.Writer) logger.Interface {
	return logger.New(writer, logger.Config{
		SlowThreshold:             200 * time.Millisecond,
		Colorful:                  true,
		IgnoreRecordNotFoundError: false,
		ParameterizedQueries:      true,
		LogLevel:                  logger.Warn,
	})
}

func Migrate(db *gorm.DB) error {
	if db.Dialector.Name() != "mysql" {
		return migrateSchema(db)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("access database pool for schema migration: %w", err)
	}
	ctx := db.Statement.Context
	if ctx == nil {
		ctx = context.Background()
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("reserve schema migration lock connection: %w", err)
	}
	defer conn.Close()
	return runWithMigrationLock(func() (func() error, error) {
		var acquired int
		if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", "cargoflows_schema_migrate", 60).Scan(&acquired); err != nil {
			return nil, fmt.Errorf("acquire schema migration lock: %w", err)
		}
		if acquired != 1 {
			return nil, fmt.Errorf("acquire schema migration lock: timed out")
		}
		return func() error {
			var released int
			if err := conn.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", "cargoflows_schema_migrate").Scan(&released); err != nil {
				return fmt.Errorf("release schema migration lock: %w", err)
			}
			if released != 1 {
				return fmt.Errorf("release schema migration lock: lock was not held")
			}
			return nil
		}, nil
	}, func() error { return migrateSchema(db) })
}

func runWithMigrationLock(acquire func() (func() error, error), migrate func() error) error {
	unlock, err := acquire()
	if err != nil {
		return err
	}
	migrateErr := migrate()
	return errors.Join(migrateErr, unlock())
}

func migrateSchema(db *gorm.DB) error {
	if err := migrateUserSchema(db); err != nil {
		return err
	}
	if err := backfillLegacyPublicIDs(db); err != nil {
		return err
	}
	if err := migrateAIContentTemplateSchema(db); err != nil {
		return err
	}
	if err := migrateAITextSchema(db); err != nil {
		return err
	}
	if err := db.AutoMigrate(
		&models.User{},
		&models.Category{},
		&models.Tag{},
		&models.Brand{},
		&models.BrandIcon{},
		&models.BrandIconUpload{},
		&models.Product{},
		&models.SKU{},
		&models.ModelFamily{},
		&models.ModelFamilyMember{},
		&models.VariantIdentityManifest{},
		&models.VariantIdentityManifestVersion{},
		&models.VariantDifferenceRegion{},
		&models.VariantDifferenceRegionEvidenceAsset{},
		&models.StyleReferenceGrant{},
		&models.ModelFamilyReferenceAsset{},
		&models.InventoryAdjustment{},
		&models.CaptureSOP{},
		&models.SOPVersion{},
		&models.SOPView{},
		&models.SOPViewReferenceImage{},
		&models.SOPReferenceUpload{},
		&models.PhotoSession{},
		&models.Asset{},
		&models.AssetReview{},
		&models.OpenAIProviderSetting{},
		&models.AIContentTemplate{},
		&models.AIContentTemplateVersion{},
		&models.AIContentSlot{},
		&models.AIJob{},
		&models.AIJobItem{},
		&models.AIImageThread{},
		&models.AIImageTurn{},
		&models.AIExecution{},
		&models.AIImageResult{},
		&models.AIAuditEvent{},
		&models.AIUsageLedger{},
		&models.AITextResult{},
		&models.SKUPlatformContent{},
		&models.SKUPlatformContentRevision{},
	); err != nil {
		return err
	}
	if err := backfillBrands(db); err != nil {
		return err
	}
	return backfillAIModelConfiguration(db)
}

func backfillBrands(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var products []models.Product
		if err := tx.Where("brand_id IS NULL AND TRIM(brand) <> ''").Find(&products).Error; err != nil {
			return err
		}
		for _, product := range products {
			name := strings.TrimSpace(product.Brand)
			key := strings.ToLower(name)
			var brand models.Brand
			if err := tx.Where("name_key = ?", key).First(&brand).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				brand = models.Brand{PublicID: uuid.NewString(), Name: name, NameKey: key}
				if err := tx.Create(&brand).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
			if err := tx.Model(&models.Product{}).Where("id = ?", product.ID).Updates(map[string]any{"brand_id": brand.ID, "brand": brand.Name}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func backfillAIModelConfiguration(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var settings []models.OpenAIProviderSetting
		if err := tx.Find(&settings).Error; err != nil {
			return err
		}
		for _, setting := range settings {
			mode := setting.ImageAPIMode
			responsesModel := setting.ImageResponsesModel
			generationModel := setting.ImageGenerationModel
			legacy := setting.ImageModel
			if strings.HasPrefix(strings.ToLower(legacy), "gpt-image-") {
				mode = "images"
				generationModel = legacy
			} else if legacy != "" {
				mode = "responses"
				responsesModel = legacy
			}
			if mode == "" {
				mode = "responses"
			}
			if responsesModel == "" {
				responsesModel = "gpt-5.6"
			}
			if generationModel == "" {
				generationModel = "gpt-image-2"
			}
			if err := tx.Model(&models.OpenAIProviderSetting{}).Where("id = ?", setting.ID).Updates(map[string]any{"image_api_mode": mode, "image_responses_model": responsesModel, "image_generation_model": generationModel}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func migrateUserSchema(db *gorm.DB) error {
	if err := db.AutoMigrate(&models.User{}); err != nil {
		return fmt.Errorf("migrate users: %w", err)
	}
	return db.Transaction(func(tx *gorm.DB) error {
		legacyAdminEmail := "admin@" + "cargo" + "flow.local"
		const adminEmail = "admin@cargoflows.cc"
		var legacyAdmin models.User
		if err := tx.Where("email = ?", legacyAdminEmail).First(&legacyAdmin).Error; err == nil {
			var existing int64
			if err := tx.Model(&models.User{}).Where("email = ? AND id <> ?", adminEmail, legacyAdmin.ID).Count(&existing).Error; err != nil {
				return err
			}
			if existing > 0 {
				return fmt.Errorf("migrate administrator email: %s is already in use", adminEmail)
			}
			if err := tx.Model(&legacyAdmin).Update("email", adminEmail).Error; err != nil {
				return fmt.Errorf("migrate administrator email: %w", err)
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("find legacy administrator: %w", err)
		}

		var users []models.User
		if err := tx.Order("id ASC").Find(&users).Error; err != nil {
			return err
		}
		for index := range users {
			updates := map[string]any{}
			if users[index].PublicID == "" {
				updates["public_id"] = uuid.NewString()
			}
			if users[index].SessionVersion == 0 {
				updates["session_version"] = 1
			}
			if users[index].Role == models.RolePhotographer || users[index].Role == models.RoleViewer {
				updates["role"] = models.RoleOperator
				users[index].Role = models.RoleOperator
			}
			if len(updates) > 0 {
				if err := tx.Model(&models.User{}).Where("id = ?", users[index].ID).Updates(updates).Error; err != nil {
					return err
				}
			}
		}

		var superAdmins []models.User
		if err := tx.Where("role = ?", models.RoleSuperAdmin).Order("id ASC").Find(&superAdmins).Error; err != nil {
			return err
		}
		if len(superAdmins) == 0 {
			var oldestAdmin models.User
			if err := tx.Where("role = ?", models.RoleAdmin).Order("id ASC").First(&oldestAdmin).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) && len(users) == 0 {
					return nil
				}
				return fmt.Errorf("select initial super admin: %w", err)
			}
			return tx.Model(&oldestAdmin).Update("role", models.RoleSuperAdmin).Error
		}
		for _, duplicate := range superAdmins[1:] {
			if err := tx.Model(&duplicate).Update("role", models.RoleAdmin).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func migrateAITextSchema(db *gorm.DB) error {
	if db.Migrator().HasIndex(&models.AIAuditEvent{}, "idx_ai_audit_execution_event") {
		if err := db.Migrator().DropIndex(&models.AIAuditEvent{}, "idx_ai_audit_execution_event"); err != nil {
			return err
		}
	}
	if db.Migrator().HasTable(&models.SKUPlatformContent{}) && db.Migrator().HasColumn("sku_platform_contents", "skuid") && !db.Migrator().HasColumn("sku_platform_contents", "sku_id") {
		if db.Migrator().HasIndex(&models.SKUPlatformContent{}, "idx_sku_platform_content") {
			if err := db.Migrator().DropIndex(&models.SKUPlatformContent{}, "idx_sku_platform_content"); err != nil {
				return err
			}
		}
		if err := db.Migrator().RenameColumn("sku_platform_contents", "skuid", "sku_id"); err != nil {
			return err
		}
	}
	return nil
}

func backfillLegacyPublicIDs(db *gorm.DB) error {
	for _, table := range []string{
		"capture_sops",
		"sop_versions",
		"sop_views",
		"sop_view_reference_images",
		"photo_sessions",
	} {
		if !db.Migrator().HasTable(table) || !db.Migrator().HasColumn(table, "public_id") {
			continue
		}
		var ids []uint
		if err := db.Table(table).Where("public_id IS NULL OR TRIM(public_id) = ''").Pluck("id", &ids).Error; err != nil {
			return fmt.Errorf("find legacy blank public IDs in %s: %w", table, err)
		}
		for _, id := range ids {
			if err := db.Table(table).Where("id = ? AND (public_id IS NULL OR TRIM(public_id) = '')", id).Update("public_id", uuid.NewString()).Error; err != nil {
				return fmt.Errorf("backfill legacy public ID in %s: %w", table, err)
			}
		}
	}
	return nil
}

func migrateAIContentTemplateSchema(db *gorm.DB) error {
	templateModels := []any{&models.AIContentTemplate{}, &models.AIContentTemplateVersion{}, &models.AIContentSlot{}}
	if !db.Migrator().HasTable(&models.AIContentTemplate{}) ||
		!db.Migrator().HasTable(&models.AIContentTemplateVersion{}) ||
		!db.Migrator().HasTable(&models.AIContentSlot{}) {
		return db.AutoMigrate(templateModels...)
	}

	// Remove lifecycle/index constraints before normalizing legacy data. These
	// operations are idempotent so interrupted upgrades can safely resume.
	if db.Migrator().HasIndex(&models.AIContentTemplateVersion{}, "idx_ai_template_draft_guard") {
		if err := db.Migrator().DropIndex(&models.AIContentTemplateVersion{}, "idx_ai_template_draft_guard"); err != nil {
			return err
		}
	}
	if db.Migrator().HasIndex(&models.AIContentSlot{}, "idx_ai_slot_key") {
		if err := db.Migrator().DropIndex(&models.AIContentSlot{}, "idx_ai_slot_key"); err != nil {
			return err
		}
	}
	if !db.Migrator().HasColumn(&models.AIContentTemplateVersion{}, "draft_guard") {
		if err := db.Migrator().AddColumn(&models.AIContentTemplateVersion{}, "DraftGuard"); err != nil {
			return err
		}
	}

	if err := archiveDuplicateLegacyDrafts(db); err != nil {
		return err
	}
	if err := db.Exec(`UPDATE ai_content_template_versions
		SET draft_guard = CASE WHEN status = 'draft' THEN 'draft' ELSE NULL END`).Error; err != nil {
		return err
	}
	if err := db.Exec(`UPDATE ai_content_templates
		SET status = CASE WHEN EXISTS (
			SELECT 1 FROM ai_content_template_versions
			WHERE ai_content_template_versions.ai_content_template_id = ai_content_templates.id
			AND ai_content_template_versions.status IN ('draft', 'published')
		) THEN 'active' ELSE 'archived' END`).Error; err != nil {
		return err
	}

	// AutoMigrate now sees normalized rows, recreates the draft uniqueness guard
	// and named check constraint, and recreates slot-key lookup as non-unique.
	return db.AutoMigrate(templateModels...)
}

func archiveDuplicateLegacyDrafts(db *gorm.DB) error {
	return db.Exec(archiveDuplicateLegacyDraftsSQL(db.Dialector.Name())).Error
}

func archiveDuplicateLegacyDraftsSQL(dialect string) string {
	if dialect == "mysql" {
		return `UPDATE ai_content_template_versions AS older
			JOIN ai_content_template_versions AS newer
			  ON newer.ai_content_template_id = older.ai_content_template_id
			 AND newer.status = 'draft'
			 AND (newer.version_number > older.version_number
			      OR (newer.version_number = older.version_number AND newer.id > older.id))
			SET older.status = 'archived', older.archived_at = COALESCE(older.archived_at, CURRENT_TIMESTAMP), older.draft_guard = NULL
			WHERE older.status = 'draft'`
	}
	return `UPDATE ai_content_template_versions AS older
		SET status = 'archived', archived_at = COALESCE(archived_at, CURRENT_TIMESTAMP), draft_guard = NULL
		WHERE status = 'draft' AND EXISTS (
			SELECT 1 FROM ai_content_template_versions AS newer
			WHERE newer.ai_content_template_id = older.ai_content_template_id
			AND newer.status = 'draft'
			AND (newer.version_number > older.version_number
			     OR (newer.version_number = older.version_number AND newer.id > older.id))
		)`
}

func Seed(db *gorm.DB) error {
	if err := seedCatalog(db); err != nil {
		return err
	}

	var count int64
	if err := db.Model(&models.User{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	lastSeenAt := time.Now()
	users := []models.User{
		{Name: "Zheng Baiyi", Email: "admin@cargoflows.cc", PasswordHash: string(hash), Role: models.RoleSuperAdmin, Status: "active", LastSeenAt: &lastSeenAt},
		{Name: "Ivy Chen", Email: "ivy@cargoflows.local", PasswordHash: string(hash), Role: models.RoleOperator, Status: "active", LastSeenAt: &lastSeenAt},
		{Name: "Bo Lin", Email: "bo@cargoflows.local", PasswordHash: string(hash), Role: models.RoleOperator, Status: "active", LastSeenAt: &lastSeenAt},
	}
	if err := db.Create(&users).Error; err != nil {
		return err
	}

	var phoneCase models.Category
	if err := db.Where("name = ?", "手机壳").First(&phoneCase).Error; err != nil {
		return err
	}
	product := models.Product{CategoryID: phoneCase.ID, Name: "透明手机壳", Brand: "CargoFlows", Category: phoneCase.Name, Description: "Internal seed product for SKU and photo workflow validation."}
	if err := db.Create(&product).Error; err != nil {
		return err
	}

	sku := models.SKU{
		ProductID:         product.ID,
		Code:              "CF-CASE-CLR-IP17",
		Color:             "透明",
		Size:              "iPhone 17 Pro",
		Stock:             18,
		LowStockThreshold: 20,
		PlatformTitle:     "iPhone 17 Pro 透明防摔手机壳",
		Status:            "active",
	}
	if err := db.Create(&sku).Error; err != nil {
		return err
	}

	captureSOP := models.CaptureSOP{PublicID: uuid.NewString(), CategoryID: phoneCase.ID, CreatedByID: users[0].ID}
	if err := db.Create(&captureSOP).Error; err != nil {
		return err
	}
	publishedAt := time.Now()
	version := models.SOPVersion{
		PublicID: uuid.NewString(), CaptureSOPID: captureSOP.ID, VersionNumber: 1,
		SchemaVersion: "1.0", NameZH: "手机壳商品拍摄视图", NameEN: "Phone Case Product Capture Views",
		DescriptionZH: "手机壳标准商品拍摄视图。", DescriptionEN: "Standard product capture views for phone cases.",
		Status: models.SOPVersionPublished, CoordinateSystem: "pcs_object_v1", PublishedAt: &publishedAt,
	}
	if err := db.Create(&version).Error; err != nil {
		return err
	}
	// Keep the seeded published version immutable in behavior. Administrators can
	// copy it and add newer presets such as supplemental_info to the draft.
	for index, key := range []string{"reference_front", "back", "left", "bottom", "right", "top", "detail_label", "packaging_front"} {
		preset, ok := sop.PresetByKey(key)
		if !ok {
			return fmt.Errorf("capture SOP preset %q is unavailable", key)
		}
		pose, err := sop.CanonicalizePose(preset.CameraPosition, preset.ImageUp)
		if err != nil {
			return err
		}
		view := models.SOPView{
			PublicID: uuid.NewString(), SOPVersionID: version.ID, Sequence: index + 1,
			Role: preset.Role, ViewKind: preset.Kind, PresetKey: key,
			NameZH: preset.NameZH, NameEN: preset.NameEN, InstructionZH: preset.InstructionZH, InstructionEN: preset.InstructionEN,
			Required: preset.Required, AllowMultiple: preset.AllowMultiple,
			CameraPositionX: pose.CameraPosition[0], CameraPositionY: pose.CameraPosition[1], CameraPositionZ: pose.CameraPosition[2],
			ImageUpX: pose.ImageUp[0], ImageUpY: pose.ImageUp[1], ImageUpZ: pose.ImageUp[2],
			TargetX: preset.Target[0], TargetY: preset.Target[1], TargetZ: preset.Target[2], Composition: preset.Composition,
		}
		if err := db.Create(&view).Error; err != nil {
			return err
		}
	}

	return nil
}

func seedCatalog(db *gorm.DB) error {
	defaults := []struct {
		name   string
		nameEN string
	}{
		{name: "手机壳", nameEN: "Phone Case"},
		{name: "数据线", nameEN: "Data Cable"},
		{name: "挂绳", nameEN: "Lanyard"},
		{name: "手机贴膜", nameEN: "Screen Protector"},
	}
	for _, item := range defaults {
		category := models.Category{Name: item.name, NameEN: item.nameEN, IsSystem: true}
		if err := db.Where("name = ?", item.name).FirstOrCreate(&category).Error; err != nil {
			return err
		}
		if !category.IsSystem || category.NameEN != item.nameEN {
			if err := db.Model(&category).Updates(map[string]any{"is_system": true, "name_en": item.nameEN}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
