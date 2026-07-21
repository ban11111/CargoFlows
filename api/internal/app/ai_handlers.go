package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cargoflows/api/internal/ai"
	"cargoflows/api/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (s *Server) getOpenAISetting(c *gin.Context) {
	if s.ai.ProviderSettings == nil {
		respondAIUnavailable(c)
		return
	}
	value, err := s.ai.ProviderSettings.Get(c.Request.Context())
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, openAISettingDTOFromView(value))
}

func (s *Server) listOpenAIModels(c *gin.Context) {
	if s.ai.ProviderSettings == nil {
		respondAIUnavailable(c)
		return
	}
	values, err := s.ai.ProviderSettings.ListModels(c.Request.Context())
	if err != nil {
		respondAIError(c, err)
		return
	}
	if values == nil {
		values = []ai.ProviderModel{}
	}
	c.JSON(http.StatusOK, gin.H{"data": values})
}

func (s *Server) updateOpenAIModels(c *gin.Context) {
	if s.ai.ProviderSettings == nil {
		respondAIUnavailable(c)
		return
	}
	var req openAIModelSelectionRequest
	if err := decodeJSONStrict(c, &req); err != nil || req.TextModel == nil {
		if err == nil {
			err = errors.New("text_model and image model configuration are required")
		}
		respondAIBadRequest(c, err)
		return
	}
	config := ai.ModelConfiguration{TextModel: *req.TextModel}
	if req.ImageAPIMode != nil && req.ImageResponsesModel != nil && req.ImageGenerationModel != nil {
		config.ImageAPIMode, config.ImageResponsesModel, config.ImageGenerationModel = *req.ImageAPIMode, *req.ImageResponsesModel, *req.ImageGenerationModel
	} else if req.ImageModel != nil {
		config.ImageAPIMode, config.ImageResponsesModel, config.ImageGenerationModel = "responses", *req.ImageModel, ai.DefaultOpenAIImageGenerationModel
	} else {
		respondAIBadRequest(c, errors.New("image_api_mode, image_responses_model, and image_generation_model are required"))
		return
	}
	value, err := s.ai.ProviderSettings.UpdateModels(c.Request.Context(), currentUser(c).ID, config)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, openAISettingDTOFromView(value))
}

func (s *Server) putOpenAISetting(c *gin.Context) {
	if s.ai.ProviderSettings == nil {
		respondAIUnavailable(c)
		return
	}
	var req openAISettingRequest
	if err := decodeJSONStrict(c, &req); err != nil || req.APIKey == nil || strings.TrimSpace(*req.APIKey) == "" {
		if err == nil {
			err = errors.New("api_key is required")
		}
		respondAIBadRequest(c, err)
		return
	}
	value, err := s.ai.ProviderSettings.Configure(c.Request.Context(), currentUser(c).ID, *req.APIKey)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, openAISettingDTOFromView(value))
}

func (s *Server) disableOpenAISetting(c *gin.Context) {
	if s.ai.ProviderSettings == nil {
		respondAIUnavailable(c)
		return
	}
	value, err := s.ai.ProviderSettings.Disable(c.Request.Context(), currentUser(c).ID)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, openAISettingDTOFromView(value))
}

func (s *Server) createAIJob(c *gin.Context) {
	var req createAIJobRequest
	if err := decodeJSONStrict(c, &req); err != nil {
		respondAIBadRequest(c, err)
		return
	}
	if !isUUID(req.SKUID) || !isUUID(req.TemplateVersionPublicID) || len(req.SelectedSlotKeys) == 0 || req.SelectedAssetIDs == nil || !allUUIDs(*req.SelectedAssetIDs) || strings.TrimSpace(req.Locale) == "" {
		respondAIBadRequest(c, errors.New("sku_id, a UUID template_version_id, locale, selected_asset_ids array, and at least one selected_slot_key are required"))
		return
	}
	value, err := s.ai.Jobs.Create(c.Request.Context(), ai.CreateJobInput{
		SKUID: req.SKUID, TemplateVersionPublicID: req.TemplateVersionPublicID,
		SelectedSlotKeys: req.SelectedSlotKeys, SelectedAssetIDs: *req.SelectedAssetIDs,
		SelectedStyleReferenceIDs: req.SelectedStyleReferenceIDs,
		Locale:                    req.Locale, CreatedByID: currentUser(c).ID,
		IdempotencyKey: c.GetHeader("Idempotency-Key"), UserPreference: req.UserPreference, GenerationOverrides: req.GenerationOverrides,
		ImageCanvases: req.ImageCanvases,
	})
	if err != nil {
		respondAIError(c, err)
		return
	}
	status := http.StatusCreated
	if value.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, value)
}

