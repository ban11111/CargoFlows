package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"cargoflows/api/internal/ai"
)

func TestImagePromptEvaluationIsDisabledByDefault(t *testing.T) {
	if err := run(context.Background(), func(string) string { return "" }, nil); !errors.Is(err, errImageEvalDisabled) {
		t.Fatalf("error = %v", err)
	}
}

func TestEvaluationScenarioBuildsStableCompositeAndSingleSlots(t *testing.T) {
	slots := map[string]ai.SlotFacts{}
	for _, key := range []string{"lazada_main_gallery", "lazada_case_on_device_studio", "lazada_case_on_device_handheld", "lazada_case_on_device_lifestyle", "lazada_benefits_overview", "lazada_structure_details"} {
		slots[key] = ai.SlotFacts{SlotKey: key, PromptFragment: key + " prompt"}
	}
	count := 1
	frozen := ai.SlotFacts{CanvasKey: "frozen-canvas", Name: ai.LocalizedNameFacts{ZH: "冻结画布"}, CanvasGeneration: &ai.GenerationOverride{CandidateCount: &count}}
	composite, err := evaluationScenarioSlot(slots, frozen, liveEvalComposite)
	if err != nil {
		t.Fatal(err)
	}
	if composite.CanvasKey != "frozen-canvas" || composite.CanvasGeneration == nil || len(composite.CompositeRequirements) != 4 || composite.CompositeRequirements[3].SlotKey != "lazada_case_on_device_lifestyle" {
		t.Fatalf("composite = %#v", composite)
	}
	benefits, err := evaluationScenarioSlot(slots, frozen, liveEvalBenefits)
	if err != nil || benefits.SlotKey != "lazada_benefits_overview" || len(benefits.CompositeRequirements) != 0 {
		t.Fatalf("benefits=%#v err=%v", benefits, err)
	}
	delete(slots, "lazada_structure_details")
	if _, err := evaluationScenarioSlot(slots, frozen, liveEvalStructure); err == nil {
		t.Fatal("missing evaluation slot was accepted")
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
	providerPrompt := prompt.ImagesAPIPrompt()
	for _, required := range []string{"图片 1: 目标 SKU 商品图", "原始参考图未作为二进制输入", "仅参考装机比例、空间关系和构图", "禁止继承外形、颜色、开孔"} {
		if !strings.Contains(providerPrompt, required) {
			t.Fatalf("provider prompt missing %q: %s", required, providerPrompt)
		}
	}
}
