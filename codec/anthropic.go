package codec

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"
)

type anthropicClientResp struct {
	ID           string                `json:"id"`
	Type         string                `json:"type"`
	Role         string                `json:"role"`
	Model        string                `json:"model"`
	Content      []any                 `json:"content"`
	StopReason   string                `json:"stop_reason"`
	StopSequence *string               `json:"stop_sequence"`
	Usage        *anthropicClientUsage `json:"usage,omitempty"`
}

type anthropicClientBlock struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Input     any    `json:"input,omitempty"`
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
}

type anthropicClientUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// EncodeAnthropicClientResponse encodes a Response as an Anthropic
// Messages JSON response for the client.
func EncodeAnthropicClientResponse(resp *Response) ([]byte, error) {
	out := anthropicClientResp{
		ID:           resp.ID,
		Type:         "message",
		Role:         "assistant",
		Model:        resp.Model,
		StopReason:   resp.StopReason,
		StopSequence: nil,
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
			case "web_search":
				toolUseID := b.WebSearchToolUseID
				out.Content = append(out.Content, map[string]any{
					"type":  "server_tool_use",
					"id":    toolUseID,
					"name":  "web_search",
					"input": map[string]any{},
				})
				content := make([]any, len(b.WebSearchResults))
				for i, r := range b.WebSearchResults {
					content[i] = map[string]any{
						"type":              "web_search_result",
						"url":               r.URL,
						"title":             r.Title,
						"encrypted_content": "",
						"page_age":          nil,
					}
				}
				out.Content = append(out.Content, map[string]any{
					"type":        "web_search_tool_result",
					"tool_use_id": toolUseID,
					"content":     content,
				})
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

// NewAnthropicMessageID returns a client-facing Anthropic-style message id.
func NewAnthropicMessageID() string {
	return "msg_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

// NormalizeAnthropicResponse rewrites client-facing metadata so the response
// looks like an Anthropic Messages response instead of leaking upstream ids.
func NormalizeAnthropicResponse(resp *Response, clientResponseID, clientModel string) {
	if resp == nil {
		return
	}
	if clientResponseID != "" {
		resp.ID = clientResponseID
	}
	if clientModel != "" {
		resp.Model = clientModel
	}
}

// NormalizeAnthropicStreamEvent rewrites client-facing metadata on stream
// events before encoding them as Anthropic SSE.
func NormalizeAnthropicStreamEvent(event StreamEvent, clientResponseID, clientModel string) StreamEvent {
	if clientResponseID != "" {
		event.ResponseID = clientResponseID
	}
	if clientModel != "" {
		event.Model = clientModel
	}
	return event
}

func strPtr(s string) *string { return &s }
