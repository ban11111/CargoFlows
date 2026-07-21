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
	"sync"
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
	executor, err := buildExecutor(cfg, db)
	if err != nil {
		log.Fatalf("configure AI worker: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	mode := "real text"
	if cfg.AIWorkerDryRun {
		mode = "dry-run"
	}
	processID := newWorkerID()
	queue := ai.NewQueue(db)
	settings := ai.NewWorkerSettingsService(db)
	log.Printf("AI %s worker pool %s started", mode, processID)
	if err := runPool(ctx, settings, func(slot int) runOnceWorker {
		return ai.NewWorker(queue, fmt.Sprintf("%s-slot-%d", processID, slot), leaseTTL, ai.SystemClock{}, executor)
	}, cfg.AIWorkerPollInterval); err != nil && !errors.Is(err, context.Canceled) {
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
	imageProvider := &ai.OpenAIImageProvider{
		Responses: ai.NewOpenAIImageResponsesClient(cfg.OpenAIBaseURL, nil, ai.OpenAIImageResponsesConfig{Model: cfg.OpenAIImageToolModel, RequestTimeout: cfg.OpenAIImageRequestTimeout}),
		Images:    ai.NewOpenAIImagesClient(cfg.OpenAIBaseURL, nil, ai.OpenAIImagesConfig{Model: ai.DefaultOpenAIImageGenerationModel, RequestTimeout: cfg.OpenAIImageRequestTimeout}),
	}
	image := ai.NewImageExecutor(db, settings, imageProvider, storage, cfg.OpenAIImageToolModel)
	return ai.NewKindRoutingExecutor(false, dryRun, text, image), nil
}

type runOnceWorker interface {
	RunOnce(context.Context) (bool, error)
}

type workerSettingsReader interface {
	Get(context.Context) (ai.WorkerConcurrency, error)
}

type poolSlot struct {
	retire   chan struct{}
	retiring bool
}

func runPool(ctx context.Context, settings workerSettingsReader, factory func(int) runOnceWorker, pollInterval time.Duration) error {
	if pollInterval <= 0 {
		return errors.New("worker poll interval must be positive")
	}
	current, err := settings.Get(ctx)
	if err != nil {
		return fmt.Errorf("load AI worker concurrency: %w", err)
	}
	target := current.MaxWorkersGlobal
	slots := map[int]*poolSlot{}
	done := make(chan int, ai.MaxWorkerLimit)
	var wg sync.WaitGroup
	startSlot := func(index int) {
		slot := &poolSlot{retire: make(chan struct{})}
		slots[index] = slot
		wg.Add(1)
		go func() {
			defer wg.Done()
			runWorkerSlot(ctx, factory(index), pollInterval, slot.retire)
			done <- index
		}()
	}
	reconcile := func() {
		for index := 1; index <= target; index++ {
			if _, exists := slots[index]; !exists {
				startSlot(index)
			}
		}
		for index, slot := range slots {
			if index > target && !slot.retiring {
				slot.retiring = true
				close(slot.retire)
			}
		}
	}
	reconcile()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		case index := <-done:
			delete(slots, index)
			reconcile()
		case <-ticker.C:
			next, refreshErr := settings.Get(ctx)
			if refreshErr != nil {
				log.Printf("refresh AI worker concurrency failed: %v", refreshErr)
				continue
			}
			target = next.MaxWorkersGlobal
			reconcile()
		}
	}
}

func runWorkerSlot(ctx context.Context, worker runOnceWorker, pollInterval time.Duration, retire <-chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-retire:
			return
		default:
		}
		worked, err := worker.RunOnce(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
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
			return
		case <-retire:
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
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
