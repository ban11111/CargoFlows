package database

import (
	"fmt"
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
		Logger:                                   logger.Default.LogMode(logger.Warn),
		DisableForeignKeyConstraintWhenMigrating: true,
	})
}

func Migrate(db *gorm.DB) error {
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
