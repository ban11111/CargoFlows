package ai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"strings"
	"time"
)

const (
	maxStoredImageBytes          = 50 << 20
	maxStoredImagePixels         = 20_000_000
	generatedImageReadURLSeconds = 300
)

var (
	ErrImageInvalid            = errors.New("image bytes are invalid")
	ErrImageTooLarge           = errors.New("image byte limit exceeded")
	ErrImageTooManyPixels      = errors.New("image pixel limit exceeded")
	ErrImageDimensionsInvalid  = errors.New("image dimensions do not match the request")
	ErrImageAspectInvalid      = errors.New("image aspect ratio does not match the request")
	ErrGeneratedImageReference = errors.New("generated image reference is invalid")
	ErrImageStorageUnavailable = errors.New("image storage is unavailable")
	ErrImageStoragePending     = errors.New("generated image storage is pending")
	ErrImageStorageCleanup     = errors.New("generated image storage cleanup failed")
)

// ImageObjectStore is the storage boundary for frozen source images and private
// generated images. Callers supply only internal source keys; generated keys are
// derived by ImageStorage from immutable identifiers and the verified content hash.
type ImageObjectStore interface {
	ReadSource(context.Context, string) (ImageInput, error)
	ClaimGenerated(context.Context, string, string, []byte) (GeneratedObjectClaim, error)
	CommitGenerated(context.Context, GeneratedObjectClaim) error
	CleanupGenerated(context.Context, GeneratedObjectClaim) error
	GeneratedReadURL(context.Context, string, int) (string, error)
}

type GeneratedObjectState string

const (
	GeneratedObjectCreated   GeneratedObjectState = "created"
	GeneratedObjectPending   GeneratedObjectState = "pending"
	GeneratedObjectCommitted GeneratedObjectState = "committed"
)

type GeneratedObjectClaim struct {
	ObjectKey string
	Token     string
	State     GeneratedObjectState
}

type ImageValidationRequest struct {
	Bytes               []byte
	MaxBytes            int
	MaxPixels           int64
	ExpectedWidth       int
	ExpectedHeight      int
	ExpectedAspectRatio float64
	AspectTolerance     float64
}

type ValidatedImage struct {
	MIMEType  string
	Extension string
	Width     int
	Height    int
	ByteCount int64
	SHA256    string
}

type GeneratedImageStoreRequest struct {
	JobPublicID         string
	ItemPublicID        string
	TurnPublicID        string
	CandidateIndex      int
	Bytes               []byte
	MaxBytes            int
	MaxPixels           int64
	ExpectedWidth       int
	ExpectedHeight      int
	ExpectedAspectRatio float64
	AspectTolerance     float64
	Persist             func(context.Context, StoredGeneratedImage) error
}

type StoredGeneratedImage struct {
	ObjectKey string
	MIMEType  string
	Width     int
	Height    int
	ByteCount int64
	SHA256    string
}

type ImageStorage struct {
	objects ImageObjectStore
}

func NewImageStorage(objects ImageObjectStore) *ImageStorage {
	return &ImageStorage{objects: objects}
}

func (s *ImageStorage) ReadSource(ctx context.Context, objectKey string) (ImageInput, error) {
	if s == nil || s.objects == nil || !safeSourceObjectKey(objectKey) {
		return ImageInput{}, ErrImageInvalid
	}
	input, err := s.objects.ReadSource(ctx, objectKey)
	if err != nil {
		return ImageInput{}, ErrImageStorageUnavailable
	}
	validated, err := s.Validate(ImageValidationRequest{Bytes: input.Bytes})
	if err != nil {
		return ImageInput{}, err
	}
	return ImageInput{MIMEType: validated.MIMEType, Bytes: input.Bytes}, nil
}

func (s *ImageStorage) GeneratedReadURL(ctx context.Context, objectKey string) (string, error) {
	if s == nil || s.objects == nil || !safeGeneratedObjectKey(objectKey) {
		return "", ErrGeneratedImageReference
	}
	url, err := s.objects.GeneratedReadURL(ctx, objectKey, generatedImageReadURLSeconds)
	if err != nil {
		return "", ErrImageStorageUnavailable
	}
	return url, nil
}

