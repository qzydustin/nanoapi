package codec

import (
	"encoding/json"
	"fmt"
)

// ---------------------------------------------------------------------------
// Anthropic Messages decoding
// ---------------------------------------------------------------------------

type anthropicRequest struct {
	Model         string                 `json:"model"`
	System        json.RawMessage        `json:"system,omitempty"`
	Messages      []anthropicMessage     `json:"messages"`
	Stream        *bool                  `json:"stream,omitempty"`
	MaxTokens     *int                   `json:"max_tokens,omitempty"`
	Temperature   *float64               `json:"temperature,omitempty"`
	TopP          *float64               `json:"top_p,omitempty"`
	StopSequences []string               `json:"stop_sequences,omitempty"`
	Tools         []anthropicTool        `json:"tools,omitempty"`
	ToolChoice    *anthropicToolChoice   `json:"tool_choice,omitempty"`
	Thinking      *anthropicThinking     `json:"thinking,omitempty"`
	OutputConfig  *anthropicOutputConfig `json:"output_config,omitempty"`
}

type anthropicOutputConfig struct {
	Effort *string         `json:"effort,omitempty"`
	Format json.RawMessage `json:"format,omitempty"`
}

type anthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type anthropicContentBlock struct {
	Type string `json:"type"`

	// text
	Text string `json:"text,omitempty"`

	// image
	Source *anthropicImageSource `json:"source,omitempty"`

	// tool_use
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`

	// tool_result
	ToolUseID     string          `json:"tool_use_id,omitempty"`
	ResultContent json.RawMessage `json:"content,omitempty"`
	IsError       bool            `json:"is_error,omitempty"`

	// thinking
	Thinking string `json:"thinking,omitempty"`
}

type anthropicImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

type anthropicTool struct {
	Type        string `json:"type,omitempty"` // "web_search_20250305" for built-in tools
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema,omitempty"`
	MaxUses     *int   `json:"max_uses,omitempty"` // for web_search
}

type anthropicToolChoice struct {
	Type string `json:"type,omitempty"`
	Name string `json:"name,omitempty"`
}

type anthropicThinking struct {
	Type         string `json:"type"` // "enabled", "disabled", "adaptive"
	BudgetTokens *int   `json:"budget_tokens,omitempty"`
}

// DecodeAnthropicRequest decodes a raw JSON request body into a Request.
func DecodeAnthropicRequest(body []byte) (*Request, error) {
	var raw anthropicRequest
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode anthropic request: %w", err)
	}
	if raw.Model == "" {
		return nil, fmt.Errorf("anthropic request: model is required")
	}

	req := &Request{
		ClientModel: raw.Model,
	}

	if raw.Stream != nil && *raw.Stream {
		req.Stream = true
	}

	// Params
	req.Params.MaxTokens = raw.MaxTokens
	req.Params.Temperature = raw.Temperature
	req.Params.TopP = raw.TopP
	req.Params.Stop = raw.StopSequences

	if raw.Thinking != nil {
		req.Params.Reasoning = &Reasoning{
			Mode:         raw.Thinking.Type,
			BudgetTokens: raw.Thinking.BudgetTokens,
		}
	}
	if raw.OutputConfig != nil && raw.OutputConfig.Effort != nil {
		if req.Params.Reasoning == nil {
			req.Params.Reasoning = &Reasoning{}
		}
		req.Params.Reasoning.Effort = raw.OutputConfig.Effort
	}
	if raw.OutputConfig != nil && len(raw.OutputConfig.Format) > 0 && string(raw.OutputConfig.Format) != "null" {
		req.Params.ResponseFormat = decodeAnthropicResponseFormat(raw.OutputConfig.Format)
	}

	if raw.ToolChoice != nil {
		switch raw.ToolChoice.Type {
		case "auto":
			req.ToolChoice = &ToolChoice{Type: "auto"}
		case "any":
			req.ToolChoice = &ToolChoice{Type: "required"}
		case "tool":
			req.ToolChoice = &ToolChoice{Type: "tool", Name: raw.ToolChoice.Name}
		}
	}

	// Tools
	for _, t := range raw.Tools {
		tool := Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
		if t.Type == "web_search_20250305" {
			tool.Type = "web_search"
		} else if t.Type != "" {
			tool.Type = t.Type
		}
		if t.MaxUses != nil {
			tool.MaxUses = t.MaxUses
		}
		req.Tools = append(req.Tools, tool)
	}

	// System
	if len(raw.System) > 0 && string(raw.System) != "null" {
		sysBlocks, err := decodeAnthropicSystem(raw.System)
		if err != nil {
			return nil, err
		}
		req.System = sysBlocks
	}

	// Messages
	for _, m := range raw.Messages {
		blocks, err := decodeAnthropicContent(m.Content)
		if err != nil {
			return nil, fmt.Errorf("decode anthropic message: %w", err)
		}
		if len(blocks) == 0 {
			continue
		}
		req.Messages = append(req.Messages, Message{
			Role:    m.Role,
			Content: blocks,
		})
	}

	return req, nil
}

