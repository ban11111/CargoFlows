package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"cargoflows/api/internal/ai"
	"cargoflows/api/internal/models"
	"github.com/gin-gonic/gin"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	if err := s.db.Where("email = ? AND status = ?", strings.ToLower(strings.TrimSpace(req.Email)), "active").First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "invalid credentials"})
		return
	}

	lastSeenAt := time.Now()
	user.LastSeenAt = &lastSeenAt
	_ = s.db.Save(&user).Error

	token, err := s.issueToken(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "issue token failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": token, "user": userDTOFromModel(user)})
}

type userDTO struct {
	PublicID           string      `json:"public_id"`
	Name               string      `json:"name"`
	Email              string      `json:"email"`
	Role               models.Role `json:"role"`
	Status             string      `json:"status"`
	MustChangePassword bool        `json:"must_change_password"`
	LastSeenAt         *time.Time  `json:"last_seen_at"`
	CreatedAt          time.Time   `json:"created_at"`
}

func userDTOFromModel(user models.User) userDTO {
	return userDTO{PublicID: user.PublicID, Name: user.Name, Email: user.Email, Role: user.Role, Status: user.Status,
		MustChangePassword: user.MustChangePassword, LastSeenAt: user.LastSeenAt, CreatedAt: user.CreatedAt}
}

