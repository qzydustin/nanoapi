package execute

import (
	"testing"

	"github.com/qzydustin/nanoapi/canonical"
	"github.com/qzydustin/nanoapi/codec"
)

func TestDecideMode(t *testing.T) {
	tests := []struct {
		clientStream bool
		forceStream  bool
		want         ExecutionMode
	}{
		{true, false, ModePassthroughStream},
		{true, true, ModePassthroughStream},
		{false, true, ModeAggregateStream},
		{false, false, ModeDirectNonStream},
	}
	for _, tt := range tests {
		got := DecideMode(tt.clientStream, tt.forceStream)
		if got != tt.want {
			t.Errorf("DecideMode(%v, %v) = %q, want %q", tt.clientStream, tt.forceStream, got, tt.want)
		}
	}
}

func TestBuildUpstreamURL(t *testing.T) {
	tests := []struct {
		base     string
		protocol canonical.Protocol
		want     string
	}{
		{"https://api.openai.com", canonical.ProtocolOpenAIChat, "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/", canonical.ProtocolOpenAIChat, "https://api.openai.com/v1/chat/completions"},
		{"https://api.anthropic.com", canonical.ProtocolAnthropicMessage, "https://api.anthropic.com/v1/messages"},
	}
	for _, tt := range tests {
		got := BuildUpstreamURL(tt.base, tt.protocol)
		if got != tt.want {
			t.Errorf("BuildUpstreamURL(%q, %q) = %q, want %q", tt.base, tt.protocol, got, tt.want)
		}
	}
}

func TestStreamAggregation_TextOnly(t *testing.T) {
	state := &StreamAggregateState{}
	state.Apply(canonical.CanonicalStreamEvent{
		Type: canonical.EventTextDelta, Text: "Hello ",
		ResponseID: "msg_1", Model: "gpt-4o",
	})
	state.Apply(canonical.CanonicalStreamEvent{
		Type: canonical.EventTextDelta, Text: "World",
	})
	state.Apply(canonical.CanonicalStreamEvent{
		Type: canonical.EventMessageStop, StopReason: "end_turn",
	})

	resp := state.Finalize()
	if resp.ID != "msg_1" {
		t.Errorf("id = %q", resp.ID)
	}
	text := resp.Output[0].Content[0]
	if *text.Text != "Hello World" {
		t.Errorf("text = %q", *text.Text)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("stop_reason = %q", resp.StopReason)
	}
}

func TestStreamAggregation_ThinkingAndText(t *testing.T) {
	state := &StreamAggregateState{}
	state.Apply(canonical.CanonicalStreamEvent{
		Type: canonical.EventThinkingDelta, Text: "Let me think...",
		ResponseID: "msg_1", Model: "claude-3",
	})
	state.Apply(canonical.CanonicalStreamEvent{
		Type: canonical.EventTextDelta, Text: "Answer",
	})
	state.Apply(canonical.CanonicalStreamEvent{
		Type: canonical.EventMessageStop, StopReason: "end_turn",
	})

	resp := state.Finalize()
	blocks := resp.Output[0].Content
	if len(blocks) != 2 {
		t.Fatalf("blocks = %d", len(blocks))
	}
	if blocks[0].Type != "thinking" || *blocks[0].Thinking.Text != "Let me think..." {
		t.Errorf("thinking = %+v", blocks[0])
	}
	if blocks[1].Type != "text" || *blocks[1].Text != "Answer" {
		t.Errorf("text = %+v", blocks[1])
	}
}

func TestStreamAggregation_ToolCalls(t *testing.T) {
	state := &StreamAggregateState{}
	state.Apply(canonical.CanonicalStreamEvent{
		Type:       canonical.EventToolCallStart,
		ToolCallID: "call_1", ToolCallName: "get_weather",
		ResponseID: "msg_1", Model: "gpt-4o",
	})
	state.Apply(canonical.CanonicalStreamEvent{
		Type: canonical.EventToolCallDelta, ArgumentsDelta: `{"location":`,
	})
	state.Apply(canonical.CanonicalStreamEvent{
		Type: canonical.EventToolCallDelta, ArgumentsDelta: `"NYC"}`,
	})
	state.Apply(canonical.CanonicalStreamEvent{Type: canonical.EventToolCallEnd})
	state.Apply(canonical.CanonicalStreamEvent{
		Type: canonical.EventMessageStop, StopReason: "tool_use",
	})

	resp := state.Finalize()
	tc := resp.Output[0].Content[0]
	if tc.Type != "tool_call" {
		t.Errorf("type = %q", tc.Type)
	}
	if tc.ToolCall.ID != "call_1" {
		t.Errorf("id = %q", tc.ToolCall.ID)
	}
	if tc.ToolCall.Name != "get_weather" {
		t.Errorf("name = %q", tc.ToolCall.Name)
	}
	// Verify arguments were reassembled.
	args, ok := tc.ToolCall.Arguments.(map[string]any)
	if !ok {
		t.Fatalf("arguments type = %T", tc.ToolCall.Arguments)
	}
	if args["location"] != "NYC" {
		t.Errorf("arguments = %v", args)
	}
}

