package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	maxImageResponsesBodyBytes = 72 << 20
	maxImageInputBytes         = 50 << 20
	maxImageOutputBytes        = 50 << 20
)

type OpenAIImageResponsesConfig struct {
	Model          string
	MaxAttempts    int
	RequestTimeout time.Duration
}

type OpenAIImageResponsesClient struct {
	baseURL string
	client  *http.Client
	config  OpenAIImageResponsesConfig
	sleep   func(context.Context, time.Duration) error
}

func NewOpenAIImageResponsesClient(baseURL string, client *http.Client, config OpenAIImageResponsesConfig) *OpenAIImageResponsesClient {
	if config.Model == "" {
		config.Model = "gpt-5.6"
	}
	if config.MaxAttempts < 1 {
		config.MaxAttempts = 3
	}
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = 180 * time.Second
	}
	if client == nil {
		client = &http.Client{Timeout: config.RequestTimeout}
	} else {
		cloned := *client
		cloned.Timeout = config.RequestTimeout
		client = &cloned
	}
	return &OpenAIImageResponsesClient{baseURL: strings.TrimRight(baseURL, "/"), client: client, config: config, sleep: sleepWithContext}
}

func (client *OpenAIImageResponsesClient) Generate(ctx context.Context, apiKey []byte, request ImageRequest) (ImageResponse, error) {
	body, err := client.requestBody(request)
	if err != nil {
		return ImageResponse{}, err
	}
	defer clearByteSlice(body)
	for attempt := 1; attempt <= client.config.MaxAttempts; attempt++ {
		response, err := client.send(ctx, apiKey, body)
		if err != nil {
			if ctx.Err() != nil {
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderAmbiguousTimeout}
				}
				return ImageResponse{}, ctx.Err()
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderAmbiguousTimeout}
			}
			return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderAmbiguousTransport}
		}
		requestID := response.Header.Get("x-request-id")
		if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
			return decodeOpenAIImageResponse(response, requestID)
		}
		errorCode := readOpenAIErrorCode(response.Body)
		_ = response.Body.Close()
		switch {
		case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
			return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderAuthentication, StatusCode: response.StatusCode, RequestID: requestID}
		case response.StatusCode == http.StatusTooManyRequests:
			if attempt < client.config.MaxAttempts {
				if err := client.sleep(ctx, retryDelay(attempt, response.Header)); err != nil {
					return ImageResponse{}, err
				}
				continue
			}
			return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderRateLimit, StatusCode: response.StatusCode, RequestID: requestID}
		case response.StatusCode >= http.StatusInternalServerError:
			return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderAmbiguousTransport, StatusCode: response.StatusCode, RequestID: requestID}
		case strings.Contains(errorCode, "moderation") || strings.Contains(errorCode, "safety"):
			return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderModeration, StatusCode: response.StatusCode, RequestID: requestID}
		default:
			return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderInvalidRequest, StatusCode: response.StatusCode, RequestID: requestID}
		}
	}
	return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderRateLimit}
}

func (client *OpenAIImageResponsesClient) requestBody(request ImageRequest) ([]byte, error) {
	model := strings.TrimSpace(request.Model)
	if model == "" {
		model = client.config.Model
	}
	if len(request.Prompt.NormalizedInputJSON) == 0 || len(request.Prompt.OrderedInputListJSON) == 0 || request.Prompt.Instructions == "" || request.Prompt.ToolConfig.Moderation != "auto" || (request.Prompt.ToolConfig.Action != "generate" && request.Prompt.ToolConfig.Action != "edit") || !supportedImageSize(request.Prompt.ToolConfig.Size) || !supportedQuality(request.Prompt.ToolConfig.Quality) || len(request.Inputs) == 0 || len(request.Metadata) > 16 {
		return nil, &ImageProviderError{Kind: ErrImageProviderInvalidRequest}
	}
	for key, value := range request.Metadata {
		if strings.TrimSpace(key) == "" || len(key) > 64 || len(value) > 512 || containsForbiddenTextPromptString(key) || containsForbiddenTextPromptString(value) {
			return nil, &ImageProviderError{Kind: ErrImageProviderInvalidRequest}
		}
	}
	var inputBytes int
	content := make([]map[string]any, 0, len(request.Inputs)+1)
	inputEnvelope, err := json.Marshal(struct {
		NormalizedInput json.RawMessage `json:"normalized_input"`
		OrderedInputs   json.RawMessage `json:"ordered_inputs"`
	}{request.Prompt.NormalizedInputJSON, request.Prompt.OrderedInputListJSON})
	if err != nil {
		return nil, &ImageProviderError{Kind: ErrImageProviderInvalidRequest}
	}
	content = append(content, map[string]any{"type": "input_text", "text": string(inputEnvelope)})
	clearByteSlice(inputEnvelope)
	for _, input := range request.Inputs {
		if input.MIMEType != "image/png" && input.MIMEType != "image/jpeg" && input.MIMEType != "image/webp" {
			return nil, &ImageProviderError{Kind: ErrImageProviderInvalidRequest}
		}
		inputBytes += len(input.Bytes)
		if len(input.Bytes) == 0 || inputBytes > maxImageInputBytes {
			return nil, &ImageProviderError{Kind: ErrImageProviderInvalidRequest}
		}
		content = append(content, map[string]any{"type": "input_image", "image_url": "data:" + input.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(input.Bytes)})
	}
	payload := struct {
		Model        string            `json:"model"`
		Instructions string            `json:"instructions"`
		Input        []map[string]any  `json:"input"`
		Store        bool              `json:"store"`
		Tools        []map[string]any  `json:"tools"`
		ToolChoice   map[string]string `json:"tool_choice"`
		Metadata     map[string]string `json:"metadata,omitempty"`
	}{
		Model: model, Instructions: request.Prompt.Instructions, Store: false,
		Input:      []map[string]any{{"role": "user", "content": content}},
		Tools:      []map[string]any{{"type": "image_generation", "action": request.Prompt.ToolConfig.Action, "size": request.Prompt.ToolConfig.Size, "quality": request.Prompt.ToolConfig.Quality, "moderation": "auto"}},
		ToolChoice: map[string]string{"type": "image_generation"}, Metadata: request.Metadata,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, &ImageProviderError{Kind: ErrImageProviderInvalidRequest}
	}
	return body, nil
}