func (s *Server) me(c *gin.Context) {
	c.JSON(http.StatusOK, userDTOFromModel(currentUser(c)))
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func validManagedPassword(password string) bool {
	return len([]byte(password)) >= 12 && len([]byte(password)) <= 72
}

func (s *Server) changePassword(c *gin.Context) {
	var req changePasswordRequest
	if err := decodeJSONStrict(c, &req); err != nil || !validManagedPassword(req.NewPassword) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "validation_failed", "message": "new_password must be between 12 and 72 bytes"})
		return
	}
	user := currentUser(c)
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "current_password_invalid", "message": "current password is incorrect"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "change password failed"})
		return
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		return tx.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
			"password_hash": hash, "must_change_password": false, "session_version": gorm.Expr("session_version + 1"),
		}).Error
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "change password failed"})
		return
	}
	if err := s.db.First(&user, user.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "reload user failed"})
		return
	}
	token, err := s.issueToken(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "issue token failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "user": userDTOFromModel(user)})
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

type skuProductDTO struct {
	CategoryID      uint            `json:"category_id"`
	Name            string          `json:"name"`
	Brand           string          `json:"brand"`
	Category        string          `json:"category"`
	Description     string          `json:"description"`
	CatalogCategory models.Category `json:"category_record"`
}

type skuDTO struct {
	PublicID          string         `json:"public_id"`
	Code              string         `json:"code"`
	Color             string         `json:"color"`
	Size              string         `json:"size"`
	Barcode           string         `json:"barcode"`
	Stock             int            `json:"stock"`
	LowStockThreshold int            `json:"low_stock_threshold"`
	PlatformTitle     string         `json:"platform_title"`
	SellingPoints     string         `json:"selling_points"`
	Status            string         `json:"status"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
	Product           skuProductDTO  `json:"product"`
	Tags              []publicTagDTO `json:"tags"`
}

type publicTagDTO struct {
	Name string `json:"name"`
}

func publicTagDTOs(tags []models.Tag) []publicTagDTO {
	result := make([]publicTagDTO, 0, len(tags))
	for _, tag := range tags {
		result = append(result, publicTagDTO{Name: tag.Name})
	}
	return result
}

func skuDTOFromModel(value models.SKU) skuDTO {
	return skuDTO{
		PublicID: value.PublicID, Code: value.Code, Color: value.Color, Size: value.Size, Barcode: value.Barcode,
		Stock: value.Stock, LowStockThreshold: value.LowStockThreshold, PlatformTitle: value.PlatformTitle,
		SellingPoints: value.SellingPoints, Status: value.Status, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
		Product: skuProductDTO{CategoryID: value.Product.CategoryID, Name: value.Product.Name, Brand: value.Product.Brand,
			Category: value.Product.Category, Description: value.Product.Description, CatalogCategory: value.Product.CatalogCategory},
		Tags: publicTagDTOs(value.Tags),
	}
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
	data := make([]skuDTO, 0, len(skus))
	for _, sku := range skus {
		data = append(data, skuDTOFromModel(sku))
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
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

	c.JSON(http.StatusCreated, skuDTOFromModel(sku))
}

func (s *Server) getSKU(c *gin.Context) {
	publicID, ok := requireSKUPublicID(c)
	if !ok {
		return
	}
	var sku models.SKU
	if err := s.db.Preload("Product.CatalogCategory").Preload("Tags").Where("public_id = ?", publicID).First(&sku).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "sku not found"})
		return
	}
	c.JSON(http.StatusOK, skuDTOFromModel(sku))
}

func (s *Server) updateSKU(c *gin.Context) {
	publicID, ok := requireSKUPublicID(c)
	if !ok {
		return
	}
	var sku models.SKU
	if err := s.db.Preload("Product.CatalogCategory").Preload("Tags").Where("public_id = ?", publicID).First(&sku).Error; err != nil {
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
	c.JSON(http.StatusOK, skuDTOFromModel(sku))
}

func (s *Server) deleteSKU(c *gin.Context) {
	publicID, ok := requireSKUPublicID(c)
	if !ok {
		return
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var sku models.SKU
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("public_id = ?", publicID).First(&sku).Error; err != nil {
			return err
		}
		for _, reference := range []struct{ table, column string }{
			{"inventory_adjustments", "sk_uid"}, {"photo_sessions", "sk_uid"}, {"assets", "sk_uid"},
			{"model_family_members", "sk_uid"}, {"variant_identity_manifests", "sk_uid"}, {"ai_jobs", "sk_uid"},
			{"sku_platform_contents", "sku_id"},
		} {
			var count int64
			if err := tx.Table(reference.table).Where(reference.column+" = ?", sku.ID).Count(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				return errSKUInUse
			}
		}
		if err := tx.Model(&sku).Association("Tags").Clear(); err != nil {
			return err
		}
		if err := tx.Delete(&sku).Error; err != nil {
			return err
		}
		var siblingCount int64
		if err := tx.Model(&models.SKU{}).Where("product_id = ?", sku.ProductID).Count(&siblingCount).Error; err != nil {
			return err
		}
		if siblingCount == 0 {
			return tx.Delete(&models.Product{}, sku.ProductID).Error
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "sku not found"})
		return
	}
	if errors.Is(err, errSKUInUse) {
		c.JSON(http.StatusConflict, gin.H{"code": "sku_in_use", "message": "SKU has inventory, media, model-family, AI, or published-content history; disable it instead"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "delete sku failed"})
		return
	}
	c.Status(http.StatusNoContent)
	c.Writer.WriteHeaderNow()
}

var errSKUInUse = errors.New("SKU is referenced by business history")

type inventoryAdjustmentRequest struct {
	QuantityDelta int    `json:"quantity_delta" binding:"required"`
	Reason        string `json:"reason" binding:"required"`
	Note          string `json:"note"`
}

func (s *Server) createInventoryAdjustment(c *gin.Context) {
	publicID, ok := requireSKUPublicID(c)
	if !ok {
		return
	}

	var req inventoryAdjustmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	user := currentUser(c)
	var adjustment models.InventoryAdjustment
	var sku models.SKU
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("public_id = ?", publicID).First(&sku).Error; err != nil {
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
			SKUID:         sku.ID,
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

	c.JSON(http.StatusCreated, inventoryAdjustmentDTOFromModel(adjustment, sku.PublicID))
}

func (s *Server) listInventoryHistory(c *gin.Context) {
	publicID, ok := requireSKUPublicID(c)
	if !ok {
		return
	}
	var sku models.SKU
	if err := s.db.Select("id", "public_id").Where("public_id = ?", publicID).First(&sku).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "sku not found"})
		return
	}
	var adjustments []models.InventoryAdjustment
	if err := s.db.Preload("Operator").Where("sku_id = ?", sku.ID).Order("created_at DESC").Find(&adjustments).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	data := make([]inventoryAdjustmentDTO, 0, len(adjustments))
	for _, adjustment := range adjustments {
		data = append(data, inventoryAdjustmentDTOFromModel(adjustment, sku.PublicID))
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

type inventoryAdjustmentDTO struct {
	SKUID         string    `json:"sku_id"`
	QuantityDelta int       `json:"quantity_delta"`
	Reason        string    `json:"reason"`
	Note          string    `json:"note"`
	OperatorName  string    `json:"operator_name"`
	CreatedAt     time.Time `json:"created_at"`
}

func inventoryAdjustmentDTOFromModel(value models.InventoryAdjustment, skuPublicID string) inventoryAdjustmentDTO {
	return inventoryAdjustmentDTO{SKUID: skuPublicID, QuantityDelta: value.QuantityDelta, Reason: value.Reason, Note: value.Note, OperatorName: value.Operator.Name, CreatedAt: value.CreatedAt}
}

func requireSKUPublicID(c *gin.Context) (string, bool) {
	value := c.Param("sku_id")
	canonical, ok := canonicalUUID(value)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "sku_id must be a UUID"})
		return "", false
	}
	return canonical, true
}

type createPhotoSessionRequest struct {
	SKUID        string `json:"sku_id"`
	SOPVersionID string `json:"sop_version_id"`
}

type photoSessionResponse struct {
	PublicID     string    `json:"public_id"`
	Code         string    `json:"code"`
	SKUID        string    `json:"sku_id"`
	SOPVersionID string    `json:"sop_version_id"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}

var (
	errCaptureVersionNotFound = errors.New("SOP version not found")
	errVersionNotPublished    = errors.New("SOP version is not published")
	errSKUNotFound            = errors.New("SKU not found")
	errSKUCategoryMismatch    = errors.New("SKU category does not match the capture SOP category")
	errPhotoSessionNotFound   = errors.New("photo session not found")
	errPhotoSessionForbidden  = errors.New("photo session does not belong to the current user")
	errSOPViewNotFound        = errors.New("SOP view not found")
	errViewVersionMismatch    = errors.New("SOP view does not belong to the session version")
	errInvalidUploadTicket    = errors.New("upload completion ticket is invalid")
	errUploadedObjectNotFound = errors.New("uploaded object was not found")
)

func (s *Server) createPhotoSession(c *gin.Context) {
	var req createPhotoSessionRequest
	if err := decodeJSONStrict(c, &req); err != nil || !isUUID(req.SKUID) || !isUUID(req.SOPVersionID) {
		respondSOPBadRequest(c, errOr(err, "sku_id and sop_version_id must be UUIDs"))
		return
	}
	user := currentUser(c)
	var session models.PhotoSession
	var selectedVersionPublicID string
	var selectedSKUPublicID string
	err := s.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var version models.SOPVersion
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("public_id = ?", req.SOPVersionID).First(&version).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errCaptureVersionNotFound
			}
			return err
		}
		if version.Status != models.SOPVersionPublished {
			return errVersionNotPublished
		}
		var captureSOP models.CaptureSOP
		if err := tx.Select("id", "category_id").First(&captureSOP, version.CaptureSOPID).Error; err != nil {
			return err
		}
		var sku models.SKU
		if err := tx.Model(&models.SKU{}).Select("skus.*").
			Joins("JOIN products ON products.id = skus.product_id").
			Preload("Product").Where("skus.public_id = ?", req.SKUID).First(&sku).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errSKUNotFound
			}
			return err
		}
		if sku.Product.CategoryID != captureSOP.CategoryID {
			return errSKUCategoryMismatch
		}
		selectedVersionPublicID = version.PublicID
		selectedSKUPublicID = sku.PublicID
		session = models.PhotoSession{
			PublicID: uuid.NewString(), Code: fmt.Sprintf("PS-%s-%d", time.Now().Format("20060102"), time.Now().UnixNano()),
			SKUID: sku.ID, SOPVersionID: version.ID, PhotographerID: user.ID, Status: "in_progress",
		}
		return tx.Create(&session).Error
	})
	if err != nil {
		respondCaptureError(c, err)
		return
	}
	c.JSON(http.StatusCreated, photoSessionResponse{PublicID: session.PublicID, Code: session.Code, SKUID: selectedSKUPublicID, SOPVersionID: selectedVersionPublicID, Status: session.Status, CreatedAt: session.CreatedAt})
}

