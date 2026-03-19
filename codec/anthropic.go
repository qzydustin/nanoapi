package codec

import (
	"encoding/json"
	"fmt"

	"github.com/qzydustin/nanoapi/canonical"
	"github.com/qzydustin/nanoapi/config"
)

// ---------------------------------------------------------------------------
// Anthropic Request Encoding (canonical → Anthropic JSON)
// ---------------------------------------------------------------------------

type anthropicOutRequest struct {
	Model         string                `json:"model"`
	System        []anthropicOutSys     `json:"system,omitempty"`
	Messages      []anthropicOutMsg     `json:"messages"`
	Stream        bool                  `json:"stream,omitempty"`
	MaxTokens     int                   `json:"max_tokens"`
	Temperature   *float64              `json:"temperature,omitempty"`
	TopP          *float64              `json:"top_p,omitempty"`
	StopSequences []string              `json:"stop_sequences,omitempty"`
	Tools         []anthropicOutTool    `json:"tools,omitempty"`
	Thinking      *anthropicOutThinking `json:"thinking,omitempty"`
	OutputConfig  *anthropicOutConfig   `json:"output_config,omitempty"`
}

type anthropicOutConfig struct {
	Effort *string `json:"effort,omitempty"`
}

type anthropicOutSys struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicOutMsg struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type anthropicOutBlock struct {
	Type string `json:"type"`

	// text
	Text string `json:"text,omitempty"`

	// image
	Source *anthropicOutImgSrc `json:"source,omitempty"`

	// tool_use
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`

	// tool_result
	ToolUseID     string `json:"tool_use_id,omitempty"`
	ResultContent any    `json:"content,omitempty"`

	// thinking
	Thinking string `json:"thinking,omitempty"`
}

type anthropicOutImgSrc struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

type anthropicOutTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema"`
}

type anthropicOutThinking struct {
	Type         string `json:"type"`
	BudgetTokens *int   `json:"budget_tokens,omitempty"`
}


const defaultAnthropicMaxTokens = 4096

// EncodeAnthropicRequest converts a CanonicalRequest into an Anthropic
// Messages JSON request body.
func EncodeAnthropicRequest(req *canonical.CanonicalRequest, upstreamModel string, stream bool, capability *config.ReasoningCapability) ([]byte, error) {
	maxTokens := defaultAnthropicMaxTokens
	if req.Params.MaxTokens != nil {
		maxTokens = *req.Params.MaxTokens
	}

	out := anthropicOutRequest{
		Model:         upstreamModel,
		Stream:        stream,
		MaxTokens:     maxTokens,
		Temperature:   req.Params.Temperature,
		TopP:          req.Params.TopP,
		StopSequences: req.Params.Stop,
	}

	// Reasoning / thinking
	if r := req.Params.Reasoning; r != nil {
		out.Thinking, out.OutputConfig = buildAnthropicReasoning(r, capability)
		if anthropicThinkingAllowsBudget(out.Thinking) {
			// Anthropic requires max_tokens to be at least budget_tokens + some margin
			if out.Thinking.BudgetTokens != nil && maxTokens <= *out.Thinking.BudgetTokens {
				out.MaxTokens = *out.Thinking.BudgetTokens + defaultAnthropicMaxTokens
			}
		}
	}

	// System
	for _, sys := range req.System {
		if sys.Type == "text" && sys.Text != nil {
			out.System = append(out.System, anthropicOutSys{Type: "text", Text: *sys.Text})
		}
	}

	// Tools
	for _, t := range req.Tools {
		schema := t.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		out.Tools = append(out.Tools, anthropicOutTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		})
	}

	// Messages
	for _, m := range req.Messages {
		encoded := encodeAnthropicMessage(m)
		out.Messages = append(out.Messages, encoded)
	}

	return json.Marshal(out)
}

func buildAnthropicReasoning(
	r *canonical.CanonicalReasoning,
	capability *config.ReasoningCapability,
) (*anthropicOutThinking, *anthropicOutConfig) {
	if r == nil {
		return nil, nil
	}

	var thinking *anthropicOutThinking
	var outputConfig *anthropicOutConfig
	if r.Effort != nil {
		switch normalizeEffortValue(*r.Effort) {
		case "none":
			thinking = &anthropicOutThinking{Type: "disabled"}
		case "auto":
			thinking = &anthropicOutThinking{Type: "adaptive"}
		default:
			thinking = &anthropicOutThinking{Type: "adaptive"}
			if mappedEffort, ok := mapAnthropicEffort(*r.Effort, capability); ok {
				outputConfig = &anthropicOutConfig{Effort: &mappedEffort}
			}
		}
		if r.BudgetTokens != nil && anthropicThinkingAllowsBudget(thinking) {
			thinking.BudgetTokens = r.BudgetTokens
		}
	}

	thinkingType := r.Mode
	if thinkingType == "" && r.Effort != nil && thinking != nil {
		thinkingType = thinking.Type
	}
	if thinking == nil && thinkingType != "" {
		thinking = &anthropicOutThinking{Type: thinkingType}
		if r.BudgetTokens != nil {
			thinking.BudgetTokens = r.BudgetTokens
		}
	}

	return thinking, outputConfig
}


