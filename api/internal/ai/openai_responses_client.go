package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"cargoflow/api/internal/models"
)

const maxResponsesBodyBytes = 4 << 20

type OpenAIResponsesConfig struct {
	Model           string
	ReasoningEffort string
	MaxAttempts     int
	RequestTimeout  time.Duration
}

type OpenAIResponsesClient struct {
	baseURL string
	client  *http.Client
	config  OpenAIResponsesConfig
	sleep   func(context.Context, time.Duration) error
}

func NewOpenAIResponsesClient(baseURL string, client *http.Client, config OpenAIResponsesConfig) *OpenAIResponsesClient {
	if config.Model == "" {
		config.Model = "gpt-5.6-terra"
	}
	if config.ReasoningEffort == "" {
		config.ReasoningEffort = "low"
	}
	if config.MaxAttempts < 1 {
		config.MaxAttempts = 3
	}
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = 120 * time.Second
	}
	if client == nil {
		client = &http.Client{Timeout: config.RequestTimeout}
	} else {
		configuredClient := *client
		configuredClient.Timeout = config.RequestTimeout
		client = &configuredClient
	}
	return &OpenAIResponsesClient{baseURL: strings.TrimRight(baseURL, "/"), client: client, config: config, sleep: sleepWithContext}
}

func (client *OpenAIResponsesClient) Generate(ctx context.Context, apiKey []byte, request TextRequest) (TextResponse, error) {
	body, err := client.requestBody(request)
	if err != nil {
		return TextResponse{}, err
	}
	for attempt := 1; attempt <= client.config.MaxAttempts; attempt++ {
		response, err := client.send(ctx, apiKey, body)
		if err != nil {
			if ctx.Err() != nil {
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					return TextResponse{}, &TextProviderError{Kind: ErrTextProviderAmbiguousTimeout}
				}
				return TextResponse{}, ctx.Err()
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return TextResponse{}, &TextProviderError{Kind: ErrTextProviderAmbiguousTimeout}
			}
			return TextResponse{}, &TextProviderError{Kind: ErrTextProviderAmbiguousTransport}
		}

		requestID := response.Header.Get("x-request-id")
		if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
			return decodeOpenAITextResponse(response, requestID, request.Prompt)
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponsesBodyBytes))
		_ = response.Body.Close()
		switch {
		case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
			return TextResponse{}, &TextProviderError{Kind: ErrTextProviderAuthentication, StatusCode: response.StatusCode, RequestID: requestID}
		case response.StatusCode == http.StatusTooManyRequests:
			if attempt < client.config.MaxAttempts {
				if err := client.sleep(ctx, retryDelay(attempt, response.Header)); err != nil {
					return TextResponse{}, err
				}
				continue
			}
			return TextResponse{}, &TextProviderError{Kind: ErrTextProviderRateLimit, StatusCode: response.StatusCode, RequestID: requestID}
		case response.StatusCode >= http.StatusInternalServerError:
			return TextResponse{}, &TextProviderError{Kind: ErrTextProviderAmbiguousTransport, StatusCode: response.StatusCode, RequestID: requestID}
		default:
			return TextResponse{}, &TextProviderError{Kind: ErrTextProviderInvalidRequest, StatusCode: response.StatusCode, RequestID: requestID}
		}
	}
	return TextResponse{}, &TextProviderError{Kind: ErrTextProviderRetryable}
}

