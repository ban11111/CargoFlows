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
)

// ImageObjectStore is the storage boundary for frozen source images and private
// generated images. Callers supply only internal source keys; generated keys are
// derived by ImageStorage from immutable identifiers and the verified content hash.
type ImageObjectStore interface {
	ReadSource(context.Context, string) (ImageInput, error)
	PutGenerated(context.Context, string, string, []byte) (created bool, err error)
	DeleteGenerated(context.Context, string) error
	GeneratedReadURL(context.Context, string, int) (string, error)
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
	JobPublicID    string
	ItemPublicID   string
	TurnPublicID   string
	CandidateIndex int
	Bytes          []byte
	MaxBytes       int
	MaxPixels      int64
	ExpectedWidth  int
	ExpectedHeight int
	Persist        func(context.Context, StoredGeneratedImage) error
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
	width, height, err := imageDimensions(request.Bytes, mimeType)
	if err != nil || width <= 0 || height <= 0 {
		return ValidatedImage{}, ErrImageInvalid
	}
	if int64(width) > maxPixels/int64(height) {
		return ValidatedImage{}, ErrImageTooManyPixels
	}
	if _, _, err := image.Decode(bytes.NewReader(request.Bytes)); err != nil {
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
	validated, err := s.Validate(ImageValidationRequest{Bytes: request.Bytes, MaxBytes: request.MaxBytes, MaxPixels: request.MaxPixels, ExpectedWidth: request.ExpectedWidth, ExpectedHeight: request.ExpectedHeight})
	if err != nil {
		return StoredGeneratedImage{}, err
	}
	stored := StoredGeneratedImage{
		ObjectKey: fmt.Sprintf("generated/%s/%s/%s/%d-%s.%s", request.JobPublicID, request.ItemPublicID, request.TurnPublicID, request.CandidateIndex, validated.SHA256, validated.Extension),
		MIMEType:  validated.MIMEType, Width: validated.Width, Height: validated.Height, ByteCount: validated.ByteCount, SHA256: validated.SHA256,
	}
	created, err := s.objects.PutGenerated(ctx, stored.ObjectKey, stored.MIMEType, request.Bytes)
	if err != nil {
		return StoredGeneratedImage{}, ErrImageStorageUnavailable
	}
	if request.Persist != nil {
		if err := request.Persist(ctx, stored); err != nil {
			if created {
				_ = s.objects.DeleteGenerated(ctx, stored.ObjectKey)
			}
			return StoredGeneratedImage{}, err
		}
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

func imageDimensions(value []byte, mimeType string) (int, int, error) {
	if mimeType == "image/webp" {
		return webPDimensions(value)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(value))
	if err != nil || (mimeType == "image/png" && format != "png") || (mimeType == "image/jpeg" && format != "jpeg") {
		return 0, 0, ErrImageInvalid
	}
	return config.Width, config.Height, nil
}

func webPDimensions(value []byte) (int, int, error) {
	if len(value) < 20 || string(value[:4]) != "RIFF" || string(value[8:12]) != "WEBP" {
		return 0, 0, ErrImageInvalid
	}
	switch string(value[12:16]) {
	case "VP8X":
		if len(value) < 30 || binary.LittleEndian.Uint32(value[16:20]) < 10 {
			return 0, 0, ErrImageInvalid
		}
		width := 1 + int(value[24]) + int(value[25])<<8 + int(value[26])<<16
		height := 1 + int(value[27]) + int(value[28])<<8 + int(value[29])<<16
		return width, height, nil
	case "VP8L":
		if len(value) < 25 || value[20] != 0x2f || binary.LittleEndian.Uint32(value[16:20]) < 5 {
			return 0, 0, ErrImageInvalid
		}
		packed := binary.LittleEndian.Uint32(value[21:25])
		return 1 + int(packed&0x3fff), 1 + int((packed>>14)&0x3fff), nil
	case "VP8 ":
		if len(value) < 30 || binary.LittleEndian.Uint32(value[16:20]) < 10 || value[23] != 0x9d || value[24] != 0x01 || value[25] != 0x2a {
			return 0, 0, ErrImageInvalid
		}
		return int(binary.LittleEndian.Uint16(value[26:28]) & 0x3fff), int(binary.LittleEndian.Uint16(value[28:30]) & 0x3fff), nil
	default:
		return 0, 0, ErrImageInvalid
	}
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
