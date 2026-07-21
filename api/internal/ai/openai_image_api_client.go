package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

type OpenAIImagesConfig struct {
	Model          string
	MaxAttempts    int
	RequestTimeout time.Duration
}

type OpenAIImagesClient struct {
	baseURL string
	client  *http.Client
	config  OpenAIImagesConfig
}

func NewOpenAIImagesClient(baseURL string, client *http.Client, config OpenAIImagesConfig) *OpenAIImagesClient {
	if config.Model == "" {
		config.Model = DefaultOpenAIImageGenerationModel
	}
	if config.MaxAttempts < 1 {
		config.MaxAttempts = 2
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
	return &OpenAIImagesClient{baseURL: strings.TrimRight(baseURL, "/"), client: client, config: config}
}

func (client *OpenAIImagesClient) Generate(ctx context.Context, apiKey []byte, request ImageRequest) (ImageResponse, error) {
	model := configuredModel(request.Model, client.config.Model)
	if !strings.HasPrefix(strings.ToLower(model), "gpt-image-") || request.Prompt.Instructions == "" || !supportedImageSize(request.Prompt.ToolConfig.Size) || !supportedQuality(request.Prompt.ToolConfig.Quality) {
		return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderInvalidRequest}
	}
	for attempt := 1; attempt <= client.config.MaxAttempts; attempt++ {
		body, contentType, path, err := imagesRequestBody(model, request)
		if err != nil {
			return ImageResponse{}, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+path, bytes.NewReader(body))
		if err != nil {
			clearByteSlice(body)
			return ImageResponse{}, err
		}
		req.Header.Set("Authorization", "Bearer "+string(apiKey))
		req.Header.Set("Content-Type", contentType)
		response, err := client.client.Do(req)
		clearByteSlice(body)
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderAmbiguousTimeout}
			}
			return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderAmbiguousTransport}
		}
		requestID := response.Header.Get("x-request-id")
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return decodeOpenAIImagesResponse(response, requestID, model)
		}
		code := readOpenAIErrorCode(response.Body)
		_ = response.Body.Close()
		switch {
		case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
			return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderAuthentication, StatusCode: response.StatusCode, RequestID: requestID}
		case response.StatusCode == http.StatusTooManyRequests:
			if attempt < client.config.MaxAttempts {
				if err := sleepWithContext(ctx, time.Duration(attempt)*time.Second); err != nil {
					return ImageResponse{}, err
				}
				continue
			}
			return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderRateLimit, StatusCode: response.StatusCode, RequestID: requestID}
		case response.StatusCode >= 500:
			return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderAmbiguousTransport, StatusCode: response.StatusCode, RequestID: requestID}
		case strings.Contains(code, "moderation") || strings.Contains(code, "safety"):
			return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderModeration, StatusCode: response.StatusCode, RequestID: requestID}
		default:
			return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderInvalidRequest, StatusCode: response.StatusCode, RequestID: requestID}
		}
	}
	return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderRateLimit}
}

func imagesRequestBody(model string, request ImageRequest) ([]byte, string, string, error) {
	if len(request.Inputs) == 0 {
		body, err := json.Marshal(map[string]any{"model": model, "prompt": request.Prompt.Instructions, "n": 1, "size": request.Prompt.ToolConfig.Size, "quality": request.Prompt.ToolConfig.Quality, "output_format": "png"})
		return body, "application/json", "/images/generations", err
	}
	var buffer bytes.Buffer
	w := multipart.NewWriter(&buffer)
	fields := map[string]string{"model": model, "prompt": request.Prompt.Instructions, "n": "1", "size": request.Prompt.ToolConfig.Size, "quality": request.Prompt.ToolConfig.Quality, "output_format": "png"}
	for key, value := range fields {
		if err := w.WriteField(key, value); err != nil {
			return nil, "", "", err
		}
	}
	for index, input := range request.Inputs {
		if len(input.Bytes) == 0 || (input.MIMEType != "image/png" && input.MIMEType != "image/jpeg" && input.MIMEType != "image/webp") {
			return nil, "", "", &ImageProviderError{Kind: ErrImageProviderInvalidRequest}
		}
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image[]"; filename="source-%d.%s"`, index+1, multipartImageExtension(input.MIMEType)))
		header.Set("Content-Type", input.MIMEType)
		part, err := w.CreatePart(header)
		if err != nil {
			return nil, "", "", err
		}
		if _, err := part.Write(input.Bytes); err != nil {
			return nil, "", "", err
		}
	}
	if request.Mask != nil {
		if request.Mask.MIMEType != "image/png" || len(request.Mask.Bytes) == 0 {
			return nil, "", "", &ImageProviderError{Kind: ErrImageProviderInvalidRequest}
		}
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", `form-data; name="mask"; filename="mask.png"`)
		header.Set("Content-Type", "image/png")
		part, err := w.CreatePart(header)
		if err != nil {
			return nil, "", "", err
		}
		if _, err := part.Write(request.Mask.Bytes); err != nil {
			return nil, "", "", err
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", "", err
	}
	return buffer.Bytes(), w.FormDataContentType(), "/images/edits", nil
}

func multipartImageExtension(mimeType string) string {
	if mimeType == "image/jpeg" {
		return "jpg"
	}
	if mimeType == "image/webp" {
		return "webp"
	}
	return "png"
}

func decodeOpenAIImagesResponse(response *http.Response, requestID, model string) (ImageResponse, error) {
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxImageResponsesBodyBytes+1))
	if err != nil || len(body) > maxImageResponsesBodyBytes {
		return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderInvalidResponse, RequestID: requestID}
	}
	defer clearByteSlice(body)
	var payload struct {
		Data []struct {
			B64JSON string `json:"b64_json"`
		} `json:"data"`
		Usage struct {
			TotalTokens  int64 `json:"total_tokens"`
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
			InputDetails struct {
				TextTokens  int64 `json:"text_tokens"`
				ImageTokens int64 `json:"image_tokens"`
			} `json:"input_tokens_details"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &payload) != nil || len(payload.Data) != 1 || payload.Data[0].B64JSON == "" {
		return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderInvalidResponse, RequestID: requestID}
	}
	imageBytes, err := base64.StdEncoding.Strict().DecodeString(payload.Data[0].B64JSON)
	if err != nil || len(imageBytes) == 0 || len(imageBytes) > maxImageOutputBytes {
		clearByteSlice(imageBytes)
		return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderInvalidResponse, RequestID: requestID}
	}
	responseID := requestID
	if responseID == "" {
		responseID = "images-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return ImageResponse{ResponseID: responseID, RequestID: requestID, ImageCallID: responseID, Model: model, MIMEType: "image/png", ImageBytes: imageBytes, Usage: ImageUsage{InputTextTokens: payload.Usage.InputDetails.TextTokens, InputImageTokens: payload.Usage.InputDetails.ImageTokens, OutputImageTokens: payload.Usage.OutputTokens, TotalTokens: payload.Usage.TotalTokens}}, nil
}

type OpenAIImageProvider struct {
	Responses ImageProvider
	Images    ImageProvider
}

func (provider *OpenAIImageProvider) Generate(ctx context.Context, key []byte, request ImageRequest) (ImageResponse, error) {
	if request.APIMode == "images" {
		if provider == nil || provider.Images == nil {
			return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderInvalidRequest}
		}
		return provider.Images.Generate(ctx, key, request)
	}
	if provider == nil || provider.Responses == nil {
		return ImageResponse{}, &ImageProviderError{Kind: ErrImageProviderInvalidRequest}
	}
	return provider.Responses.Generate(ctx, key, request)
}
