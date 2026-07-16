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
	CategoryID  uint             `json:"category_id"`
	Name        localizedTextDTO `json:"name"`
	Description localizedTextDTO `json:"description"`
}

type updateSOPVersionRequest struct {
	Name        localizedTextDTO `json:"name"`
	Description localizedTextDTO `json:"description"`
}

type requestPose struct {
	Space                   string      `json:"space"`
	CameraPositionDirection sop.Vector3 `json:"camera_position_direction"`
	ImageUpDirection        sop.Vector3 `json:"image_up_direction"`
	Target                  sop.Vector3 `json:"target"`
}

type viewMutationRequest struct {
	Role        models.SOPViewRole `json:"role"`
	ViewKind    models.SOPViewKind `json:"view_kind"`
	Name        localizedTextDTO   `json:"name"`
	Instruction localizedTextDTO   `json:"instruction"`
	Required    bool               `json:"required"`
	Pose        requestPose        `json:"pose"`
	Composition models.Composition `json:"composition"`
}

type addSOPViewRequest struct {
	PresetKey string               `json:"preset_key"`
	Custom    *viewMutationRequest `json:"custom"`
}

type reorderRequest struct {
	PublicIDs []string `json:"public_ids"`
}
type copySOPVersionRequest struct {
	SourceVersionID string `json:"source_version_id"`
}

func decodeJSONStrict(c *gin.Context, target any) error {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain exactly one JSON object")
	}
	return nil
}

func (s *Server) createCaptureSOP(c *gin.Context) {
	var req createCaptureSOPRequest
	if err := decodeJSONStrict(c, &req); err != nil || req.CategoryID == 0 || strings.TrimSpace(req.Name.ZHCN) == "" || strings.TrimSpace(req.Name.EN) == "" {
		respondSOPBadRequest(c, errOr(err, "category_id and bilingual name are required"))
		return
	}
	created, err := NewSOPService(s.db).Create(c, CreateSOPInput{CategoryID: req.CategoryID, CreatedByID: currentUser(c).ID, NameZH: req.Name.ZHCN, NameEN: req.Name.EN, DescriptionZH: req.Description.ZHCN, DescriptionEN: req.Description.EN})
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
	version, err := NewSOPService(s.db).UpdateVersion(c, c.Param("version_id"), UpdateVersionInput{NameZH: req.Name.ZHCN, NameEN: req.Name.EN, DescriptionZH: req.Description.ZHCN, DescriptionEN: req.Description.EN})
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
	input := AddViewInput{PresetKey: req.PresetKey}
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
	if req.Pose.Space != "object" {
		respondSOPBadRequest(c, errors.New("pose.space must be object"))
		return
	}
	_, err := NewSOPService(s.db).UpdateView(c, c.Param("version_id"), c.Param("view_id"), UpdateViewInput{Role: req.Role, ViewKind: req.ViewKind, NameZH: req.Name.ZHCN, NameEN: req.Name.EN, InstructionZH: req.Instruction.ZHCN, InstructionEN: req.Instruction.EN, Required: req.Required, CameraPosition: req.Pose.CameraPositionDirection, ImageUp: req.Pose.ImageUpDirection, Target: req.Pose.Target, Composition: req.Composition})
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
	if err := decodeJSONStrict(c, &req); err != nil || !allUUIDs(req.PublicIDs) {
		respondSOPBadRequest(c, errOr(err, "public_ids must contain UUIDs"))
		return
	}
	if err := NewSOPService(s.db).Reorder(c, c.Param("version_id"), req.PublicIDs); err != nil {
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
	if err := decodeJSONStrict(c, &req); err != nil || !isUUID(req.SourceVersionID) {
		respondSOPBadRequest(c, errOr(err, "source_version_id must be a UUID"))
		return
	}
	version, err := NewSOPService(s.db).CopyVersion(c, c.Param("sop_id"), req.SourceVersionID)
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
		FileName    string `json:"file_name"`
		ContentType string `json:"content_type"`
	}
	if err := decodeJSONStrict(c, &req); err != nil {
		respondSOPBadRequest(c, err)
		return
	}
	if !strings.HasPrefix(req.ContentType, "image/") {
		respondSOPBadRequest(c, errors.New("only image uploads are supported"))
		return
	}
	if err := NewSOPService(s.db).RequireDraftView(c, c.Param("version_id"), c.Param("view_id")); err != nil {
		respondSOPError(c, err)
		return
	}
	name := strings.ReplaceAll(filepath.Base(req.FileName), " ", "-")
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
	c.JSON(http.StatusOK, gin.H{"method": "PUT", "upload_url": uploadURL, "asset_url": assetURL, "object_key": key, "expires_in": 900, "headers": gin.H{"content-type": req.ContentType}})
}

func (s *Server) addSOPReferenceImage(c *gin.Context) {
	if !requireUUIDParam(c, "version_id") || !requireUUIDParam(c, "view_id") {
		return
	}
	var req struct {
		ObjectKey    string           `json:"object_key"`
		ThumbnailURL string           `json:"thumbnail_url"`
		Caption      localizedTextDTO `json:"caption"`
		SortOrder    int              `json:"sort_order"`
	}
	if err := decodeJSONStrict(c, &req); err != nil {
		respondSOPBadRequest(c, err)
		return
	}
	prefix := fmt.Sprintf("sop-references/%s/%s/", c.Param("version_id"), c.Param("view_id"))
	if !strings.HasPrefix(req.ObjectKey, prefix) || strings.TrimPrefix(req.ObjectKey, prefix) == "" {
		respondSOPBadRequest(c, errors.New("object_key is outside this SOP view upload scope"))
		return
	}
	image, err := NewSOPService(s.db).AddReferenceImage(c, c.Param("version_id"), c.Param("view_id"), ReferenceImageInput{ObjectKey: req.ObjectKey, ThumbnailURL: req.ThumbnailURL, CaptionZH: req.Caption.ZHCN, CaptionEN: req.Caption.EN, SortOrder: req.SortOrder})
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
	if err := decodeJSONStrict(c, &req); err != nil || !allUUIDs(req.PublicIDs) {
		respondSOPBadRequest(c, errOr(err, "public_ids must contain UUIDs"))
		return
	}
	if err := NewSOPService(s.db).ReorderReferenceImages(c, c.Param("version_id"), c.Param("view_id"), req.PublicIDs); err != nil {
		respondSOPError(c, err)
		return
	}
	s.respondVersion(c, http.StatusOK, c.Param("version_id"))
}

func viewInputFromRequest(req viewMutationRequest) (sop.ViewInput, error) {
	if req.Pose.Space != "object" {
		return sop.ViewInput{}, errors.New("pose.space must be object")
	}
	return sop.ViewInput{Role: req.Role, Kind: req.ViewKind, NameZH: req.Name.ZHCN, NameEN: req.Name.EN, InstructionZH: req.Instruction.ZHCN, InstructionEN: req.Instruction.EN, Required: req.Required, CameraPosition: req.Pose.CameraPositionDirection, ImageUp: req.Pose.ImageUpDirection, Target: req.Pose.Target, Composition: req.Composition}, nil
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
	return uint(parsed), err
}
