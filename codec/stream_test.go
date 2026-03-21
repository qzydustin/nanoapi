package codec

import (
	"strings"
	"testing"
)


func TestDecodeOpenAIStream_TextDelta(t *testing.T) {
	line := fixtureString(t, "openai_stream_text_delta.sse")
	events, done, err := DecodeOpenAIStreamLine(line)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if done {
		t.Error("should not be done")
	}
	if len(events) != 1 {
		t.Fatalf("events = %d", len(events))
	}
	if events[0].Type != EventTextDelta {
		t.Errorf("type = %q", events[0].Type)
	}
	if events[0].Text != "Hello" {
		t.Errorf("text = %q", events[0].Text)
	}
	if events[0].ResponseID != "chatcmpl-123" {
		t.Errorf("response_id = %q", events[0].ResponseID)
	}
}

func TestDecodeOpenAIStream_ToolCall(t *testing.T) {
	// Tool call start
	line1 := fixtureString(t, "openai_stream_tool_call_start.sse")
	events1, _, err := DecodeOpenAIStreamLine(line1)
	if err != nil {
		t.Fatalf("decode start: %v", err)
	}
	if len(events1) != 1 || events1[0].Type != EventToolCallStart {
		t.Errorf("expected tool_call_start, got %+v", events1)
	}

	// Tool call delta
	line2 := fixtureString(t, "openai_stream_tool_call_delta.sse")
	events2, _, err := DecodeOpenAIStreamLine(line2)
	if err != nil {
		t.Fatalf("decode delta: %v", err)
	}
	if len(events2) != 1 || events2[0].Type != EventToolCallDelta {
		t.Errorf("expected tool_call_delta, got %+v", events2)
	}
	if events2[0].ArgumentsDelta != `{"loc` {
		t.Errorf("args_delta = %q", events2[0].ArgumentsDelta)
	}
}

func TestDecodeOpenAIStream_FinishReason(t *testing.T) {
	line := fixtureString(t, "openai_stream_finish_reason.sse")
	events, _, err := DecodeOpenAIStreamLine(line)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, e := range events {
		if e.Type == EventMessageStop {
			found = true
			if e.StopReason != "end_turn" {
				t.Errorf("stop_reason = %q", e.StopReason)
			}
		}
	}
	if !found {
		t.Error("missing message_stop event")
	}
}

func TestDecodeOpenAIStream_UsageFinal(t *testing.T) {
	line := fixtureString(t, "openai_stream_usage_final.sse")
	events, _, err := DecodeOpenAIStreamLine(line)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, e := range events {
		if e.Type == EventUsageFinal {
			found = true
			if *e.Usage.InputTokens != 10 {
				t.Errorf("input_tokens = %d", *e.Usage.InputTokens)
			}
		}
	}
	if !found {
		t.Error("missing usage_final event")
	}
}

func TestDecodeOpenAIStream_UsageFinalWithDetailsObject(t *testing.T) {
	line := `data: {"id":"chatcmpl-123","model":"gpt-4o","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"completion_tokens_details":{"reasoning_tokens":3},"prompt_tokens_details":{"cached_tokens":2}}}`
	events, _, err := DecodeOpenAIStreamLine(line)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d", len(events))
	}
	if events[0].Type != EventUsageFinal {
		t.Fatalf("type = %q", events[0].Type)
	}
	if events[0].Usage == nil || events[0].Usage.ReasoningTokens == nil || *events[0].Usage.ReasoningTokens != 3 {
		t.Errorf("reasoning_tokens = %+v", events[0].Usage)
	}
	if events[0].Usage.CacheReadTokens == nil || *events[0].Usage.CacheReadTokens != 2 {
		t.Errorf("cache_read_tokens = %+v", events[0].Usage)
	}
}

func TestDecodeOpenAIStream_UsageFinalWithTopLevelCacheFallbacks(t *testing.T) {
	line := `data: {"id":"chatcmpl-123","model":"gpt-4o","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"cache_creation_input_tokens":7,"cache_read_input_tokens":9}}`
	events, _, err := DecodeOpenAIStreamLine(line)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d", len(events))
	}
	if events[0].Type != EventUsageFinal {
		t.Fatalf("type = %q", events[0].Type)
	}
	if events[0].Usage == nil || events[0].Usage.CacheWriteTokens == nil || *events[0].Usage.CacheWriteTokens != 7 {
		t.Fatalf("cache_write_tokens = %+v", events[0].Usage)
	}
	if events[0].Usage.CacheReadTokens == nil || *events[0].Usage.CacheReadTokens != 9 {
		t.Fatalf("cache_read_tokens = %+v", events[0].Usage)
	}
}