type uploadURLRequest struct {
	FileName       string `json:"file_name"`
	ContentType    string `json:"content_type"`
	PhotoSessionID string `json:"photo_session_id"`
	SOPViewID      string `json:"sop_view_id"`
}

type assetUploadClaims struct {
	PhotoSessionID string `json:"photo_session_id"`
	SOPViewID      string `json:"sop_view_id"`
	UploadID       string `json:"upload_id"`
	ContentType    string `json:"content_type"`
	ActorBinding   string `json:"actor_binding"`
	jwt.RegisteredClaims
}

func (s *Server) createUploadURL(c *gin.Context) {
	var req uploadURLRequest
	if err := decodeJSONStrict(c, &req); err != nil || strings.TrimSpace(req.FileName) == "" || !isUUID(req.PhotoSessionID) || !isUUID(req.SOPViewID) {
		respondSOPBadRequest(c, errOr(err, "file_name, photo_session_id, and sop_view_id are required; identifiers must be UUIDs"))
		return
	}
	if !strings.HasPrefix(normalizedImageContentType(req.ContentType), "image/") {
		respondSOPBadRequest(c, errors.New("only image uploads are supported"))
		return
	}
	if _, _, err := s.resolveCaptureBinding(c, req.PhotoSessionID, req.SOPViewID); err != nil {
		respondCaptureError(c, err)
		return
	}

	extension, ok := imageExtension(req.ContentType)
	if !ok {
		respondSOPBadRequest(c, errors.New("unsupported image content type"))
		return
	}
	uploadID := uuid.NewString()
	objectKey := fmt.Sprintf("photo-sessions/%s/views/%s/%s%s", req.PhotoSessionID, req.SOPViewID, uploadID, extension)
	uploadURL, _, err := s.storage.createUploadURL(c.Request.Context(), objectKey)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "prepare object storage upload failed"})
		return
	}
	completionToken, err := s.issueAssetUploadTicket(currentUser(c).ID, req.PhotoSessionID, req.SOPViewID, uploadID, normalizedImageContentType(req.ContentType))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "issue upload completion ticket failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"method":           "PUT",
		"upload_url":       uploadURL,
		"completion_token": completionToken,
		"expires_in":       900,
		"headers":          gin.H{"content-type": req.ContentType},
	})
}