// decodeAnthropicSystem handles the Anthropic system field which can be a
// string or an array of content blocks.
func decodeAnthropicSystem(raw json.RawMessage) ([]ContentBlock, error) {
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("decode anthropic system string: %w", err)
		}
		return []ContentBlock{{Type: "text", Text: strPtr(s)}}, nil
	}

	var blocks []anthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, fmt.Errorf("decode anthropic system blocks: %w", err)
	}
	var result []ContentBlock
	for _, b := range blocks {
		if b.Type == "text" {
			result = append(result, ContentBlock{Type: "text", Text: strPtr(b.Text)})
		}
	}
	return result, nil
}

// decodeAnthropicContent handles the Anthropic message content field which
// can be a string or an array of content blocks.
func decodeAnthropicContent(raw json.RawMessage) ([]ContentBlock, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return []ContentBlock{{Type: "text", Text: strPtr(s)}}, nil
	}

	var blocks []anthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, err
	}

	var result []ContentBlock
	for _, b := range blocks {
		switch b.Type {
		case "text":
			result = append(result, ContentBlock{Type: "text", Text: strPtr(b.Text)})

		case "image":
			if b.Source == nil {
				continue
			}
			img := &Image{}
			switch b.Source.Type {
			case "base64":
				img.SourceType = "base64"
				img.MediaType = strPtr(b.Source.MediaType)
				img.Data = strPtr(b.Source.Data)
			case "url":
				img.SourceType = "url"
				img.URL = strPtr(b.Source.URL)
			default:
				img.SourceType = b.Source.Type
			}
			result = append(result, ContentBlock{Type: "image", Image: img})

		case "tool_use":
			result = append(result, ContentBlock{
				Type: "tool_call",
				ToolCall: &ToolCall{
					ID:        b.ID,
					Name:      b.Name,
					Arguments: b.Input,
				},
			})

		case "tool_result":
			var innerBlocks []ContentBlock
			if len(b.ResultContent) > 0 && string(b.ResultContent) != "null" {
				var err error
				innerBlocks, err = decodeAnthropicToolResultContent(b.ResultContent)
				if err != nil {
					return nil, fmt.Errorf("decode anthropic tool_result content: %w", err)
				}
			}
			result = append(result, ContentBlock{
				Type: "tool_result",
				ToolResult: &ToolResult{
					ToolCallID: b.ToolUseID,
					Content:    innerBlocks,
					IsError:    b.IsError,
				},
			})

		case "thinking":
			result = append(result, ContentBlock{
				Type:     "thinking",
				Thinking: &ThinkingBlock{Text: strPtr(b.Thinking)},
			})

		default:
			// Unknown request-side Anthropic blocks are dropped because the
			// OpenWebUI upstream does not have a meaningful way to consume them.
		}
	}
	return result, nil
}

// decodeAnthropicToolResultContent handles the content field inside a
// tool_result block, which can be a string or an array of content blocks.
func decodeAnthropicToolResultContent(raw json.RawMessage) ([]ContentBlock, error) {
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return []ContentBlock{{Type: "text", Text: strPtr(s)}}, nil
	}
	// Recurse into block array.
	return decodeAnthropicContent(raw)
}

func decodeAnthropicResponseFormat(raw json.RawMessage) *ResponseFormat {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}

	formatType, _ := m["type"].(string)
	switch formatType {
	case "json_object":
		return &ResponseFormat{Type: "json_object"}

	case "json_schema":
		rf := &ResponseFormat{
			Type: "json_schema",
			Name: "structured_output",
		}

		if name, ok := m["name"].(string); ok && name != "" {
			rf.Name = name
		}
		if strict, ok := m["strict"].(bool); ok {
			rf.Strict = &strict
		}

		if schema, ok := m["schema"]; ok {
			rf.Schema = schema
			return rf
		}

		if jsonSchema, ok := m["json_schema"].(map[string]any); ok {
			if name, ok := jsonSchema["name"].(string); ok && name != "" {
				rf.Name = name
			}
			if strict, ok := jsonSchema["strict"].(bool); ok {
				rf.Strict = &strict
			}
			if schema, ok := jsonSchema["schema"]; ok {
				rf.Schema = schema
				return rf
			}
		}
	}

	return nil
}