func (s *ImageStorage) Validate(request ImageValidationRequest) (ValidatedImage, error) {
	maxBytes := request.MaxBytes
	if maxBytes == 0 {
		maxBytes = maxStoredImageBytes
	}
	maxPixels := request.MaxPixels
	if maxPixels == 0 {
		maxPixels = maxStoredImagePixels
	}
	if len(request.Bytes) == 0 || maxBytes <= 0 || maxPixels <= 0 {
		return ValidatedImage{}, ErrImageInvalid
	}
	if len(request.Bytes) > maxBytes {
		return ValidatedImage{}, ErrImageTooLarge
	}
	mimeType, err := DetectImageMIME(request.Bytes)
	if err != nil {
		return ValidatedImage{}, err
	}
	width, height, innerWidth, innerHeight, err := imageGeometry(request.Bytes, mimeType)
	if err != nil || width <= 0 || height <= 0 {
		return ValidatedImage{}, ErrImageInvalid
	}
	if exceedsPixelLimit(width, height, maxPixels) || exceedsPixelLimit(innerWidth, innerHeight, maxPixels) {
		return ValidatedImage{}, ErrImageTooManyPixels
	}
	decoded, format, err := image.Decode(bytes.NewReader(request.Bytes))
	if err != nil || format != strings.TrimPrefix(mimeType, "image/") || decoded.Bounds().Dx() != width || decoded.Bounds().Dy() != height {
		return ValidatedImage{}, ErrImageInvalid
	}
	if (request.ExpectedWidth == 0) != (request.ExpectedHeight == 0) {
		return ValidatedImage{}, ErrImageDimensionsInvalid
	}
	if request.ExpectedWidth != 0 && (width != request.ExpectedWidth || height != request.ExpectedHeight) {
		return ValidatedImage{}, ErrImageDimensionsInvalid
	}
	if request.ExpectedAspectRatio != 0 {
		tolerance := request.AspectTolerance
		if tolerance == 0 {
			tolerance = 0.01
		}
		if request.ExpectedAspectRatio <= 0 || tolerance < 0 || tolerance > 0.1 || math.Abs(float64(width)/float64(height)-request.ExpectedAspectRatio) > tolerance {
			return ValidatedImage{}, ErrImageAspectInvalid
		}
	}
	hash := sha256.Sum256(request.Bytes)
	return ValidatedImage{MIMEType: mimeType, Extension: imageExtension(mimeType), Width: width, Height: height, ByteCount: int64(len(request.Bytes)), SHA256: hex.EncodeToString(hash[:])}, nil
}

