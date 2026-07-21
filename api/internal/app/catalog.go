package app

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"cargoflows/api/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type categorySummary struct {
	ID                  uint   `json:"id"`
	Name                string `json:"name"`
	NameEN              string `json:"name_en"`
	IsSystem            bool   `json:"is_system"`
	SKUCount            int64  `json:"sku_count"`
	CaptureSOPCount     int64  `json:"capture_sop_count"`
	AIReferenceSOPCount int64  `json:"ai_reference_sop_count"`
}

func (s *Server) listCategories(c *gin.Context) {
	var categories []models.Category
	if err := s.db.Order("is_system DESC, name ASC").Find(&categories).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	result := make([]categorySummary, 0, len(categories))
	for _, category := range categories {
		var skuCount, captureSOPCount, aiReferenceSOPCount int64
		_ = s.db.Model(&models.Product{}).Where("category_id = ?", category.ID).Count(&skuCount).Error
		_ = s.db.Model(&models.CaptureSOP{}).Where("category_id = ?", category.ID).Count(&captureSOPCount).Error
		_ = s.db.Model(&models.AIReferenceSOP{}).Where("category_id = ?", category.ID).Count(&aiReferenceSOPCount).Error
		result = append(result, categorySummary{
			ID:                  category.ID,
			Name:                category.Name,
			NameEN:              category.NameEN,
			IsSystem:            category.IsSystem,
			SKUCount:            skuCount,
			CaptureSOPCount:     captureSOPCount,
			AIReferenceSOPCount: aiReferenceSOPCount,
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

	var productCount, captureSOPCount, aiReferenceSOPCount int64
	_ = s.db.Model(&models.Product{}).Where("category_id = ?", category.ID).Count(&productCount).Error
	_ = s.db.Model(&models.CaptureSOP{}).Where("category_id = ?", category.ID).Count(&captureSOPCount).Error
	_ = s.db.Model(&models.AIReferenceSOP{}).Where("category_id = ?", category.ID).Count(&aiReferenceSOPCount).Error
	if productCount > 0 || captureSOPCount > 0 || aiReferenceSOPCount > 0 {
		c.JSON(http.StatusConflict, gin.H{"message": "category is in use by SKU, capture SOPs, or AI reference SOPs"})
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
	PublicID         string            `json:"public_id"`
	MediaURL         string            `json:"media_url"`
	ReviewStatus     string            `json:"review_status"`
	CapturedAt       string            `json:"captured_at"`
	SOPViewKey       string            `json:"sop_view_key"`
	SOPViewName      localizedViewName `json:"sop_view_name"`
	PhotoSessionCode string            `json:"photo_session_code"`
	OriginType       string            `json:"origin_type"`
	SourceSummary    map[string]string `json:"source_summary"`
}

type localizedViewName struct {
	ZHCN string `json:"zh-CN"`
	EN   string `json:"en"`
}

type paginationDTO struct {
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type assetReviewCounts struct {
	Pending  int64 `json:"pending"`
	Approved int64 `json:"approved"`
	Rejected int64 `json:"rejected"`
	Total    int64 `json:"total"`
}

type assetReviewCover struct {
	PublicID     string `json:"public_id"`
	MediaURL     string `json:"media_url"`
	ReviewStatus string `json:"review_status"`
	OriginType   string `json:"origin_type"`
}

type assetReviewCategory struct {
	ID       uint   `json:"id"`
	Name     string `json:"name"`
	NameEN   string `json:"name_en"`
	IsSystem bool   `json:"is_system"`
}

type assetReviewSKUSummary struct {
	PublicID        string              `json:"public_id"`
	Code            string              `json:"code"`
	ProductName     string              `json:"product_name"`
	Category        assetReviewCategory `json:"category"`
	Tags            []publicTagDTO      `json:"tags"`
	Counts          assetReviewCounts   `json:"counts"`
	LatestAt        time.Time           `json:"latest_asset_at"`
	LatestPendingAt *time.Time          `json:"latest_pending_at"`
	CoverAsset      *assetReviewCover   `json:"cover_asset"`
}

type assetReviewCoverRow struct {
	SKUID        uint
	PublicID     string
	ReviewStatus string
	OriginType   string
}

type assetReviewSKURow struct {
	SKUID           uint
	PublicID        string
	Code            string
	ProductName     string
	CategoryID      uint
	CategoryName    string
	CategoryNameEN  string
	CategorySystem  bool
	PendingCount    int64
	ApprovedCount   int64
	RejectedCount   int64
	TotalCount      int64
	LatestAt        nullableDatabaseTime
	LatestPendingAt nullableDatabaseTime
}

type nullableDatabaseTime struct {
	Time  time.Time
	Valid bool
}

func (value nullableDatabaseTime) Value() (driver.Value, error) {
	if !value.Valid {
		return nil, nil
	}
	return value.Time, nil
}

func (value *nullableDatabaseTime) Scan(source any) error {
	if source == nil {
		value.Valid = false
		return nil
	}
	if parsed, ok := source.(time.Time); ok {
		value.Time, value.Valid = parsed, true
		return nil
	}
	var raw string
	switch typed := source.(type) {
	case string:
		raw = typed
	case []byte:
		raw = string(typed)
	default:
		return fmt.Errorf("unsupported database time type %T", source)
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			value.Time, value.Valid = parsed, true
			return nil
		}
	}
	return fmt.Errorf("unsupported database time value %q", raw)
}

func parseAssetReviewPagination(c *gin.Context, defaultPageSize int) (int, int, bool) {
	page, pageSize := 1, defaultPageSize
	var err error
	if raw := c.Query("page"); raw != "" {
		page, err = strconv.Atoi(raw)
		if err != nil || page < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "page must be a positive integer"})
			return 0, 0, false
		}
	}
	if raw := c.Query("page_size"); raw != "" {
		pageSize, err = strconv.Atoi(raw)
		if err != nil || pageSize < 1 || pageSize > 100 {
			c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "page_size must be between 1 and 100"})
			return 0, 0, false
		}
	}
	return page, pageSize, true
}

