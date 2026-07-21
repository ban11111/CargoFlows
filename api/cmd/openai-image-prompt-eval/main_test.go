package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestImagePromptEvaluationIsDisabledByDefault(t *testing.T) {
	if err := run(context.Background(), func(string) string { return "" }, nil); !errors.Is(err, errImageEvalDisabled) {
		t.Fatalf("error = %v", err)
	}
}

func TestEvaluationPromptUsesImageV3AndStrictReferenceRoles(t *testing.T) {
	prompt, err := evaluationPrompt()
	if err != nil {
		t.Fatal(err)
	}
	if prompt.CompilerVersion != "image-v3" {
		t.Fatalf("compiler = %q", prompt.CompilerVersion)
	}
	for _, required := range []string{"Image 1: TARGET SKU", "Image 2: REFERENCE SOP — USAGE EFFECT ONLY", "NON-TARGET PLACEHOLDER", "禁止继承外形、颜色、开孔"} {
		if !strings.Contains(prompt.TaskBrief, required) {
			t.Fatalf("task brief missing %q: %s", required, prompt.TaskBrief)
		}
	}
}
