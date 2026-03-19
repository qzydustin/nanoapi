package canonical

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DecodeRequest decodes a raw JSON request body into a CanonicalRequest based
// on the specified protocol.
func DecodeRequest(protocol Protocol, body []byte) (*CanonicalRequest, error) {
	switch protocol {
	case ProtocolOpenAIChat:
		return decodeOpenAIChat(body)
	case ProtocolAnthropicMessage:
		return decodeAnthropicMessages(body)
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", protocol)
	}
}

// ---------------------------------------------------------------------------
// OpenAI Chat Completions decoding
// ---------------------------------------------------------------------------

type openAIRequest struct {
	Model            string            `json:"model"`
	Messages         []openAIMessage   `json:"messages"`
	Stream           *bool             `json:"stream,omitempty"`
	MaxTokens        *int              `json:"max_tokens,omitempty"`
	MaxCompTokens    *int              `json:"max_completion_tokens,omitempty"`
	Temperature      *float64          `json:"temperature,omitempty"`
	TopP             *float64          `json:"top_p,omitempty"`
	Stop             json.RawMessage   `json:"stop,omitempty"`
	Tools            []openAITool      `json:"tools,omitempty"`
	ReasoningEffort  *string           `json:"reasoning_effort,omitempty"`
}

type openAIMessage struct {
	Role       string            `json:"role"`
	Content    json.RawMessage   `json:"content,omitempty"`
	ToolCalls  []openAIToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
	Name       string            `json:"name,omitempty"`
}

type openAIContentPart struct {
	Type     string         `json:"type"`
	Text     string         `json:"text,omitempty"`
	ImageURL *openAIImageURL `json:"image_url,omitempty"`
}

type openAIImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAITool struct {
	Type     string           `json:"type"`
	Function openAIToolFunc   `json:"function"`
}

type openAIToolFunc struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

func decodeOpenAIChat(body []byte) (*CanonicalRequest, error) {
	var raw openAIRequest
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode openai request: %w", err)
	}
	if raw.Model == "" {
		return nil, fmt.Errorf("openai request: model is required")
	}

	req := &CanonicalRequest{
		ClientProtocol: ProtocolOpenAIChat,
		ClientModel:    raw.Model,
	}

	if raw.Stream != nil && *raw.Stream {
		req.Stream = true
	}

	// Params
	req.Params.Temperature = raw.Temperature
	req.Params.TopP = raw.TopP
	if raw.MaxCompTokens != nil {
		req.Params.MaxTokens = raw.MaxCompTokens
	} else {
		req.Params.MaxTokens = raw.MaxTokens
	}
	req.Params.Stop = decodeOpenAIStop(raw.Stop)

	if raw.ReasoningEffort != nil {
		req.Params.Reasoning = &CanonicalReasoning{
			Effort: raw.ReasoningEffort,
		}
	}

	// Tools
	for _, t := range raw.Tools {
		req.Tools = append(req.Tools, CanonicalTool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		})
	}

	// Messages
	for _, m := range raw.Messages {
		switch m.Role {
		case "system", "developer":
			blocks, err := decodeOpenAIContent(m.Content)
			if err != nil {
				return nil, fmt.Errorf("decode openai system content: %w", err)
			}
			req.System = append(req.System, blocks...)

		case "tool":
			// tool result message
			blocks, err := decodeOpenAIContent(m.Content)
			if err != nil {
				return nil, fmt.Errorf("decode openai tool content: %w", err)
			}
			req.Messages = append(req.Messages, CanonicalMessage{
				Role: "tool",
				Content: []CanonicalContentBlock{
					{
						Type: "tool_result",
						ToolResult: &CanonicalToolResult{
							ToolCallID: m.ToolCallID,
							Content:    blocks,
						},
					},
				},
			})

		default:
			blocks, err := decodeOpenAIContent(m.Content)
			if err != nil {
				return nil, fmt.Errorf("decode openai message content: %w", err)
			}

			// Append tool_call blocks from assistant messages.
			for _, tc := range m.ToolCalls {
				var args any
				if tc.Function.Arguments != "" {
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
						args = tc.Function.Arguments // keep as string on failure
					}
				}
				blocks = append(blocks, CanonicalContentBlock{
					Type: "tool_call",
					ToolCall: &CanonicalToolCall{
						ID:        tc.ID,
						Name:      tc.Function.Name,
						Arguments: args,
					},
				})
			}

			req.Messages = append(req.Messages, CanonicalMessage{
				Role:    m.Role,
				Content: blocks,
			})
		}
	}

	return req, nil
}