func reviewStatusFilter(c *gin.Context) (string, bool) {
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && status != "pending" && status != "approved" && status != "rejected" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "status must be pending, approved, or rejected"})
		return "", false
	}
	return status, true
}

func pagination(page, pageSize int, total int64) paginationDTO {
	totalPages := 0
	if total > 0 {
		totalPages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}
	return paginationDTO{Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages}
}

func (s *Server) assetReviewSKUQuery(c *gin.Context) (*gorm.DB, string, bool) {
	status, ok := reviewStatusFilter(c)
	if !ok {
		return nil, "", false
	}
	query := s.db.Table("assets").
		Joins("JOIN skus ON skus.id = assets.sk_uid").
		Joins("JOIN products ON products.id = skus.product_id").
		Joins("LEFT JOIN categories ON categories.id = products.category_id")
	if categoryID := strings.TrimSpace(c.Query("category_id")); categoryID != "" {
		value, err := strconv.ParseUint(categoryID, 10, 64)
		if err != nil || value == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "category_id must be a positive integer"})
			return nil, "", false
		}
		query = query.Where("products.category_id = ?", value)
	}
	if search := strings.TrimSpace(c.Query("q")); search != "" {
		like := "%" + search + "%"
		query = query.Where("skus.code LIKE ? OR products.name LIKE ? OR categories.name LIKE ? OR categories.name_en LIKE ? OR EXISTS (SELECT 1 FROM sku_tags JOIN tags ON tags.id = sku_tags.tag_id WHERE sku_tags.sk_uid = skus.id AND tags.name LIKE ?)", like, like, like, like, like)
	}
	return query, status, true
}