func (s *Server) listAIJobs(c *gin.Context) {
	apiMode := c.Query("api_mode")
	if apiMode == "" {
		apiMode = c.Query("image_api_mode")
	}
	if apiMode != "" && apiMode != "responses" && apiMode != "images" {
		respondAIBadRequest(c, errors.New("api_mode must be responses or images"))
		return
	}
	values, err := s.ai.Jobs.ListFiltered(c.Request.Context(), ai.JobListFilters{CreatedBy: c.Query("created_by"), Model: c.Query("model"), APIMode: apiMode})
	if err != nil {
		respondAIError(c, err)
		return
	}
	if values == nil {
		values = []ai.JobDocument{}
	}
	c.JSON(http.StatusOK, gin.H{"data": values})
}

func (s *Server) getAIJob(c *gin.Context) {
	if !requireAIUUIDParam(c, "job_id") {
		return
	}
	value, err := s.ai.Jobs.Get(c.Request.Context(), c.Param("job_id"))
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) listAITextResults(c *gin.Context) {
	if !requireAIUUIDParam(c, "job_id") {
		return
	}
	values, err := s.ai.TextResults.List(c.Request.Context(), c.Param("job_id"))
	if err != nil {
		respondAIError(c, err)
		return
	}
	if values == nil {
		values = []ai.TextResultDocument{}
	}
	c.JSON(http.StatusOK, gin.H{"data": values})
}

func (s *Server) listAIImageResults(c *gin.Context) {
	if !requireAIUUIDParam(c, "job_id") {
		return
	}
	values, err := s.ai.ImageResults.List(c.Request.Context(), c.Param("job_id"))
	if err != nil {
		respondAIError(c, err)
		return
	}
	if values == nil {
		values = []ai.ImageResultDocument{}
	}
	for index := range values {
		values[index].MediaURL = "/api/v1/ai-jobs/" + c.Param("job_id") + "/image-results/" + values[index].PublicID + "/media"
	}
	c.JSON(http.StatusOK, gin.H{"data": values})
}

func (s *Server) listAIImageThreads(c *gin.Context) {
	if !requireAIUUIDParam(c, "job_id") {
		return
	}
	values, err := s.ai.ImageResults.ListThreads(c.Request.Context(), c.Param("job_id"))
	if err != nil {
		respondAIError(c, err)
		return
	}
	if values == nil {
		values = []ai.ImageThreadDocument{}
	}
	c.JSON(http.StatusOK, gin.H{"data": values})
}

