package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"cargoflow/api/internal/ai"
	"cargoflow/api/internal/models"
)

var errSmokeDisabled = errors.New("OpenAI smoke test is disabled; set OPENAI_SMOKE_TEST=1 explicitly")
var safeProviderIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := run(ctx, os.Getenv, os.Stdout, nil, false); err != nil {
		fmt.Fprintln(os.Stderr, "OpenAI smoke test failed:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, getenv func(string) string, output io.Writer, httpClient *http.Client, allowTestBaseURL bool) error {
	if getenv("OPENAI_SMOKE_TEST") != "1" {
		return errSmokeDisabled
	}
	key := []byte(strings.TrimSpace(getenv("OPENAI_API_KEY")))
	if len(key) == 0 {
		return errors.New("OPENAI_API_KEY is required for the opt-in smoke test")
	}
	defer clearBytes(key)

	baseURL := strings.TrimSpace(getenv("OPENAI_BASE_URL"))
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if !allowTestBaseURL && !officialOpenAIBaseURL(baseURL) {
		return errors.New("OPENAI_BASE_URL must be the official https://api.openai.com/v1 endpoint for a real smoke test")
	}
	model := strings.TrimSpace(getenv("OPENAI_TEXT_MODEL"))
	if model == "" {
		model = "gpt-5.6-terra"
	}
	prompt, err := smokePrompt()
	if err != nil {
		return err
	}
	client := ai.NewOpenAIResponsesClient(baseURL, httpClient, ai.OpenAIResponsesConfig{
		Model: model, ReasoningEffort: "low", MaxAttempts: 1, RequestTimeout: 90 * time.Second,
	})
	response, err := client.Generate(ctx, key, ai.TextRequest{Prompt: prompt, Metadata: map[string]string{
		"job_id": "10000000-0000-4000-8000-000000000001", "execution_id": "10000000-0000-4000-8000-000000000002",
	}})
	if err != nil {
		return err
	}
	if !safeProviderIdentifier.MatchString(response.ResponseID) || !safeProviderIdentifier.MatchString(response.RequestID) || !safeProviderIdentifier.MatchString(response.Model) {
		return errors.New("provider returned unsafe identifier metadata")
	}
	// Deliberately omit generated content, prompts, credentials, and request bodies.
	_, err = fmt.Fprintf(output, "response_id=%s request_id=%s model=%s input_tokens=%d output_tokens=%d total_tokens=%d reasoning_tokens=%d\n",
		response.ResponseID, response.RequestID, response.Model, response.Usage.InputTextTokens, response.Usage.OutputTextTokens, response.Usage.TotalTokens, response.Usage.ReasoningTokens)
	return err
}

func officialOpenAIBaseURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	return err == nil && parsed.Scheme == "https" && parsed.Host == "api.openai.com" && parsed.Path == "/v1" && parsed.RawQuery == "" && parsed.Fragment == "" && parsed.User == nil
}

func smokePrompt() (ai.CompiledTextPrompt, error) {
	slot := ai.SlotFacts{
		PublicID: "10000000-0000-4000-8000-000000000005", SlotKey: "title", Kind: models.AIContentSlotTitle,
		Name:           ai.LocalizedNameFacts{ZH: "商品标题", EN: "Product title"},
		PromptFragment: "Create one accurate, natural product title using only the supplied product facts.",
		Constraints:    json.RawMessage(`{"min_length":10,"max_length":120}`), GenerationConfig: json.RawMessage(`{"candidate_count":1}`),
	}
	snapshot := ai.ProductSnapshotV1{
		Schema: ai.ProductSnapshotSchemaV1, Locale: "en-SG", TargetPlatform: "lazada",
		Product:             ai.ProductFacts{Name: "Protective phone case", Brand: "CargoFlow Test", Category: ai.CategoryFacts{NameZH: "手机壳", NameEN: "Phone cases"}},
		SKU:                 ai.SKUFacts{Code: "SMOKE-CASE-001", Color: "clear"},
		SOP:                 ai.SOPFacts{PublicID: "10000000-0000-4000-8000-000000000006", VersionPublicID: "10000000-0000-4000-8000-000000000007", VersionNumber: 1, SchemaVersion: "v1", CoordinateSystem: "pcs_object_v1"},
		Template:            ai.TemplateFacts{TemplatePublicID: "10000000-0000-4000-8000-000000000003", VersionPublicID: "10000000-0000-4000-8000-000000000004", VersionNumber: 1, PlatformPrompt: "Create accurate Lazada product content.", SelectedSlots: []ai.SlotFacts{slot}},
		GenerationOverrides: map[string]ai.GenerationOverride{},
	}
	return ai.CompileTextPrompt(snapshot, slot)
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