type completeAssetRequest struct {
	PhotoSessionID  string `json:"photo_session_id"`
	SOPViewID       string `json:"sop_view_id"`
	CompletionToken string `json:"completion_token"`
	CapturedAt      string `json:"captured_at"`
}

type completedAssetResponse struct {
	PublicID       string    `json:"public_id"`
	SKUID          string    `json:"sku_id"`
	PhotoSessionID string    `json:"photo_session_id"`
	SOPViewID      string    `json:"sop_view_id"`
	MediaURL       string    `json:"media_url"`
	ReviewStatus   string    `json:"review_status"`
	CapturedAt     time.Time `json:"captured_at"`
}

func (s *Server) completeAssetUpload(c *gin.Context) {
	var req completeAssetRequest
	if err := decodeJSONStrict(c, &req); err != nil || !isUUID(req.PhotoSessionID) || !isUUID(req.SOPViewID) || strings.TrimSpace(req.CompletionToken) == "" {
		respondSOPBadRequest(c, errOr(err, "photo_session_id, sop_view_id, and completion_token are required; identifiers must be UUIDs"))
		return
	}
	capturedAt := time.Now()
	if req.CapturedAt != "" {
		parsed, err := time.Parse(time.RFC3339, req.CapturedAt)
		if err != nil {
			respondSOPBadRequest(c, errors.New("captured_at must be an RFC3339 timestamp"))
			return
		}
		capturedAt = parsed
	}
	session, view, err := s.resolveCaptureBinding(c, req.PhotoSessionID, req.SOPViewID)
	if err != nil {
		respondCaptureError(c, err)
		return
	}
	claims, err := s.verifyAssetUploadTicket(req.CompletionToken)
	if err != nil {
		respondCaptureError(c, errInvalidUploadTicket)
		return
	}
	extension, supported := imageExtension(claims.ContentType)
	if !supported || !isUUID(claims.UploadID) || !hmac.Equal([]byte(claims.ActorBinding), []byte(s.assetUploadActorBinding(currentUser(c).ID))) || claims.PhotoSessionID != session.PublicID || claims.SOPViewID != view.PublicID {
		respondCaptureError(c, errInvalidUploadTicket)
		return
	}
	temporaryObjectKey := fmt.Sprintf("photo-sessions/%s/views/%s/%s%s", session.PublicID, view.PublicID, claims.UploadID, extension)
	if !isScopedAssetObjectKey(temporaryObjectKey, session.PublicID, view.PublicID) {
		respondCaptureError(c, errInvalidUploadTicket)
		return
	}
	existing, found, err := s.findCompletedAsset(claims.UploadID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
		return
	}
	if found {
		if existing.SKUID != session.SKUID || existing.PhotoSessionID != session.ID || existing.SOPViewID != view.ID {
			respondCaptureError(c, errInvalidUploadTicket)
			return
		}
		s.writeCompletedAsset(c, http.StatusOK, existing, session.PublicID, view.PublicID)
		return
	}
	exists, err := s.storage.objectExists(c.Request.Context(), temporaryObjectKey)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "verify uploaded object failed"})
		return
	}
	if !exists {
		for attempt := 0; attempt < 6; attempt++ {
			existing, found, lookupErr := s.findCompletedAsset(claims.UploadID)
			if lookupErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
				return
			}
			if found {
				if existing.SKUID != session.SKUID || existing.PhotoSessionID != session.ID || existing.SOPViewID != view.ID {
					respondCaptureError(c, errInvalidUploadTicket)
					return
				}
				s.writeCompletedAsset(c, http.StatusOK, existing, session.PublicID, view.PublicID)
				return
			}
			time.Sleep(time.Duration(1<<attempt) * time.Millisecond)
		}
		respondCaptureError(c, errUploadedObjectNotFound)
		return
	}
	source, err := s.storage.ReadSource(c.Request.Context(), temporaryObjectKey)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "read uploaded object failed"})
		return
	}
	metadata, err := new(ai.ImageStorage).Validate(ai.ImageValidationRequest{Bytes: source.Bytes})
	if err != nil || metadata.MIMEType != claims.ContentType {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_uploaded_image", "message": "uploaded image is invalid"})
		return
	}
	assetPublicID := uuid.NewString()
	finalObjectKey := finalizedAssetObjectKey(assetPublicID, extension)
	if err := s.storage.promoteSource(c.Request.Context(), temporaryObjectKey, finalObjectKey, metadata.MIMEType, source.Bytes); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "finalize uploaded object failed"})
		return
	}
	uploadID := claims.UploadID
	asset := models.Asset{
		PublicID:       assetPublicID,
		SKUID:          session.SKUID,
		PhotoSessionID: session.ID,
		SOPViewID:      view.ID,
		UploadID:       &uploadID,
		ObjectKey:      finalObjectKey,
		OriginalURL:    "",
		ReviewStatus:   "pending",
		MIMEType:       metadata.MIMEType,
		Width:          metadata.Width,
		Height:         metadata.Height,
		ByteCount:      metadata.ByteCount,
		SHA256:         metadata.SHA256,
		CapturedAt:     capturedAt,
	}
	asset, created, err := s.createCompletedAsset(asset)
	if err != nil {
		_ = s.storage.deleteSource(c.Request.Context(), finalObjectKey)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
		return
	}
	if !created {
		_ = s.storage.deleteSource(c.Request.Context(), finalObjectKey)
	}
	if asset.SKUID != session.SKUID || asset.PhotoSessionID != session.ID || asset.SOPViewID != view.ID {
		respondCaptureError(c, errInvalidUploadTicket)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	s.writeCompletedAsset(c, status, asset, session.PublicID, view.PublicID)
}