func anthropicThinkingAllowsBudget(thinking *anthropicOutThinking) bool {
	return thinking != nil && thinking.Type != "disabled"
}

func encodeAnthropicMessage(m canonical.CanonicalMessage) anthropicOutMsg {
	var blocks []anthropicOutBlock

	for _, b := range m.Content {
		switch b.Type {
		case "text":
			if b.Text != nil {
				blocks = append(blocks, anthropicOutBlock{Type: "text", Text: *b.Text})
			}
		case "image":
			if b.Image != nil {
				blocks = append(blocks, anthropicOutBlock{
					Type:   "image",
					Source: encodeAnthropicImage(b.Image),
				})
			}
		case "tool_call":
			if b.ToolCall != nil {
				blocks = append(blocks, anthropicOutBlock{
					Type:  "tool_use",
					ID:    b.ToolCall.ID,
					Name:  b.ToolCall.Name,
					Input: b.ToolCall.Arguments,
				})
			}
		case "tool_result":
			if b.ToolResult != nil {
				blocks = append(blocks, anthropicOutBlock{
					Type:          "tool_result",
					ToolUseID:     b.ToolResult.ToolCallID,
					ResultContent: encodeAnthropicToolResultContent(b.ToolResult.Content),
				})
			}
		case "thinking":
			if b.Thinking != nil && b.Thinking.Text != nil {
				blocks = append(blocks, anthropicOutBlock{Type: "thinking", Thinking: *b.Thinking.Text})
			}
		}
	}

	// Use role mapping for tool messages.
	role := m.Role
	if role == "tool" {
		role = "user"
	}

	if len(blocks) == 1 && blocks[0].Type == "text" {
		return anthropicOutMsg{Role: role, Content: blocks[0].Text}
	}
	return anthropicOutMsg{Role: role, Content: blocks}
}

func encodeAnthropicToolResultContent(blocks []canonical.CanonicalContentBlock) any {
	if len(blocks) == 1 && blocks[0].Type == "text" && blocks[0].Text != nil {
		return *blocks[0].Text
	}

	var innerBlocks []anthropicOutBlock
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != nil {
				innerBlocks = append(innerBlocks, anthropicOutBlock{
					Type: "text",
					Text: *b.Text,
				})
			}
		case "image":
			if b.Image != nil {
				innerBlocks = append(innerBlocks, anthropicOutBlock{
					Type:   "image",
					Source: encodeAnthropicImage(b.Image),
				})
			}
		}
	}

	if len(innerBlocks) == 0 {
		return nil
	}
	return innerBlocks
}

func encodeAnthropicImage(img *canonical.CanonicalImage) *anthropicOutImgSrc {
	switch img.SourceType {
	case "data_url", "base64":
		src := &anthropicOutImgSrc{Type: "base64"}
		if img.MediaType != nil {
			src.MediaType = *img.MediaType
		}
		if img.Data != nil {
			src.Data = *img.Data
		}
		return src
	case "url":
		src := &anthropicOutImgSrc{Type: "url"}
		if img.URL != nil {
			src.URL = *img.URL
		}
		return src
	}
	return nil
}

// ---------------------------------------------------------------------------
// Anthropic Response Decoding (upstream JSON → CanonicalResponse)
// ---------------------------------------------------------------------------

type anthropicResponse struct {
	ID         string               `json:"id"`
	Type       string               `json:"type"`
	Model      string               `json:"model"`
	Role       string               `json:"role"`
	Content    []anthropicRespBlock `json:"content"`
	StopReason *string              `json:"stop_reason,omitempty"`
	Usage      *anthropicUsage      `json:"usage,omitempty"`
}

