package app

import (
	"net/http"
	"strings"
	"time"

	"cargoflow/api/internal/config"
	"cargoflow/api/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

type Server struct {
	cfg     config.Config
	db      *gorm.DB
	storage *objectStore
}

func NewRouter(cfg config.Config, db *gorm.DB) *gin.Engine {
	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	storage, err := newObjectStore(cfg)
	if err != nil {
		panic("configure object storage: " + err.Error())
	}
	server := &Server{cfg: cfg, db: db, storage: storage}
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	v1 := router.Group("/api/v1")
	v1.POST("/auth/login", server.login)

	protected := v1.Group("")
	protected.Use(server.requireAuth())
	protected.GET("/skus", server.listSKUs)
	protected.POST("/skus", server.createSKU)
	protected.GET("/skus/:id", server.getSKU)
	protected.PATCH("/skus/:id", server.updateSKU)
	protected.POST("/skus/:id/inventory-adjustments", server.createInventoryAdjustment)
	protected.GET("/skus/:id/inventory-history", server.listInventoryHistory)
	protected.GET("/categories", server.listCategories)
	protected.POST("/categories", server.createCategory)
	protected.DELETE("/categories/:id", server.deleteCategory)
	protected.GET("/tags", server.listTags)
	protected.POST("/capture-sops", server.createCaptureSOP)
	protected.GET("/capture-sops", server.listCaptureSOPs)
	protected.GET("/capture-sops/:sop_id", server.getCaptureSOP)
	protected.GET("/sop-versions/:version_id", server.getSOPVersion)
	protected.PATCH("/sop-versions/:version_id", server.updateSOPVersion)
	protected.POST("/sop-versions/:version_id/views", server.addSOPView)
	protected.PATCH("/sop-versions/:version_id/views/:view_id", server.updateSOPView)
	protected.DELETE("/sop-versions/:version_id/views/:view_id", server.deleteSOPView)
	protected.PUT("/sop-versions/:version_id/view-order", server.reorderSOPViews)
	protected.POST("/sop-versions/:version_id/validate", server.validateSOPVersion)
	protected.POST("/sop-versions/:version_id/publish", server.publishSOPVersion)
	protected.POST("/capture-sops/:sop_id/versions", server.copySOPVersion)
	protected.POST("/sop-versions/:version_id/archive", server.archiveSOPVersion)
	protected.POST("/sop-versions/:version_id/views/:view_id/reference-images/upload-url", server.createSOPReferenceUploadURL)
	protected.POST("/sop-versions/:version_id/views/:view_id/reference-images", server.addSOPReferenceImage)
	protected.DELETE("/sop-versions/:version_id/views/:view_id/reference-images/:image_id", server.deleteSOPReferenceImage)
	protected.PUT("/sop-versions/:version_id/views/:view_id/reference-image-order", server.reorderSOPReferenceImages)
	protected.POST("/photo-sessions", server.createPhotoSession)
	protected.POST("/assets/upload-url", server.createUploadURL)
	protected.POST("/assets/complete", server.completeAssetUpload)
	protected.GET("/assets/review", server.listAssetsForReview)
	protected.GET("/assets/review/hierarchy", server.listAssetReviewHierarchy)
	protected.PATCH("/assets/:id/review", server.reviewAsset)
	protected.GET("/ai-jobs", server.listAIJobs)
	protected.POST("/ai-jobs", server.createAIJob)
	protected.GET("/users", server.listUsers)

	return router
}

func (s *Server) requireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		tokenValue := strings.TrimPrefix(header, "Bearer ")
		if tokenValue == header || tokenValue == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "missing bearer token"})
			return
		}

		claims := jwt.MapClaims{}
		token, err := jwt.ParseWithClaims(tokenValue, claims, func(token *jwt.Token) (any, error) {
			return []byte(s.cfg.JWTSecret), nil
		})
		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "invalid token"})
			return
		}

		userID, ok := claims["sub"].(float64)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "invalid token subject"})
			return
		}

		var user models.User
		if err := s.db.First(&user, uint(userID)).Error; err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "user not found"})
			return
		}

		c.Set("user", user)
		c.Next()
	}
}

func currentUser(c *gin.Context) models.User {
	value, _ := c.Get("user")
	user, _ := value.(models.User)
	return user
}

func (s *Server) issueToken(user models.User) (string, error) {
	claims := jwt.MapClaims{
		"sub":  user.ID,
		"role": user.Role,
		"exp":  time.Now().Add(7 * 24 * time.Hour).Unix(),
		"iat":  time.Now().Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.cfg.JWTSecret))
}
