package codec

import (
	"encoding/json"
	"fmt"
	"strings"
)

type openAIInRequest struct {
	Model               string          `json:"model"`
	Messages            []openAIInMsg   `json:"messages"`
	Stream              *bool           `json:"stream,omitempty"`
	MaxCompletionTokens *int            `json:"max_completion_tokens,omitempty"`
	MaxTokensLegacy     *int            `json:"max_tokens,omitempty"`
	Temperature         *float64        `json:"temperature,omitempty"`
	TopP                *float64        `json:"top_p,omitempty"`
	Stop                json.RawMessage `json:"stop,omitempty"`
	Tools               []openAIInTool  `json:"tools,omitempty"`
	ToolChoice          json.RawMessage `json:"tool_choice,omitempty"`
	ResponseFormat      json.RawMessage `json:"response_format,omitempty"`
	ReasoningEffort     *string         `json:"reasoning_effort,omitempty"`
	Features            map[string]bool `json:"features,omitempty"`
}

type openAIInMsg struct {
	Role             string          `json:"role"`
	Content          json.RawMessage `json:"content,omitempty"`
	ToolCalls        []openAIInTC    `json:"tool_calls,omitempty"`
	ToolCallID       string          `json:"tool_call_id,omitempty"`
	ReasoningContent *string         `json:"reasoning_content,omitempty"`
	ThinkingBlocks   []openAIInTB    `json:"thinking_blocks,omitempty"`
}

type openAIInTC struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function openAIInTCFunc `json:"function"`
}

type openAIInTCFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIInTB struct {
	Type      string  `json:"type"`
	Thinking  *string `json:"thinking,omitempty"`
	Signature *string `json:"signature,omitempty"`
}

type openAIInTool struct {
	Type     string           `json:"type"`
	Function openAIInToolFunc `json:"function"`
}

type openAIInToolFunc struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type openAIInContentPart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *openAIInImgURL `json:"image_url,omitempty"`
}

type openAIInImgURL struct {
	URL string `json:"url"`
}