func (s *Server) createAIImageTurn(c *gin.Context) {
	if !requireAIUUIDParam(c, "job_id") || !requireAIUUIDParam(c, "item_id") {
		return
	}
	operation, parent, instruction := "", "", ""
	var mask *ai.ImageTurnMask
	if strings.HasPrefix(c.ContentType(), "multipart/form-data") {
		if err := c.Request.ParseMultipartForm(20 << 20); err != nil {
			respondAIBadRequest(c, errors.New("invalid image edit form"))
			return
		}
		operation, parent, instruction = c.PostForm("operation"), c.PostForm("parent_result_id"), c.PostForm("user_instruction")
		file, header, err := c.Request.FormFile("mask")
		if err == nil {
			defer file.Close()
			if header.Size <= 0 || header.Size > 10<<20 {
				respondAIBadRequest(c, errors.New("mask must be a PNG smaller than 10 MB"))
				return
			}
			value, readErr := io.ReadAll(io.LimitReader(file, (10<<20)+1))
			if readErr != nil || len(value) > 10<<20 {
				respondAIBadRequest(c, errors.New("mask could not be read"))
				return
			}
			decoded, decodeErr := png.Decode(bytes.NewReader(value))
			if decodeErr != nil {
				respondAIBadRequest(c, errors.New("mask must be a valid PNG"))
				return
			}
			bounds := decoded.Bounds()
			editable, protected := false, false
			for y := bounds.Min.Y; y < bounds.Max.Y && (!editable || !protected); y++ {
				for x := bounds.Min.X; x < bounds.Max.X; x++ {
					_, _, _, alpha := decoded.At(x, y).RGBA()
					editable = editable || alpha < 0xffff
					protected = protected || alpha > 0
				}
			}
			if !editable || !protected {
				respondAIBadRequest(c, errors.New("mask must contain both a marked edit region and a protected region"))
				return
			}
			validated, validErr := (&ai.ImageStorage{}).Validate(ai.ImageValidationRequest{Bytes: value, MaxBytes: 10 << 20, MaxPixels: 20_000_000})
			if validErr != nil {
				respondAIBadRequest(c, errors.New("mask dimensions or content are invalid"))
				return
			}
			key := "ai-masks/" + uuid.NewString() + "-" + validated.SHA256 + ".png"
			store, ok := s.storage.(interface {
				StoreAIMask(context.Context, string, []byte) error
			})
			if !ok || store.StoreAIMask(c.Request.Context(), key, value) != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "mask could not be stored"})
				return
			}
			mask = &ai.ImageTurnMask{ObjectKey: key, SHA256: validated.SHA256, Width: bounds.Dx(), Height: bounds.Dy(), ByteCount: int64(len(value))}
		}
	} else {
		var req struct {
			Operation       string `json:"operation"`
			ParentResultID  string `json:"parent_result_id"`
			UserInstruction string `json:"user_instruction"`
		}
		if err := decodeJSONStrict(c, &req); err != nil {
			respondAIBadRequest(c, err)
			return
		}
		operation, parent, instruction = req.Operation, req.ParentResultID, req.UserInstruction
	}
	value, err := s.ai.ImageResults.CreateTurn(c.Request.Context(), ai.CreateImageTurnInput{JobPublicID: c.Param("job_id"), ItemPublicID: c.Param("item_id"), Operation: operation, ParentResultPublicID: parent, UserInstruction: instruction, ActorID: currentUser(c).ID, Mask: mask})
	if err != nil {
		if mask != nil {
			_ = s.storage.deleteSource(context.WithoutCancel(c.Request.Context()), mask.ObjectKey)
		}
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, value)
}

func (s *Server) selectAIImageResult(c *gin.Context) {
	if !requireAIUUIDParam(c, "job_id") || !requireAIUUIDParam(c, "item_id") || !requireAIUUIDParam(c, "result_id") {
		return
	}
	if err := s.ai.ImageResults.Select(c.Request.Context(), c.Param("job_id"), c.Param("item_id"), c.Param("result_id"), currentUser(c).ID); err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"selected_result_id": c.Param("result_id")})
}