func (s *Server) findCompletedAsset(uploadID string) (models.Asset, bool, error) {
	var lastErr error
	for attempt := 0; attempt < 6; attempt++ {
		var asset models.Asset
		err := s.db.Where("upload_id = ?", uploadID).First(&asset).Error
		if err == nil {
			return asset, true, nil
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.Asset{}, false, nil
		}
		lastErr = err
		if !s.retryableAssetDBError(err) || attempt == 5 {
			return models.Asset{}, false, err
		}
		time.Sleep(time.Duration(1<<attempt) * time.Millisecond)
	}
	return models.Asset{}, false, lastErr
}

func (s *Server) createCompletedAsset(asset models.Asset) (models.Asset, bool, error) {
	var lastErr error
	for attempt := 0; attempt < 6; attempt++ {
		candidate := asset
		result := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&candidate)
		if result.Error == nil && result.RowsAffected == 1 {
			return candidate, true, nil
		}
		if result.Error != nil {
			lastErr = result.Error
			if !s.retryableAssetDBError(result.Error) || attempt == 5 {
				return models.Asset{}, false, result.Error
			}
		} else {
			if asset.UploadID == nil {
				return models.Asset{}, false, fmt.Errorf("asset upload ID is required")
			}
			existing, found, err := s.findCompletedAsset(*asset.UploadID)
			if err == nil && found {
				return existing, false, nil
			}
			if err != nil {
				lastErr = err
				if !s.retryableAssetDBError(err) || attempt == 5 {
					return models.Asset{}, false, err
				}
			}
		}
		time.Sleep(time.Duration(1<<attempt) * time.Millisecond)
	}
	return models.Asset{}, false, lastErr
}

func finalizedAssetObjectKey(assetPublicID, extension string) string {
	return "assets/final/" + assetPublicID + extension
}

func (s *Server) retryableAssetDBError(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1205 || mysqlErr.Number == 1213
	}
	if s.db.Dialector.Name() == "sqlite" {
		message := strings.ToLower(err.Error())
		return strings.Contains(message, "database is locked") || strings.Contains(message, "database table is locked")
	}
	return false
}

func (s *Server) writeCompletedAsset(c *gin.Context, status int, asset models.Asset, sessionPublicID, viewPublicID string) {
	var sku models.SKU
	if err := s.db.Select("public_id").First(&sku, asset.SKUID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
		return
	}
	c.JSON(status, completedAssetResponse{PublicID: asset.PublicID, SKUID: sku.PublicID, PhotoSessionID: sessionPublicID, SOPViewID: viewPublicID, MediaURL: "/api/v1/assets/" + asset.PublicID + "/media", ReviewStatus: asset.ReviewStatus, CapturedAt: asset.CapturedAt})
}