// decodeOpenAIContent handles the flexible OpenAI content field which can be
// a JSON string or an array of content part objects.
func decodeOpenAIContent(raw json.RawMessage) ([]CanonicalContentBlock, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	// Try string first.
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return []CanonicalContentBlock{{Type: "text", Text: strPtr(s)}}, nil
	}

	// Array of content parts.
	var parts []openAIContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, err
	}

	var blocks []CanonicalContentBlock
	for _, p := range parts {
		switch p.Type {
		case "text":
			blocks = append(blocks, CanonicalContentBlock{Type: "text", Text: strPtr(p.Text)})
		case "image_url":
			if p.ImageURL == nil {
				continue
			}
			img := parseOpenAIImageURL(p.ImageURL.URL)
			blocks = append(blocks, CanonicalContentBlock{Type: "image", Image: img})
		default:
			// Unknown content part types are silently skipped in V1.
		}
	}
	return blocks, nil
}

// parseOpenAIImageURL converts an OpenAI image_url.url string into a
// CanonicalImage, distinguishing data URLs from HTTP URLs.
func parseOpenAIImageURL(url string) *CanonicalImage {
	if mediaType, data, ok := parseDataURL(url); ok {
		return &CanonicalImage{
			SourceType: "data_url",
			MediaType:  strPtr(mediaType),
			Data:       strPtr(data),
		}
	}
	return &CanonicalImage{
		SourceType: "url",
		URL:        strPtr(url),
	}
}

// decodeOpenAIStop handles the stop field which can be a string or an array.
func decodeOpenAIStop(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil
		}
		return []string{s}
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil
	}
	return arr
}

// ---------------------------------------------------------------------------
// Anthropic Messages decoding
// ---------------------------------------------------------------------------

type anthropicRequest struct {
	Model         string              `json:"model"`
	System        json.RawMessage     `json:"system,omitempty"`
	Messages      []anthropicMessage  `json:"messages"`
	Stream        *bool               `json:"stream,omitempty"`
	MaxTokens     *int                `json:"max_tokens,omitempty"`
	Temperature   *float64            `json:"temperature,omitempty"`
	TopP          *float64            `json:"top_p,omitempty"`
	StopSequences []string            `json:"stop_sequences,omitempty"`
	Tools         []anthropicTool     `json:"tools,omitempty"`
	Thinking      *anthropicThinking  `json:"thinking,omitempty"`
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
	ToolUseID      string          `json:"tool_use_id,omitempty"`
	ResultContent  json.RawMessage `json:"content,omitempty"`

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
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema,omitempty"`
}

type anthropicThinking struct {
	Type         string `json:"type"` // "enabled", "disabled", "adaptive"
	BudgetTokens *int   `json:"budget_tokens,omitempty"`
}

func decodeAnthropicMessages(body []byte) (*CanonicalRequest, error) {
	var raw anthropicRequest
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode anthropic request: %w", err)
	}
	if raw.Model == "" {
		return nil, fmt.Errorf("anthropic request: model is required")
	}

	req := &CanonicalRequest{
		ClientProtocol: ProtocolAnthropicMessage,
		ClientModel:    raw.Model,
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
		req.Params.Reasoning = &CanonicalReasoning{
			Mode:         raw.Thinking.Type,
			BudgetTokens: raw.Thinking.BudgetTokens,
		}
	}

	// Tools
	for _, t := range raw.Tools {
		req.Tools = append(req.Tools, CanonicalTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
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
		req.Messages = append(req.Messages, CanonicalMessage{
			Role:    m.Role,
			Content: blocks,
		})
	}

	return req, nil
}