func (s *Server) submitAIImageResultToAssets(c *gin.Context) {
	if !requireAIUUIDParam(c, "job_id") || !requireAIUUIDParam(c, "item_id") || !requireAIUUIDParam(c, "result_id") {
		return
	}
	var row struct {
		ResultID, SelectedResultID, SKUID, JobID, ItemID uint
		ObjectKey, MIMEType, SHA256                      string
		Width, Height                                    int
		ByteCount                                        int64
		CreatedAt                                        time.Time
		Model, APIMode, RequestID                        string
	}
	err := s.db.Table("ai_image_results AS result").Select("result.id AS result_id,thread.selected_result_id,job.sk_uid,item.ai_job_id AS job_id,item.id AS item_id,result.object_key,result.mime_type,result.sha256,result.width,result.height,result.byte_count,result.created_at,execution.model,execution.api_mode,execution.open_ai_request_id AS request_id").Joins("JOIN ai_image_turns AS turn ON turn.id=result.ai_image_turn_id").Joins("JOIN ai_image_threads AS thread ON thread.id=turn.ai_image_thread_id").Joins("JOIN ai_job_items AS item ON item.id=thread.ai_job_item_id").Joins("JOIN ai_jobs AS job ON job.id=item.ai_job_id").Joins("JOIN ai_executions AS execution ON execution.id=result.ai_execution_id").Where("job.public_id=? AND item.public_id=? AND result.public_id=?", c.Param("job_id"), c.Param("item_id"), c.Param("result_id")).Scan(&row).Error
	if err != nil || row.ResultID == 0 {
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "image result not found"})
		return
	}
	if row.SelectedResultID != row.ResultID {
		c.JSON(http.StatusConflict, gin.H{"code": "image_result_not_selected", "message": "select this image before submitting it to the asset library"})
		return
	}
	var existing models.Asset
	if err := s.db.Where("source_ai_image_result_id=?", row.ResultID).First(&existing).Error; err == nil {
		c.JSON(http.StatusOK, gin.H{"public_id": existing.PublicID, "review_status": existing.ReviewStatus, "origin_type": existing.OriginType})
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		respondAIError(c, err)
		return
	}
	ext := "png"
	if row.MIMEType == "image/jpeg" {
		ext = "jpg"
	} else if row.MIMEType == "image/webp" {
		ext = "webp"
	}
	key := fmt.Sprintf("ai-generated-assets/%s/%s.%s", c.Param("job_id"), c.Param("result_id"), ext)
	promoter, ok := s.storage.(interface {
		PromoteGeneratedAsset(context.Context, string, string, string) error
	})
	if !ok || promoter.PromoteGeneratedAsset(c.Request.Context(), row.ObjectKey, key, row.MIMEType) != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "generated image could not be copied to the asset library"})
		return
	}
	provenance, _ := json.Marshal(map[string]any{"source": "ai_generated", "job_id": c.Param("job_id"), "job_item_id": c.Param("item_id"), "image_result_id": c.Param("result_id"), "model": row.Model, "api_mode": row.APIMode, "provider_request_id": row.RequestID, "submitted_by": currentUser(c).PublicID})
	asset := models.Asset{PublicID: uuid.NewString(), SKUID: row.SKUID, ObjectKey: key, OriginalURL: s.storage.assetURL(key), ReviewStatus: "pending", MIMEType: row.MIMEType, Width: row.Width, Height: row.Height, ByteCount: row.ByteCount, SHA256: row.SHA256, OriginType: "ai_generated", SourceAIImageResultID: &row.ResultID, ProvenanceJSON: provenance, CapturedAt: row.CreatedAt}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&asset).Error; err != nil {
			return err
		}
		actor := currentUser(c).ID
		metadata, _ := json.Marshal(map[string]any{"asset_id": asset.PublicID, "image_result_id": c.Param("result_id"), "review_status": "pending"})
		return tx.Create(&models.AIAuditEvent{PublicID: uuid.NewString(), EventType: "ai_image.submitted_to_assets", EntityType: "asset", EntityPublicID: asset.PublicID, ActorID: &actor, AIJobID: &row.JobID, AIJobItemID: &row.ItemID, MetadataJSON: metadata}).Error
	})
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			if s.db.Where("source_ai_image_result_id=?", row.ResultID).First(&existing).Error == nil {
				c.JSON(http.StatusOK, gin.H{"public_id": existing.PublicID, "review_status": existing.ReviewStatus, "origin_type": existing.OriginType})
				return
			}
		}
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"public_id": asset.PublicID, "review_status": asset.ReviewStatus, "origin_type": asset.OriginType})
}

func (s *Server) aiImageResultMedia(c *gin.Context) {
	if !requireAIUUIDParam(c, "job_id") || !requireAIUUIDParam(c, "result_id") {
		return
	}
	result, err := s.ai.ImageResults.GetForJob(c.Request.Context(), c.Param("job_id"), c.Param("result_id"))
	if errors.Is(err, ai.ErrImageResultNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "image result not found"})
		return
	}
	if err != nil {
		respondAIError(c, err)
		return
	}
	reader, ok := s.storage.(interface {
		ReadGenerated(context.Context, string) (ai.ImageInput, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "generated image storage is unavailable"})
		return
	}
	source, err := reader.ReadGenerated(c.Request.Context(), result.ObjectKey)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "read generated image failed"})
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", "inline")
	c.Data(http.StatusOK, result.MIMEType, source.Bytes)
}

