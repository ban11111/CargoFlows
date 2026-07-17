package main

import (
	"bytes"
	"encoding/base64"
	"testing"

	"cargoflow/api/internal/config"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

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
	cfg := config.Config{AIWorkerDryRun: false, SecretsMasterKey: encoded, OpenAIBaseURL: "https://api.openai.invalid/v1", OpenAITextModel: "fake-model", OpenAIReasoningEffort: "low"}
	if executor, err := buildExecutor(cfg, db); err != nil || executor == nil {
		t.Fatalf("real executor=%#v error=%v", executor, err)
	}
}
