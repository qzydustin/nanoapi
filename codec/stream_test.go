package codec

import (
	"strings"
	"testing"

	"github.com/qzydustin/nanoapi/canonical"
)

// ---------------------------------------------------------------------------
// OpenAI Stream Decoding
// ---------------------------------------------------------------------------

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
	if events[0].Type != canonical.EventTextDelta {
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
	if len(events1) != 1 || events1[0].Type != canonical.EventToolCallStart {
		t.Errorf("expected tool_call_start, got %+v", events1)
	}

	// Tool call delta
	line2 := fixtureString(t, "openai_stream_tool_call_delta.sse")
	events2, _, err := DecodeOpenAIStreamLine(line2)
	if err != nil {
		t.Fatalf("decode delta: %v", err)
	}
	if len(events2) != 1 || events2[0].Type != canonical.EventToolCallDelta {
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
		if e.Type == canonical.EventMessageStop {
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
		if e.Type == canonical.EventUsageFinal {
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
	if events[0].Type != canonical.EventUsageFinal {
		t.Fatalf("type = %q", events[0].Type)
	}
	if events[0].Usage == nil || events[0].Usage.ReasoningTokens == nil || *events[0].Usage.ReasoningTokens != 3 {
		t.Errorf("reasoning_tokens = %+v", events[0].Usage)
	}
	if events[0].Usage.CacheReadTokens == nil || *events[0].Usage.CacheReadTokens != 2 {
		t.Errorf("cache_read_tokens = %+v", events[0].Usage)
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

// ---------------------------------------------------------------------------
// OpenAI Stream Encoding
// ---------------------------------------------------------------------------

func TestOpenAIStreamEncoder_TextDelta(t *testing.T) {
	enc := NewOpenAIStreamEncoder()
	line := enc.Encode(canonical.CanonicalStreamEvent{
		Type: canonical.EventTextDelta, Text: "Hello",
		ResponseID: "chatcmpl-123", Model: "gpt-4o",
	})
	if !strings.Contains(line, `"content":"Hello"`) {
		t.Errorf("line = %q", line)
	}
	if !strings.HasPrefix(line, "data: ") {
		t.Errorf("should start with 'data: '")
	}
}

func TestOpenAIStreamEncoder_ThinkingDelta(t *testing.T) {
	enc := NewOpenAIStreamEncoder()
	line := enc.Encode(canonical.CanonicalStreamEvent{
		Type:       canonical.EventThinkingDelta,
		Text:       "Let me think",
		ResponseID: "chatcmpl-123",
		Model:      "gpt-4o",
	})
	if !strings.Contains(line, `"reasoning_content":"Let me think"`) {
		t.Fatalf("line = %q", line)
	}
}

func TestOpenAIStreamEncoder_MessageStop(t *testing.T) {
	enc := NewOpenAIStreamEncoder()
	line := enc.Encode(canonical.CanonicalStreamEvent{
		Type:       canonical.EventMessageStop,
		StopReason: "end_turn",
		ResponseID: "chatcmpl-123",
		Model:      "gpt-4o",
	})
	if !strings.Contains(line, `"finish_reason":"stop"`) {
		t.Fatalf("line = %q", line)
	}
}

func TestOpenAIStreamEncoder_Done(t *testing.T) {
	enc := NewOpenAIStreamEncoder()
	if enc.Done() != "data: [DONE]\n\n" {
		t.Errorf("done = %q", enc.Done())
	}
}

// ---------------------------------------------------------------------------
// Anthropic Stream Decoding
// ---------------------------------------------------------------------------

func TestAnthropicStreamDecoder_MessageStart(t *testing.T) {
	dec := NewAnthropicStreamDecoder()
	pairs := fixtureSSEPairs(t, "anthropic_stream_message_start.sse")
	events, err := dec.DecodeLine(pairs[0].Event, pairs[0].Data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d", len(events))
	}
	if events[0].ResponseID != "msg_123" {
		t.Errorf("response_id = %q", events[0].ResponseID)
	}
}

func TestAnthropicStreamDecoder_TextDelta(t *testing.T) {
	dec := NewAnthropicStreamDecoder()
	pairs := fixtureSSEPairs(t, "anthropic_stream_text_delta.sse")
	_, _ = dec.DecodeLine(pairs[0].Event, pairs[0].Data)
	events, err := dec.DecodeLine(pairs[1].Event, pairs[1].Data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events) != 1 || events[0].Type != canonical.EventTextDelta {
		t.Errorf("events = %+v", events)
	}
	if events[0].Text != "Hello" {
		t.Errorf("text = %q", events[0].Text)
	}
}

func TestAnthropicStreamDecoder_ThinkingDelta(t *testing.T) {
	dec := NewAnthropicStreamDecoder()
	pairs := fixtureSSEPairs(t, "anthropic_stream_thinking_delta.sse")
	_, _ = dec.DecodeLine(pairs[0].Event, pairs[0].Data)
	events, err := dec.DecodeLine(pairs[1].Event, pairs[1].Data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events) != 1 || events[0].Type != canonical.EventThinkingDelta {
		t.Errorf("events = %+v", events)
	}
}

func TestAnthropicStreamDecoder_ToolUse(t *testing.T) {
	dec := NewAnthropicStreamDecoder()
	pairs := fixtureSSEPairs(t, "anthropic_stream_tool_use.sse")
	events1, err := dec.DecodeLine(pairs[0].Event, pairs[0].Data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events1) != 1 || events1[0].Type != canonical.EventToolCallStart {
		t.Errorf("start events = %+v", events1)
	}

	// tool_use delta
	events2, err := dec.DecodeLine(pairs[1].Event, pairs[1].Data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events2) != 1 || events2[0].Type != canonical.EventToolCallDelta {
		t.Errorf("delta events = %+v", events2)
	}

	// tool_use stop
	events3, err := dec.DecodeLine(pairs[2].Event, pairs[2].Data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events3) != 1 || events3[0].Type != canonical.EventToolCallEnd {
		t.Errorf("stop events = %+v", events3)
	}
}

func TestAnthropicStreamDecoder_MessageDelta(t *testing.T) {
	dec := NewAnthropicStreamDecoder()
	pairs := fixtureSSEPairs(t, "anthropic_stream_message_delta.sse")
	events, err := dec.DecodeLine(pairs[0].Event, pairs[0].Data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d", len(events))
	}
	if events[0].Type != canonical.EventMessageStop || events[0].StopReason != "end_turn" {
		t.Errorf("message_stop = %+v", events[0])
	}
	if events[1].Type != canonical.EventUsageFinal || *events[1].Usage.OutputTokens != 42 {
		t.Errorf("usage_final = %+v", events[1])
	}
	if events[1].Usage.InputTokens == nil || *events[1].Usage.InputTokens != 12 {
		t.Errorf("input_tokens = %+v", events[1].Usage)
	}
	if events[1].Usage.TotalTokens == nil || *events[1].Usage.TotalTokens != 54 {
		t.Errorf("total_tokens = %+v", events[1].Usage)
	}
}

func TestAnthropicStreamDecoder_Ping(t *testing.T) {
	dec := NewAnthropicStreamDecoder()
	pairs := fixtureSSEPairs(t, "anthropic_stream_ping.sse")
	events, err := dec.DecodeLine(pairs[0].Event, pairs[0].Data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("ping should produce no events")
	}
}

// ---------------------------------------------------------------------------
// Anthropic Stream Encoding
// ---------------------------------------------------------------------------

func TestAnthropicStreamEncoder_TextDelta(t *testing.T) {
	enc := NewAnthropicStreamEncoder()
	line := enc.Encode(canonical.CanonicalStreamEvent{
		Type: canonical.EventTextDelta, Text: "Hello",
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
	line := enc.Encode(canonical.CanonicalStreamEvent{
		Type: canonical.EventUsageFinal,
		Usage: &canonical.CanonicalUsage{
			InputTokens:      &in64,
			OutputTokens:     &out64,
			CacheWriteTokens: &cacheCreate64,
			CacheReadTokens:  &cacheRead64,
		},
	})
	if line != "" {
		t.Errorf("usage-only event should wait for stop reason, got %q", line)
	}
	line = enc.Encode(canonical.CanonicalStreamEvent{
		Type: canonical.EventMessageStop, StopReason: "end_turn",
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

func TestOpenAIStreamEncoder_UsageFinalDetails(t *testing.T) {
	enc := NewOpenAIStreamEncoder()
	in64 := int64(10)
	out64 := int64(5)
	cacheRead64 := int64(3)
	reasoning64 := int64(2)
	line := enc.Encode(canonical.CanonicalStreamEvent{
		Type:       canonical.EventUsageFinal,
		ResponseID: "chatcmpl-123",
		Model:      "gpt-4o",
		Usage: &canonical.CanonicalUsage{
			InputTokens:     &in64,
			OutputTokens:    &out64,
			CacheReadTokens: &cacheRead64,
			ReasoningTokens: &reasoning64,
		},
	})
	if !strings.Contains(line, `"total_tokens":15`) {
		t.Errorf("line = %q", line)
	}
	if !strings.Contains(line, `"prompt_tokens_details":{"cached_tokens":3}`) {
		t.Errorf("line = %q", line)
	}
	if !strings.Contains(line, `"completion_tokens_details":{"reasoning_tokens":2}`) {
		t.Errorf("line = %q", line)
	}
}
