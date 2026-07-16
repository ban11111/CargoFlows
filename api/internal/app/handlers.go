package app

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cargoflow/api/internal/models"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type loginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

func (s *Server) login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	var user models.User
	if err := s.db.Where("email = ? AND status = ?", req.Email, "active").First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "invalid credentials"})
		return
	}

	user.LastSeenAt = time.Now()
	_ = s.db.Save(&user).Error

	token, err := s.issueToken(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "issue token failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": token, "user": user})
}

type createSKURequest struct {
	CategoryID        uint     `json:"category_id"`
	ProductName       string   `json:"product_name" binding:"required"`
	Brand             string   `json:"brand"`
	Category          string   `json:"category"`
	Code              string   `json:"code" binding:"required"`
	Color             string   `json:"color"`
	Size              string   `json:"size"`
	Barcode           string   `json:"barcode"`
	Stock             int      `json:"stock"`
	LowStockThreshold int      `json:"low_stock_threshold"`
	PlatformTitle     string   `json:"platform_title"`
	SellingPoints     string   `json:"selling_points"`
	Status            string   `json:"status"`
	Tags              []string `json:"tags"`
}

func (s *Server) listSKUs(c *gin.Context) {
	var skus []models.SKU
	query := s.db.Preload("Product.CatalogCategory").Preload("Tags").Joins("JOIN products ON products.id = skus.product_id").Order("skus.updated_at DESC")
	if categoryID := c.Query("category_id"); categoryID != "" {
		query = query.Where("products.category_id = ?", categoryID)
	}
	if search := c.Query("q"); search != "" {
		like := "%" + search + "%"
		query = query.Where("skus.code LIKE ? OR products.name LIKE ? OR products.category LIKE ? OR EXISTS (SELECT 1 FROM sku_tags JOIN tags ON tags.id = sku_tags.tag_id WHERE sku_tags.sku_id = skus.id AND tags.name LIKE ?)", like, like, like, like)
	}
	if err := query.Find(&skus).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": skus})
}

func (s *Server) createSKU(c *gin.Context) {
	var req createSKURequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	if req.Status == "" {
		req.Status = "active"
	}

	var sku models.SKU
	err := s.db.Transaction(func(tx *gorm.DB) error {
		category, err := s.resolveCategory(tx, req.CategoryID, req.Category)
		if err != nil {
			return err
		}
		product := models.Product{CategoryID: category.ID, Name: req.ProductName, Brand: req.Brand, Category: category.Name}
		if err := tx.Create(&product).Error; err != nil {
			return err
		}
		sku = models.SKU{
			ProductID:         product.ID,
			Code:              req.Code,
			Color:             req.Color,
			Size:              req.Size,
			Barcode:           req.Barcode,
			Stock:             req.Stock,
			LowStockThreshold: req.LowStockThreshold,
			PlatformTitle:     req.PlatformTitle,
			SellingPoints:     req.SellingPoints,
			Status:            req.Status,
		}
		if err := tx.Create(&sku).Error; err != nil {
			return err
		}
		tags, err := resolveTags(tx, req.Tags)
		if err != nil {
			return err
		}
		if err := tx.Model(&sku).Association("Tags").Replace(tags); err != nil {
			return err
		}
		return tx.Preload("Product.CatalogCategory").Preload("Tags").First(&sku, sku.ID).Error
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, sku)
}

func (s *Server) getSKU(c *gin.Context) {
	var sku models.SKU
	if err := s.db.Preload("Product.CatalogCategory").Preload("Tags").First(&sku, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "sku not found"})
		return
	}
	c.JSON(http.StatusOK, sku)
}