func imageExtension(contentType string) (string, bool) {
	switch normalizedImageContentType(contentType) {
	case "image/jpeg":
		return ".jpg", true
	case "image/png":
		return ".png", true
	case "image/webp":
		return ".webp", true
	default:
		return "", false
	}
}

func normalizedImageContentType(contentType string) string {
	return strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
}

func (s *Server) issueAssetUploadTicket(userID uint, sessionID, viewID, uploadID, contentType string) (string, error) {
	now := time.Now()
	claims := assetUploadClaims{
		PhotoSessionID: sessionID,
		SOPViewID:      viewID,
		UploadID:       uploadID,
		ContentType:    contentType,
		ActorBinding:   s.assetUploadActorBinding(userID),
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.cfg.JWTSecret))
}

func (s *Server) assetUploadActorBinding(userID uint) string {
	mac := hmac.New(sha256.New, []byte(s.cfg.JWTSecret))
	_, _ = mac.Write([]byte("asset-upload-actor:" + strconv.FormatUint(uint64(userID), 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Server) verifyAssetUploadTicket(value string) (*assetUploadClaims, error) {
	claims := &assetUploadClaims{}
	token, err := jwt.ParseWithClaims(value, claims, func(token *jwt.Token) (any, error) {
		return []byte(s.cfg.JWTSecret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil || !token.Valid {
		return nil, errInvalidUploadTicket
	}
	return claims, nil
}

func isScopedAssetObjectKey(objectKey, sessionID, viewID string) bool {
	prefix := "photo-sessions/" + sessionID + "/views/" + viewID + "/"
	base := strings.TrimPrefix(objectKey, prefix)
	if base == objectKey || base == "" || strings.Contains(base, "/") || strings.Contains(base, "\\") {
		return false
	}
	for _, extension := range []string{".jpg", ".png", ".webp"} {
		if strings.HasSuffix(base, extension) {
			_, err := uuid.Parse(strings.TrimSuffix(base, extension))
			return err == nil
		}
	}
	return false
}

func (s *Server) resolveCaptureBinding(c *gin.Context, sessionPublicID, viewPublicID string) (models.PhotoSession, models.SOPView, error) {
	var session models.PhotoSession
	if err := s.db.WithContext(c.Request.Context()).Where("public_id = ?", sessionPublicID).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return session, models.SOPView{}, errPhotoSessionNotFound
		}
		return session, models.SOPView{}, err
	}
	if session.PhotographerID != currentUser(c).ID {
		return session, models.SOPView{}, errPhotoSessionForbidden
	}
	var view models.SOPView
	if err := s.db.WithContext(c.Request.Context()).Where("public_id = ?", viewPublicID).First(&view).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return session, view, errSOPViewNotFound
		}
		return session, view, err
	}
	if view.SOPVersionID != session.SOPVersionID {
		return session, view, errViewVersionMismatch
	}
	return session, view, nil
}

func respondCaptureError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errVersionNotPublished):
		c.JSON(http.StatusConflict, gin.H{"code": "version_not_published", "message": err.Error()})
	case errors.Is(err, errViewVersionMismatch):
		c.JSON(http.StatusConflict, gin.H{"code": "view_version_mismatch", "message": err.Error()})
	case errors.Is(err, errCaptureVersionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "version_not_found", "message": err.Error()})
	case errors.Is(err, errSKUNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "sku_not_found", "message": err.Error()})
	case errors.Is(err, errSKUCategoryMismatch):
		c.JSON(http.StatusConflict, gin.H{"code": "sku_sop_category_mismatch", "message": err.Error()})
	case errors.Is(err, errPhotoSessionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "photo_session_not_found", "message": err.Error()})
	case errors.Is(err, errPhotoSessionForbidden):
		c.JSON(http.StatusForbidden, gin.H{"code": "photo_session_forbidden", "message": err.Error()})
	case errors.Is(err, errSOPViewNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "sop_view_not_found", "message": err.Error()})
	case errors.Is(err, errInvalidUploadTicket):
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_upload_ticket", "message": err.Error()})
	case errors.Is(err, errUploadedObjectNotFound):
		c.JSON(http.StatusConflict, gin.H{"code": "upload_not_found", "message": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
	}
}