func (s *Server) editAITextResult(c *gin.Context) {
	if !requireAITextResultParams(c) {
		return
	}
	var req editAITextResultRequest
	if err := decodeJSONStrict(c, &req); err != nil || len(req.Structured) == 0 || !json.Valid(req.Structured) {
		if err == nil {
			err = errors.New("structured must be a JSON object")
		}
		respondAIBadRequest(c, err)
		return
	}
	value, err := s.ai.TextResults.Edit(c.Request.Context(), c.Param("job_id"), c.Param("item_id"), c.Param("result_id"), currentUser(c).ID, req.Structured)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) approveAITextResult(c *gin.Context) {
	s.mutateAITextResult(c, s.ai.TextResults.Approve)
}

func (s *Server) rejectAITextResult(c *gin.Context) {
	s.mutateAITextResult(c, s.ai.TextResults.Reject)
}

func (s *Server) mutateAITextResult(c *gin.Context, mutate func(context.Context, string, string, string, uint) (ai.TextResultDocument, error)) {
	if !requireAITextResultParams(c) {
		return
	}
	value, err := mutate(c.Request.Context(), c.Param("job_id"), c.Param("item_id"), c.Param("result_id"), currentUser(c).ID)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) previewAITextResultApplication(c *gin.Context) {
	if !requireAITextResultParams(c) {
		return
	}
	value, err := s.ai.TextResults.Preview(c.Request.Context(), c.Param("job_id"), c.Param("item_id"), c.Param("result_id"))
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) applyAITextResult(c *gin.Context) {
	if !requireAITextResultParams(c) {
		return
	}
	value, err := s.ai.TextResults.Apply(c.Request.Context(), c.Param("job_id"), c.Param("item_id"), c.Param("result_id"), currentUser(c).ID)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) getSKUPlatformContent(c *gin.Context) {
	platform, locale := strings.TrimSpace(c.Query("platform")), strings.TrimSpace(c.Query("locale"))
	if !isUUID(c.Param("sku_id")) || platform == "" || locale == "" {
		respondAIBadRequest(c, errors.New("sku_id, platform, and locale are required"))
		return
	}
	value, err := s.ai.TextResults.GetPlatformContent(c.Request.Context(), c.Param("sku_id"), platform, locale)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func requireAITextResultParams(c *gin.Context) bool {
	return requireAIUUIDParam(c, "job_id") && requireAIUUIDParam(c, "item_id") && requireAIUUIDParam(c, "result_id")
}

func (s *Server) listAIContentTemplates(c *gin.Context) {
	includeAll, err := strconv.ParseBool(defaultString(c.Query("include_all"), "false"))
	if err != nil {
		respondAIBadRequest(c, errors.New("include_all must be a boolean"))
		return
	}
	if includeAll && !isAdministrator(currentUser(c)) {
		c.JSON(http.StatusForbidden, gin.H{"code": "forbidden", "message": "insufficient permissions"})
		return
	}
	values, err := s.ai.Templates.List(c.Request.Context(), includeAll)
	if err != nil {
		respondAIError(c, err)
		return
	}
	data := make([]aiContentTemplateDTO, 0, len(values))
	for _, value := range values {
		data = append(data, aiContentTemplateDTOFromModel(value))
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

func (s *Server) createAIContentTemplate(c *gin.Context) {
	var req aiContentTemplateMutationRequest
	if err := decodeJSONStrict(c, &req); err != nil {
		respondAIBadRequest(c, err)
		return
	}
	versionInput := templateMutationInput(req)
	created, err := s.ai.Templates.Create(c.Request.Context(), ai.CreateTemplateInput{
		NameZH: req.NameZH, NameEN: req.NameEN, TargetPlatform: req.TargetPlatform,
		CreatedByID: currentUser(c).ID, Version: versionInput,
	})
	if err != nil {
		respondAIError(c, err)
		return
	}
	created.Template.Versions = []models.AIContentTemplateVersion{created.Version}
	c.JSON(http.StatusCreated, aiContentTemplateDTOFromModel(created.Template))
}

func (s *Server) getAIContentTemplate(c *gin.Context) {
	if !requireAIUUIDParam(c, "template_id") {
		return
	}
	value, err := s.ai.Templates.Get(c.Request.Context(), c.Param("template_id"))
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, aiContentTemplateDTOFromModel(*value))
}

func (s *Server) updateAIContentTemplateVersion(c *gin.Context) {
	if !requireAIUUIDParam(c, "version_id") {
		return
	}
	var req aiContentTemplateMutationRequest
	if err := decodeJSONStrict(c, &req); err != nil {
		respondAIBadRequest(c, err)
		return
	}
	value, err := s.ai.Templates.UpdateDraft(c.Request.Context(), c.Param("version_id"), templateMutationInput(req))
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, aiContentTemplateVersionDTOFromModel(*value))
}

func (s *Server) validateAIContentTemplateVersion(c *gin.Context) {
	if !requireAIUUIDParam(c, "version_id") {
		return
	}
	issues, err := s.ai.Templates.Validate(c.Request.Context(), c.Param("version_id"))
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, templateValidationDTO(issues))
}

