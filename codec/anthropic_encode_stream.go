package codec

import (
	"encoding/json"
	"strings"
)

// AnthropicStreamEncoder tracks state for client-side Anthropic SSE encoding.
type AnthropicStreamEncoder struct {
	messageStarted      bool
	blockIdx            int
	blockOpen           bool
	blockType           string
	stopReason          string
	usage               *Usage
	messageDeltaEmitted bool
}

// NewAnthropicStreamEncoder creates a new encoder for client-side Anthropic SSE.
func NewAnthropicStreamEncoder() *AnthropicStreamEncoder {
	return &AnthropicStreamEncoder{}
}

// Encode converts a canonical stream event into Anthropic SSE line(s).
func (e *AnthropicStreamEncoder) Encode(event StreamEvent) string {
	var lines []string

	if event.Type == EventUsageFinal && event.Usage != nil {
		e.usage = event.Usage
	}

	// Emit message_start on the first real event.
	if !e.messageStarted && (event.ResponseID != "" || event.Model != "") {
		msgStart := map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":            event.ResponseID,
				"type":          "message",
				"role":          "assistant",
				"model":         event.Model,
				"content":       []any{},
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage":         e.anthropicUsagePayload(),
			},
		}
		lines = append(lines, anthropicSSE("message_start", msgStart))
		e.messageStarted = true
	}

	switch event.Type {
	case EventTextDelta:
		if event.Text == "" {
			break // skip empty metadata-only events
		}
		e.ensureBlockStart(&lines, "text")
		delta := map[string]any{
			"type":  "content_block_delta",
			"index": e.blockIdx,
			"delta": map[string]any{"type": "text_delta", "text": event.Text},
		}
		lines = append(lines, anthropicSSE("content_block_delta", delta))

	case EventThinkingDelta:
		e.ensureBlockStart(&lines, "thinking")
		delta := map[string]any{
			"type":  "content_block_delta",
			"index": e.blockIdx,
			"delta": map[string]any{"type": "thinking_delta", "thinking": event.Text},
		}
		lines = append(lines, anthropicSSE("content_block_delta", delta))

	case EventThinkingSignature:
		e.ensureBlockStart(&lines, "thinking")
		if event.Signature == "" {
			break
		}
		delta := map[string]any{
			"type":  "content_block_delta",
			"index": e.blockIdx,
			"delta": map[string]any{"type": "signature_delta", "signature": event.Signature},
		}
		lines = append(lines, anthropicSSE("content_block_delta", delta))

	case EventToolCallStart:
		e.closeBlock(&lines)
		start := map[string]any{
			"type":  "content_block_start",
			"index": e.blockIdx,
			"content_block": map[string]any{
				"type": "tool_use", "id": event.ToolCallID, "name": event.ToolCallName,
				"input": map[string]any{},
			},
		}
		lines = append(lines, anthropicSSE("content_block_start", start))
		e.blockOpen = true

	case EventToolCallDelta:
		delta := map[string]any{
			"type":  "content_block_delta",
			"index": e.blockIdx,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": event.ArgumentsDelta},
		}
		lines = append(lines, anthropicSSE("content_block_delta", delta))

	case EventToolCallEnd:
		e.closeBlock(&lines)

	case EventWebSearch:
		e.closeBlock(&lines)
		toolUseID := event.WebSearchToolUseID
		lines = append(lines, e.emitStandaloneBlock(map[string]any{
			"type":  "server_tool_use",
			"id":    toolUseID,
			"name":  "web_search",
			"input": map[string]any{},
		}))
		content := make([]any, len(event.WebSearchResults))
		for i, r := range event.WebSearchResults {
			content[i] = map[string]any{
				"type":              "web_search_result",
				"url":               r.URL,
				"title":             r.Title,
				"encrypted_content": "",
				"page_age":          nil,
			}
		}
		lines = append(lines, e.emitStandaloneBlock(map[string]any{
			"type":        "web_search_tool_result",
			"tool_use_id": toolUseID,
			"content":     content,
		}))

	case EventMessageStop:
		e.closeBlock(&lines)
		e.stopReason = event.StopReason
		if e.usage != nil && !e.messageDeltaEmitted {
			lines = append(lines, e.emitMessageDelta())
		}

	case EventUsageFinal:
		if e.stopReason != "" && !e.messageDeltaEmitted {
			lines = append(lines, e.emitMessageDelta())
		}
	}

	return strings.Join(lines, "")
}

