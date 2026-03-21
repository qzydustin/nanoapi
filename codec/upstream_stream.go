package codec

import (
	"encoding/json"
	"fmt"
	"strings"
)

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
	Role             string                `json:"role,omitempty"`
	Content          *string               `json:"content,omitempty"`
	ReasoningContent *string               `json:"reasoning_content,omitempty"`
	ThinkingBlocks   []openAIThinkingBlock `json:"thinking_blocks,omitempty"`
	ToolCalls        []openAIStreamTC      `json:"tool_calls,omitempty"`
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
func DecodeOpenAIStreamLine(line string) ([]StreamEvent, bool, error) {
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

	dataBytes := []byte(data)

	var chunk openAIStreamChunk
	if err := json.Unmarshal(dataBytes, &chunk); err != nil {
		// Non-OpenAI JSON (e.g. OpenWebUI sources); skip.
		if json.Valid(dataBytes) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("decode openai stream chunk: %w", err)
	}

	var events []StreamEvent

	if len(chunk.Choices) > 0 {
		c := chunk.Choices[0]
		d := c.Delta

		if d.ReasoningContent != nil && *d.ReasoningContent != "" {
			events = append(events, StreamEvent{
				Type:       EventThinkingDelta,
				Text:       *d.ReasoningContent,
				ResponseID: chunk.ID,
				Model:      chunk.Model,
			})
		}
		for _, tb := range d.ThinkingBlocks {
			if tb.Signature != nil && *tb.Signature != "" {
				events = append(events, StreamEvent{
					Type:       EventThinkingSignature,
					Signature:  *tb.Signature,
					ResponseID: chunk.ID,
					Model:      chunk.Model,
				})
			}
		}

		if d.Content != nil && *d.Content != "" {
			events = append(events, StreamEvent{
				Type:       EventTextDelta,
				Text:       *d.Content,
				ResponseID: chunk.ID,
				Model:      chunk.Model,
			})
		}

		for _, tc := range d.ToolCalls {
			if tc.ID != "" {
				events = append(events, StreamEvent{
					Type:         EventToolCallStart,
					ToolCallID:   tc.ID,
					ToolCallName: tc.Function.Name,
					ResponseID:   chunk.ID,
					Model:        chunk.Model,
				})
			}
			if tc.Function.Arguments != "" {
				events = append(events, StreamEvent{
					Type:           EventToolCallDelta,
					ArgumentsDelta: tc.Function.Arguments,
					ResponseID:     chunk.ID,
					Model:          chunk.Model,
				})
			}
		}

		if c.FinishReason != nil && *c.FinishReason != "" {
			events = append(events, StreamEvent{
				Type:       EventMessageStop,
				StopReason: normalizeOpenAIStopReason(*c.FinishReason),
				ResponseID: chunk.ID,
				Model:      chunk.Model,
			})
		}
	}

	// Usage (typically in the last chunk when stream_options.include_usage=true)
	if chunk.Usage != nil {
		usage := decodeOpenAIUsage(chunk.Usage)
		events = append(events, StreamEvent{
			Type:       EventUsageFinal,
			Usage:      usage,
			ResponseID: chunk.ID,
			Model:      chunk.Model,
		})
	}

	return events, false, nil
}
