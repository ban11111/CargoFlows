package database

import (
	"strings"
	"time"

	"cargoflow/api/internal/models"
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
		&models.SOPTemplate{},
		&models.SOPView{},
		&models.PhotoSession{},
		&models.Asset{},
		&models.AssetReview{},
		&models.AIJob{},
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

	template := models.SOPTemplate{CategoryID: phoneCase.ID, Name: "手机壳电商标准拍摄", Category: phoneCase.Name, Status: "active"}
	if err := db.Create(&template).Error; err != nil {
		return err
	}

	views := []models.SOPView{
		{SOPTemplateID: template.ID, Name: "Front", SortOrder: 1, Required: true, Prompt: "商品正面居中，保留完整轮廓。"},
		{SOPTemplateID: template.ID, Name: "Back", SortOrder: 2, Required: true, Prompt: "商品背面居中，避免反光。"},
		{SOPTemplateID: template.ID, Name: "Left", SortOrder: 3, Required: true, Prompt: "左侧 90 度视角。"},
		{SOPTemplateID: template.ID, Name: "Right", SortOrder: 4, Required: true, Prompt: "右侧 90 度视角。"},
		{SOPTemplateID: template.ID, Name: "Label", SortOrder: 5, Required: true, Prompt: "拍清标签、条码和材质信息。"},
	}
	return db.Create(&views).Error
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

	// Existing user-created categories predate bilingual fields. Preserve their
	// value as an English fallback until an operator provides a localized name.
	var categories []models.Category
	if err := db.Find(&categories).Error; err != nil {
		return err
	}
	for _, category := range categories {
		if strings.TrimSpace(category.NameEN) == "" {
			if err := db.Model(&category).Update("name_en", category.Name).Error; err != nil {
				return err
			}
		}
	}

	var products []models.Product
	if err := db.Find(&products).Error; err != nil {
		return err
	}
	for _, product := range products {
		if product.CategoryID != 0 || strings.TrimSpace(product.Category) == "" {
			continue
		}
		category, err := ensureCategory(db, product.Category)
		if err != nil {
			return err
		}
		if err := db.Model(&product).Update("category_id", category.ID).Error; err != nil {
			return err
		}
	}

	var templates []models.SOPTemplate
	if err := db.Find(&templates).Error; err != nil {
		return err
	}
	for _, template := range templates {
		if template.CategoryID != 0 || strings.TrimSpace(template.Category) == "" {
			continue
		}
		category, err := ensureCategory(db, template.Category)
		if err != nil {
			return err
		}
		if err := db.Model(&template).Update("category_id", category.ID).Error; err != nil {
			return err
		}
	}
	return nil
}

func ensureCategory(db *gorm.DB, name string) (models.Category, error) {
	category := models.Category{Name: strings.TrimSpace(name), NameEN: strings.TrimSpace(name)}
	err := db.Where("name = ?", category.Name).FirstOrCreate(&category).Error
	return category, err
}