func (s *Server) updateSKU(c *gin.Context) {
	var sku models.SKU
	if err := s.db.Preload("Product.CatalogCategory").Preload("Tags").First(&sku, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "sku not found"})
		return
	}

	var req createSKURequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	err := s.db.Transaction(func(tx *gorm.DB) error {
		category, err := s.resolveCategory(tx, req.CategoryID, req.Category)
		if err != nil {
			return err
		}
		sku.Code = req.Code
		sku.Color = req.Color
		sku.Size = req.Size
		sku.Barcode = req.Barcode
		sku.LowStockThreshold = req.LowStockThreshold
		sku.PlatformTitle = req.PlatformTitle
		sku.SellingPoints = req.SellingPoints
		if req.Status != "" {
			sku.Status = req.Status
		}
		if err := tx.Save(&sku).Error; err != nil {
			return err
		}

		sku.Product.Name = req.ProductName
		sku.Product.Brand = req.Brand
		sku.Product.CategoryID = category.ID
		sku.Product.Category = category.Name
		if err := tx.Save(&sku.Product).Error; err != nil {
			return err
		}
		if req.Tags != nil {
			tags, err := resolveTags(tx, req.Tags)
			if err != nil {
				return err
			}
			return tx.Model(&sku).Association("Tags").Replace(tags)
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	_ = s.db.Preload("Product.CatalogCategory").Preload("Tags").First(&sku, sku.ID).Error
	c.JSON(http.StatusOK, sku)
}

type inventoryAdjustmentRequest struct {
	QuantityDelta int    `json:"quantity_delta" binding:"required"`
	Reason        string `json:"reason" binding:"required"`
	Note          string `json:"note"`
}

func (s *Server) createInventoryAdjustment(c *gin.Context) {
	skuID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid sku id"})
		return
	}

	var req inventoryAdjustmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	user := currentUser(c)
	var adjustment models.InventoryAdjustment
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var sku models.SKU
		if err := tx.First(&sku, skuID).Error; err != nil {
			return err
		}
		sku.Stock += req.QuantityDelta
		if sku.Stock < 0 {
			return fmt.Errorf("stock cannot be negative")
		}
		if err := tx.Save(&sku).Error; err != nil {
			return err
		}
		adjustment = models.InventoryAdjustment{
			SKUID:         uint(skuID),
			QuantityDelta: req.QuantityDelta,
			Reason:        req.Reason,
			Note:          req.Note,
			OperatorID:    user.ID,
		}
		return tx.Create(&adjustment).Error
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, adjustment)
}

func (s *Server) listInventoryHistory(c *gin.Context) {
	var adjustments []models.InventoryAdjustment
	if err := s.db.Preload("Operator").Where("sku_id = ?", c.Param("id")).Order("created_at DESC").Find(&adjustments).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": adjustments})
}

func (s *Server) listSOPTemplates(c *gin.Context) {
	var templates []models.SOPTemplate
	query := s.db.Preload("Views", func(db *gorm.DB) *gorm.DB {
		return db.Order("sort_order ASC")
	}).Preload("CatalogCategory").Order("updated_at DESC")
	if categoryID := c.Query("category_id"); categoryID != "" {
		query = query.Where("category_id = ?", categoryID)
	}
	if category := c.Query("category"); category != "" {
		query = query.Where("category = ?", category)
	}
	if err := query.Find(&templates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": templates})
}

type sopTemplateRequest struct {
	CategoryID uint             `json:"category_id"`
	Name       string           `json:"name" binding:"required"`
	Category   string           `json:"category"`
	Status     string           `json:"status"`
	Views      []sopViewRequest `json:"views"`
}

type sopViewRequest struct {
	Name       string `json:"name" binding:"required"`
	SortOrder  int    `json:"sort_order"`
	Required   bool   `json:"required"`
	Prompt     string `json:"prompt"`
	ExampleURL string `json:"example_url"`
}

func (s *Server) createSOPTemplate(c *gin.Context) {
	var req sopTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if req.Status == "" {
		req.Status = "active"
	}

	category, err := s.resolveCategory(s.db, req.CategoryID, req.Category)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	template := models.SOPTemplate{CategoryID: category.ID, Name: req.Name, Category: category.Name, Status: req.Status}
	if err := s.db.Create(&template).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	for _, view := range req.Views {
		_ = s.db.Create(&models.SOPView{
			SOPTemplateID: template.ID,
			Name:          view.Name,
			SortOrder:     view.SortOrder,
			Required:      view.Required,
			Prompt:        view.Prompt,
			ExampleURL:    view.ExampleURL,
		}).Error
	}
	_ = s.db.Preload("Views").Preload("CatalogCategory").First(&template, template.ID).Error
	c.JSON(http.StatusCreated, template)
}

func (s *Server) updateSOPTemplate(c *gin.Context) {
	var template models.SOPTemplate
	if err := s.db.First(&template, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "template not found"})
		return
	}
	var req sopTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	template.Name = req.Name
	category, err := s.resolveCategory(s.db, req.CategoryID, req.Category)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	template.CategoryID = category.ID
	template.Category = category.Name
	if req.Status != "" {
		template.Status = req.Status
	}
	if err := s.db.Save(&template).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, template)
}

type photoSessionRequest struct {
	SKUID         uint `json:"sku_id" binding:"required"`
	SOPTemplateID uint `json:"sop_template_id" binding:"required"`
}