// DecodeOpenAIRequest decodes a raw JSON request body in OpenAI chat
// completion format into a canonical Request.
func DecodeOpenAIRequest(body []byte) (*Request, error) {
	var raw openAIInRequest
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode openai request: %w", err)
	}
	if raw.Model == "" {
		return nil, fmt.Errorf("openai request: model is required")
	}

	req := &Request{
		ClientModel: raw.Model,
	}

	if raw.Stream != nil && *raw.Stream {
		req.Stream = true
	}

	// Params
	if raw.MaxCompletionTokens != nil {
		req.Params.MaxTokens = raw.MaxCompletionTokens
	} else if raw.MaxTokensLegacy != nil {
		req.Params.MaxTokens = raw.MaxTokensLegacy
	}
	req.Params.Temperature = raw.Temperature
	req.Params.TopP = raw.TopP

	// Stop: string or []string
	if len(raw.Stop) > 0 && string(raw.Stop) != "null" {
		stop, err := decodeOpenAIStop(raw.Stop)
		if err != nil {
			return nil, err
		}
		req.Params.Stop = stop
	}

	// Reasoning effort
	if raw.ReasoningEffort != nil && *raw.ReasoningEffort != "" {
		req.Params.Reasoning = &Reasoning{
			Effort: raw.ReasoningEffort,
		}
	}

	// Response format
	if len(raw.ResponseFormat) > 0 && string(raw.ResponseFormat) != "null" {
		req.Params.ResponseFormat = decodeOpenAIResponseFormat(raw.ResponseFormat)
	}

	// Tool choice
	if len(raw.ToolChoice) > 0 && string(raw.ToolChoice) != "null" {
		req.ToolChoice = decodeOpenAIToolChoice(raw.ToolChoice)
	}

	// Tools
	for _, t := range raw.Tools {
		if t.Type == "function" {
			req.Tools = append(req.Tools, Tool{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				InputSchema: t.Function.Parameters,
			})
		}
	}

	// Web search via features
	if raw.Features["web_search"] {
		req.Tools = append(req.Tools, Tool{Type: "web_search"})
	}

	// Messages
	for _, m := range raw.Messages {
		if m.Role == "system" {
			sysBlocks, err := decodeOpenAIContent(m.Content)
			if err != nil {
				return nil, fmt.Errorf("decode openai system message: %w", err)
			}
			req.System = append(req.System, sysBlocks...)
			continue
		}

		var blocks []ContentBlock

		// Thinking blocks
		if len(m.ThinkingBlocks) > 0 {
			for _, tb := range m.ThinkingBlocks {
				if tb.Type != "thinking" || tb.Thinking == nil || *tb.Thinking == "" {
					continue
				}
				blocks = append(blocks, ContentBlock{
					Type: "thinking",
					Thinking: &ThinkingBlock{
						Text:      tb.Thinking,
						Signature: tb.Signature,
					},
				})
			}
		} else if m.ReasoningContent != nil && *m.ReasoningContent != "" {
			blocks = append(blocks, ContentBlock{
				Type:     "thinking",
				Thinking: &ThinkingBlock{Text: m.ReasoningContent},
			})
		}

		// Content
		if len(m.Content) > 0 && string(m.Content) != "null" {
			contentBlocks, err := decodeOpenAIContent(m.Content)
			if err != nil {
				return nil, fmt.Errorf("decode openai message content: %w", err)
			}
			blocks = append(blocks, contentBlocks...)
		}

		// Tool calls on assistant messages
		for _, tc := range m.ToolCalls {
			var args any
			if tc.Function.Arguments != "" {
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			}
			blocks = append(blocks, ContentBlock{
				Type: "tool_call",
				ToolCall: &ToolCall{
					ID:        tc.ID,
					Name:      tc.Function.Name,
					Arguments: args,
				},
			})
		}

		// Tool role → tool_result
		if m.Role == "tool" && m.ToolCallID != "" {
			var innerBlocks []ContentBlock
			if len(m.Content) > 0 && string(m.Content) != "null" {
				var err error
				innerBlocks, err = decodeOpenAIContent(m.Content)
				if err != nil {
					return nil, fmt.Errorf("decode openai tool result content: %w", err)
				}
			}
			blocks = append(blocks, ContentBlock{
				Type: "tool_result",
				ToolResult: &ToolResult{
					ToolCallID: m.ToolCallID,
					Content:    innerBlocks,
				},
			})
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

func decodeOpenAIContent(raw json.RawMessage) ([]ContentBlock, error) {
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

	// Array of content parts.
	var parts []openAIInContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, err
	}

	var result []ContentBlock
	for _, p := range parts {
		switch p.Type {
		case "text":
			result = append(result, ContentBlock{Type: "text", Text: strPtr(p.Text)})
		case "image_url":
			if p.ImageURL != nil && p.ImageURL.URL != "" {
				img := parseDataURI(p.ImageURL.URL)
				if img != nil {
					result = append(result, ContentBlock{Type: "image", Image: img})
				}
			}
		}
	}
	return result, nil
}

func parseDataURI(url string) *Image {
	if !strings.HasPrefix(url, "data:") {
		return nil
	}
	rest := strings.TrimPrefix(url, "data:")
	semicolon := strings.Index(rest, ";")
	if semicolon < 0 {
		return nil
	}
	mediaType := rest[:semicolon]
	after := rest[semicolon+1:]
	if !strings.HasPrefix(after, "base64,") {
		return nil
	}
	data := strings.TrimPrefix(after, "base64,")
	return &Image{MediaType: mediaType, Data: data}
}

func decodeOpenAIStop(raw json.RawMessage) ([]string, error) {
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("decode openai stop string: %w", err)
		}
		return []string{s}, nil
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("decode openai stop array: %w", err)
	}
	return arr, nil
}

func decodeOpenAIToolChoice(raw json.RawMessage) *ToolChoice {
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil
		}
		switch s {
		case "auto":
			return &ToolChoice{Type: "auto"}
		case "required":
			return &ToolChoice{Type: "required"}
		case "none":
			return nil
		}
		return nil
	}
	var obj struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	if obj.Type == "function" && obj.Function.Name != "" {
		return &ToolChoice{Type: "tool", Name: obj.Function.Name}
	}
	return nil
}

func decodeOpenAIResponseFormat(raw json.RawMessage) *ResponseFormat {
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
		if jsonSchema, ok := m["json_schema"].(map[string]any); ok {
			if name, ok := jsonSchema["name"].(string); ok && name != "" {
				rf.Name = name
			}
			if strict, ok := jsonSchema["strict"].(bool); ok {
				rf.Strict = &strict
			}
			if schema, ok := jsonSchema["schema"]; ok {
				rf.Schema = schema
			}
		}
		return rf
	}
	return nil
}