func (s *Server) listAssetsForReview(c *gin.Context) {
	if currentUser(c).Role == models.RoleOperator && c.Query("status") != "approved" {
		c.JSON(http.StatusForbidden, gin.H{"code": "forbidden", "message": "operators may only read approved assets"})
		return
	}
	var assets []models.Asset
	query := scopeAssetsForUser(s.db.Model(&models.Asset{}), currentUser(c)).Preload("SKU.Product.CatalogCategory").Preload("SKU.Tags").Preload("SOPView").Preload("PhotoSession").Order("assets.created_at DESC")
	if skuPublicID := c.Query("sku_id"); skuPublicID != "" {
		if !isUUID(skuPublicID) {
			c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "sku_id must be a UUID"})
			return
		}
		query = query.Joins("JOIN skus ON skus.id = assets.sk_uid").Where("skus.public_id = ?", skuPublicID)
	}
	if status := c.Query("status"); status != "" {
		query = query.Where("review_status = ?", status)
	}
	if err := query.Find(&assets).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	items := make([]assetReviewItem, 0, len(assets))
	for _, asset := range assets {
		items = append(items, assetReviewItem{
			PublicID: asset.PublicID, SKUID: asset.SKU.PublicID, MediaURL: "/api/v1/assets/" + asset.PublicID + "/media",
			ReviewStatus: asset.ReviewStatus, CapturedAt: asset.CapturedAt,
			SOPViewID:        asset.SOPView.PublicID,
			SOPViewKey:       asset.SOPView.PresetKey,
			SOPViewName:      localizedViewName{ZHCN: asset.SOPView.NameZH, EN: asset.SOPView.NameEN},
			PhotoSessionCode: asset.PhotoSession.Code,
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

type assetReviewItem struct {
	PublicID         string            `json:"public_id"`
	SKUID            string            `json:"sku_id"`
	MediaURL         string            `json:"media_url"`
	ReviewStatus     string            `json:"review_status"`
	CapturedAt       time.Time         `json:"captured_at"`
	SOPViewID        string            `json:"sop_view_id"`
	SOPViewKey       string            `json:"sop_view_key"`
	SOPViewName      localizedViewName `json:"sop_view_name"`
	PhotoSessionCode string            `json:"photo_session_code"`
}

type reviewAssetRequest struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

func (s *Server) reviewAsset(c *gin.Context) {
	if !isAdministrator(currentUser(c)) {
		c.JSON(http.StatusForbidden, gin.H{"code": "forbidden", "message": "insufficient permissions"})
		return
	}
	var req reviewAssetRequest
	if err := decodeJSONStrict(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if req.Status != "approved" && req.Status != "rejected" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "status must be approved or rejected"})
		return
	}
	user := currentUser(c)
	assetPublicID := c.Param("asset_id")
	if !isUUID(assetPublicID) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "asset_id must be a UUID"})
		return
	}
	var asset models.Asset
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("public_id = ?", assetPublicID).First(&asset).Error; err != nil {
			return err
		}
		if err := tx.Model(&asset).Update("review_status", req.Status).Error; err != nil {
			return err
		}
		asset.ReviewStatus = req.Status
		return tx.Create(&models.AssetReview{AssetID: asset.ID, ReviewerID: user.ID, Status: req.Status, Reason: req.Reason}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"message": "asset not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"public_id": asset.PublicID, "review_status": asset.ReviewStatus})
}

func (s *Server) assetMedia(c *gin.Context) {
	if !isUUID(c.Param("asset_id")) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "asset_id must be a UUID"})
		return
	}
	var asset models.Asset
	query := scopeAssetsForUser(s.db.Model(&models.Asset{}), currentUser(c))
	if err := query.Where("assets.public_id = ?", c.Param("asset_id")).First(&asset).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "asset not found"})
		return
	}
	source, err := s.storage.ReadSource(c.Request.Context(), asset.ObjectKey)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "read asset failed"})
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", "inline")
	c.Data(http.StatusOK, asset.MIMEType, source.Bytes)
}

func scopeAssetsForUser(query *gorm.DB, user models.User) *gorm.DB {
	return query
}