func (s *Server) createPhotoSession(c *gin.Context) {
	var req photoSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	user := currentUser(c)
	session := models.PhotoSession{
		Code:           fmt.Sprintf("PS-%s-%d", time.Now().Format("20060102"), time.Now().UnixNano()),
		SKUID:          req.SKUID,
		SOPTemplateID:  req.SOPTemplateID,
		PhotographerID: user.ID,
		Status:         "in_progress",
	}
	if err := s.db.Create(&session).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, session)
}

type uploadURLRequest struct {
	FileName    string `json:"file_name" binding:"required"`
	ContentType string `json:"content_type" binding:"required"`
	SKUID       uint   `json:"sku_id" binding:"required"`
	SOPViewID   uint   `json:"sop_view_id"`
}

func (s *Server) createUploadURL(c *gin.Context) {
	var req uploadURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if !strings.HasPrefix(req.ContentType, "image/") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "only image uploads are supported"})
		return
	}

	fileName := strings.ReplaceAll(filepath.Base(req.FileName), " ", "-")
	objectKey := fmt.Sprintf("skus/%d/%d-%s", req.SKUID, time.Now().UnixNano(), fileName)
	uploadURL, assetURL, err := s.storage.createUploadURL(c.Request.Context(), objectKey)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "prepare object storage upload failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"method":     "PUT",
		"upload_url": uploadURL,
		"asset_url":  assetURL,
		"object_key": objectKey,
		"expires_in": 900,
		"headers":    gin.H{"content-type": req.ContentType},
	})
}

type completeAssetRequest struct {
	SKUID          uint   `json:"sku_id" binding:"required"`
	PhotoSessionID uint   `json:"photo_session_id"`
	SOPViewID      uint   `json:"sop_view_id"`
	ObjectKey      string `json:"object_key" binding:"required"`
	OriginalURL    string `json:"original_url" binding:"required"`
	ThumbnailURL   string `json:"thumbnail_url"`
	CapturedAt     string `json:"captured_at"`
}

func (s *Server) completeAssetUpload(c *gin.Context) {
	var req completeAssetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	capturedAt := time.Now()
	if req.CapturedAt != "" {
		if parsed, err := time.Parse(time.RFC3339, req.CapturedAt); err == nil {
			capturedAt = parsed
		}
	}
	asset := models.Asset{
		SKUID:          req.SKUID,
		PhotoSessionID: req.PhotoSessionID,
		SOPViewID:      req.SOPViewID,
		ObjectKey:      req.ObjectKey,
		OriginalURL:    req.OriginalURL,
		ThumbnailURL:   req.ThumbnailURL,
		ReviewStatus:   "pending",
		CapturedAt:     capturedAt,
	}
	if err := s.db.Create(&asset).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, asset)
}

func (s *Server) listAssetsForReview(c *gin.Context) {
	var assets []models.Asset
	query := s.db.Preload("SKU.Product.CatalogCategory").Preload("SKU.Tags").Preload("SOPView").Preload("PhotoSession").Order("created_at DESC")
	if status := c.Query("status"); status != "" {
		query = query.Where("review_status = ?", status)
	}
	if err := query.Find(&assets).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": assets})
}

type reviewAssetRequest struct {
	Status string `json:"status" binding:"required"`
	Reason string `json:"reason"`
}

func (s *Server) reviewAsset(c *gin.Context) {
	var req reviewAssetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	user := currentUser(c)
	var asset models.Asset
	if err := s.db.First(&asset, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "asset not found"})
		return
	}
	asset.ReviewStatus = req.Status
	if err := s.db.Save(&asset).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	_ = s.db.Create(&models.AssetReview{AssetID: asset.ID, ReviewerID: user.ID, Status: req.Status, Reason: req.Reason}).Error
	c.JSON(http.StatusOK, asset)
}

func (s *Server) listAIJobs(c *gin.Context) {
	var jobs []models.AIJob
	if err := s.db.Preload("SKU.Product").Order("created_at DESC").Find(&jobs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": jobs})
}

type aiJobRequest struct {
	SKUID          uint   `json:"sku_id" binding:"required"`
	TargetPlatform string `json:"target_platform" binding:"required"`
	InputAssetIDs  string `json:"input_asset_ids"`
}

func (s *Server) createAIJob(c *gin.Context) {
	var req aiJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	user := currentUser(c)
	job := models.AIJob{
		SKUID:          req.SKUID,
		TargetPlatform: req.TargetPlatform,
		Status:         "pending",
		InputAssetIDs:  req.InputAssetIDs,
		CreatedByID:    user.ID,
	}
	if err := s.db.Create(&job).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, job)
}

func (s *Server) listUsers(c *gin.Context) {
	var users []models.User
	if err := s.db.Order("created_at DESC").Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": users})
}