func TestStreamAggregation_Usage(t *testing.T) {
	state := &StreamAggregateState{}
	in64 := int64(10)
	out64 := int64(5)
	total64 := int64(15)
	state.Apply(canonical.CanonicalStreamEvent{
		Type: canonical.EventUsageFinal,
		Usage: &canonical.CanonicalUsage{
			InputTokens: &in64, OutputTokens: &out64, TotalTokens: &total64,
		},
	})

	resp := state.Finalize()
	if resp.Usage == nil {
		t.Fatal("usage should not be nil")
	}
	if *resp.Usage.InputTokens != 10 {
		t.Errorf("input_tokens = %d", *resp.Usage.InputTokens)
	}
}

// ---------------------------------------------------------------------------
// Decode → Aggregate pipeline: simulated OpenAI stream → aggregated response
// ---------------------------------------------------------------------------

func TestAggregation_OpenAIStreamPipeline(t *testing.T) {
	lines := fixtureLines(t, "openai_aggregate_stream.sse")

	state := &StreamAggregateState{}
	for _, line := range lines {
		events, done, err := codec.DecodeOpenAIStreamLine(line)
		if err != nil {
			t.Fatalf("decode line %q: %v", line, err)
		}
		for _, ev := range events {
			state.Apply(ev)
		}
		if done {
			break
		}
	}

	resp := state.Finalize()
	if resp.ID != "chatcmpl-1" {
		t.Errorf("id = %q", resp.ID)
	}
	if resp.Model != "gpt-4o" {
		t.Errorf("model = %q", resp.Model)
	}
	text := resp.Output[0].Content[0]
	if *text.Text != "Hello World" {
		t.Errorf("text = %q, want 'Hello World'", *text.Text)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("stop_reason = %q", resp.StopReason)
	}
	if resp.Usage == nil || *resp.Usage.InputTokens != 5 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

// ---------------------------------------------------------------------------
// Decode → Aggregate pipeline: simulated Anthropic stream → aggregated response
// ---------------------------------------------------------------------------

func TestAggregation_AnthropicStreamPipeline(t *testing.T) {
	pairs := fixtureSSEPairs(t, "anthropic_aggregate_stream.sse")

	dec := codec.NewAnthropicStreamDecoder()
	state := &StreamAggregateState{}
	for _, p := range pairs {
		events, err := dec.DecodeLine(p.event, p.data)
		if err != nil {
			t.Fatalf("decode %q: %v", p.event, err)
		}
		for _, ev := range events {
			state.Apply(ev)
		}
	}

	resp := state.Finalize()
	if resp.ID != "msg_1" {
		t.Errorf("id = %q", resp.ID)
	}
	text := resp.Output[0].Content[0]
	if *text.Text != "Hello World" {
		t.Errorf("text = %q, want 'Hello World'", *text.Text)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("stop_reason = %q", resp.StopReason)
	}
	if resp.Usage == nil || *resp.Usage.OutputTokens != 10 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

// ---------------------------------------------------------------------------
// Aggregation with tool calls from OpenAI stream
// ---------------------------------------------------------------------------

func TestAggregation_OpenAIToolCallStream(t *testing.T) {
	lines := fixtureLines(t, "openai_tool_call_stream.sse")

	state := &StreamAggregateState{}
	for _, line := range lines {
		events, done, err := codec.DecodeOpenAIStreamLine(line)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		for _, ev := range events {
			state.Apply(ev)
		}
		if done {
			break
		}
	}

	resp := state.Finalize()
	if resp.StopReason != "tool_use" {
		t.Errorf("stop_reason = %q", resp.StopReason)
	}
	if len(resp.Output[0].Content) == 0 {
		t.Fatal("no content blocks")
	}
	tc := resp.Output[0].Content[0]
	if tc.Type != "tool_call" {
		t.Errorf("type = %q", tc.Type)
	}
	if tc.ToolCall.Name != "get_weather" {
		t.Errorf("name = %q", tc.ToolCall.Name)
	}
	args, ok := tc.ToolCall.Arguments.(map[string]any)
	if !ok {
		t.Fatalf("arguments type = %T", tc.ToolCall.Arguments)
	}
	if args["location"] != "NYC" {
		t.Errorf("args = %v", args)
	}
}

// ---------------------------------------------------------------------------
// Aggregation: empty stream produces empty response
// ---------------------------------------------------------------------------

func TestAggregation_EmptyStream(t *testing.T) {
	state := &StreamAggregateState{}
	resp := state.Finalize()
	if len(resp.Output[0].Content) != 0 {
		t.Errorf("content should be empty, got %d blocks", len(resp.Output[0].Content))
	}
}

// ---------------------------------------------------------------------------
// Aggregation: usage merge from multiple events
// ---------------------------------------------------------------------------

func TestAggregation_UsageMerge(t *testing.T) {
	state := &StreamAggregateState{}
	in64 := int64(10)
	out64 := int64(5)
	state.Apply(canonical.CanonicalStreamEvent{
		Type:  canonical.EventUsageFinal,
		Usage: &canonical.CanonicalUsage{InputTokens: &in64},
	})
	state.Apply(canonical.CanonicalStreamEvent{
		Type:  canonical.EventUsageFinal,
		Usage: &canonical.CanonicalUsage{OutputTokens: &out64},
	})

	resp := state.Finalize()
	if resp.Usage == nil {
		t.Fatal("usage nil")
	}
	if *resp.Usage.InputTokens != 10 {
		t.Errorf("input_tokens = %d", *resp.Usage.InputTokens)
	}
	if *resp.Usage.OutputTokens != 5 {
		t.Errorf("output_tokens = %d", *resp.Usage.OutputTokens)
	}
}
