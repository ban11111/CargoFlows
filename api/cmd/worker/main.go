package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cargoflow/api/internal/ai"
	"cargoflow/api/internal/config"
	"cargoflow/api/internal/database"
	"github.com/google/uuid"
)

const leaseTTL = time.Minute

func main() {
	cfg := config.Load()
	if !cfg.AIWorkerDryRun {
		log.Fatal("AI worker startup refused: Phase 1 requires AI_WORKER_DRY_RUN=true")
	}
	if cfg.AIWorkerPollInterval <= 0 {
		log.Fatal("AI worker startup refused: AI_WORKER_POLL_INTERVAL must be positive")
	}

	db, err := database.Open(cfg.DatabaseDSN)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		log.Fatalf("migrate database: %v", err)
	}

	workerID := newWorkerID()
	worker := ai.NewWorker(ai.NewQueue(db), workerID, leaseTTL, ai.SystemClock{}, ai.NewDryRunExecutor(db))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("AI dry-run worker %s started", workerID)
	if err := run(ctx, worker, cfg.AIWorkerPollInterval); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("run AI worker: %v", err)
	}
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
