package codec

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/qzydustin/nanoapi/canonical"
)

// ---------------------------------------------------------------------------
// OpenAI Stream Chunk Decoding
// ---------------------------------------------------------------------------

type openAIStreamChunk struct {
	ID      string               `json:"id"`
	Model   string               `json:"model"`
	Choices []openAIStreamChoice `json:"choices"`
	Usage   *openAIUsage         `json:"usage,omitempty"`
}

type openAIStreamChoice struct {
	Delta        openAIStreamDelta `json:"delta"`
	FinishReason *string           `json:"finish_reason,omitempty"`
}

type openAIStreamDelta struct {
	Role             string           `json:"role,omitempty"`
	Content          *string          `json:"content,omitempty"`
	ReasoningContent *string          `json:"reasoning_content,omitempty"`
	ToolCalls        []openAIStreamTC `json:"tool_calls,omitempty"`
}

type openAIStreamTC struct {
	Index    int                `json:"index"`
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type,omitempty"`
	Function openAIStreamTCFunc `json:"function,omitempty"`
}

type openAIStreamTCFunc struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// DecodeOpenAIStreamLine parses a single SSE "data: ..." line from an OpenAI
// stream into zero or more canonical stream events.
// Returns (events, done, error) where done=true means "[DONE]".
func DecodeOpenAIStreamLine(line string) ([]canonical.CanonicalStreamEvent, bool, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, ":") {
		return nil, false, nil // keep-alive or comment
	}
	if !strings.HasPrefix(line, "data: ") {
		return nil, false, nil
	}
	data := strings.TrimPrefix(line, "data: ")
	data = strings.TrimSpace(data)

	if data == "[DONE]" {
		return nil, true, nil
	}

	var chunk openAIStreamChunk
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return nil, false, fmt.Errorf("decode openai stream chunk: %w", err)
	}

	var events []canonical.CanonicalStreamEvent

	// Metadata from first chunk.
	if chunk.ID != "" || chunk.Model != "" {
		// We embed metadata in every event; the consumer picks the first one.
		// This is simpler than a separate metadata event for V1.
	}

	if len(chunk.Choices) > 0 {
		c := chunk.Choices[0]
		d := c.Delta

		// Thinking/reasoning delta
		if d.ReasoningContent != nil && *d.ReasoningContent != "" {
			events = append(events, canonical.CanonicalStreamEvent{
				Type:       canonical.EventThinkingDelta,
				Text:       *d.ReasoningContent,
				ResponseID: chunk.ID,
				Model:      chunk.Model,
			})
		}

		// Text delta
		if d.Content != nil && *d.Content != "" {
			events = append(events, canonical.CanonicalStreamEvent{
				Type:       canonical.EventTextDelta,
				Text:       *d.Content,
				ResponseID: chunk.ID,
				Model:      chunk.Model,
			})
		}

		// Tool call deltas
		for _, tc := range d.ToolCalls {
			if tc.ID != "" {
				// tool_call_start
				events = append(events, canonical.CanonicalStreamEvent{
					Type:         canonical.EventToolCallStart,
					ToolCallID:   tc.ID,
					ToolCallName: tc.Function.Name,
					ResponseID:   chunk.ID,
					Model:        chunk.Model,
				})
			}
			if tc.Function.Arguments != "" {
				events = append(events, canonical.CanonicalStreamEvent{
					Type:           canonical.EventToolCallDelta,
					ArgumentsDelta: tc.Function.Arguments,
					ResponseID:     chunk.ID,
					Model:          chunk.Model,
				})
			}
		}

		// Finish reason
		if c.FinishReason != nil && *c.FinishReason != "" {
			events = append(events, canonical.CanonicalStreamEvent{
				Type:       canonical.EventMessageStop,
				StopReason: normalizeOpenAIStopReason(*c.FinishReason),
				ResponseID: chunk.ID,
				Model:      chunk.Model,
			})
		}
	}

	// Usage (typically in the last chunk when stream_options.include_usage=true)
	if chunk.Usage != nil {
		in64 := int64(chunk.Usage.PromptTokens)
		out64 := int64(chunk.Usage.CompletionTokens)
		total64 := int64(chunk.Usage.TotalTokens)
		usage := &canonical.CanonicalUsage{
			InputTokens:  &in64,
			OutputTokens: &out64,
			TotalTokens:  &total64,
		}
		if chunk.Usage.CompletionTokensDetails != nil && chunk.Usage.CompletionTokensDetails.ReasoningTokens != nil {
			reasoning64 := int64(*chunk.Usage.CompletionTokensDetails.ReasoningTokens)
			usage.ReasoningTokens = &reasoning64
		}
		if chunk.Usage.PromptTokensDetails != nil && chunk.Usage.PromptTokensDetails.CachedTokens != nil {
			cacheRead64 := int64(*chunk.Usage.PromptTokensDetails.CachedTokens)
			usage.CacheReadTokens = &cacheRead64
		}
		events = append(events, canonical.CanonicalStreamEvent{
			Type:       canonical.EventUsageFinal,
			Usage:      usage,
			ResponseID: chunk.ID,
			Model:      chunk.Model,
		})
	}

	return events, false, nil
}