func (s *ImageStorage) StoreGenerated(ctx context.Context, request GeneratedImageStoreRequest) (StoredGeneratedImage, error) {
	if s == nil || s.objects == nil || request.CandidateIndex < 1 || !safeGeneratedPathSegment(request.JobPublicID, false) || !safeGeneratedPathSegment(request.ItemPublicID, false) || !safeGeneratedPathSegment(request.TurnPublicID, false) {
		return StoredGeneratedImage{}, ErrGeneratedImageReference
	}
	validated, err := s.Validate(ImageValidationRequest{Bytes: request.Bytes, MaxBytes: request.MaxBytes, MaxPixels: request.MaxPixels, ExpectedWidth: request.ExpectedWidth, ExpectedHeight: request.ExpectedHeight, ExpectedAspectRatio: request.ExpectedAspectRatio, AspectTolerance: request.AspectTolerance})
	if err != nil {
		return StoredGeneratedImage{}, err
	}
	stored := StoredGeneratedImage{
		ObjectKey: fmt.Sprintf("generated/%s/%s/%s/%d-%s.%s", request.JobPublicID, request.ItemPublicID, request.TurnPublicID, request.CandidateIndex, validated.SHA256, validated.Extension),
		MIMEType:  validated.MIMEType, Width: validated.Width, Height: validated.Height, ByteCount: validated.ByteCount, SHA256: validated.SHA256,
	}
	claim, err := s.objects.ClaimGenerated(ctx, stored.ObjectKey, stored.MIMEType, request.Bytes)
	if err != nil {
		return StoredGeneratedImage{}, ErrImageStorageUnavailable
	}
	switch claim.State {
	case GeneratedObjectPending:
		return StoredGeneratedImage{}, ErrImageStoragePending
	case GeneratedObjectCommitted:
		return stored, nil
	case GeneratedObjectCreated:
	default:
		return StoredGeneratedImage{}, ErrImageStorageUnavailable
	}
	if request.Persist != nil {
		if err := request.Persist(ctx, stored); err != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			cleanupErr := s.objects.CleanupGenerated(cleanupCtx, claim)
			cancel()
			if cleanupErr != nil {
				return StoredGeneratedImage{}, errors.Join(err, ErrImageStorageCleanup)
			}
			return StoredGeneratedImage{}, err
		}
	}
	if err := s.objects.CommitGenerated(ctx, claim); err != nil {
		return StoredGeneratedImage{}, ErrImageStorageUnavailable
	}
	return stored, nil
}

