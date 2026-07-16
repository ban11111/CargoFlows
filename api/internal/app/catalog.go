package app

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"cargoflow/api/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type categorySummary struct {
	ID              uint   `json:"id"`
	Name            string `json:"name"`
	NameEN          string `json:"name_en"`
	IsSystem        bool   `json:"is_system"`
	SKUCount        int64  `json:"sku_count"`
	CaptureSOPCount int64  `json:"capture_sop_count"`
}

func (s *Server) listCategories(c *gin.Context) {
	var categories []models.Category
	if err := s.db.Order("is_system DESC, name ASC").Find(&categories).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	result := make([]categorySummary, 0, len(categories))
	for _, category := range categories {
		var skuCount, captureSOPCount int64
		_ = s.db.Model(&models.Product{}).Where("category_id = ?", category.ID).Count(&skuCount).Error
		_ = s.db.Model(&models.CaptureSOP{}).Where("category_id = ?", category.ID).Count(&captureSOPCount).Error
		result = append(result, categorySummary{
			ID:              category.ID,
			Name:            category.Name,
			NameEN:          category.NameEN,
			IsSystem:        category.IsSystem,
			SKUCount:        skuCount,
			CaptureSOPCount: captureSOPCount,
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

type categoryRequest struct {
	Name   string `json:"name" binding:"required"`
	NameEN string `json:"name_en" binding:"required"`
}

func (s *Server) createCategory(c *gin.Context) {
	var req categoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	name := strings.TrimSpace(req.Name)
	nameEN := strings.TrimSpace(req.NameEN)
	if name == "" || nameEN == "" || len([]rune(name)) > 120 || len([]rune(nameEN)) > 120 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid category name"})
		return
	}

	category := models.Category{Name: name, NameEN: nameEN}
	if err := s.db.Create(&category).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"message": "category already exists"})
		return
	}
	c.JSON(http.StatusCreated, category)
}

func (s *Server) deleteCategory(c *gin.Context) {
	var category models.Category
	if err := s.db.First(&category, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "category not found"})
		return
	}
	if category.IsSystem {
		c.JSON(http.StatusForbidden, gin.H{"message": "system category cannot be deleted"})
		return
	}

	var productCount, captureSOPCount int64
	_ = s.db.Model(&models.Product{}).Where("category_id = ?", category.ID).Count(&productCount).Error
	_ = s.db.Model(&models.CaptureSOP{}).Where("category_id = ?", category.ID).Count(&captureSOPCount).Error
	if productCount > 0 || captureSOPCount > 0 {
		c.JSON(http.StatusConflict, gin.H{"message": "category is in use by SKU or capture SOPs"})
		return
	}
	if err := s.db.Delete(&category).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) listTags(c *gin.Context) {
	var tags []models.Tag
	if err := s.db.Order("name ASC").Find(&tags).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tags})
}

func (s *Server) resolveCategory(db *gorm.DB, categoryID uint, categoryName string) (models.Category, error) {
	var category models.Category
	if categoryID != 0 {
		if err := db.First(&category, categoryID).Error; err != nil {
			return category, fmt.Errorf("category not found")
		}
		return category, nil
	}

	name := strings.TrimSpace(categoryName)
	if name == "" {
		return category, fmt.Errorf("category is required")
	}
	if err := db.Where("name = ?", name).First(&category).Error; err != nil {
		return category, fmt.Errorf("category not found")
	}
	return category, nil
}

