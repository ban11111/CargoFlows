package ai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"reflect"
	"strings"
	"testing"
)

type memoryImageObjectStore struct {
	source      map[string]ImageInput
	generated   map[string]storedObject
	putCalls    int
	removeCalls int
	readExpiry  int
	readKey     string
	putErr      error
}

type storedObject struct {
	MIMEType string
	Bytes    []byte
}

func (s *memoryImageObjectStore) ReadSource(_ context.Context, objectKey string) (ImageInput, error) {
	input, ok := s.source[objectKey]
	if !ok {
		return ImageInput{}, errors.New("source object not found")
	}
	return input, nil
}

func (s *memoryImageObjectStore) PutGenerated(_ context.Context, objectKey, mimeType string, value []byte) (bool, error) {
	if s.putErr != nil {
		return false, s.putErr
	}
	if _, ok := s.generated[objectKey]; ok {
		return false, nil
	}
	s.putCalls++
	s.generated[objectKey] = storedObject{MIMEType: mimeType, Bytes: append([]byte(nil), value...)}
	return true, nil
}

func TestImageStorageDoesNotReturnObjectKeysFromStorageErrors(t *testing.T) {
	t.Parallel()
	objects := &memoryImageObjectStore{generated: map[string]storedObject{}, putErr: errors.New("put generated/job/item/turn/1-sensitive.png failed")}
	storage := NewImageStorage(objects)
	_, err := storage.StoreGenerated(t.Context(), GeneratedImageStoreRequest{JobPublicID: "job", ItemPublicID: "item", TurnPublicID: "turn", CandidateIndex: 1, Bytes: pngFixture(t, 2, 2)})
	if err == nil || strings.Contains(err.Error(), "generated/") {
		t.Fatalf("StoreGenerated() leaked an object key in error: %v", err)
	}
}

func (s *memoryImageObjectStore) DeleteGenerated(_ context.Context, objectKey string) error {
	s.removeCalls++
	delete(s.generated, objectKey)
	return nil
}

func (s *memoryImageObjectStore) GeneratedReadURL(_ context.Context, objectKey string, expiry int) (string, error) {
	s.readKey = objectKey
	s.readExpiry = expiry
	return "https://signed.example.test/" + objectKey, nil
}

func TestImageStorageDetectsSupportedMagicBytes(t *testing.T) {
	t.Parallel()
	for name, fixture := range map[string]struct {
		bytes []byte
		mime  string
	}{
		"png":  {bytes: pngFixture(t, 2, 2), mime: "image/png"},
		"jpeg": {bytes: jpegFixture(t, 2, 2), mime: "image/jpeg"},
		"webp": {bytes: webPFixture(2, 2), mime: "image/webp"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := DetectImageMIME(fixture.bytes)
			if err != nil || got != fixture.mime {
				t.Fatalf("DetectImageMIME() = %q, %v; want %q, nil", got, err, fixture.mime)
			}
		})
	}
}

func TestImageStorageRejectsInvalidAndOversizedImagesBeforeStorage(t *testing.T) {
	t.Parallel()
	storage := NewImageStorage(&memoryImageObjectStore{generated: map[string]storedObject{}})
	truncatedPNG := pngFixture(t, 2, 2)
	truncatedPNG = truncatedPNG[:len(truncatedPNG)-12]
	for name, input := range map[string]ImageValidationRequest{
		"bad magic":           {Bytes: []byte("not-an-image")},
		"decode failure":      {Bytes: append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 20)...)},
		"truncated PNG data":  {Bytes: truncatedPNG},
		"truncated WebP data": {Bytes: webPFixture(2, 2)},
		"decompression bomb":  {Bytes: pngHeader(100000, 100000)},
		"max bytes":           {Bytes: pngFixture(t, 2, 2), MaxBytes: 8},
		"max pixels":          {Bytes: pngFixture(t, 3, 3), MaxPixels: 8},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := storage.Validate(input); err == nil {
				t.Fatal("Validate() unexpectedly accepted unsafe image")
			}
		})
	}
}

