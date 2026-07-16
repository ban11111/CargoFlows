package database

import (
	"fmt"
	"log"
	"os"
	"time"

	"cargoflow/api/internal/models"
	"cargoflow/api/internal/sop"
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
	if err := migrateAIContentTemplateSchema(db); err != nil {
		return err
	}
	return db.AutoMigrate(
		&models.User{},
		&models.Category{},
		&models.Tag{},
		&models.Product{},
		&models.SKU{},
		&models.InventoryAdjustment{},
		&models.CaptureSOP{},
		&models.SOPVersion{},
		&models.SOPView{},
		&models.SOPViewReferenceImage{},
		&models.PhotoSession{},
		&models.Asset{},
		&models.AssetReview{},
		&models.OpenAIProviderSetting{},
		&models.AIContentTemplate{},
		&models.AIContentTemplateVersion{},
		&models.AIContentSlot{},
		&models.AIJob{},
		&models.AIJobItem{},
		&models.AIExecution{},
		&models.AIAuditEvent{},
		&models.AIUsageLedger{},
	)
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
	if db.Dialector.Name() == "mysql" {
		return db.Exec(`UPDATE ai_content_template_versions AS older
			JOIN ai_content_template_versions AS newer
			  ON newer.ai_content_template_id = older.ai_content_template_id
			 AND newer.status = 'draft'
			 AND (newer.version_number > older.version_number
			      OR (newer.version_number = older.version_number AND newer.id > older.id))
			SET older.status = 'archived', older.archived_at = COALESCE(older.archived_at, CURRENT_TIMESTAMP), older.draft_guard = NULL
			WHERE older.status = 'draft'`).Error
	}
	return db.Exec(`UPDATE ai_content_template_versions AS older
		SET status = 'archived', archived_at = COALESCE(archived_at, CURRENT_TIMESTAMP), draft_guard = NULL
		WHERE status = 'draft' AND EXISTS (
			SELECT 1 FROM ai_content_template_versions AS newer
			WHERE newer.ai_content_template_id = older.ai_content_template_id
			AND newer.status = 'draft'
			AND (newer.version_number > older.version_number
			     OR (newer.version_number = older.version_number AND newer.id > older.id))
		)`).Error
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

	users := []models.User{
		{Name: "Zheng Baiyi", Email: "admin@cargoflow.local", PasswordHash: string(hash), Role: models.RoleAdmin, Status: "active", LastSeenAt: time.Now()},
		{Name: "Ivy Chen", Email: "ivy@cargoflow.local", PasswordHash: string(hash), Role: models.RoleOperator, Status: "active", LastSeenAt: time.Now()},
		{Name: "Bo Lin", Email: "bo@cargoflow.local", PasswordHash: string(hash), Role: models.RolePhotographer, Status: "active", LastSeenAt: time.Now()},
	}
	if err := db.Create(&users).Error; err != nil {
		return err
	}

	var phoneCase models.Category
	if err := db.Where("name = ?", "手机壳").First(&phoneCase).Error; err != nil {
		return err
	}
	product := models.Product{CategoryID: phoneCase.ID, Name: "透明手机壳", Brand: "CargoFlow", Category: phoneCase.Name, Description: "Internal seed product for SKU and photo workflow validation."}
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
			Required:        preset.Required,
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
