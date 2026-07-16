package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cargoflow/api/internal/models"
	"cargoflow/api/internal/sop"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type createCaptureSOPRequest struct {
	CategoryID  *uint                 `json:"category_id"`
	Name        *localizedTextRequest `json:"name"`
	Description *localizedTextRequest `json:"description"`
}

type updateSOPVersionRequest struct {
	Name        *localizedTextRequest `json:"name"`
	Description *localizedTextRequest `json:"description"`
}

type requestPose struct {
	Space                   *string      `json:"space"`
	CameraPositionDirection *sop.Vector3 `json:"camera_position_direction"`
	ImageUpDirection        *sop.Vector3 `json:"image_up_direction"`
	Target                  *sop.Vector3 `json:"target"`
}

type viewMutationRequest struct {
	Role        *models.SOPViewRole   `json:"role"`
	ViewKind    *models.SOPViewKind   `json:"view_kind"`
	Name        *localizedTextRequest `json:"name"`
	Instruction *localizedTextRequest `json:"instruction"`
	Required    *bool                 `json:"required"`
	Pose        *requestPose          `json:"pose"`
	Composition *compositionRequest   `json:"composition"`
}

type addSOPViewRequest struct {
	PresetKey *string              `json:"preset_key"`
	Custom    *viewMutationRequest `json:"custom"`
}

type reorderRequest struct {
	PublicIDs *[]string `json:"public_ids"`
}
type copySOPVersionRequest struct {
	SourceVersionID *string `json:"source_version_id"`
}

type localizedTextRequest struct {
	ZHCN *string `json:"zh-CN"`
	EN   *string `json:"en"`
}
type compositionRequest struct {
	FrameOccupancy          *float64 `json:"frame_occupancy"`
	AspectRatio             *string  `json:"aspect_ratio"`
	AllowRotationCorrection *bool    `json:"allow_rotation_correction"`
	AllowMirror             *bool    `json:"allow_mirror"`
}

func decodeJSONStrict(c *gin.Context, target any) error {
	decoder := json.NewDecoder(c.Request.Body)
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain exactly one JSON object")
	}
	if string(raw) == "null" {
		return errors.New("request body must be a JSON object")
	}
	strict := json.NewDecoder(strings.NewReader(string(raw)))
	strict.DisallowUnknownFields()
	return strict.Decode(target)
}

func (s *Server) createCaptureSOP(c *gin.Context) {
	var req createCaptureSOPRequest
	if err := decodeJSONStrict(c, &req); err != nil {
		respondSOPBadRequest(c, err)
		return
	}
	name, err := requiredLocalized(req.Name, "name")
	if err != nil || req.CategoryID == nil || *req.CategoryID == 0 || strings.TrimSpace(name.ZHCN) == "" || strings.TrimSpace(name.EN) == "" {
		respondSOPBadRequest(c, errOr(err, "category_id and bilingual name are required"))
		return
	}
	description := localizedTextDTO{}
	if req.Description != nil {
		description, err = requiredLocalized(req.Description, "description")
		if err != nil {
			respondSOPBadRequest(c, err)
			return
		}
	}
	created, err := NewSOPService(s.db).Create(c, CreateSOPInput{CategoryID: *req.CategoryID, CreatedByID: currentUser(c).ID, NameZH: name.ZHCN, NameEN: name.EN, DescriptionZH: description.ZHCN, DescriptionEN: description.EN})
	if err != nil {
		respondSOPError(c, err)
		return
	}
	c.JSON(http.StatusCreated, versionDTOFromModel(created.Version, created.SOP.PublicID))
}