// ---------------------------------------------------------------------------
// OpenAI Stream Encoding (canonical event → SSE line for client)
// ---------------------------------------------------------------------------

// openAIStreamState tracks tool call indexing for client-side SSE encoding.
type OpenAIStreamEncoder struct {
	chunkID     string
	model       string
	toolCallIdx int
}

// NewOpenAIStreamEncoder creates a new encoder for client-side OpenAI SSE.
func NewOpenAIStreamEncoder() *OpenAIStreamEncoder {
	return &OpenAIStreamEncoder{}
}

// Encode converts a canonical stream event into an OpenAI SSE line.
func (e *OpenAIStreamEncoder) Encode(event canonical.CanonicalStreamEvent) string {
	if event.ResponseID != "" {
		e.chunkID = event.ResponseID
	}
	if event.Model != "" {
		e.model = event.Model
	}

	switch event.Type {
	case canonical.EventTextDelta:
		chunk := map[string]any{
			"id": e.chunkID, "object": "chat.completion.chunk", "model": e.model,
			"choices": []map[string]any{{
				"index": 0,
				"delta": map[string]any{"content": event.Text},
			}},
		}
		return "data: " + mustJSON(chunk) + "\n\n"

	case canonical.EventThinkingDelta:
		chunk := map[string]any{
			"id": e.chunkID, "object": "chat.completion.chunk", "model": e.model,
			"choices": []map[string]any{{
				"index": 0,
				"delta": map[string]any{"reasoning_content": event.Text},
			}},
		}
		return "data: " + mustJSON(chunk) + "\n\n"

	case canonical.EventToolCallStart:
		tc := map[string]any{
			"index":    e.toolCallIdx,
			"id":       event.ToolCallID,
			"type":     "function",
			"function": map[string]any{"name": event.ToolCallName, "arguments": ""},
		}
		chunk := map[string]any{
			"id": e.chunkID, "object": "chat.completion.chunk", "model": e.model,
			"choices": []map[string]any{{
				"index": 0,
				"delta": map[string]any{"tool_calls": []map[string]any{tc}},
			}},
		}
		return "data: " + mustJSON(chunk) + "\n\n"

	case canonical.EventToolCallDelta:
		tc := map[string]any{
			"index":    e.toolCallIdx,
			"function": map[string]any{"arguments": event.ArgumentsDelta},
		}
		chunk := map[string]any{
			"id": e.chunkID, "object": "chat.completion.chunk", "model": e.model,
			"choices": []map[string]any{{
				"index": 0,
				"delta": map[string]any{"tool_calls": []map[string]any{tc}},
			}},
		}
		return "data: " + mustJSON(chunk) + "\n\n"

	case canonical.EventToolCallEnd:
		e.toolCallIdx++
		return ""

	case canonical.EventMessageStop:
		fr := denormalizeOpenAIStopReason(event.StopReason)
		chunk := map[string]any{
			"id": e.chunkID, "object": "chat.completion.chunk", "model": e.model,
			"choices": []map[string]any{{
				"index":         0,
				"delta":         map[string]any{},
				"finish_reason": fr,
			}},
		}
		return "data: " + mustJSON(chunk) + "\n\n"

	case canonical.EventUsageFinal:
		u := map[string]any{}
		if event.Usage != nil {
			if event.Usage.InputTokens != nil {
				u["prompt_tokens"] = *event.Usage.InputTokens
			}
			if event.Usage.OutputTokens != nil {
				u["completion_tokens"] = *event.Usage.OutputTokens
			}
			if event.Usage.TotalTokens != nil {
				u["total_tokens"] = *event.Usage.TotalTokens
			} else if event.Usage.InputTokens != nil && event.Usage.OutputTokens != nil {
				u["total_tokens"] = *event.Usage.InputTokens + *event.Usage.OutputTokens
			}
			if event.Usage.CacheReadTokens != nil {
				u["prompt_tokens_details"] = map[string]any{
					"cached_tokens": *event.Usage.CacheReadTokens,
				}
			}
			if event.Usage.ReasoningTokens != nil {
				u["completion_tokens_details"] = map[string]any{
					"reasoning_tokens": *event.Usage.ReasoningTokens,
				}
			}
		}
		chunk := map[string]any{
			"id": e.chunkID, "object": "chat.completion.chunk", "model": e.model,
			"choices": []any{},
			"usage":   u,
		}
		return "data: " + mustJSON(chunk) + "\n\n"
	}

	return ""
}

// Done returns the final [DONE] SSE line.
func (e *OpenAIStreamEncoder) Done() string {
	return "data: [DONE]\n\n"
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
