package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cargoflow/api/internal/ai"
	"cargoflow/api/internal/config"
	"github.com/minio/minio-go/v7"
)

func TestGeneratedBucketPrivateMinIOIntegration(t *testing.T) {
	endpoint := os.Getenv("MINIO_INTEGRATION_ENDPOINT")
	if endpoint == "" {
		t.Skip("set MINIO_INTEGRATION_ENDPOINT to run against disposable MinIO")
	}
	bucketSuffix := fmt.Sprintf("%d", time.Now().UnixNano())
	cfg := config.Config{
		MinIOEndpoint: endpoint, MinIOPublicEndpoint: endpoint,
		MinIOAccessKey: os.Getenv("MINIO_INTEGRATION_ACCESS_KEY"), MinIOSecretKey: os.Getenv("MINIO_INTEGRATION_SECRET_KEY"),
		MinIOBucket: "cf-source-" + bucketSuffix, MinIOAIBucket: "cf-private-" + bucketSuffix,
	}
	if cfg.MinIOAccessKey == "" {
		cfg.MinIOAccessKey = "cargoflow"
	}
	if cfg.MinIOSecretKey == "" {
		cfg.MinIOSecretKey = "cargoflow123"
	}
	store, err := newObjectStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ensureBucket(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = store.internal.RemoveBucket(t.Context(), cfg.MinIOAIBucket)
		_ = store.internal.RemoveBucket(t.Context(), cfg.MinIOBucket)
	}()

	sourceKey := "assets/source.png"
	source := minioPutPNG(t)
	uploadURL, _, err := store.createUploadURL(t.Context(), sourceKey)
	if err != nil {
		t.Fatal(err)
	}
	uploadRequest, err := http.NewRequestWithContext(t.Context(), http.MethodPut, uploadURL, bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	uploadRequest.Header.Set("Content-Type", "image/png")
	uploadResponse, err := http.DefaultClient.Do(uploadRequest)
	if err != nil {
		t.Fatal(err)
	}
	uploadResponse.Body.Close()
	if uploadResponse.StatusCode < 200 || uploadResponse.StatusCode >= 300 {
		t.Fatalf("presigned source PUT status = %d, want 2xx", uploadResponse.StatusCode)
	}
	defer func() {
		_ = store.internal.RemoveObject(t.Context(), cfg.MinIOBucket, sourceKey, minio.RemoveObjectOptions{})
	}()
	storage := ai.NewImageStorage(store)
	validated, err := storage.ReadSource(t.Context(), sourceKey)
	if err != nil {
		t.Fatalf("authenticated worker source read failed: %v", err)
	}
	finalKey := "assets/final/immutable.png"
	defer func() { _ = store.deleteSource(t.Context(), finalKey) }()
	if err := store.promoteSource(t.Context(), sourceKey, finalKey, "image/png", validated.Bytes); err != nil {
		t.Fatalf("promote validated source failed: %v", err)
	}
	overwrite := append([]byte(nil), source...)
	overwrite[len(overwrite)-1] ^= 0x01
	overwriteRequest, err := http.NewRequestWithContext(t.Context(), http.MethodPut, uploadURL, bytes.NewReader(overwrite))
	if err != nil {
		t.Fatal(err)
	}
	overwriteRequest.Header.Set("Content-Type", "image/png")
	overwriteResponse, err := http.DefaultClient.Do(overwriteRequest)
	if err != nil {
		t.Fatal(err)
	}
	overwriteResponse.Body.Close()
	if overwriteResponse.StatusCode < 200 || overwriteResponse.StatusCode >= 300 {
		t.Fatalf("presigned temporary overwrite status = %d, want 2xx", overwriteResponse.StatusCode)
	}
	final, err := storage.ReadSource(t.Context(), finalKey)
	if err != nil || !bytes.Equal(final.Bytes, source) {
		t.Fatalf("final object changed after temporary URL overwrite: bytes=%d err=%v", len(final.Bytes), err)
	}
	sourcePolicy, err := store.internal.GetBucketPolicy(t.Context(), cfg.MinIOBucket)
	if err != nil || sourcePolicy != "" {
		t.Fatalf("private source bucket policy = %q, %v", sourcePolicy, err)
	}
	sourceResponse, err := http.Get("http://" + endpoint + "/" + cfg.MinIOBucket + "/" + sourceKey) // #nosec G107 -- disposable loopback MinIO endpoint provided by the test harness.
	if err != nil {
		t.Fatal(err)
	}
	defer sourceResponse.Body.Close()
	if sourceResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("anonymous source object GET status = %d, want %d", sourceResponse.StatusCode, http.StatusForbidden)
	}
	stored, err := storage.StoreGenerated(t.Context(), ai.GeneratedImageStoreRequest{JobPublicID: "job", ItemPublicID: "item", TurnPublicID: "turn", CandidateIndex: 1, Bytes: source})
	if err != nil {
		t.Fatalf("authenticated worker generated write failed: %v", err)
	}
	defer func() { _ = store.DeleteGenerated(t.Context(), stored.ObjectKey) }()
	policy, err := store.internal.GetBucketPolicy(t.Context(), cfg.MinIOAIBucket)
	if err != nil || policy != "" {
		t.Fatalf("private generated bucket policy = %q, %v", policy, err)
	}
	response, err := http.Get("http://" + endpoint + "/" + cfg.MinIOAIBucket + "/" + stored.ObjectKey) // #nosec G107 -- disposable loopback MinIO endpoint provided by the test harness.
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("anonymous generated object GET status = %d, want %d", response.StatusCode, http.StatusForbidden)
	}
	pendingEntered := make(chan struct{})
	pendingRelease := make(chan struct{})
	pendingDone := make(chan error, 1)
	pendingRequest := ai.GeneratedImageStoreRequest{JobPublicID: "job", ItemPublicID: "item", TurnPublicID: "pending", CandidateIndex: 1, Bytes: source, Persist: func(context.Context, ai.StoredGeneratedImage) error {
		close(pendingEntered)
		<-pendingRelease
		return errors.New("database unavailable")
	}}
	go func() {
		_, err := storage.StoreGenerated(context.Background(), pendingRequest)
		pendingDone <- err
	}()
	<-pendingEntered
	var pendingPersists atomic.Int32
	secondPending := pendingRequest
	secondPending.Persist = func(context.Context, ai.StoredGeneratedImage) error {
		pendingPersists.Add(1)
		return nil
	}
	if _, err := storage.StoreGenerated(t.Context(), secondPending); !errors.Is(err, ai.ErrImageStoragePending) {
		t.Fatalf("concurrent pending StoreGenerated() error = %v, want ErrImageStoragePending", err)
	}
	if pendingPersists.Load() != 0 {
		t.Fatalf("concurrent pending Persist calls = %d, want 0", pendingPersists.Load())
	}
	close(pendingRelease)
	if err := <-pendingDone; err == nil {
		t.Fatal("creator pending StoreGenerated() unexpectedly succeeded")
	}
	pendingKey := "generated/job/item/pending/1-" + fmt.Sprintf("%x", sha256.Sum256(source)) + ".png"
	if _, err := store.internal.StatObject(t.Context(), cfg.MinIOAIBucket, pendingKey, minio.StatObjectOptions{}); !isMissingObject(err) {
		t.Fatalf("failed creator left a generated object: %v", err)
	}
	concurrentKey := "generated/job/item/turn/2-concurrent.png"
	defer func() { _ = store.DeleteGenerated(t.Context(), concurrentKey) }()
	var created atomic.Int32
	var wait sync.WaitGroup
	start := make(chan struct{})
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			claim, err := store.ClaimGenerated(t.Context(), concurrentKey, "image/png", source)
			if err != nil {
				t.Errorf("concurrent generated write failed: %v", err)
				return
			}
			if claim.State == ai.GeneratedObjectCreated {
				created.Add(1)
				if err := store.CommitGenerated(t.Context(), claim); err != nil {
					t.Errorf("concurrent generated commit failed: %v", err)
				}
			}
		}()
	}
	close(start)
	wait.Wait()
	if created.Load() != 1 {
		t.Fatalf("concurrent generated creates = %d, want exactly one exclusive create", created.Load())
	}
	firstUseStore := *store
	firstUseStore.aiBucket = "cf-first-use-" + bucketSuffix
	defer func() { _ = firstUseStore.internal.RemoveBucket(t.Context(), firstUseStore.aiBucket) }()
	var firstUse sync.WaitGroup
	firstUseStart := make(chan struct{})
	for index := range 16 {
		firstUse.Add(1)
		go func(index int) {
			defer firstUse.Done()
			<-firstUseStart
			key := fmt.Sprintf("generated/job/item/turn/first-%d.png", index)
			claim, err := firstUseStore.ClaimGenerated(t.Context(), key, "image/png", source)
			if err != nil {
				t.Errorf("first-use generated claim failed: %v", err)
				return
			}
			if claim.State != ai.GeneratedObjectCreated {
				t.Errorf("first-use claim state = %q, want created", claim.State)
				return
			}
			if err := firstUseStore.CleanupGenerated(t.Context(), claim); err != nil {
				t.Errorf("first-use generated cleanup failed: %v", err)
			}
		}(index)
	}
	close(firstUseStart)
	firstUse.Wait()
}

func minioPutPNG(t *testing.T) []byte {
	t.Helper()
	imageValue := image.NewRGBA(image.Rect(0, 0, 2, 2))
	imageValue.Set(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	var output bytes.Buffer
	if err := png.Encode(&output, imageValue); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