func (s *Server) listAssetReviewSKUs(c *gin.Context) {
	page, pageSize, ok := parseAssetReviewPagination(c, 40)
	if !ok {
		return
	}
	base, status, ok := s.assetReviewSKUQuery(c)
	if !ok {
		return
	}
	group := "skus.id, skus.public_id, skus.code, products.name, categories.id, categories.name, categories.name_en, categories.is_system"
	grouped := base.Select("skus.id AS sk_uid").Group(group)
	if status != "" {
		grouped = grouped.Having("SUM(CASE WHEN assets.review_status = ? THEN 1 ELSE 0 END) > 0", status)
	}
	var total int64
	if err := s.db.Table("(?) AS matching_skus", grouped).Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	base, status, ok = s.assetReviewSKUQuery(c)
	if !ok {
		return
	}

	selectFields := `skus.id AS sk_uid, skus.public_id, skus.code, products.name AS product_name,
		categories.id AS category_id, categories.name AS category_name, categories.name_en AS category_name_en,
		categories.is_system AS category_system,
		SUM(CASE WHEN assets.review_status = 'pending' THEN 1 ELSE 0 END) AS pending_count,
		SUM(CASE WHEN assets.review_status = 'approved' THEN 1 ELSE 0 END) AS approved_count,
		SUM(CASE WHEN assets.review_status = 'rejected' THEN 1 ELSE 0 END) AS rejected_count,
		COUNT(assets.id) AS total_count, MAX(assets.captured_at) AS latest_at,
		MAX(CASE WHEN assets.review_status = 'pending' THEN assets.captured_at ELSE NULL END) AS latest_pending_at`
	query := base.Select(selectFields).Group(group)
	if status != "" {
		query = query.Having("SUM(CASE WHEN assets.review_status = ? THEN 1 ELSE 0 END) > 0", status)
	}
	var rows []assetReviewSKURow
	if err := query.Order("CASE WHEN SUM(CASE WHEN assets.review_status = 'pending' THEN 1 ELSE 0 END) > 0 THEN 0 ELSE 1 END ASC").
		Order("latest_pending_at DESC").Order("skus.code ASC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	ids := make([]uint, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.SKUID)
	}
	tagsBySKU := make(map[uint][]publicTagDTO)
	coverBySKU := make(map[uint]assetReviewCover)
	if len(ids) > 0 {
		var skus []models.SKU
		if err := s.db.Preload("Tags").Where("id IN ?", ids).Find(&skus).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		for _, sku := range skus {
			tagsBySKU[sku.ID] = publicTagDTOs(sku.Tags)
		}
		var covers []assetReviewCoverRow
		coverQuery := `SELECT sk_uid, public_id, review_status, origin_type FROM (
			SELECT sk_uid, public_id, review_status, origin_type,
			ROW_NUMBER() OVER (PARTITION BY sk_uid ORDER BY CASE review_status WHEN 'pending' THEN 0 WHEN 'approved' THEN 1 ELSE 2 END, captured_at DESC) AS rn
			FROM assets WHERE sk_uid IN ?
		) ranked_assets WHERE rn = 1`
		if err := s.db.Raw(coverQuery, ids).Scan(&covers).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		for _, asset := range covers {
			coverBySKU[asset.SKUID] = assetReviewCover{PublicID: asset.PublicID, MediaURL: "/api/v1/assets/" + asset.PublicID + "/media", ReviewStatus: asset.ReviewStatus, OriginType: asset.OriginType}
		}
	}

	items := make([]assetReviewSKUSummary, 0, len(rows))
	for _, row := range rows {
		var cover *assetReviewCover
		if value, exists := coverBySKU[row.SKUID]; exists {
			copy := value
			cover = &copy
		}
		var latestPendingAt *time.Time
		if row.LatestPendingAt.Valid {
			copy := row.LatestPendingAt.Time
			latestPendingAt = &copy
		}
		items = append(items, assetReviewSKUSummary{
			PublicID: row.PublicID, Code: row.Code, ProductName: row.ProductName,
			Category: assetReviewCategory{ID: row.CategoryID, Name: row.CategoryName, NameEN: row.CategoryNameEN, IsSystem: row.CategorySystem},
			Tags:     tagsBySKU[row.SKUID], Counts: assetReviewCounts{Pending: row.PendingCount, Approved: row.ApprovedCount, Rejected: row.RejectedCount, Total: row.TotalCount},
			LatestAt: row.LatestAt.Time, LatestPendingAt: latestPendingAt, CoverAsset: cover,
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "pagination": pagination(page, pageSize, total)})
}

func (s *Server) getAssetReviewSKU(c *gin.Context) {
	publicID := c.Param("sku_id")
	if !isUUID(publicID) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "sku_id must be a UUID"})
		return
	}
	var sku models.SKU
	if err := s.db.Preload("Product.CatalogCategory").Preload("Tags").Where("public_id = ?", publicID).First(&sku).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"message": "sku not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	var counts assetReviewCounts
	if err := s.db.Model(&models.Asset{}).Where("sk_uid = ?", sku.ID).
		Select("COALESCE(SUM(CASE WHEN review_status = 'pending' THEN 1 ELSE 0 END), 0) AS pending, COALESCE(SUM(CASE WHEN review_status = 'approved' THEN 1 ELSE 0 END), 0) AS approved, COALESCE(SUM(CASE WHEN review_status = 'rejected' THEN 1 ELSE 0 END), 0) AS rejected, COUNT(*) AS total").Scan(&counts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	category := sku.Product.CatalogCategory
	c.JSON(http.StatusOK, gin.H{"public_id": sku.PublicID, "code": sku.Code, "product_name": sku.Product.Name, "category": assetReviewCategory{ID: category.ID, Name: category.Name, NameEN: category.NameEN, IsSystem: category.IsSystem}, "tags": publicTagDTOs(sku.Tags), "counts": counts})
}