func (s *Server) listCaptureSOPs(c *gin.Context) {
	categoryID, err := parseOptionalUint(c.Query("category_id"))
	if err != nil {
		respondSOPBadRequest(c, err)
		return
	}
	values, err := NewSOPService(s.db).List(c, categoryID)
	if err != nil {
		respondSOPError(c, err)
		return
	}
	data := make([]captureSOPSummaryDTO, 0, len(values))
	for _, value := range values {
		data = append(data, summaryDTOFromModel(value))
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

func (s *Server) getCaptureSOP(c *gin.Context) {
	if !requireUUIDParam(c, "sop_id") {
		return
	}
	value, err := NewSOPService(s.db).Get(c, c.Param("sop_id"))
	if err != nil {
		respondSOPError(c, err)
		return
	}
	c.JSON(http.StatusOK, summaryDTOFromModel(*value))
}

func (s *Server) getSOPVersion(c *gin.Context) {
	if !requireUUIDParam(c, "version_id") {
		return
	}
	s.respondVersion(c, http.StatusOK, c.Param("version_id"))
}

func (s *Server) updateSOPVersion(c *gin.Context) {
	if !requireUUIDParam(c, "version_id") {
		return
	}
	var req updateSOPVersionRequest
	if err := decodeJSONStrict(c, &req); err != nil {
		respondSOPBadRequest(c, err)
		return
	}
	name, err := requiredLocalized(req.Name, "name")
	if err != nil {
		respondSOPBadRequest(c, err)
		return
	}
	description, err := requiredLocalized(req.Description, "description")
	if err != nil {
		respondSOPBadRequest(c, err)
		return
	}
	version, err := NewSOPService(s.db).UpdateVersion(c, c.Param("version_id"), UpdateVersionInput{NameZH: name.ZHCN, NameEN: name.EN, DescriptionZH: description.ZHCN, DescriptionEN: description.EN})
	if err != nil {
		respondSOPError(c, err)
		return
	}
	s.respondVersionModel(c, http.StatusOK, *version)
}

func (s *Server) addSOPView(c *gin.Context) {
	if !requireUUIDParam(c, "version_id") {
		return
	}
	var req addSOPViewRequest
	if err := decodeJSONStrict(c, &req); err != nil {
		respondSOPBadRequest(c, err)
		return
	}
	if (req.PresetKey == nil) == (req.Custom == nil) {
		respondSOPBadRequest(c, errors.New("provide exactly one of preset_key or custom"))
		return
	}
	input := AddViewInput{}
	if req.PresetKey != nil {
		input.PresetKey = *req.PresetKey
	}
	if req.Custom != nil {
		custom, err := viewInputFromRequest(*req.Custom)
		if err != nil {
			respondSOPBadRequest(c, err)
			return
		}
		input.Custom = &custom
	}
	if _, err := NewSOPService(s.db).AddView(c, c.Param("version_id"), input); err != nil {
		respondSOPError(c, err)
		return
	}
	s.respondVersion(c, http.StatusCreated, c.Param("version_id"))
}

func (s *Server) updateSOPView(c *gin.Context) {
	if !requireUUIDParam(c, "version_id") || !requireUUIDParam(c, "view_id") {
		return
	}
	var req viewMutationRequest
	if err := decodeJSONStrict(c, &req); err != nil {
		respondSOPBadRequest(c, err)
		return
	}
	viewInput, err := viewInputFromRequest(req)
	if err != nil {
		respondSOPBadRequest(c, err)
		return
	}
	_, err = NewSOPService(s.db).UpdateView(c, c.Param("version_id"), c.Param("view_id"), UpdateViewInput{Role: viewInput.Role, ViewKind: viewInput.Kind, NameZH: viewInput.NameZH, NameEN: viewInput.NameEN, InstructionZH: viewInput.InstructionZH, InstructionEN: viewInput.InstructionEN, Required: viewInput.Required, CameraPosition: viewInput.CameraPosition, ImageUp: viewInput.ImageUp, Target: viewInput.Target, Composition: viewInput.Composition})
	if err != nil {
		respondSOPError(c, err)
		return
	}
	s.respondVersion(c, http.StatusOK, c.Param("version_id"))
}

func (s *Server) deleteSOPView(c *gin.Context) {
	if !requireUUIDParam(c, "version_id") || !requireUUIDParam(c, "view_id") {
		return
	}
	if err := NewSOPService(s.db).DeleteView(c, c.Param("version_id"), c.Param("view_id")); err != nil {
		respondSOPError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) reorderSOPViews(c *gin.Context) {
	if !requireUUIDParam(c, "version_id") {
		return
	}
	var req reorderRequest
	if err := decodeJSONStrict(c, &req); err != nil || req.PublicIDs == nil || !allUUIDs(*req.PublicIDs) {
		respondSOPBadRequest(c, errOr(err, "public_ids must contain UUIDs"))
		return
	}
	if err := NewSOPService(s.db).Reorder(c, c.Param("version_id"), *req.PublicIDs); err != nil {
		respondSOPError(c, err)
		return
	}
	s.respondVersion(c, http.StatusOK, c.Param("version_id"))
}

func (s *Server) validateSOPVersion(c *gin.Context) {
	if !requireUUIDParam(c, "version_id") {
		return
	}
	errorsFound, err := NewSOPService(s.db).Validate(c, c.Param("version_id"))
	if err != nil {
		respondSOPError(c, err)
		return
	}
	code := "sop_valid"
	if len(errorsFound) > 0 {
		code = "sop_validation_failed"
	}
	c.JSON(http.StatusOK, gin.H{"code": code, "errors": errorsFound})
}

func (s *Server) publishSOPVersion(c *gin.Context) {
	if !requireUUIDParam(c, "version_id") {
		return
	}
	version, err := NewSOPService(s.db).Publish(c, c.Param("version_id"))
	if err != nil {
		respondSOPError(c, err)
		return
	}
	s.respondVersionModel(c, http.StatusOK, *version)
}

func (s *Server) copySOPVersion(c *gin.Context) {
	if !requireUUIDParam(c, "sop_id") {
		return
	}
	var req copySOPVersionRequest
	if err := decodeJSONStrict(c, &req); err != nil || req.SourceVersionID == nil || !isUUID(*req.SourceVersionID) {
		respondSOPBadRequest(c, errOr(err, "source_version_id must be a UUID"))
		return
	}
	version, err := NewSOPService(s.db).CopyVersion(c, c.Param("sop_id"), *req.SourceVersionID)
	if err != nil {
		respondSOPError(c, err)
		return
	}
	s.respondVersionModel(c, http.StatusCreated, *version)
}

func (s *Server) archiveSOPVersion(c *gin.Context) {
	if !requireUUIDParam(c, "version_id") {
		return
	}
	if err := NewSOPService(s.db).Archive(c, c.Param("version_id")); err != nil {
		respondSOPError(c, err)
		return
	}
	s.respondVersion(c, http.StatusOK, c.Param("version_id"))
}

func (s *Server) createSOPReferenceUploadURL(c *gin.Context) {
	if !requireUUIDParam(c, "version_id") || !requireUUIDParam(c, "view_id") {
		return
	}
	var req struct {
		FileName    *string `json:"file_name"`
		ContentType *string `json:"content_type"`
	}
	if err := decodeJSONStrict(c, &req); err != nil {
		respondSOPBadRequest(c, err)
		return
	}
	if req.FileName == nil || req.ContentType == nil || !strings.HasPrefix(*req.ContentType, "image/") {
		respondSOPBadRequest(c, errors.New("only image uploads are supported"))
		return
	}
	if err := NewSOPService(s.db).RequireDraftView(c, c.Param("version_id"), c.Param("view_id")); err != nil {
		respondSOPError(c, err)
		return
	}
	name := strings.ReplaceAll(filepath.Base(*req.FileName), " ", "-")
	if name == "" || name == "." {
		respondSOPBadRequest(c, errors.New("file_name is required"))
		return
	}
	key := fmt.Sprintf("sop-references/%s/%s/%d-%s", c.Param("version_id"), c.Param("view_id"), time.Now().UnixNano(), name)
	uploadURL, assetURL, err := s.storage.createUploadURL(c, key)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "object_storage_unavailable", "message": "prepare object storage upload failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"method": "PUT", "upload_url": uploadURL, "asset_url": assetURL, "object_key": key, "expires_in": 900, "headers": gin.H{"content-type": *req.ContentType}})
}

func (s *Server) addSOPReferenceImage(c *gin.Context) {
	if !requireUUIDParam(c, "version_id") || !requireUUIDParam(c, "view_id") {
		return
	}
	var req struct {
		ObjectKey    *string               `json:"object_key"`
		ThumbnailURL *string               `json:"thumbnail_url"`
		Caption      *localizedTextRequest `json:"caption"`
		SortOrder    *int                  `json:"sort_order"`
	}
	if err := decodeJSONStrict(c, &req); err != nil {
		respondSOPBadRequest(c, err)
		return
	}
	caption, err := requiredLocalized(req.Caption, "caption")
	if err != nil || req.ObjectKey == nil || req.ThumbnailURL == nil || strings.TrimSpace(*req.ThumbnailURL) == "" {
		respondSOPBadRequest(c, errOr(err, "object_key, thumbnail_url, and caption are required"))
		return
	}
	prefix := fmt.Sprintf("sop-references/%s/%s/", c.Param("version_id"), c.Param("view_id"))
	if !strings.HasPrefix(*req.ObjectKey, prefix) || strings.TrimPrefix(*req.ObjectKey, prefix) == "" {
		respondSOPBadRequest(c, errors.New("object_key is outside this SOP view upload scope"))
		return
	}
	sortOrder := 0
	if req.SortOrder != nil {
		sortOrder = *req.SortOrder
	}
	image, err := NewSOPService(s.db).AddReferenceImage(c, c.Param("version_id"), c.Param("view_id"), ReferenceImageInput{ObjectKey: *req.ObjectKey, ThumbnailURL: *req.ThumbnailURL, CaptionZH: caption.ZHCN, CaptionEN: caption.EN, SortOrder: sortOrder})
	if err != nil {
		respondSOPError(c, err)
		return
	}
	c.JSON(http.StatusCreated, referenceImageDTO{PublicID: image.PublicID, ObjectKey: image.ObjectKey, ThumbnailURL: image.ThumbnailURL, SortOrder: image.SortOrder, Caption: localizedTextDTO{ZHCN: image.CaptionZH, EN: image.CaptionEN}, CreatedAt: image.CreatedAt})
}

func (s *Server) deleteSOPReferenceImage(c *gin.Context) {
	if !requireUUIDParam(c, "version_id") || !requireUUIDParam(c, "view_id") || !requireUUIDParam(c, "image_id") {
		return
	}
	if err := NewSOPService(s.db).DeleteReferenceImage(c, c.Param("version_id"), c.Param("view_id"), c.Param("image_id")); err != nil {
		respondSOPError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) reorderSOPReferenceImages(c *gin.Context) {
	if !requireUUIDParam(c, "version_id") || !requireUUIDParam(c, "view_id") {
		return
	}
	var req reorderRequest
	if err := decodeJSONStrict(c, &req); err != nil || req.PublicIDs == nil || !allUUIDs(*req.PublicIDs) {
		respondSOPBadRequest(c, errOr(err, "public_ids must contain UUIDs"))
		return
	}
	if err := NewSOPService(s.db).ReorderReferenceImages(c, c.Param("version_id"), c.Param("view_id"), *req.PublicIDs); err != nil {
		respondSOPError(c, err)
		return
	}
	s.respondVersion(c, http.StatusOK, c.Param("version_id"))
}

func viewInputFromRequest(req viewMutationRequest) (sop.ViewInput, error) {
	if req.Role == nil || req.ViewKind == nil || req.Required == nil || req.Pose == nil || req.Composition == nil {
		return sop.ViewInput{}, errors.New("role, view_kind, required, pose, and composition are required")
	}
	name, err := requiredLocalized(req.Name, "name")
	if err != nil {
		return sop.ViewInput{}, err
	}
	instruction, err := requiredLocalized(req.Instruction, "instruction")
	if err != nil {
		return sop.ViewInput{}, err
	}
	if req.Pose.Space == nil || *req.Pose.Space != "object" || req.Pose.CameraPositionDirection == nil || req.Pose.ImageUpDirection == nil || req.Pose.Target == nil {
		return sop.ViewInput{}, errors.New("pose.space must be object")
	}
	composition, err := requiredComposition(req.Composition)
	if err != nil {
		return sop.ViewInput{}, err
	}
	return sop.ViewInput{Role: *req.Role, Kind: *req.ViewKind, NameZH: name.ZHCN, NameEN: name.EN, InstructionZH: instruction.ZHCN, InstructionEN: instruction.EN, Required: *req.Required, CameraPosition: *req.Pose.CameraPositionDirection, ImageUp: *req.Pose.ImageUpDirection, Target: *req.Pose.Target, Composition: composition}, nil
}

func requiredLocalized(value *localizedTextRequest, field string) (localizedTextDTO, error) {
	if value == nil || value.ZHCN == nil || value.EN == nil {
		return localizedTextDTO{}, fmt.Errorf("%s.zh-CN and %s.en are required", field, field)
	}
	return localizedTextDTO{ZHCN: *value.ZHCN, EN: *value.EN}, nil
}

func requiredComposition(value *compositionRequest) (models.Composition, error) {
	if value == nil || value.FrameOccupancy == nil || value.AspectRatio == nil || value.AllowRotationCorrection == nil || value.AllowMirror == nil {
		return models.Composition{}, errors.New("all composition fields are required")
	}
	return models.Composition{FrameOccupancy: *value.FrameOccupancy, AspectRatio: *value.AspectRatio, AllowRotationCorrection: *value.AllowRotationCorrection, AllowMirror: *value.AllowMirror}, nil
}

func (s *Server) respondVersion(c *gin.Context, status int, versionID string) {
	version, err := NewSOPService(s.db).GetVersion(c, versionID)
	if err != nil {
		respondSOPError(c, err)
		return
	}
	s.respondVersionModel(c, status, *version)
}

func (s *Server) respondVersionModel(c *gin.Context, status int, version models.SOPVersion) {
	var parent models.CaptureSOP
	if err := s.db.Select("public_id").First(&parent, version.CaptureSOPID).Error; err != nil {
		respondSOPError(c, err)
		return
	}
	if len(version.Views) == 0 {
		loaded, err := NewSOPService(s.db).GetVersion(c, version.PublicID)
		if err != nil {
			respondSOPError(c, err)
			return
		}
		version = *loaded
	}
	c.JSON(status, versionDTOFromModel(version, parent.PublicID))
}

func respondSOPError(c *gin.Context, err error) {
	var validation *SOPValidationError
	switch {
	case errors.As(err, &validation):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "sop_validation_failed", "errors": validation.Errors})
	case errors.Is(err, ErrVersionNotFound), errors.Is(err, ErrCaptureSOPNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": err.Error()})
	case errors.Is(err, ErrVersionImmutable):
		c.JSON(http.StatusConflict, gin.H{"code": "version_immutable", "message": err.Error()})
	case errors.Is(err, ErrReferenceLocked):
		c.JSON(http.StatusConflict, gin.H{"code": "reference_front_locked", "message": err.Error()})
	case errors.Is(err, ErrDraftExists):
		c.JSON(http.StatusConflict, gin.H{"code": "draft_exists", "message": err.Error()})
	case errors.Is(err, ErrSourceVersionNotPublished):
		c.JSON(http.StatusConflict, gin.H{"code": "source_version_not_published", "message": err.Error()})
	default:
		if isInputError(err) {
			respondSOPBadRequest(c, err)
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "internal server error"})
		}
	}
}

func isInputError(err error) bool {
	return errors.Is(err, sop.ErrZeroVector) || errors.Is(err, sop.ErrNonFiniteVector) || errors.Is(err, sop.ErrParallelVectors) || strings.Contains(err.Error(), "must") || strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "provide either") || strings.Contains(err.Error(), "unknown SOP preset") || strings.Contains(err.Error(), "does not belong") || strings.Contains(err.Error(), "appears more than once") || strings.Contains(err.Error(), "sort order")
}
func respondSOPBadRequest(c *gin.Context, err error) {
	c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": err.Error()})
}
func errOr(err error, message string) error {
	if err != nil {
		return err
	}
	return errors.New(message)
}
func isUUID(value string) bool { _, err := uuid.Parse(value); return err == nil }
func allUUIDs(values []string) bool {
	for _, value := range values {
		if !isUUID(value) {
			return false
		}
	}
	return true
}
func requireUUIDParam(c *gin.Context, name string) bool {
	if isUUID(c.Param(name)) {
		return true
	}
	respondSOPBadRequest(c, fmt.Errorf("%s must be a UUID", name))
	return false
}
func parseOptionalUint(value string) (uint, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err == nil && parsed == 0 {
		return 0, errors.New("value must be greater than zero")
	}
	return uint(parsed), err
}
