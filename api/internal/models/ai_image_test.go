package models

import (
	"bytes"
	"testing"
)

func TestAIImageModelsApplySafeDefaults(t *testing.T) {
	turn := AIImageTurn{}
	if err := turn.BeforeCreate(nil); err != nil {
		t.Fatal(err)
	}
	if turn.Status != AIImageTurnQueued || turn.RequestedCandidateCount != 1 {
		t.Fatalf("unexpected turn defaults: %#v", turn)
	}
	if !bytes.Equal(turn.CompiledRequestMetadataJSON, []byte(`{}`)) {
		t.Fatalf("compiled metadata default = %q, want {}", turn.CompiledRequestMetadataJSON)
	}
}

func TestAIExecutionQueuedIsAStableStatus(t *testing.T) {
	if AIExecutionQueued != AIExecutionStatus("queued") {
		t.Fatalf("AIExecutionQueued = %q", AIExecutionQueued)
	}
}
