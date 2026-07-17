package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAITextModelsHideInternalRelationshipIDs(t *testing.T) {
	approver := uint(910101)
	values := []any{
		AITextResult{AIExecutionID: 910102, ApprovedByID: &approver, AppliedByID: &approver},
		SKUPlatformContent{SKUID: 910103, SourceAITextResultID: &approver, UpdatedByID: 910104},
		SKUPlatformContentRevision{SKUPlatformContentID: 910105, SourceAITextResultID: &approver, ActorID: 910106},
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"ai_execution_id", "approved_by_id", "applied_by_id", "sku_id", "source_ai_text_result_id", "updated_by_id", "sku_platform_content_id", "actor_id"} {
		if strings.Contains(string(encoded), `"`+key+`"`) {
			t.Fatalf("serialized internal relationship key %q: %s", key, encoded)
		}
	}
	for _, key := range []string{"raw_structured", "validation", "edited_structured", "selling_points", "search_keywords", "before", "after"} {
		if strings.Contains(string(encoded), `"`+key+`"`) {
			t.Fatalf("persistence JSON field %q must only be exposed through a typed DTO: %s", key, encoded)
		}
	}
}

func TestAITextResultStatesAreExplicit(t *testing.T) {
	states := []AITextResultState{AITextResultCandidate, AITextResultApproved, AITextResultRejected}
	want := []string{"candidate", "approved", "rejected"}
	for index := range states {
		if string(states[index]) != want[index] {
			t.Fatalf("state %d = %q, want %q", index, states[index], want[index])
		}
	}
}