func (client *OpenAIResponsesClient) requestBody(request TextRequest) ([]byte, error) {
	if len(request.Prompt.InputJSON) == 0 || len(request.Prompt.JSONSchema) == 0 || request.Prompt.SchemaName == "" || request.Prompt.CandidateCount < 1 || len(request.Metadata) > 16 {
		return nil, &TextProviderError{Kind: ErrTextProviderInvalidRequest}
	}
	for key, value := range request.Metadata {
		if strings.TrimSpace(key) == "" || len(key) > 64 || len(value) > 512 || containsForbiddenTextPromptString(key) || containsForbiddenTextPromptString(value) {
			return nil, &TextProviderError{Kind: ErrTextProviderInvalidRequest}
		}
	}
	payload := struct {
		Model        string            `json:"model"`
		Instructions string            `json:"instructions"`
		Input        string            `json:"input"`
		Store        bool              `json:"store"`
		Reasoning    map[string]string `json:"reasoning"`
		Text         any               `json:"text"`
		Metadata     map[string]string `json:"metadata,omitempty"`
	}{
		Model: client.config.Model, Instructions: request.Prompt.Instructions, Input: string(request.Prompt.InputJSON), Store: false,
		Reasoning: map[string]string{"effort": client.config.ReasoningEffort},
		Text:      map[string]any{"format": map[string]any{"type": "json_schema", "name": request.Prompt.SchemaName, "schema": request.Prompt.JSONSchema, "strict": true}},
		Metadata:  request.Metadata,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, &TextProviderError{Kind: ErrTextProviderInvalidRequest}
	}
	return body, nil
}

func (client *OpenAIResponsesClient) send(ctx context.Context, apiKey, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+string(apiKey))
	req.Header.Set("Content-Type", "application/json")
	return client.client.Do(req)
}

func decodeOpenAITextResponse(response *http.Response, requestID string, prompt CompiledTextPrompt) (TextResponse, error) {
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maxResponsesBodyBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil || len(body) > maxResponsesBodyBytes {
		return TextResponse{}, &TextProviderError{Kind: ErrTextProviderInvalidResponse, RequestID: requestID}
	}
	var decoded struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Model  string `json:"model"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type    string `json:"type"`
				Text    string `json:"text"`
				Refusal string `json:"refusal"`
			} `json:"content"`
		} `json:"output"`
		Usage *struct {
			InputTokens   int64 `json:"input_tokens"`
			OutputTokens  int64 `json:"output_tokens"`
			TotalTokens   int64 `json:"total_tokens"`
			OutputDetails struct {
				ReasoningTokens int64 `json:"reasoning_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil || decoded.Status != "completed" || decoded.ID == "" || strings.TrimSpace(decoded.Model) == "" || !validTextUsage(decoded.Usage) {
		return TextResponse{}, &TextProviderError{Kind: ErrTextProviderInvalidResponse, RequestID: requestID}
	}
	var output strings.Builder
	for _, item := range decoded.Output {
		if item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			switch content.Type {
			case "refusal":
				return TextResponse{}, &TextProviderError{Kind: ErrTextProviderRefusal, RequestID: requestID}
			case "output_text":
				output.WriteString(content.Text)
			}
		}
	}
	outputJSON := []byte(output.String())
	if err := validateTextCandidates(outputJSON, prompt); err != nil {
		return TextResponse{}, &TextProviderError{Kind: ErrTextProviderInvalidResponse, RequestID: requestID}
	}
	return TextResponse{
		ResponseID: decoded.ID, RequestID: requestID, Model: decoded.Model, OutputJSON: append(json.RawMessage(nil), outputJSON...),
		Usage: TextUsage{InputTextTokens: decoded.Usage.InputTokens, OutputTextTokens: decoded.Usage.OutputTokens, TotalTokens: decoded.Usage.TotalTokens, ReasoningTokens: decoded.Usage.OutputDetails.ReasoningTokens},
	}, nil
}

func validTextUsage(usage *struct {
	InputTokens   int64 `json:"input_tokens"`
	OutputTokens  int64 `json:"output_tokens"`
	TotalTokens   int64 `json:"total_tokens"`
	OutputDetails struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}) bool {
	return usage != nil && usage.InputTokens >= 0 && usage.OutputTokens >= 0 && usage.TotalTokens >= 0 &&
		usage.OutputDetails.ReasoningTokens >= 0 && usage.OutputDetails.ReasoningTokens <= usage.OutputTokens &&
		usage.TotalTokens == usage.InputTokens+usage.OutputTokens
}