func (s *Server) listUsers(c *gin.Context) {
	var users []models.User
	if err := s.db.Order("created_at DESC").Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	items := make([]userDTO, 0, len(users))
	for _, user := range users {
		items = append(items, userDTOFromModel(user))
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

type createUserRequest struct {
	Name     string      `json:"name"`
	Email    string      `json:"email"`
	Role     models.Role `json:"role"`
	Password string      `json:"password"`
}

func isManagedRole(role models.Role) bool {
	return role == models.RoleAdmin || role == models.RoleOperator
}

func isDuplicateUserError(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint") || strings.Contains(message, "duplicate entry")
}

func (s *Server) createUser(c *gin.Context) {
	var req createUserRequest
	if err := decodeJSONStrict(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": err.Error()})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	parsedEmail, emailErr := mail.ParseAddress(req.Email)
	if req.Name == "" || len(req.Name) > 120 || req.Email == "" || len(req.Email) > 180 || emailErr != nil || parsedEmail.Address != req.Email || !isManagedRole(req.Role) || !validManagedPassword(req.Password) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "validation_failed", "message": "valid name, email, role, and 12-72 byte password are required"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "hash password failed"})
		return
	}
	user := models.User{Name: req.Name, Email: req.Email, PasswordHash: string(hash), Role: req.Role, Status: "active", MustChangePassword: true}
	if err := s.db.Create(&user).Error; err != nil {
		if isDuplicateUserError(err) {
			c.JSON(http.StatusConflict, gin.H{"code": "email_conflict", "message": "email already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "create user failed"})
		return
	}
	c.JSON(http.StatusCreated, userDTOFromModel(user))
}

type updateUserRequest struct {
	Role   *models.Role `json:"role"`
	Status *string      `json:"status"`
}

func (s *Server) findManagedUser(c *gin.Context) (models.User, bool) {
	if !isUUID(c.Param("user_id")) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "user_id must be a UUID"})
		return models.User{}, false
	}
	var target models.User
	if err := s.db.Where("public_id = ?", c.Param("user_id")).First(&target).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "user not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "load user failed"})
		}
		return models.User{}, false
	}
	return target, true
}

func (s *Server) updateUser(c *gin.Context) {
	var req updateUserRequest
	if err := decodeJSONStrict(c, &req); err != nil || (req.Role == nil && req.Status == nil) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "role or status is required"})
		return
	}
	target, ok := s.findManagedUser(c)
	if !ok {
		return
	}
	actor := currentUser(c)
	if target.Role == models.RoleSuperAdmin {
		c.JSON(http.StatusForbidden, gin.H{"code": "system_owner_protected", "message": "the system owner cannot be modified"})
		return
	}
	if req.Role != nil && !isManagedRole(*req.Role) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "validation_failed", "message": "role must be admin or operator"})
		return
	}
	if req.Status != nil && *req.Status != "active" && *req.Status != "disabled" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "validation_failed", "message": "status must be active or disabled"})
		return
	}
	if target.ID == actor.ID && ((req.Role != nil && *req.Role != actor.Role) || (req.Status != nil && *req.Status != "active")) {
		c.JSON(http.StatusForbidden, gin.H{"code": "self_management_forbidden", "message": "you cannot disable or change your own role"})
		return
	}
	updates := map[string]any{}
	if req.Role != nil {
		updates["role"] = *req.Role
	}
	if req.Status != nil {
		updates["status"] = *req.Status
		if *req.Status == "disabled" && target.Status != "disabled" {
			updates["session_version"] = gorm.Expr("session_version + 1")
		}
	}
	if err := s.db.Model(&target).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "update user failed"})
		return
	}
	if err := s.db.First(&target, target.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "reload user failed"})
		return
	}
	c.JSON(http.StatusOK, userDTOFromModel(target))
}

func (s *Server) deleteUser(c *gin.Context) {
	target, ok := s.findManagedUser(c)
	if !ok {
		return
	}
	actor := currentUser(c)
	if target.Role == models.RoleSuperAdmin {
		c.JSON(http.StatusForbidden, gin.H{"code": "system_owner_protected", "message": "the system owner cannot be deleted"})
		return
	}
	if target.ID == actor.ID {
		c.JSON(http.StatusForbidden, gin.H{"code": "self_management_forbidden", "message": "you cannot delete your own account"})
		return
	}
	if target.Status != "disabled" {
		c.JSON(http.StatusConflict, gin.H{"code": "user_must_be_disabled", "message": "disable the user before deletion"})
		return
	}
	if err := s.db.Delete(&target).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "delete user failed"})
		return
	}
	c.Status(http.StatusNoContent)
}

type resetUserPasswordRequest struct {
	Password string `json:"password"`
}

func (s *Server) resetUserPassword(c *gin.Context) {
	var req resetUserPasswordRequest
	if err := decodeJSONStrict(c, &req); err != nil || !validManagedPassword(req.Password) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "validation_failed", "message": "password must be between 12 and 72 bytes"})
		return
	}
	target, ok := s.findManagedUser(c)
	if !ok {
		return
	}
	if target.Role == models.RoleSuperAdmin || target.ID == currentUser(c).ID {
		c.JSON(http.StatusForbidden, gin.H{"code": "password_reset_forbidden", "message": "use the personal password change flow for this account"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "hash password failed"})
		return
	}
	if err := s.db.Model(&target).Updates(map[string]any{"password_hash": hash, "must_change_password": true, "session_version": gorm.Expr("session_version + 1")}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "reset password failed"})
		return
	}
	if err := s.db.First(&target, target.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "reload user failed"})
		return
	}
	c.JSON(http.StatusOK, userDTOFromModel(target))
}