func (s *Server) publishAIContentTemplateVersion(c *gin.Context) {
	if !requireAIUUIDParam(c, "version_id") {
		return
	}
	issues, err := s.ai.Templates.Publish(c.Request.Context(), c.Param("version_id"), currentUser(c).ID)
	if err != nil {
		respondAIError(c, err)
		return
	}
	if len(issues) != 0 {
		c.JSON(http.StatusUnprocessableEntity, templateValidationDTO(issues))
		return
	}
	value, err := s.ai.Templates.GetVersion(c.Request.Context(), c.Param("version_id"))
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, aiContentTemplateVersionDTOFromModel(*value))
}

func (s *Server) copyAIContentTemplateVersion(c *gin.Context) {
	if !requireAIUUIDParam(c, "template_id") {
		return
	}
	var req copyAIContentTemplateVersionRequest
	if err := decodeJSONStrict(c, &req); err != nil || req.SourceVersionID == nil || !isUUID(*req.SourceVersionID) {
		if err == nil {
			err = errors.New("source_version_id must be a UUID")
		}
		respondAIBadRequest(c, err)
		return
	}
	value, err := s.ai.Templates.CopyVersion(c.Request.Context(), c.Param("template_id"), *req.SourceVersionID, currentUser(c).ID)
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusCreated, aiContentTemplateVersionDTOFromModel(*value))
}

func (s *Server) archiveAIContentTemplateVersion(c *gin.Context) {
	if !requireAIUUIDParam(c, "version_id") {
		return
	}
	if err := s.ai.Templates.Archive(c.Request.Context(), c.Param("version_id")); err != nil {
		respondAIError(c, err)
		return
	}
	value, err := s.ai.Templates.GetVersion(c.Request.Context(), c.Param("version_id"))
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, aiContentTemplateVersionDTOFromModel(*value))
}

func (s *Server) restoreAIContentTemplateVersion(c *gin.Context) {
	if !requireAIUUIDParam(c, "version_id") {
		return
	}
	if err := s.ai.Templates.Restore(c.Request.Context(), c.Param("version_id")); err != nil {
		respondAIError(c, err)
		return
	}
	value, err := s.ai.Templates.GetVersion(c.Request.Context(), c.Param("version_id"))
	if err != nil {
		respondAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, aiContentTemplateVersionDTOFromModel(*value))
}