type titleTextCandidate struct {
	Title        *string   `json:"title"`
	Keywords     *[]string `json:"keywords"`
	SourceFields *[]string `json:"source_fields"`
}

type seoTextCandidate struct {
	ShortDescription *string   `json:"short_description"`
	SellingPoints    *[]string `json:"selling_points"`
	LongDescription  *string   `json:"long_description"`
	SearchKeywords   *[]string `json:"search_keywords"`
	SourceFields     *[]string `json:"source_fields"`
}

type textLengthBounds struct {
	MinLength *int `json:"minLength"`
	MaxLength *int `json:"maxLength"`
}

func validateTextCandidates(outputJSON []byte, prompt CompiledTextPrompt) error {
	var envelope struct {
		Candidates *[]json.RawMessage `json:"candidates"`
	}
	if len(outputJSON) == 0 || strictJSONDecode(outputJSON, &envelope) != nil || envelope.Candidates == nil || len(*envelope.Candidates) != prompt.CandidateCount {
		return ErrTextProviderInvalidResponse
	}
	bounds, err := textResponseLengthBounds(prompt)
	if err != nil {
		return ErrTextProviderInvalidResponse
	}
	rules, requiredValues, err := textPromptValidationRules(prompt)
	if err != nil {
		return ErrTextProviderInvalidResponse
	}
	for _, raw := range *envelope.Candidates {
		switch prompt.SchemaName {
		case "cargoflow_product_title":
			var candidate titleTextCandidate
			if strictJSONDecode(raw, &candidate) != nil || candidate.Title == nil || candidate.Keywords == nil || candidate.SourceFields == nil || !withinTextBounds(*candidate.Title, bounds["title"]) || !validateTitleCandidateRules(candidate, rules, requiredValues) {
				return ErrTextProviderInvalidResponse
			}
		case "cargoflow_product_seo":
			var candidate seoTextCandidate
			if strictJSONDecode(raw, &candidate) != nil || candidate.ShortDescription == nil || candidate.SellingPoints == nil || candidate.LongDescription == nil || candidate.SearchKeywords == nil || candidate.SourceFields == nil ||
				!withinTextBounds(*candidate.ShortDescription, bounds["short_description"]) || !withinTextBounds(*candidate.LongDescription, bounds["long_description"]) || !validateSEOCandidateRules(candidate, rules, requiredValues) {
				return ErrTextProviderInvalidResponse
			}
		default:
			return ErrTextProviderInvalidResponse
		}
	}
	return nil
}

func textPromptValidationRules(prompt CompiledTextPrompt) (textConstraintRules, map[string][]string, error) {
	var input struct {
		Product ProductFacts `json:"product"`
		SKU     SKUFacts     `json:"sku"`
		Slot    struct {
			Constraints json.RawMessage `json:"constraints"`
		} `json:"slot"`
	}
	if err := json.Unmarshal(prompt.InputJSON, &input); err != nil {
		return textConstraintRules{}, nil, err
	}
	kind := models.AIContentSlotSEODescription
	if prompt.SchemaName == "cargoflow_product_title" {
		kind = models.AIContentSlotTitle
	}
	rules, err := parseTextConstraintRules(input.Slot.Constraints, kind)
	return rules, requiredTextFieldValues(input.Product, input.SKU), err
}

func validateTitleCandidateRules(candidate titleTextCandidate, rules textConstraintRules, requiredValues map[string][]string) bool {
	return validateTextRuleValues([]string{*candidate.Title}, *candidate.Keywords, rules, requiredValues)
}

func validateSEOCandidateRules(candidate seoTextCandidate, rules textConstraintRules, requiredValues map[string][]string) bool {
	content := []string{*candidate.ShortDescription, *candidate.LongDescription}
	content = append(content, (*candidate.SellingPoints)...)
	return validateTextRuleValues(content, *candidate.SearchKeywords, rules, requiredValues)
}