type anthropicRespBlock struct {
	Type string `json:"type"`

	// text
	Text string `json:"text,omitempty"`

	// tool_use
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`

	// thinking
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int  `json:"input_tokens"`
	OutputTokens int  `json:"output_tokens"`
	CacheRead    *int `json:"cache_read_input_tokens,omitempty"`
	CacheCreate  *int `json:"cache_creation_input_tokens,omitempty"`
}

// DecodeAnthropicResponse parses an Anthropic Messages response into a
// CanonicalResponse.
func DecodeAnthropicResponse(body []byte) (*canonical.CanonicalResponse, error) {
	var raw anthropicResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode anthropic response: %w", err)
	}

	resp := &canonical.CanonicalResponse{
		UpstreamProtocol: canonical.ProtocolAnthropicMessage,
		Model:            raw.Model,
		ID:               raw.ID,
	}

	var blocks []canonical.CanonicalContentBlock
	for _, b := range raw.Content {
		switch b.Type {
		case "text":
			blocks = append(blocks, canonical.CanonicalContentBlock{
				Type: "text", Text: strPtr(b.Text),
			})
		case "tool_use":
			blocks = append(blocks, canonical.CanonicalContentBlock{
				Type: "tool_call",
				ToolCall: &canonical.CanonicalToolCall{
					ID:        b.ID,
					Name:      b.Name,
					Arguments: b.Input,
				},
			})
		case "thinking":
			blocks = append(blocks, canonical.CanonicalContentBlock{
				Type:     "thinking",
				Thinking: &canonical.CanonicalThinkingBlock{Text: strPtr(b.Thinking), Signature: strPtr(b.Signature)},
			})
		}
	}

	resp.Output = []canonical.CanonicalMessage{
		{Role: raw.Role, Content: blocks},
	}

	if raw.StopReason != nil {
		resp.StopReason = *raw.StopReason
	}

	if raw.Usage != nil {
		in64 := int64(raw.Usage.InputTokens)
		out64 := int64(raw.Usage.OutputTokens)
		total64 := in64 + out64
		resp.Usage = &canonical.CanonicalUsage{
			InputTokens:  &in64,
			OutputTokens: &out64,
			TotalTokens:  &total64,
		}
		if raw.Usage.CacheRead != nil {
			v := int64(*raw.Usage.CacheRead)
			resp.Usage.CacheReadTokens = &v
		}
		if raw.Usage.CacheCreate != nil {
			v := int64(*raw.Usage.CacheCreate)
			resp.Usage.CacheWriteTokens = &v
		}
	}

	return resp, nil
}


// ---------------------------------------------------------------------------
// Anthropic Client Response Encoding (CanonicalResponse → Anthropic JSON)
// ---------------------------------------------------------------------------

type anthropicClientResp struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	Role       string                 `json:"role"`
	Model      string                 `json:"model"`
	Content    []anthropicClientBlock `json:"content"`
	StopReason string                 `json:"stop_reason"`
	Usage      *anthropicClientUsage  `json:"usage,omitempty"`
}

type anthropicClientBlock struct {
	Type string `json:"type"`

	// text
	Text string `json:"text,omitempty"`

	// tool_use
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`

	// thinking
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
}

type anthropicClientUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// EncodeAnthropicClientResponse encodes a CanonicalResponse as an Anthropic
// Messages JSON response for the client.
func EncodeAnthropicClientResponse(resp *canonical.CanonicalResponse) ([]byte, error) {
	out := anthropicClientResp{
		ID:         resp.ID,
		Type:       "message",
		Role:       "assistant",
		Model:      resp.Model,
		StopReason: resp.StopReason,
	}

	if len(resp.Output) > 0 {
		for _, b := range resp.Output[0].Content {
			switch b.Type {
			case "text":
				if b.Text != nil {
					out.Content = append(out.Content, anthropicClientBlock{
						Type: "text", Text: *b.Text,
					})
				}
			case "tool_call":
				if b.ToolCall != nil {
					out.Content = append(out.Content, anthropicClientBlock{
						Type:  "tool_use",
						ID:    b.ToolCall.ID,
						Name:  b.ToolCall.Name,
						Input: b.ToolCall.Arguments,
					})
				}
			case "thinking":
				if b.Thinking != nil && b.Thinking.Text != nil {
					block := anthropicClientBlock{
						Type:     "thinking",
						Thinking: *b.Thinking.Text,
					}
					if b.Thinking.Signature != nil {
						block.Signature = *b.Thinking.Signature
					}
					out.Content = append(out.Content, block)
				}
			}
		}
	}

	if resp.Usage != nil {
		u := &anthropicClientUsage{}
		if resp.Usage.InputTokens != nil {
			u.InputTokens = int(*resp.Usage.InputTokens)
		}
		if resp.Usage.OutputTokens != nil {
			u.OutputTokens = int(*resp.Usage.OutputTokens)
		}
		if resp.Usage.CacheWriteTokens != nil {
			u.CacheCreationInputTokens = int(*resp.Usage.CacheWriteTokens)
		}
		if resp.Usage.CacheReadTokens != nil {
			u.CacheReadInputTokens = int(*resp.Usage.CacheReadTokens)
		}
		out.Usage = u
	}

	return json.Marshal(out)
}


// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func strPtr(s string) *string { return &s }
