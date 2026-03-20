package protocol

import (
	"encoding/json"
	"fmt"
	"io"
)

type ToolFunction struct {
	Name        string         `json:"name"`
	Description *string        `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ChatCompletionRequest struct {
	Raw        map[string]any
	Model      string
	Messages   []map[string]any
	Tools      []Tool
	ToolChoice any
	Stream     bool
}

type ParsedToolCall struct {
	Name string
	Args map[string]any
}

func DecodeChatCompletionRequest(r io.Reader) (*ChatCompletionRequest, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	return DecodeChatCompletionRequestBytes(data)
}

func DecodeChatCompletionRequestBytes(data []byte) (*ChatCompletionRequest, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode request body: %w", err)
	}

	var typed struct {
		Model      string           `json:"model"`
		Messages   []map[string]any `json:"messages"`
		Tools      []Tool           `json:"tools"`
		ToolChoice any              `json:"tool_choice"`
		Stream     bool             `json:"stream"`
	}
	if err := json.Unmarshal(data, &typed); err != nil {
		return nil, fmt.Errorf("decode typed request body: %w", err)
	}

	if typed.Model == "" {
		return nil, fmt.Errorf("model is required")
	}
	if len(typed.Messages) == 0 {
		return nil, fmt.Errorf("messages is required")
	}

	return &ChatCompletionRequest{
		Raw:        raw,
		Model:      typed.Model,
		Messages:   typed.Messages,
		Tools:      typed.Tools,
		ToolChoice: typed.ToolChoice,
		Stream:     typed.Stream,
	}, nil
}
