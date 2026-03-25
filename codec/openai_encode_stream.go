package codec

import (
	"encoding/json"
	"strings"
	"time"
)

type OpenAIStreamEncoder struct {
	responseID string
	model      string
	created    int64
	started    bool
	usage      *Usage
}

func NewOpenAIStreamEncoder() *OpenAIStreamEncoder {
	return &OpenAIStreamEncoder{
		created: time.Now().Unix(),
	}
}

func (e *OpenAIStreamEncoder) Encode(event StreamEvent) string {
	if event.ResponseID != "" {
		e.responseID = event.ResponseID
	}
	if event.Model != "" {
		e.model = event.Model
	}

	if event.Type == EventUsageFinal && event.Usage != nil {
		e.usage = event.Usage
		return e.encodeUsageChunk()
	}

	var lines []string

	switch event.Type {
	case EventTextDelta:
		if event.Text == "" {
			break
		}
		delta := map[string]any{"content": event.Text}
		if !e.started {
			delta["role"] = "assistant"
			e.started = true
		}
		lines = append(lines, e.chunk(delta, nil))

	case EventThinkingDelta:
		if event.Text == "" {
			break
		}
		delta := map[string]any{"reasoning_content": event.Text}
		if !e.started {
			delta["role"] = "assistant"
			e.started = true
		}
		lines = append(lines, e.chunk(delta, nil))

	case EventToolCallStart:
		if !e.started {
			lines = append(lines, e.chunk(map[string]any{"role": "assistant"}, nil))
			e.started = true
		}
		tc := map[string]any{
			"index": event.ToolCallIndex,
			"id":    event.ToolCallID,
			"type":  "function",
			"function": map[string]any{
				"name":      event.ToolCallName,
				"arguments": "",
			},
		}
		lines = append(lines, e.chunk(map[string]any{"tool_calls": []any{tc}}, nil))

	case EventToolCallDelta:
		if event.ArgumentsDelta == "" {
			break
		}
		tc := map[string]any{
			"index":    event.ToolCallIndex,
			"function": map[string]any{"arguments": event.ArgumentsDelta},
		}
		lines = append(lines, e.chunk(map[string]any{"tool_calls": []any{tc}}, nil))

	case EventToolCallEnd:
	case EventMessageStop:
		finishReason := denormalizeOpenAIStopReason(event.StopReason)
		lines = append(lines, e.chunk(map[string]any{}, &finishReason))
	}

	return strings.Join(lines, "")
}

func (e *OpenAIStreamEncoder) Done() string {
	return "data: [DONE]\n\n"
}

func (e *OpenAIStreamEncoder) chunk(delta map[string]any, finishReason *string) string {
	payload := map[string]any{
		"id":      e.responseID,
		"object":  "chat.completion.chunk",
		"created": e.created,
		"model":   e.model,
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         delta,
			"finish_reason": finishReason,
		}},
	}
	b, _ := json.Marshal(payload)
	return "data: " + string(b) + "\n\n"
}

func (e *OpenAIStreamEncoder) encodeUsageChunk() string {
	payload := map[string]any{
		"id":      e.responseID,
		"object":  "chat.completion.chunk",
		"created": e.created,
		"model":   e.model,
		"choices": []any{},
		"usage":   encodeOpenAIUsage(e.usage),
	}
	b, _ := json.Marshal(payload)
	return "data: " + string(b) + "\n\n"
}
