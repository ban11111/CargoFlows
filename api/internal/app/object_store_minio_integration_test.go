package app

import (
	"bytes"
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
	if _, err := store.internal.PutObject(t.Context(), cfg.MinIOBucket, sourceKey, bytes.NewReader(source), int64(len(source)), minio.PutObjectOptions{ContentType: "image/png"}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = store.internal.RemoveObject(t.Context(), cfg.MinIOBucket, sourceKey, minio.RemoveObjectOptions{})
	}()
	storage := ai.NewImageStorage(store)
	if _, err := storage.ReadSource(t.Context(), sourceKey); err != nil {
		t.Fatalf("authenticated worker source read failed: %v", err)
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
			wasCreated, err := store.PutGenerated(t.Context(), concurrentKey, "image/png", source)
			if err != nil {
				t.Errorf("concurrent generated write failed: %v", err)
				return
			}
			if wasCreated {
				created.Add(1)
			}
		}()
	}
	close(start)
	wait.Wait()
	if created.Load() != 1 {
		t.Fatalf("concurrent generated creates = %d, want exactly one exclusive create", created.Load())
	}
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
