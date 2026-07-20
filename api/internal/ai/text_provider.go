package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

var (
	ErrTextProviderAuthentication     = errors.New("text provider authentication failed")
	ErrTextProviderRateLimit          = errors.New("text provider rate limited")
	ErrTextProviderRetryable          = errors.New("text provider temporary failure")
	ErrTextProviderInvalidRequest     = errors.New("text provider request is invalid")
	ErrTextProviderInvalidResponse    = errors.New("text provider response is invalid")
	ErrTextProviderRefusal            = errors.New("text provider refused the request")
	ErrTextProviderAmbiguousTimeout   = errors.New("text provider request timed out after it may have been sent")
	ErrTextProviderAmbiguousTransport = errors.New("text provider transport failed after the request may have been sent")
)

type TextRequest struct {
	Prompt   CompiledTextPrompt
	Inputs   []ImageInput
	Metadata map[string]string
}

type TextUsage struct {
	InputTextTokens  int64
	InputImageTokens int64
	OutputTextTokens int64
	TotalTokens      int64
	ReasoningTokens  int64
}

type TextResponse struct {
	ResponseID string
	RequestID  string
	Model      string
	OutputJSON json.RawMessage
	Usage      TextUsage
}

type TextProvider interface {
	Generate(context.Context, []byte, TextRequest) (TextResponse, error)
}

type TextProviderError struct {
	Kind       error
	StatusCode int
	RequestID  string
}

func (err *TextProviderError) Error() string {
	if err == nil || err.Kind == nil {
		return "text provider failed"
	}
	if err.StatusCode != 0 {
		return fmt.Sprintf("%s (status %d)", err.Kind.Error(), err.StatusCode)
	}
	return err.Kind.Error()
}

func (err *TextProviderError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Kind
}