// decodeAnthropicSystem handles the Anthropic system field which can be a
// string or an array of content blocks.
func decodeAnthropicSystem(raw json.RawMessage) ([]CanonicalContentBlock, error) {
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("decode anthropic system string: %w", err)
		}
		return []CanonicalContentBlock{{Type: "text", Text: strPtr(s)}}, nil
	}

	var blocks []anthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, fmt.Errorf("decode anthropic system blocks: %w", err)
	}
	var result []CanonicalContentBlock
	for _, b := range blocks {
		if b.Type == "text" {
			result = append(result, CanonicalContentBlock{Type: "text", Text: strPtr(b.Text)})
		}
	}
	return result, nil
}

// decodeAnthropicContent handles the Anthropic message content field which
// can be a string or an array of content blocks.
func decodeAnthropicContent(raw json.RawMessage) ([]CanonicalContentBlock, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return []CanonicalContentBlock{{Type: "text", Text: strPtr(s)}}, nil
	}

	var blocks []anthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, err
	}

	var result []CanonicalContentBlock
	for _, b := range blocks {
		switch b.Type {
		case "text":
			result = append(result, CanonicalContentBlock{Type: "text", Text: strPtr(b.Text)})

		case "image":
			if b.Source == nil {
				continue
			}
			img := &CanonicalImage{}
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
			result = append(result, CanonicalContentBlock{Type: "image", Image: img})

		case "tool_use":
			result = append(result, CanonicalContentBlock{
				Type: "tool_call",
				ToolCall: &CanonicalToolCall{
					ID:        b.ID,
					Name:      b.Name,
					Arguments: b.Input,
				},
			})

		case "tool_result":
			var innerBlocks []CanonicalContentBlock
			if len(b.ResultContent) > 0 && string(b.ResultContent) != "null" {
				var err error
				innerBlocks, err = decodeAnthropicToolResultContent(b.ResultContent)
				if err != nil {
					return nil, fmt.Errorf("decode anthropic tool_result content: %w", err)
				}
			}
			result = append(result, CanonicalContentBlock{
				Type: "tool_result",
				ToolResult: &CanonicalToolResult{
					ToolCallID: b.ToolUseID,
					Content:    innerBlocks,
				},
			})

		case "thinking":
			result = append(result, CanonicalContentBlock{
				Type:     "thinking",
				Thinking: &CanonicalThinkingBlock{Text: strPtr(b.Thinking)},
			})
		}
	}
	return result, nil
}

// decodeAnthropicToolResultContent handles the content field inside a
// tool_result block, which can be a string or an array of content blocks.
func decodeAnthropicToolResultContent(raw json.RawMessage) ([]CanonicalContentBlock, error) {
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return []CanonicalContentBlock{{Type: "text", Text: strPtr(s)}}, nil
	}
	// Recurse into block array.
	return decodeAnthropicContent(raw)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func strPtr(s string) *string { return &s }

// parseDataURL extracts the media type and base64 data from a data URL of
// the form "data:<mediaType>;base64,<data>".
func parseDataURL(url string) (mediaType, base64Data string, ok bool) {
	if !strings.HasPrefix(url, "data:") {
		return "", "", false
	}
	rest := url[5:] // after "data:"
	// Must contain ";base64," marker.
	marker := ";base64,"
	idx := strings.Index(rest, marker)
	if idx == -1 {
		return "", "", false
	}
	mediaType = rest[:idx]
	base64Data = rest[idx+len(marker):]
	return mediaType, base64Data, true
}