func (s *Server) deleteAIContentTemplateDraft(c *gin.Context) {
	if !requireAIUUIDParam(c, "version_id") {
		return
	}
	if err := s.ai.Templates.DeleteDraft(c.Request.Context(), c.Param("version_id")); err != nil {
		respondAIError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func templateValidationDTO(issues []ai.ValidationIssue) aiTemplateValidationDTO {
	if issues == nil {
		issues = []ai.ValidationIssue{}
	}
	code := "template_valid"
	if len(issues) != 0 {
		code = "template_validation_failed"
	}
	return aiTemplateValidationDTO{Code: code, Issues: issues}
}

func respondAIError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ai.ErrInvalidAPIKey):
		respondAIBadRequest(c, err)
	case errors.Is(err, ai.ErrCredentialVerification):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "credential_verification_failed", "message": "OpenAI credential verification failed"})
	case errors.Is(err, ai.ErrProviderNotConfigured):
		c.JSON(http.StatusNotFound, gin.H{"code": "provider_not_configured", "message": err.Error()})
	case errors.Is(err, ai.ErrProviderNotActive):
		c.JSON(http.StatusConflict, gin.H{"code": "provider_not_active", "message": err.Error()})
	case errors.Is(err, ai.ErrProviderModelsUnavailable):
		c.JSON(http.StatusBadGateway, gin.H{"code": "provider_models_unavailable", "message": "OpenAI model list is unavailable"})
	case errors.Is(err, ai.ErrProviderModelInvalid):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "provider_model_invalid", "message": "Selected OpenAI model is not available to this credential"})
	case errors.Is(err, ai.ErrTemplateNotFound), errors.Is(err, ai.ErrTemplateVersionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": err.Error()})
	case errors.Is(err, ai.ErrJobNotFound), errors.Is(err, ai.ErrSKUNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": err.Error()})
	case errors.Is(err, ai.ErrTextResultNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": err.Error()})
	case errors.Is(err, ai.ErrImageResultNotFound), errors.Is(err, ai.ErrImageThreadNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": err.Error()})
	case errors.Is(err, ai.ErrImageTurnInvalid):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "image_turn_invalid", "message": err.Error()})
	case errors.Is(err, ai.ErrImageTurnConflict), errors.Is(err, ai.ErrImageResultNotSelected):
		c.JSON(http.StatusConflict, gin.H{"code": "lifecycle_conflict", "message": err.Error()})
	case errors.Is(err, ai.ErrTextResultInvalid):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "text_result_invalid", "message": err.Error()})
	case errors.Is(err, ai.ErrTextResultLifecycleConflict), errors.Is(err, ai.ErrTextResultApprovalRequired), errors.Is(err, ai.ErrTextResultNotEffective):
		c.JSON(http.StatusConflict, gin.H{"code": "lifecycle_conflict", "message": err.Error()})
	case errors.Is(err, ai.ErrSlotSelectionInvalid):
		respondAIBadRequest(c, err)
	case errors.Is(err, ai.ErrIdempotencyKeyInvalid), errors.Is(err, ai.ErrLocaleInvalid), errors.Is(err, ai.ErrUserPreferenceInvalid), errors.Is(err, ai.ErrUserPreferenceNotAllowed), errors.Is(err, ai.ErrGenerationOverrideInvalid), errors.Is(err, ai.ErrTemplateVersionIDInvalid), errors.Is(err, ai.ErrSKUIDInvalid), errors.Is(err, ai.ErrAssetIDInvalid):
		respondAIBadRequest(c, err)
	case errors.Is(err, ai.ErrPublishedTemplateConfigInvalid):
		c.JSON(http.StatusInternalServerError, gin.H{"code": "ai_template_configuration_invalid", "message": "Published AI template configuration is invalid"})
	case errors.Is(err, ai.ErrIdempotencyConflict):
		c.JSON(http.StatusConflict, gin.H{"code": "idempotency_conflict", "message": err.Error()})
	case errors.Is(err, ai.ErrAssetNotEligible), errors.Is(err, ai.ErrStyleReferenceNotEligible), errors.Is(err, ai.ErrPublishedSOPNotFound):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "job_input_not_eligible", "message": err.Error()})
	case errors.Is(err, ai.ErrTemplateVersionImmutable), errors.Is(err, ai.ErrTemplateDraftExists), errors.Is(err, ai.ErrTemplateSourceNotPublished):
		c.JSON(http.StatusConflict, gin.H{"code": "lifecycle_conflict", "message": err.Error()})
	case errors.Is(err, ai.ErrTemplateVersionNotPublished):
		c.JSON(http.StatusConflict, gin.H{"code": "lifecycle_conflict", "message": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "unexpected server error"})
	}
}

func respondAIBadRequest(c *gin.Context, err error) {
	c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": err.Error()})
}

func respondAIUnavailable(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{"code": "ai_not_configured", "message": "AI credential encryption is not configured"})
}

func requireAIUUIDParam(c *gin.Context, name string) bool {
	if !isUUID(c.Param(name)) {
		respondAIBadRequest(c, errors.New(name+" must be a UUID"))
		return false
	}
	return true
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
