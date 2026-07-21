package ai

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrImageProviderAuthentication     = errors.New("image provider authentication failed")
	ErrImageProviderRateLimit          = errors.New("image provider rate limited")
	ErrImageProviderInvalidRequest     = errors.New("image provider request is invalid")
	ErrImageProviderInvalidResponse    = errors.New("image provider response is invalid")
	ErrImageProviderRefusal            = errors.New("image provider refused the request")
	ErrImageProviderModeration         = errors.New("image provider blocked the request for moderation")
	ErrImageProviderAmbiguousTimeout   = errors.New("image provider request timed out after it may have been sent")
	ErrImageProviderAmbiguousTransport = errors.New("image provider transport failed after the request may have been sent")
)

type ImageInput struct {
	MIMEType string
	Bytes    []byte
}

type ImageRequest struct {
	Model    string
	APIMode  string
	Prompt   CompiledImagePrompt
	Inputs   []ImageInput
	Mask     *ImageInput
	Metadata map[string]string
}

type ImageUsage struct {
	InputTextTokens   int64
	InputImageTokens  int64
	OutputTextTokens  int64
	OutputImageTokens int64
	TotalTokens       int64
}

type ImageResponse struct {
	ResponseID  string
	RequestID   string
	ImageCallID string
	Model       string
	MIMEType    string
	ImageBytes  []byte
	Usage       ImageUsage
}

type ImageProvider interface {
	Generate(context.Context, []byte, ImageRequest) (ImageResponse, error)
}

type ImageProviderError struct {
	Kind       error
	StatusCode int
	RequestID  string
}

func (err *ImageProviderError) Error() string {
	if err == nil || err.Kind == nil {
		return "image provider failed"
	}
	if err.StatusCode != 0 {
		return fmt.Sprintf("%s (status %d)", err.Kind.Error(), err.StatusCode)
	}
	return err.Kind.Error()
}

func (err *ImageProviderError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Kind
}
