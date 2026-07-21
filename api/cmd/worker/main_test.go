package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"sync"
	"testing"
	"time"

	"cargoflows/api/internal/ai"
	"cargoflows/api/internal/config"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type mutableWorkerSettings struct {
	mu    sync.Mutex
	value ai.WorkerConcurrency
}

func (s *mutableWorkerSettings) Get(context.Context) (ai.WorkerConcurrency, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value, nil
}

func (s *mutableWorkerSettings) Set(value ai.WorkerConcurrency) {
	s.mu.Lock()
	s.value = value
	s.mu.Unlock()
}

type blockingPoolWorker struct {
	started chan<- int
	slot    int
	release <-chan struct{}
}

func (w blockingPoolWorker) RunOnce(ctx context.Context) (bool, error) {
	w.started <- w.slot
	select {
	case <-ctx.Done():
		return true, ctx.Err()
	case <-w.release:
		return true, nil
	}
}

func TestBuildExecutorRequiresMasterKeyOnlyForRealMode(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if executor, err := buildExecutor(config.Config{AIWorkerDryRun: true}, db); err != nil || executor == nil {
		t.Fatalf("dry-run executor=%#v error=%v", executor, err)
	}
	if _, err := buildExecutor(config.Config{AIWorkerDryRun: false}, db); err == nil {
		t.Fatal("real mode accepted missing master key")
	}
	if _, err := buildExecutor(config.Config{AIWorkerDryRun: false, SecretsMasterKey: "invalid"}, db); err == nil {
		t.Fatal("real mode accepted malformed master key")
	}
	encoded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
	cfg := config.Config{AIWorkerDryRun: false, SecretsMasterKey: encoded, OpenAIBaseURL: "https://api.openai.invalid/v1", OpenAITextModel: "fake-model", OpenAIReasoningEffort: "low", MinIOEndpoint: "127.0.0.1:9000", MinIOPublicEndpoint: "127.0.0.1:9000", MinIOAccessKey: "test", MinIOSecretKey: "test", MinIOBucket: "source", MinIOAIBucket: "generated"}
	if executor, err := buildExecutor(cfg, db); err != nil || executor == nil {
		t.Fatalf("real executor=%#v error=%v", executor, err)
	}
}

func TestRunPoolScalesUpAndRetiresOnlyAfterCurrentWork(t *testing.T) {
	settings := &mutableWorkerSettings{value: ai.WorkerConcurrency{MaxWorkersPerJob: 2, MaxWorkersGlobal: 2}}
	started := make(chan int, 16)
	releases := map[int]chan struct{}{}
	var releaseMu sync.Mutex
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- runPool(ctx, settings, func(slot int) runOnceWorker {
			releaseMu.Lock()
			release := make(chan struct{})
			releases[slot] = release
			releaseMu.Unlock()
			return blockingPoolWorker{started: started, slot: slot, release: release}
		}, 5*time.Millisecond)
	}()
	wantStartedSlots(t, started, map[int]bool{1: true, 2: true})
	settings.Set(ai.WorkerConcurrency{MaxWorkersPerJob: 4, MaxWorkersGlobal: 4})
	wantStartedSlots(t, started, map[int]bool{3: true, 4: true})
	settings.Set(ai.WorkerConcurrency{MaxWorkersPerJob: 1, MaxWorkersGlobal: 1})
	time.Sleep(15 * time.Millisecond)
	for _, slot := range []int{2, 3, 4} {
		releaseMu.Lock()
		close(releases[slot])
		releaseMu.Unlock()
	}
	select {
	case slot := <-started:
		if slot != 1 {
			t.Fatalf("retired slot %d accepted another item", slot)
		}
	case <-time.After(25 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("runPool error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runPool did not stop")
	}
}

func wantStartedSlots(t *testing.T, started <-chan int, want map[int]bool) {
	t.Helper()
	deadline := time.After(time.Second)
	for len(want) > 0 {
		select {
		case slot := <-started:
			if want[slot] {
				delete(want, slot)
			}
		case <-deadline:
			t.Fatalf("worker slots did not start: %#v", want)
		}
	}
}
