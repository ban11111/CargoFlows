package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"cargoflows/api/internal/ai"
	"cargoflows/api/internal/app"
	"cargoflows/api/internal/config"
	"cargoflows/api/internal/database"
	"cargoflows/api/internal/secrets"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const leaseTTL = time.Minute

func main() {
	cfg := config.Load()
	if cfg.AIWorkerPollInterval <= 0 {
		log.Fatal("AI worker startup refused: AI_WORKER_POLL_INTERVAL must be positive")
	}

	db, err := database.Open(cfg.DatabaseDSN)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	if _, err := buildImageStorage(cfg); err != nil {
		log.Fatalf("configure AI image storage: %v", err)
	}
	workerID := newWorkerID()
	executor, err := buildExecutor(cfg, db)
	if err != nil {
		log.Fatalf("configure AI worker: %v", err)
	}
	worker := ai.NewWorker(ai.NewQueue(db), workerID, leaseTTL, ai.SystemClock{}, executor)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	mode := "real text"
	if cfg.AIWorkerDryRun {
		mode = "dry-run"
	}
	log.Printf("AI %s worker %s started", mode, workerID)
	if err := run(ctx, worker, cfg.AIWorkerPollInterval); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("run AI worker: %v", err)
	}
}

func buildImageStorage(cfg config.Config) (*ai.ImageStorage, error) {
	objects, err := app.NewImageObjectStore(cfg)
	if err != nil {
		return nil, err
	}
	return ai.NewImageStorage(objects), nil
}

func buildExecutor(cfg config.Config, db *gorm.DB) (ai.ItemExecutor, error) {
	dryRun := ai.NewDryRunExecutor(db)
	if cfg.AIWorkerDryRun {
		return ai.NewKindRoutingExecutor(true, dryRun, nil), nil
	}
	encoded := strings.TrimSpace(cfg.SecretsMasterKey)
	if encoded == "" {
		return nil, fmt.Errorf("CARGOFLOWS_SECRETS_MASTER_KEY is required when AI_WORKER_DRY_RUN=false")
	}
	masterKey, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(masterKey) != 32 {
		return nil, fmt.Errorf("CARGOFLOWS_SECRETS_MASTER_KEY must be base64 for exactly 32 bytes")
	}
	box, err := secrets.NewAESGCM(masterKey)
	for index := range masterKey {
		masterKey[index] = 0
	}
	if err != nil {
		return nil, fmt.Errorf("initialize credential encryption: %w", err)
	}
	settings := ai.NewProviderSettingsService(db, box, nil)
	provider := ai.NewOpenAIResponsesClient(cfg.OpenAIBaseURL, nil, ai.OpenAIResponsesConfig{
		Model: cfg.OpenAITextModel, ReasoningEffort: cfg.OpenAIReasoningEffort, RequestTimeout: cfg.OpenAIRequestTimeout,
	})
	storage, err := buildImageStorage(cfg)
	if err != nil {
		return nil, fmt.Errorf("configure AI image storage: %w", err)
	}
	text := ai.NewTextExecutor(db, settings, provider, ai.TextExecutorConfig{Model: cfg.OpenAITextModel, ReasoningEffort: cfg.OpenAIReasoningEffort, Storage: storage})
	imageProvider := ai.NewOpenAIImageResponsesClient(cfg.OpenAIBaseURL, nil, ai.OpenAIImageResponsesConfig{Model: cfg.OpenAIImageToolModel, RequestTimeout: cfg.OpenAIImageRequestTimeout})
	image := ai.NewImageExecutor(db, settings, imageProvider, storage, cfg.OpenAIImageToolModel)
	return ai.NewKindRoutingExecutor(false, dryRun, text, image), nil
}

type runOnceWorker interface {
	RunOnce(context.Context) (bool, error)
}

func run(ctx context.Context, worker runOnceWorker, pollInterval time.Duration) error {
	for {
		worked, err := worker.RunOnce(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			log.Printf("AI worker cycle failed: %v", err)
		}
		if worked {
			continue
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func newWorkerID() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "worker"
	}
	return fmt.Sprintf("%s-%s", hostname, uuid.NewString())
}