func DetectImageMIME(value []byte) (string, error) {
	switch {
	case len(value) >= 8 && string(value[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png", nil
	case len(value) >= 3 && value[0] == 0xff && value[1] == 0xd8 && value[2] == 0xff:
		return "image/jpeg", nil
	case len(value) >= 16 && string(value[:4]) == "RIFF" && string(value[8:12]) == "WEBP":
		return "image/webp", nil
	default:
		return "", ErrImageInvalid
	}
}

func exceedsPixelLimit(width, height int, maxPixels int64) bool {
	return width <= 0 || height <= 0 || int64(width) > maxPixels/int64(height)
}

func imageGeometry(value []byte, mimeType string) (int, int, int, int, error) {
	if mimeType == "image/webp" {
		geometry, err := parseWebPGeometry(value)
		if err != nil {
			return 0, 0, 0, 0, err
		}
		return geometry.canvasWidth, geometry.canvasHeight, geometry.frameWidth, geometry.frameHeight, nil
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(value))
	if err != nil || (mimeType == "image/png" && format != "png") || (mimeType == "image/jpeg" && format != "jpeg") {
		return 0, 0, 0, 0, ErrImageInvalid
	}
	return config.Width, config.Height, config.Width, config.Height, nil
}

func webPDimensions(value []byte) (int, int, error) {
	geometry, err := parseWebPGeometry(value)
	if err != nil {
		return 0, 0, err
	}
	return geometry.canvasWidth, geometry.canvasHeight, nil
}

type webPGeometry struct {
	canvasWidth  int
	canvasHeight int
	frameWidth   int
	frameHeight  int
}

func parseWebPGeometry(value []byte) (webPGeometry, error) {
	if len(value) < 20 || string(value[:4]) != "RIFF" || string(value[8:12]) != "WEBP" || uint64(binary.LittleEndian.Uint32(value[4:8]))+8 != uint64(len(value)) {
		return webPGeometry{}, ErrImageInvalid
	}
	var result webPGeometry
	var hasCanvas, hasFrame bool
	for offset := 12; offset < len(value); {
		if len(value)-offset < 8 {
			return webPGeometry{}, ErrImageInvalid
		}
		kind := string(value[offset : offset+4])
		size := int64(binary.LittleEndian.Uint32(value[offset+4 : offset+8]))
		dataStart := offset + 8
		dataEnd := int64(dataStart) + size
		if size < 0 || dataEnd > int64(len(value)) {
			return webPGeometry{}, ErrImageInvalid
		}
		data := value[dataStart:dataEnd]
		next := dataEnd
		if size%2 == 1 {
			next++
		}
		if next > int64(len(value)) {
			return webPGeometry{}, ErrImageInvalid
		}
		switch kind {
		case "VP8X":
			if hasCanvas || size != 10 {
				return webPGeometry{}, ErrImageInvalid
			}
			hasCanvas = true
			result.canvasWidth = 1 + int(data[4]) + int(data[5])<<8 + int(data[6])<<16
			result.canvasHeight = 1 + int(data[7]) + int(data[8])<<8 + int(data[9])<<16
		case "VP8L":
			if hasFrame {
				return webPGeometry{}, ErrImageInvalid
			}
			width, height, err := vp8LDimensions(data)
			if err != nil {
				return webPGeometry{}, err
			}
			hasFrame, result.frameWidth, result.frameHeight = true, width, height
		case "VP8 ":
			if hasFrame {
				return webPGeometry{}, ErrImageInvalid
			}
			width, height, err := vp8Dimensions(data)
			if err != nil {
				return webPGeometry{}, err
			}
			hasFrame, result.frameWidth, result.frameHeight = true, width, height
		case "ANIM", "ANMF":
			return webPGeometry{}, ErrImageInvalid
		}
		offset = int(next)
	}
	if !hasFrame {
		return webPGeometry{}, ErrImageInvalid
	}
	if !hasCanvas {
		result.canvasWidth, result.canvasHeight = result.frameWidth, result.frameHeight
	}
	if result.canvasWidth <= 0 || result.canvasHeight <= 0 || result.frameWidth <= 0 || result.frameHeight <= 0 || result.frameWidth > result.canvasWidth || result.frameHeight > result.canvasHeight {
		return webPGeometry{}, ErrImageInvalid
	}
	return result, nil
}

func vp8LDimensions(data []byte) (int, int, error) {
	if len(data) < 5 || data[0] != 0x2f {
		return 0, 0, ErrImageInvalid
	}
	packed := binary.LittleEndian.Uint32(data[1:5])
	return 1 + int(packed&0x3fff), 1 + int((packed>>14)&0x3fff), nil
}

func vp8Dimensions(data []byte) (int, int, error) {
	if len(data) < 10 || data[3] != 0x9d || data[4] != 0x01 || data[5] != 0x2a {
		return 0, 0, ErrImageInvalid
	}
	return int(binary.LittleEndian.Uint16(data[6:8]) & 0x3fff), int(binary.LittleEndian.Uint16(data[8:10]) & 0x3fff), nil
}

func imageExtension(mimeType string) string {
	if mimeType == "image/jpeg" {
		return "jpg"
	}
	if mimeType == "image/webp" {
		return "webp"
	}
	return "png"
}

func safeGeneratedPathSegment(value string, allowSlash bool) bool {
	if value == "" || len(value) > 512 || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") || strings.Contains(value, "..") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || (!allowSlash && strings.Contains(value, "/")) {
			return false
		}
		for _, character := range segment {
			if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_') {
				return false
			}
		}
	}
	return true
}

func safeSourceObjectKey(value string) bool {
	if value == "" || len(value) > 512 || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		for _, character := range segment {
			if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.') {
				return false
			}
		}
	}
	return true
}

func safeGeneratedObjectKey(value string) bool {
	if !strings.HasPrefix(value, "generated/") {
		return false
	}
	segments := strings.Split(strings.TrimPrefix(value, "generated/"), "/")
	if len(segments) != 4 {
		return false
	}
	for _, segment := range segments[:3] {
		if !safeGeneratedPathSegment(segment, false) {
			return false
		}
	}
	fileParts := strings.Split(segments[3], ".")
	if len(fileParts) != 2 || (fileParts[1] != "png" && fileParts[1] != "jpg" && fileParts[1] != "webp") {
		return false
	}
	keyParts := strings.Split(fileParts[0], "-")
	if len(keyParts) != 2 || !positiveDecimal(keyParts[0]) || len(keyParts[1]) != 64 {
		return false
	}
	for _, character := range keyParts[1] {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func positiveDecimal(value string) bool {
	if value == "" || value == "0" || (len(value) > 1 && value[0] == '0') {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