// Done returns the final Anthropic SSE termination events.
func (e *AnthropicStreamEncoder) Done() string {
	var lines []string
	e.closeBlock(&lines)
	if !e.messageDeltaEmitted && (e.stopReason != "" || e.usage != nil) {
		lines = append(lines, e.emitMessageDelta())
	}
	lines = append(lines, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	return strings.Join(lines, "")
}

func (e *AnthropicStreamEncoder) ensureBlockStart(lines *[]string, blockType string) {
	if e.blockOpen {
		if e.blockType == blockType {
			return
		}
		e.closeBlock(lines)
	}
	var contentBlock map[string]any
	if blockType == "text" {
		contentBlock = map[string]any{"type": "text", "text": ""}
	} else {
		contentBlock = map[string]any{"type": "thinking", "thinking": ""}
	}
	start := map[string]any{
		"type":          "content_block_start",
		"index":         e.blockIdx,
		"content_block": contentBlock,
	}
	*lines = append(*lines, anthropicSSE("content_block_start", start))
	e.blockOpen = true
	e.blockType = blockType
}

func (e *AnthropicStreamEncoder) closeBlock(lines *[]string) {
	if !e.blockOpen {
		return
	}
	stop := map[string]any{"type": "content_block_stop", "index": e.blockIdx}
	*lines = append(*lines, anthropicSSE("content_block_stop", stop))
	e.blockOpen = false
	e.blockType = ""
	e.blockIdx++
}

func (e *AnthropicStreamEncoder) emitMessageDelta() string {
	stopReason := e.stopReason
	if stopReason == "" {
		stopReason = "end_turn"
	}
	msg := map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": e.anthropicUsagePayload(),
	}
	e.messageDeltaEmitted = true
	return anthropicSSE("message_delta", msg)
}

func (e *AnthropicStreamEncoder) anthropicUsagePayload() map[string]any {
	u := map[string]any{
		"input_tokens":  0,
		"output_tokens": 0,
	}
	if e.usage == nil {
		return u
	}
	if e.usage.InputTokens != nil {
		u["input_tokens"] = *e.usage.InputTokens
	}
	if e.usage.OutputTokens != nil {
		u["output_tokens"] = *e.usage.OutputTokens
	}
	if e.usage.CacheWriteTokens != nil {
		u["cache_creation_input_tokens"] = *e.usage.CacheWriteTokens
	}
	if e.usage.CacheReadTokens != nil {
		u["cache_read_input_tokens"] = *e.usage.CacheReadTokens
	}
	return u
}

func anthropicSSE(event string, payload any) string {
	b, _ := json.Marshal(payload)
	return "event: " + event + "\ndata: " + string(b) + "\n\n"
}

func (e *AnthropicStreamEncoder) emitStandaloneBlock(contentBlock map[string]any) string {
	var lines []string
	e.closeBlock(&lines)
	start := map[string]any{
		"type":          "content_block_start",
		"index":         e.blockIdx,
		"content_block": contentBlock,
	}
	stop := map[string]any{
		"type":  "content_block_stop",
		"index": e.blockIdx,
	}
	lines = append(lines, anthropicSSE("content_block_start", start))
	lines = append(lines, anthropicSSE("content_block_stop", stop))
	e.blockOpen = false
	e.blockType = ""
	e.blockIdx++
	return strings.Join(lines, "")
}