func resolveTags(db *gorm.DB, rawTags []string) ([]models.Tag, error) {
	seen := make(map[string]struct{})
	tags := make([]models.Tag, 0, len(rawTags))
	for _, rawTag := range rawTags {
		name := strings.TrimSpace(rawTag)
		if name == "" {
			continue
		}
		if len([]rune(name)) > 80 {
			return nil, fmt.Errorf("tag is too long")
		}
		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		tag := models.Tag{Name: name}
		if err := db.Where("name = ?", name).FirstOrCreate(&tag).Error; err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, nil
}

type assetReviewHierarchyAsset struct {
	ID               uint              `json:"id"`
	OriginalURL      string            `json:"original_url"`
	ThumbnailURL     string            `json:"thumbnail_url"`
	ReviewStatus     string            `json:"review_status"`
	CapturedAt       string            `json:"captured_at"`
	SOPViewName      localizedViewName `json:"sop_view_name"`
	PhotoSessionCode string            `json:"photo_session_code"`
}

type localizedViewName struct {
	ZHCN string `json:"zh-CN"`
	EN   string `json:"en"`
}

type assetReviewHierarchySKU struct {
	ID          uint                        `json:"id"`
	Code        string                      `json:"code"`
	ProductName string                      `json:"product_name"`
	Tags        []models.Tag                `json:"tags"`
	Assets      []assetReviewHierarchyAsset `json:"assets"`
}

type assetReviewHierarchyCategory struct {
	ID       uint                      `json:"id"`
	Name     string                    `json:"name"`
	NameEN   string                    `json:"name_en"`
	IsSystem bool                      `json:"is_system"`
	SKUs     []assetReviewHierarchySKU `json:"skus"`
}

func (s *Server) listAssetReviewHierarchy(c *gin.Context) {
	var assets []models.Asset
	query := s.db.Preload("SKU.Product.CatalogCategory").Preload("SKU.Tags").Preload("SOPView").Preload("PhotoSession").Order("captured_at DESC")
	if status := c.Query("status"); status != "" {
		query = query.Where("review_status = ?", status)
	}
	if err := query.Find(&assets).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	categories := make([]assetReviewHierarchyCategory, 0)
	categoryIndex := make(map[uint]int)
	skuIndex := make(map[uint]map[uint]int)
	for _, asset := range assets {
		category := asset.SKU.Product.CatalogCategory
		if category.ID == 0 {
			category = models.Category{Name: asset.SKU.Product.Category}
		}
		categoryKey := category.ID
		if _, exists := categoryIndex[categoryKey]; !exists {
			categoryIndex[categoryKey] = len(categories)
			skuIndex[categoryKey] = make(map[uint]int)
			categories = append(categories, assetReviewHierarchyCategory{
				ID: category.ID, Name: category.Name, NameEN: category.NameEN, IsSystem: category.IsSystem,
			})
		}
		categoryPosition := categoryIndex[categoryKey]
		if _, exists := skuIndex[categoryKey][asset.SKU.ID]; !exists {
			skuIndex[categoryKey][asset.SKU.ID] = len(categories[categoryPosition].SKUs)
			categories[categoryPosition].SKUs = append(categories[categoryPosition].SKUs, assetReviewHierarchySKU{
				ID: asset.SKU.ID, Code: asset.SKU.Code, ProductName: asset.SKU.Product.Name, Tags: asset.SKU.Tags,
			})
		}
		skuPosition := skuIndex[categoryKey][asset.SKU.ID]
		categories[categoryPosition].SKUs[skuPosition].Assets = append(categories[categoryPosition].SKUs[skuPosition].Assets, assetReviewHierarchyAsset{
			ID: asset.ID, OriginalURL: asset.OriginalURL, ThumbnailURL: asset.ThumbnailURL,
			ReviewStatus: asset.ReviewStatus, CapturedAt: asset.CapturedAt.Format(time.RFC3339),
			SOPViewName: localizedViewName{ZHCN: asset.SOPView.NameZH, EN: asset.SOPView.NameEN}, PhotoSessionCode: asset.PhotoSession.Code,
		})
	}

	sort.Slice(categories, func(i, j int) bool { return categories[i].Name < categories[j].Name })
	for index := range categories {
		sort.Slice(categories[index].SKUs, func(i, j int) bool { return categories[index].SKUs[i].Code < categories[index].SKUs[j].Code })
	}
	c.JSON(http.StatusOK, gin.H{"data": categories})
}