type assetReviewHierarchySKU struct {
	PublicID    string                      `json:"public_id"`
	Code        string                      `json:"code"`
	ProductName string                      `json:"product_name"`
	Tags        []publicTagDTO              `json:"tags"`
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
	query := scopeAssetsForUser(s.db.Model(&models.Asset{}), currentUser(c)).Preload("SKU.Product.CatalogCategory").Preload("SKU.Tags").Preload("SOPView").Preload("PhotoSession").Order("assets.captured_at DESC")
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
				PublicID: asset.SKU.PublicID, Code: asset.SKU.Code, ProductName: asset.SKU.Product.Name, Tags: publicTagDTOs(asset.SKU.Tags),
			})
		}
		skuPosition := skuIndex[categoryKey][asset.SKU.ID]
		categories[categoryPosition].SKUs[skuPosition].Assets = append(categories[categoryPosition].SKUs[skuPosition].Assets, assetReviewHierarchyAsset{
			PublicID: asset.PublicID, MediaURL: "/api/v1/assets/" + asset.PublicID + "/media",
			ReviewStatus: asset.ReviewStatus, CapturedAt: asset.CapturedAt.Format(time.RFC3339),
			SOPViewKey: asset.SOPView.PresetKey, SOPViewName: localizedViewName{ZHCN: asset.SOPView.NameZH, EN: asset.SOPView.NameEN}, PhotoSessionCode: asset.PhotoSession.Code, OriginType: asset.OriginType, SourceSummary: safeAssetSourceSummary(asset.ProvenanceJSON),
		})
	}

	sort.Slice(categories, func(i, j int) bool { return categories[i].Name < categories[j].Name })
	for index := range categories {
		sort.Slice(categories[index].SKUs, func(i, j int) bool { return categories[index].SKUs[i].Code < categories[index].SKUs[j].Code })
	}
	c.JSON(http.StatusOK, gin.H{"data": categories})
}