func (client *OpenAIImageResponsesClient) send(ctx context.Context, apiKey, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+string(apiKey))
	req.Header.Set("Content-Type", "application/json")
	return client.client.Do(req)
}

func decodeOpenAIImageResponse(response *http.Response, requestID string) (ImageResponse, error) {
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxImageResponsesBodyBytes+1))
	if err != nil || len(body) > maxImageResponsesBodyBytes {
		return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderInvalidResponse, RequestID: requestID}
	}
	defer clearByteSlice(body)
	var decoded struct {
		ID          string `json:"id"`
		Status      string `json:"status"`
		Model       string `json:"model"`
		ServiceTier string `json:"service_tier"`
		Output      []struct {
			Type    string `json:"type"`
			ID      string `json:"id"`
			Status  string `json:"status"`
			Result  string `json:"result"`
			Content []struct {
				Type string `json:"type"`
			} `json:"content"`
		} `json:"output"`
		Usage *struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
			TotalTokens  int64 `json:"total_tokens"`
			InputDetails struct {
				ImageTokens  int64 `json:"image_tokens"`
				CachedTokens int64 `json:"cached_tokens"`
			} `json:"input_tokens_details"`
			OutputDetails struct {
				ImageTokens     int64 `json:"image_tokens"`
				ReasoningTokens int64 `json:"reasoning_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &decoded) != nil || decoded.ID == "" || decoded.Status != "completed" || strings.TrimSpace(decoded.Model) == "" || decoded.Usage == nil || decoded.Usage.InputTokens < 0 || decoded.Usage.OutputTokens < 0 || decoded.Usage.TotalTokens != decoded.Usage.InputTokens+decoded.Usage.OutputTokens || decoded.Usage.InputDetails.ImageTokens < 0 || decoded.Usage.InputDetails.ImageTokens > decoded.Usage.InputTokens || decoded.Usage.InputDetails.CachedTokens < 0 || decoded.Usage.InputDetails.CachedTokens > decoded.Usage.InputTokens || decoded.Usage.OutputDetails.ImageTokens < 0 || decoded.Usage.OutputDetails.ImageTokens > decoded.Usage.OutputTokens || decoded.Usage.OutputDetails.ReasoningTokens < 0 || decoded.Usage.OutputDetails.ReasoningTokens > decoded.Usage.OutputTokens {
		return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderInvalidResponse, RequestID: requestID}
	}
	var callID, encoded string
	for _, output := range decoded.Output {
		if output.Type == "message" {
			for _, content := range output.Content {
				if content.Type == "refusal" {
					return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderRefusal, RequestID: requestID}
				}
			}
		}
		if output.Type != "image_generation_call" {
			continue
		}
		if callID != "" || output.ID == "" || output.Status != "completed" || output.Result == "" {
			return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderInvalidResponse, RequestID: requestID}
		}
		callID, encoded = output.ID, output.Result
	}
	if callID == "" || base64.StdEncoding.DecodedLen(len(encoded)) > maxImageOutputBytes {
		return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderInvalidResponse, RequestID: requestID}
	}
	imageBytes, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(imageBytes) == 0 || len(imageBytes) > maxImageOutputBytes {
		clearByteSlice(imageBytes)
		return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderInvalidResponse, RequestID: requestID}
	}
	return ImageResponse{
		ResponseID: decoded.ID, RequestID: requestID, ImageCallID: callID, Model: decoded.Model, ServiceTier: defaultString(decoded.ServiceTier, "default"), MIMEType: "image/png", ImageBytes: imageBytes,
		Usage: ImageUsage{InputTextTokens: decoded.Usage.InputTokens - decoded.Usage.InputDetails.ImageTokens, CachedInputTokens: decoded.Usage.InputDetails.CachedTokens, InputImageTokens: decoded.Usage.InputDetails.ImageTokens, OutputTextTokens: decoded.Usage.OutputTokens - decoded.Usage.OutputDetails.ImageTokens, OutputImageTokens: decoded.Usage.OutputDetails.ImageTokens, ReasoningTokens: decoded.Usage.OutputDetails.ReasoningTokens, TotalTokens: decoded.Usage.TotalTokens},
	}, nil
}

func readOpenAIErrorCode(body io.Reader) string {
	limited, err := io.ReadAll(io.LimitReader(body, 64<<10))
	if err != nil {
		return ""
	}
	defer clearByteSlice(limited)
	var decoded struct {
		Error struct {
			Code string `json:"code"`
			Type string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(limited, &decoded) != nil {
		return ""
	}
	return strings.ToLower(decoded.Error.Code + " " + decoded.Error.Type)
}

func clearByteSlice(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

var _ ImageProvider = (*OpenAIImageResponsesClient)(nil)