func validateTextRuleValues(content, keywords []string, rules textConstraintRules, requiredValues map[string][]string) bool {
	joined := strings.ToLower(strings.Join(append(append([]string{}, content...), keywords...), "\n"))
	for _, term := range rules.ForbiddenTerms {
		if strings.Contains(joined, strings.ToLower(strings.TrimSpace(term))) {
			return false
		}
	}
	for _, required := range rules.RequiredFields {
		values := requiredValues[normalizeRequiredSourceField(required)]
		matched := false
		for _, value := range values {
			if normalized := strings.ToLower(strings.TrimSpace(value)); normalized != "" && strings.Contains(joined, normalized) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if rules.KeywordPolicy == "natural" {
		seen := make(map[string]struct{}, len(keywords))
		for _, keyword := range keywords {
			normalized := strings.ToLower(strings.TrimSpace(keyword))
			if normalized == "" || utf8.RuneCountInString(keyword) > 100 {
				return false
			}
			if _, exists := seen[normalized]; exists {
				return false
			}
			seen[normalized] = struct{}{}
		}
	}
	return true
}

func requiredTextFieldValues(product ProductFacts, sku SKUFacts) map[string][]string {
	return map[string][]string{
		"product.name":     {product.Name},
		"product.brand":    {product.Brand},
		"product.category": {product.Category.NameZH, product.Category.NameEN},
		"sku.code":         {sku.Code},
		"sku.color":        {sku.Color},
		"sku.size":         {sku.Size},
	}
}

func normalizeRequiredSourceField(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "brand":
		return "product.brand"
	case "product_type":
		return "product.category"
	default:
		return normalized
	}
}

func strictJSONDecode(raw []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return ErrTextProviderInvalidResponse
		}
		return err
	}
	return nil
}

func textResponseLengthBounds(prompt CompiledTextPrompt) (map[string]textLengthBounds, error) {
	var schema struct {
		Properties struct {
			Candidates struct {
				Items struct {
					Properties map[string]textLengthBounds `json:"properties"`
				} `json:"items"`
			} `json:"candidates"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(prompt.JSONSchema, &schema); err != nil || schema.Properties.Candidates.Items.Properties == nil {
		return nil, ErrTextProviderInvalidResponse
	}
	return schema.Properties.Candidates.Items.Properties, nil
}

func withinTextBounds(value string, bounds textLengthBounds) bool {
	length := utf8.RuneCountInString(value)
	return (bounds.MinLength == nil || length >= *bounds.MinLength) && (bounds.MaxLength == nil || length <= *bounds.MaxLength)
}

func retryDelay(attempt int, headers http.Header) time.Duration {
	if milliseconds, err := strconv.ParseFloat(strings.TrimSpace(headers.Get("retry-after-ms")), 64); err == nil && milliseconds >= 0 {
		return cappedRetryDelay(time.Duration(milliseconds * float64(time.Millisecond)))
	}
	retryAfter := strings.TrimSpace(headers.Get("Retry-After"))
	if seconds, err := strconv.ParseFloat(retryAfter, 64); err == nil && seconds >= 0 {
		return cappedRetryDelay(time.Duration(seconds * float64(time.Second)))
	}
	if when, err := http.ParseTime(retryAfter); err == nil {
		delay := time.Until(when)
		if delay < 0 {
			return 0
		}
		return cappedRetryDelay(delay)
	}
	return time.Duration(attempt) * 200 * time.Millisecond
}

func cappedRetryDelay(delay time.Duration) time.Duration {
	if delay > 30*time.Second {
		return 30 * time.Second
	}
	return delay
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

var _ TextProvider = (*OpenAIResponsesClient)(nil)

func (client *OpenAIResponsesClient) String() string {
	return fmt.Sprintf("OpenAI Responses text client (%s)", client.config.Model)
}