func TestImageStorageEnforcesExactDimensionsAndAspectTolerance(t *testing.T) {
	t.Parallel()
	storage := NewImageStorage(&memoryImageObjectStore{generated: map[string]storedObject{}})
	if _, err := storage.Validate(ImageValidationRequest{Bytes: pngFixture(t, 1023, 1024), ExpectedWidth: 1024, ExpectedHeight: 1024}); err == nil {
		t.Fatal("Validate() accepted an image with non-exact requested dimensions")
	}
	if _, err := storage.Validate(ImageValidationRequest{Bytes: pngFixture(t, 150, 100), ExpectedAspectRatio: 1.5, AspectTolerance: 0.01}); err != nil {
		t.Fatalf("Validate() rejected matching aspect ratio: %v", err)
	}
	if _, err := storage.Validate(ImageValidationRequest{Bytes: pngFixture(t, 148, 100), ExpectedAspectRatio: 1.5, AspectTolerance: 0.01}); err == nil {
		t.Fatal("Validate() accepted an image outside aspect tolerance")
	}
}

func TestImageStorageStoresWithDeterministicSafeKeyAndHash(t *testing.T) {
	t.Parallel()
	objects := &memoryImageObjectStore{generated: map[string]storedObject{}}
	storage := NewImageStorage(objects)
	value := pngFixture(t, 2, 2)
	stored, err := storage.StoreGenerated(t.Context(), GeneratedImageStoreRequest{
		JobPublicID: "job-123", ItemPublicID: "item-456", TurnPublicID: "turn-789", CandidateIndex: 2,
		Bytes: value, ExpectedWidth: 2, ExpectedHeight: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(value)
	wantKey := "generated/job-123/item-456/turn-789/2-" + fmtHex(hash[:]) + ".png"
	if stored.ObjectKey != wantKey || stored.SHA256 != fmtHex(hash[:]) || stored.MIMEType != "image/png" || stored.Width != 2 || stored.Height != 2 || stored.ByteCount != int64(len(value)) {
		t.Fatalf("unexpected stored image: %#v", stored)
	}
	if strings.Contains(stored.ObjectKey, "..") || strings.Contains(stored.ObjectKey, "\\") || objects.putCalls != 1 {
		t.Fatalf("unsafe key or unexpected write: key=%q writes=%d", stored.ObjectKey, objects.putCalls)
	}
}

func TestImageStorageIsIdempotentForTheSameHash(t *testing.T) {
	t.Parallel()
	objects := &memoryImageObjectStore{generated: map[string]storedObject{}}
	storage := NewImageStorage(objects)
	request := GeneratedImageStoreRequest{JobPublicID: "job", ItemPublicID: "item", TurnPublicID: "turn", CandidateIndex: 1, Bytes: pngFixture(t, 2, 2)}
	first, err := storage.StoreGenerated(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := storage.StoreGenerated(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || objects.putCalls != 1 || len(objects.generated) != 1 {
		t.Fatalf("same image was not idempotent: first=%#v second=%#v writes=%d objects=%d", first, second, objects.putCalls, len(objects.generated))
	}
}

func TestImageStorageCleansUpNewObjectWhenPersistenceFails(t *testing.T) {
	t.Parallel()
	objects := &memoryImageObjectStore{generated: map[string]storedObject{}}
	storage := NewImageStorage(objects)
	request := GeneratedImageStoreRequest{JobPublicID: "job", ItemPublicID: "item", TurnPublicID: "turn", CandidateIndex: 1, Bytes: pngFixture(t, 2, 2), Persist: func(context.Context, StoredGeneratedImage) error {
		return errors.New("database unavailable")
	}}
	if _, err := storage.StoreGenerated(t.Context(), request); err == nil {
		t.Fatal("StoreGenerated() unexpectedly succeeded")
	}
	if len(objects.generated) != 0 || objects.removeCalls != 1 {
		t.Fatalf("new object was not cleaned up after persistence failure: objects=%d removes=%d", len(objects.generated), objects.removeCalls)
	}
}

func TestImageStorageDoesNotAcceptCallerOutputKeys(t *testing.T) {
	t.Parallel()
	typeOfRequest := reflect.TypeFor[GeneratedImageStoreRequest]()
	if _, exists := typeOfRequest.FieldByName("ObjectKey"); exists {
		t.Fatal("GeneratedImageStoreRequest must not accept a caller-provided output key")
	}
}

func TestImageStorageReadsSourcesAndIssuesShortLivedAccess(t *testing.T) {
	t.Parallel()
	objects := &memoryImageObjectStore{source: map[string]ImageInput{"assets/source.png": {MIMEType: "image/png", Bytes: pngFixture(t, 2, 2)}}, generated: map[string]storedObject{}}
	storage := NewImageStorage(objects)
	input, err := storage.ReadSource(t.Context(), "assets/source.png")
	if err != nil || input.MIMEType != "image/png" {
		t.Fatalf("ReadSource() = %#v, %v", input, err)
	}
	url, err := storage.GeneratedReadURL(t.Context(), "generated/job/item/turn/1-"+strings.Repeat("a", 64)+".png")
	if err != nil || url == "" || objects.readExpiry != generatedImageReadURLSeconds || objects.readKey == "" {
		t.Fatalf("GeneratedReadURL() = %q, %v; expiry=%d key=%q", url, err, objects.readExpiry, objects.readKey)
	}
}

func TestImageStorageRejectsNonCanonicalGeneratedReadKeys(t *testing.T) {
	t.Parallel()
	storage := NewImageStorage(&memoryImageObjectStore{generated: map[string]storedObject{}})
	if _, err := storage.GeneratedReadURL(t.Context(), "generated/job/item/turn/not-a-content-hash.png"); !errors.Is(err, ErrGeneratedImageReference) {
		t.Fatalf("GeneratedReadURL() error = %v, want ErrGeneratedImageReference", err)
	}
}

func pngFixture(t *testing.T, width, height int) []byte {
	t.Helper()
	imageValue := image.NewRGBA(image.Rect(0, 0, width, height))
	imageValue.Set(0, 0, color.RGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff})
	var output bytes.Buffer
	if err := png.Encode(&output, imageValue); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func jpegFixture(t *testing.T, width, height int) []byte {
	t.Helper()
	imageValue := image.NewRGBA(image.Rect(0, 0, width, height))
	var output bytes.Buffer
	if err := jpeg.Encode(&output, imageValue, nil); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func pngHeader(width, height uint32) []byte {
	value := make([]byte, 33)
	copy(value, "\x89PNG\r\n\x1a\n")
	copy(value[12:], "IHDR")
	value[16], value[17], value[18], value[19] = byte(width>>24), byte(width>>16), byte(width>>8), byte(width)
	value[20], value[21], value[22], value[23] = byte(height>>24), byte(height>>16), byte(height>>8), byte(height)
	value[24] = 8
	value[25] = 2
	return value
}

func webPFixture(width, height uint32) []byte {
	value := make([]byte, 30)
	copy(value[0:], "RIFF")
	copy(value[8:], "WEBPVP8X")
	value[16] = 10
	width--
	height--
	value[24], value[25], value[26] = byte(width), byte(width>>8), byte(width>>16)
	value[27], value[28], value[29] = byte(height), byte(height>>8), byte(height>>16)
	return value
}

func fmtHex(value []byte) string {
	const alphabet = "0123456789abcdef"
	output := make([]byte, len(value)*2)
	for index, current := range value {
		output[index*2], output[index*2+1] = alphabet[current>>4], alphabet[current&0x0f]
	}
	return string(output)
}