func TestDecodeOpenAIStream_Done(t *testing.T) {
	_, done, err := DecodeOpenAIStreamLine("data: [DONE]")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !done {
		t.Error("should be done")
	}
}

func TestDecodeOpenAIStream_KeepAlive(t *testing.T) {
	events, done, err := DecodeOpenAIStreamLine(": keep-alive")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if done {
		t.Error("should not be done")
	}
	if len(events) != 0 {
		t.Errorf("events = %d", len(events))
	}
}


func TestAnthropicStreamEncoder_TextDelta(t *testing.T) {
	enc := NewAnthropicStreamEncoder()
	line := enc.Encode(StreamEvent{
		Type: EventTextDelta, Text: "Hello",
		ResponseID: "msg_123", Model: "claude-3",
	})
	if !strings.Contains(line, "event: message_start") {
		t.Error("should contain message_start")
	}
	if !strings.Contains(line, "event: content_block_start") {
		t.Error("should contain content_block_start")
	}
	if !strings.Contains(line, `"text":"Hello"`) {
		t.Error("should contain text delta")
	}
}

func TestAnthropicStreamEncoder_Done(t *testing.T) {
	enc := NewAnthropicStreamEncoder()
	line := enc.Done()
	if !strings.Contains(line, "event: message_stop") {
		t.Errorf("done = %q", line)
	}
}

func TestAnthropicStreamEncoder_UsageFinal(t *testing.T) {
	enc := NewAnthropicStreamEncoder()
	in64 := int64(10)
	out64 := int64(5)
	cacheCreate64 := int64(2)
	cacheRead64 := int64(3)
	line := enc.Encode(StreamEvent{
		Type: EventUsageFinal,
		Usage: &Usage{
			InputTokens:      &in64,
			OutputTokens:     &out64,
			CacheWriteTokens: &cacheCreate64,
			CacheReadTokens:  &cacheRead64,
		},
	})
	if line != "" {
		t.Errorf("usage-only event should wait for stop reason, got %q", line)
	}
	line = enc.Encode(StreamEvent{
		Type: EventMessageStop, StopReason: "end_turn",
	})
	if !strings.Contains(line, `"input_tokens":10`) {
		t.Errorf("line = %q", line)
	}
	if !strings.Contains(line, `"cache_creation_input_tokens":2`) {
		t.Errorf("line = %q", line)
	}
	if !strings.Contains(line, `"cache_read_input_tokens":3`) {
		t.Errorf("line = %q", line)
	}
	if !strings.Contains(line, `"stop_reason":"end_turn"`) {
		t.Errorf("line = %q", line)
	}
}

func TestAnthropicStreamEncoder_MessageStartUsesKnownUsage(t *testing.T) {
	enc := NewAnthropicStreamEncoder()
	in64 := int64(10)
	out64 := int64(5)
	cacheCreate64 := int64(2)
	cacheRead64 := int64(3)

	line := enc.Encode(StreamEvent{
		Type:       EventUsageFinal,
		ResponseID: "msg_123",
		Model:      "claude-3",
		Usage: &Usage{
			InputTokens:      &in64,
			OutputTokens:     &out64,
			CacheWriteTokens: &cacheCreate64,
			CacheReadTokens:  &cacheRead64,
		},
	})

	if !strings.Contains(line, "event: message_start") {
		t.Fatalf("should contain message_start, got %q", line)
	}
	if !strings.Contains(line, `"input_tokens":10`) {
		t.Fatalf("line = %q", line)
	}
	if !strings.Contains(line, `"output_tokens":5`) {
		t.Fatalf("line = %q", line)
	}
	if !strings.Contains(line, `"cache_creation_input_tokens":2`) {
		t.Fatalf("line = %q", line)
	}
	if !strings.Contains(line, `"cache_read_input_tokens":3`) {
		t.Fatalf("line = %q", line)
	}
}

func TestAnthropicStreamEncoder_WebSearchBlocks(t *testing.T) {
	enc := NewAnthropicStreamEncoder()

	// Emit message_start first using metadata-only text event.
	_ = enc.Encode(StreamEvent{
		Type: EventTextDelta, ResponseID: "msg_123", Model: "claude-opus-4-6",
	})

	var out strings.Builder
	ev := SynthesizeWebSearchSSE([]WebSearchResult{
		{URL: "https://example.com", Title: "Example"},
	})
	out.WriteString(enc.Encode(ev))

	s := out.String()
	if !strings.Contains(s, `"type":"server_tool_use"`) {
		t.Fatalf("missing server_tool_use block: %q", s)
	}
	if !strings.Contains(s, `"type":"web_search_tool_result"`) {
		t.Fatalf("missing web_search_tool_result block: %q", s)
	}
	if !strings.Contains(s, `"url":"https://example.com"`) {
		t.Fatalf("missing search result url: %q", s)
	}
}
