package codec

import (
	"encoding/json"
	"testing"

	"github.com/qzydustin/nanoapi/canonical"
)

func sp(s string) *string   { return &s }
func ip(i int) *int         { return &i }
func fp(f float64) *float64 { return &f }

// ---------------------------------------------------------------------------
// OpenAI Request Encoding
// ---------------------------------------------------------------------------

func TestEncodeOpenAI_SimpleText(t *testing.T) {
	req := &canonical.CanonicalRequest{
		ClientModel: "gpt-4o",
		System:      []canonical.CanonicalContentBlock{{Type: "text", Text: sp("Be helpful.")}},
		Messages: []canonical.CanonicalMessage{
			{Role: "user", Content: []canonical.CanonicalContentBlock{
				{Type: "text", Text: sp("Hello")},
			}},
		},
		Params: canonical.CanonicalParams{
			MaxTokens:   ip(1024),
			Temperature: fp(0.5),
		},
	}

	body, err := EncodeOpenAIRequest(req, "gpt-4o", false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)

	if raw["model"] != "gpt-4o" {
		t.Errorf("model = %v", raw["model"])
	}
	msgs := raw["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages count = %d, want 2 (system + user)", len(msgs))
	}
	sysMsg := msgs[0].(map[string]any)
	if sysMsg["role"] != "system" {
		t.Errorf("system role = %v", sysMsg["role"])
	}
}

func TestEncodeOpenAI_ImageDataURL(t *testing.T) {
	req := &canonical.CanonicalRequest{
		Messages: []canonical.CanonicalMessage{
			{Role: "user", Content: []canonical.CanonicalContentBlock{
				{Type: "image", Image: &canonical.CanonicalImage{
					SourceType: "data_url",
					MediaType:  sp("image/png"),
					Data:       sp("iVBOR"),
				}},
			}},
		},
	}

	body, err := EncodeOpenAIRequest(req, "gpt-4o", false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	msgs := raw["messages"].([]any)
	msg := msgs[0].(map[string]any)
	content := msg["content"].([]any)
	part := content[0].(map[string]any)
	imgURL := part["image_url"].(map[string]any)["url"].(string)
	if imgURL != "data:image/png;base64,iVBOR" {
		t.Errorf("image_url = %q", imgURL)
	}
}

func TestEncodeOpenAI_ToolCalls(t *testing.T) {
	req := &canonical.CanonicalRequest{
		Messages: []canonical.CanonicalMessage{
			{Role: "assistant", Content: []canonical.CanonicalContentBlock{
				{Type: "tool_call", ToolCall: &canonical.CanonicalToolCall{
					ID: "call_1", Name: "get_weather",
					Arguments: map[string]any{"location": "NYC"},
				}},
			}},
		},
	}

	body, err := EncodeOpenAIRequest(req, "gpt-4o", false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	msgs := raw["messages"].([]any)
	msg := msgs[0].(map[string]any)
	tcs := msg["tool_calls"].([]any)
	if len(tcs) != 1 {
		t.Fatalf("tool_calls count = %d", len(tcs))
	}
}

// ---------------------------------------------------------------------------
// OpenAI Response Decoding
// ---------------------------------------------------------------------------

func TestDecodeOpenAIResponse_Text(t *testing.T) {
	resp, err := DecodeOpenAIResponse(fixtureBytes(t, "openai_response_text.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ID != "chatcmpl-123" {
		t.Errorf("id = %q", resp.ID)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("stop_reason = %q", resp.StopReason)
	}
	if *resp.Usage.InputTokens != 10 {
		t.Errorf("input_tokens = %d", *resp.Usage.InputTokens)
	}
	text := resp.Output[0].Content[0]
	if text.Type != "text" || *text.Text != "Hello!" {
		t.Errorf("text = %+v", text)
	}
}

func TestDecodeOpenAIResponse_ToolCalls(t *testing.T) {
	resp, err := DecodeOpenAIResponse(fixtureBytes(t, "openai_response_tool_calls.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.StopReason != "tool_use" {
		t.Errorf("stop_reason = %q", resp.StopReason)
	}
	tc := resp.Output[0].Content[0]
	if tc.Type != "tool_call" || tc.ToolCall.Name != "f" {
		t.Errorf("tool_call = %+v", tc)
	}
}

// ---------------------------------------------------------------------------
// OpenAI Client Response Encoding
// ---------------------------------------------------------------------------

func TestEncodeOpenAIClientResponse(t *testing.T) {
	in64 := int64(10)
	out64 := int64(5)
	total64 := int64(15)
	cacheRead64 := int64(3)
	reasoning64 := int64(2)
	resp := &canonical.CanonicalResponse{
		ID: "chatcmpl-123", Model: "gpt-4o", StopReason: "end_turn",
		Output: []canonical.CanonicalMessage{
			{Role: "assistant", Content: []canonical.CanonicalContentBlock{
				{Type: "text", Text: sp("Hello!")},
			}},
		},
		Usage: &canonical.CanonicalUsage{
			InputTokens: &in64, OutputTokens: &out64, TotalTokens: &total64,
			CacheReadTokens: &cacheRead64, ReasoningTokens: &reasoning64,
		},
	}

	body, err := EncodeOpenAIClientResponse(resp)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	if raw["object"] != "chat.completion" {
		t.Errorf("object = %v", raw["object"])
	}
	choices := raw["choices"].([]any)
	choice := choices[0].(map[string]any)
	if choice["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v", choice["finish_reason"])
	}
	usage := raw["usage"].(map[string]any)
	promptDetails := usage["prompt_tokens_details"].(map[string]any)
	if promptDetails["cached_tokens"] != float64(3) {
		t.Errorf("cached_tokens = %v", promptDetails["cached_tokens"])
	}
	completionDetails := usage["completion_tokens_details"].(map[string]any)
	if completionDetails["reasoning_tokens"] != float64(2) {
		t.Errorf("reasoning_tokens = %v", completionDetails["reasoning_tokens"])
	}
}

// ---------------------------------------------------------------------------
// Anthropic Request Encoding
// ---------------------------------------------------------------------------

func TestEncodeAnthropic_SimpleText(t *testing.T) {
	req := &canonical.CanonicalRequest{
		System: []canonical.CanonicalContentBlock{{Type: "text", Text: sp("Be helpful.")}},
		Messages: []canonical.CanonicalMessage{
			{Role: "user", Content: []canonical.CanonicalContentBlock{
				{Type: "text", Text: sp("Hello")},
			}},
		},
		Params: canonical.CanonicalParams{MaxTokens: ip(1024)},
	}

	body, err := EncodeAnthropicRequest(req, "claude-3-7-sonnet-20250219", false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	if raw["model"] != "claude-3-7-sonnet-20250219" {
		t.Errorf("model = %v", raw["model"])
	}
	if raw["max_tokens"] != float64(1024) {
		t.Errorf("max_tokens = %v", raw["max_tokens"])
	}
	sys := raw["system"].([]any)
	if len(sys) != 1 {
		t.Fatalf("system count = %d", len(sys))
	}
}

func TestEncodeAnthropic_Thinking(t *testing.T) {
	req := &canonical.CanonicalRequest{
		Messages: []canonical.CanonicalMessage{
			{Role: "user", Content: []canonical.CanonicalContentBlock{
				{Type: "text", Text: sp("Think about this")},
			}},
		},
		Params: canonical.CanonicalParams{
			MaxTokens: ip(16000),
			Reasoning: &canonical.CanonicalReasoning{
				Mode:         "enabled",
				BudgetTokens: ip(10000),
			},
		},
	}

	body, err := EncodeAnthropicRequest(req, "claude-3-7-sonnet-20250219", false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	thinking := raw["thinking"].(map[string]any)
	if thinking["type"] != "enabled" {
		t.Errorf("thinking.type = %v", thinking["type"])
	}
	if thinking["budget_tokens"] != float64(10000) {
		t.Errorf("thinking.budget_tokens = %v", thinking["budget_tokens"])
	}
}

func TestEncodeAnthropic_ReasoningEffortEnablesThinking(t *testing.T) {
	req := &canonical.CanonicalRequest{
		Messages: []canonical.CanonicalMessage{
			{Role: "user", Content: []canonical.CanonicalContentBlock{
				{Type: "text", Text: sp("Think about this")},
			}},
		},
		Params: canonical.CanonicalParams{
			MaxTokens: ip(2048),
			Reasoning: &canonical.CanonicalReasoning{
				Effort: sp("high"),
			},
		},
	}

	body, err := EncodeAnthropicRequest(req, "claude-3-7-sonnet-20250219", false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	thinking := raw["thinking"].(map[string]any)
	if thinking["type"] != "enabled" {
		t.Errorf("thinking.type = %v", thinking["type"])
	}
}

func TestEncodeAnthropic_Base64Image(t *testing.T) {
	req := &canonical.CanonicalRequest{
		Messages: []canonical.CanonicalMessage{
			{Role: "user", Content: []canonical.CanonicalContentBlock{
				{Type: "image", Image: &canonical.CanonicalImage{
					SourceType: "base64",
					MediaType:  sp("image/jpeg"),
					Data:       sp("/9j/4"),
				}},
			}},
		},
		Params: canonical.CanonicalParams{MaxTokens: ip(1024)},
	}

	body, err := EncodeAnthropicRequest(req, "claude-3-7-sonnet-20250219", false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	msgs := raw["messages"].([]any)
	msg := msgs[0].(map[string]any)
	content := msg["content"].([]any)
	block := content[0].(map[string]any)
	src := block["source"].(map[string]any)
	if src["type"] != "base64" {
		t.Errorf("source.type = %v", src["type"])
	}
	if src["media_type"] != "image/jpeg" {
		t.Errorf("source.media_type = %v", src["media_type"])
	}
}

// ---------------------------------------------------------------------------
// Anthropic Response Decoding
// ---------------------------------------------------------------------------

func TestDecodeAnthropicResponse_Text(t *testing.T) {
	resp, err := DecodeAnthropicResponse(fixtureBytes(t, "anthropic_response_text.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ID != "msg_123" {
		t.Errorf("id = %q", resp.ID)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("stop_reason = %q", resp.StopReason)
	}
	if *resp.Usage.InputTokens != 10 {
		t.Errorf("input_tokens = %d", *resp.Usage.InputTokens)
	}
	if resp.Usage.TotalTokens == nil || *resp.Usage.TotalTokens != 15 {
		t.Errorf("total_tokens = %+v", resp.Usage.TotalTokens)
	}
}

func TestDecodeAnthropicResponse_ToolUse(t *testing.T) {
	resp, err := DecodeAnthropicResponse(fixtureBytes(t, "anthropic_response_tool_use.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	tc := resp.Output[0].Content[0]
	if tc.Type != "tool_call" || tc.ToolCall.Name != "f" {
		t.Errorf("tool_call = %+v", tc)
	}
}

func TestDecodeAnthropicResponse_Thinking(t *testing.T) {
	resp, err := DecodeAnthropicResponse(fixtureBytes(t, "anthropic_response_thinking.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	blocks := resp.Output[0].Content
	if len(blocks) != 2 {
		t.Fatalf("blocks = %d", len(blocks))
	}
	if blocks[0].Type != "thinking" {
		t.Errorf("blocks[0].type = %q", blocks[0].Type)
	}
}

// ---------------------------------------------------------------------------
// Cross-protocol round-trip
// ---------------------------------------------------------------------------

func TestCrossProtocol_OpenAIToAnthropic(t *testing.T) {
	// Decode an OpenAI request
	req, err := canonical.DecodeRequest(canonical.ProtocolOpenAIChat, fixtureBytes(t, "cross_openai_request.json"))
	if err != nil {
		t.Fatalf("decode openai: %v", err)
	}

	// Encode as Anthropic request
	anthBody, err := EncodeAnthropicRequest(req, "claude-3-7-sonnet-20250219", false)
	if err != nil {
		t.Fatalf("encode anthropic: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(anthBody, &raw)

	if raw["model"] != "claude-3-7-sonnet-20250219" {
		t.Errorf("model = %v", raw["model"])
	}
	// System should be in top-level system field
	sys := raw["system"].([]any)
	if len(sys) != 1 {
		t.Fatalf("system count = %d", len(sys))
	}
	// Messages should only have user message
	msgs := raw["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages count = %d, want 1 (user only)", len(msgs))
	}
}

func TestCrossProtocol_AnthropicToOpenAI(t *testing.T) {
	// Decode an Anthropic request
	req, err := canonical.DecodeRequest(canonical.ProtocolAnthropicMessage, fixtureBytes(t, "cross_anthropic_request.json"))
	if err != nil {
		t.Fatalf("decode anthropic: %v", err)
	}

	// Encode as OpenAI request
	openaiBody, err := EncodeOpenAIRequest(req, "gpt-4o", false)
	if err != nil {
		t.Fatalf("encode openai: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(openaiBody, &raw)

	if raw["model"] != "gpt-4o" {
		t.Errorf("model = %v", raw["model"])
	}
	msgs := raw["messages"].([]any)
	// Should have system message + user message
	if len(msgs) != 2 {
		t.Fatalf("messages count = %d, want 2", len(msgs))
	}
	if msgs[0].(map[string]any)["role"] != "system" {
		t.Errorf("msgs[0].role = %v", msgs[0].(map[string]any)["role"])
	}
}

func TestCrossProtocol_AnthropicThinkingToOpenAIReasoning(t *testing.T) {
	req, err := canonical.DecodeRequest(canonical.ProtocolAnthropicMessage, fixtureBytes(t, "cross_anthropic_thinking_request.json"))
	if err != nil {
		t.Fatalf("decode anthropic: %v", err)
	}

	openaiBody, err := EncodeOpenAIRequest(req, "gpt-4o", false)
	if err != nil {
		t.Fatalf("encode openai: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(openaiBody, &raw)
	if raw["reasoning_effort"] != "medium" {
		t.Errorf("reasoning_effort = %v", raw["reasoning_effort"])
	}
}

// ---------------------------------------------------------------------------
// OpenAI Request Encoding — tool_result
// ---------------------------------------------------------------------------

func TestEncodeOpenAI_ToolResult(t *testing.T) {
	req := &canonical.CanonicalRequest{
		Messages: []canonical.CanonicalMessage{
			{Role: "tool", Content: []canonical.CanonicalContentBlock{
				{Type: "tool_result", ToolResult: &canonical.CanonicalToolResult{
					ToolCallID: "call_1",
					Content:    []canonical.CanonicalContentBlock{{Type: "text", Text: sp("Sunny, 72°F")}},
				}},
			}},
		},
	}

	body, err := EncodeOpenAIRequest(req, "gpt-4o", false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	msgs := raw["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages count = %d, want 1", len(msgs))
	}
	msg := msgs[0].(map[string]any)
	if msg["role"] != "tool" {
		t.Errorf("role = %v", msg["role"])
	}
	if msg["tool_call_id"] != "call_1" {
		t.Errorf("tool_call_id = %v", msg["tool_call_id"])
	}
	if msg["content"] != "Sunny, 72°F" {
		t.Errorf("content = %v", msg["content"])
	}
}

// ---------------------------------------------------------------------------
// OpenAI Request Encoding — image base64 → data_url
// ---------------------------------------------------------------------------

func TestEncodeOpenAI_ImageBase64ToDataURL(t *testing.T) {
	req := &canonical.CanonicalRequest{
		Messages: []canonical.CanonicalMessage{
			{Role: "user", Content: []canonical.CanonicalContentBlock{
				{Type: "image", Image: &canonical.CanonicalImage{
					SourceType: "base64",
					MediaType:  sp("image/jpeg"),
					Data:       sp("/9j/4AAQ"),
				}},
			}},
		},
	}

	body, err := EncodeOpenAIRequest(req, "gpt-4o", false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	msgs := raw["messages"].([]any)
	msg := msgs[0].(map[string]any)
	content := msg["content"].([]any)
	part := content[0].(map[string]any)
	imgURL := part["image_url"].(map[string]any)["url"].(string)
	if imgURL != "data:image/jpeg;base64,/9j/4AAQ" {
		t.Errorf("image_url = %q, want data:image/jpeg;base64,/9j/4AAQ", imgURL)
	}
}

// ---------------------------------------------------------------------------
// OpenAI Request Encoding — image URL passthrough
// ---------------------------------------------------------------------------

func TestEncodeOpenAI_ImageURL(t *testing.T) {
	req := &canonical.CanonicalRequest{
		Messages: []canonical.CanonicalMessage{
			{Role: "user", Content: []canonical.CanonicalContentBlock{
				{Type: "image", Image: &canonical.CanonicalImage{
					SourceType: "url",
					URL:        sp("https://example.com/img.png"),
				}},
			}},
		},
	}

	body, err := EncodeOpenAIRequest(req, "gpt-4o", false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	msgs := raw["messages"].([]any)
	msg := msgs[0].(map[string]any)
	content := msg["content"].([]any)
	part := content[0].(map[string]any)
	imgURL := part["image_url"].(map[string]any)["url"].(string)
	if imgURL != "https://example.com/img.png" {
		t.Errorf("image_url = %q", imgURL)
	}
}

// ---------------------------------------------------------------------------
// OpenAI Request Encoding — reasoning_effort
// ---------------------------------------------------------------------------

func TestEncodeOpenAI_ReasoningEffort(t *testing.T) {
	req := &canonical.CanonicalRequest{
		Messages: []canonical.CanonicalMessage{
			{Role: "user", Content: []canonical.CanonicalContentBlock{
				{Type: "text", Text: sp("Think hard")},
			}},
		},
		Params: canonical.CanonicalParams{
			Reasoning: &canonical.CanonicalReasoning{
				Effort: sp("high"),
			},
		},
	}

	body, err := EncodeOpenAIRequest(req, "gpt-4o", false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	if raw["reasoning_effort"] != "high" {
		t.Errorf("reasoning_effort = %v", raw["reasoning_effort"])
	}
}

// ---------------------------------------------------------------------------
// OpenAI Response Decoding — reasoning_content
// ---------------------------------------------------------------------------

func TestDecodeOpenAIResponse_ReasoningContent(t *testing.T) {
	resp, err := DecodeOpenAIResponse(fixtureBytes(t, "openai_response_reasoning_content.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	blocks := resp.Output[0].Content
	if len(blocks) != 2 {
		t.Fatalf("blocks count = %d, want 2 (thinking + text)", len(blocks))
	}
	if blocks[0].Type != "thinking" {
		t.Errorf("blocks[0].type = %q, want thinking", blocks[0].Type)
	}
	if *blocks[0].Thinking.Text != "Let me think step by step..." {
		t.Errorf("thinking = %q", *blocks[0].Thinking.Text)
	}
	if blocks[1].Type != "text" {
		t.Errorf("blocks[1].type = %q, want text", blocks[1].Type)
	}
	if *blocks[1].Text != "The answer is 42." {
		t.Errorf("text = %q", *blocks[1].Text)
	}
}

// ---------------------------------------------------------------------------
// OpenAI Client Response Encoding — with tool calls
// ---------------------------------------------------------------------------

func TestEncodeOpenAIClientResponse_ToolCalls(t *testing.T) {
	resp := &canonical.CanonicalResponse{
		ID: "chatcmpl-123", Model: "gpt-4o", StopReason: "tool_use",
		Output: []canonical.CanonicalMessage{
			{Role: "assistant", Content: []canonical.CanonicalContentBlock{
				{Type: "tool_call", ToolCall: &canonical.CanonicalToolCall{
					ID: "call_1", Name: "get_weather",
					Arguments: map[string]any{"location": "NYC"},
				}},
			}},
		},
	}

	body, err := EncodeOpenAIClientResponse(resp)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	choices := raw["choices"].([]any)
	choice := choices[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Errorf("finish_reason = %v, want tool_calls", choice["finish_reason"])
	}
	msg := choice["message"].(map[string]any)
	tcs := msg["tool_calls"].([]any)
	if len(tcs) != 1 {
		t.Fatalf("tool_calls = %d", len(tcs))
	}
	tc := tcs[0].(map[string]any)
	if tc["id"] != "call_1" {
		t.Errorf("tc.id = %v", tc["id"])
	}
	fn := tc["function"].(map[string]any)
	if fn["name"] != "get_weather" {
		t.Errorf("tc.function.name = %v", fn["name"])
	}
}

// ---------------------------------------------------------------------------
// Anthropic Request Encoding — tool_result
// ---------------------------------------------------------------------------

func TestEncodeAnthropic_ToolResult(t *testing.T) {
	req := &canonical.CanonicalRequest{
		Messages: []canonical.CanonicalMessage{
			{Role: "user", Content: []canonical.CanonicalContentBlock{
				{Type: "tool_result", ToolResult: &canonical.CanonicalToolResult{
					ToolCallID: "toolu_1",
					Content:    []canonical.CanonicalContentBlock{{Type: "text", Text: sp("Sunny, 72°F")}},
				}},
			}},
		},
		Params: canonical.CanonicalParams{MaxTokens: ip(1024)},
	}

	body, err := EncodeAnthropicRequest(req, "claude-3-7-sonnet-20250219", false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	msgs := raw["messages"].([]any)
	msg := msgs[0].(map[string]any)
	content := msg["content"].([]any)
	block := content[0].(map[string]any)
	if block["type"] != "tool_result" {
		t.Errorf("type = %v", block["type"])
	}
	if block["tool_use_id"] != "toolu_1" {
		t.Errorf("tool_use_id = %v", block["tool_use_id"])
	}
	if block["content"] != "Sunny, 72°F" {
		t.Errorf("content = %v", block["content"])
	}
}

// ---------------------------------------------------------------------------
// Anthropic Request Encoding — tool_use
// ---------------------------------------------------------------------------

func TestEncodeAnthropic_ToolUse(t *testing.T) {
	req := &canonical.CanonicalRequest{
		Messages: []canonical.CanonicalMessage{
			{Role: "assistant", Content: []canonical.CanonicalContentBlock{
				{Type: "tool_call", ToolCall: &canonical.CanonicalToolCall{
					ID: "toolu_1", Name: "get_weather",
					Arguments: map[string]any{"location": "NYC"},
				}},
			}},
		},
		Params: canonical.CanonicalParams{MaxTokens: ip(1024)},
	}

	body, err := EncodeAnthropicRequest(req, "claude-3-7-sonnet-20250219", false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	msgs := raw["messages"].([]any)
	msg := msgs[0].(map[string]any)
	content := msg["content"].([]any)
	block := content[0].(map[string]any)
	if block["type"] != "tool_use" {
		t.Errorf("type = %v", block["type"])
	}
	if block["id"] != "toolu_1" {
		t.Errorf("id = %v", block["id"])
	}
	if block["name"] != "get_weather" {
		t.Errorf("name = %v", block["name"])
	}
}

// ---------------------------------------------------------------------------
// Anthropic Request Encoding — URL image
// ---------------------------------------------------------------------------

func TestEncodeAnthropic_URLImage(t *testing.T) {
	req := &canonical.CanonicalRequest{
		Messages: []canonical.CanonicalMessage{
			{Role: "user", Content: []canonical.CanonicalContentBlock{
				{Type: "image", Image: &canonical.CanonicalImage{
					SourceType: "url",
					URL:        sp("https://example.com/img.png"),
				}},
			}},
		},
		Params: canonical.CanonicalParams{MaxTokens: ip(1024)},
	}

	body, err := EncodeAnthropicRequest(req, "claude-3-7-sonnet-20250219", false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	msgs := raw["messages"].([]any)
	msg := msgs[0].(map[string]any)
	content := msg["content"].([]any)
	block := content[0].(map[string]any)
	src := block["source"].(map[string]any)
	if src["type"] != "url" {
		t.Errorf("source.type = %v", src["type"])
	}
	if src["url"] != "https://example.com/img.png" {
		t.Errorf("source.url = %v", src["url"])
	}
}

// ---------------------------------------------------------------------------
// Anthropic Request Encoding — data_url image → base64
// ---------------------------------------------------------------------------

func TestEncodeAnthropic_DataURLImageToBase64(t *testing.T) {
	req := &canonical.CanonicalRequest{
		Messages: []canonical.CanonicalMessage{
			{Role: "user", Content: []canonical.CanonicalContentBlock{
				{Type: "image", Image: &canonical.CanonicalImage{
					SourceType: "data_url",
					MediaType:  sp("image/png"),
					Data:       sp("iVBOR"),
				}},
			}},
		},
		Params: canonical.CanonicalParams{MaxTokens: ip(1024)},
	}

	body, err := EncodeAnthropicRequest(req, "claude-3-7-sonnet-20250219", false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	msgs := raw["messages"].([]any)
	msg := msgs[0].(map[string]any)
	content := msg["content"].([]any)
	block := content[0].(map[string]any)
	src := block["source"].(map[string]any)
	if src["type"] != "base64" {
		t.Errorf("source.type = %v, want base64", src["type"])
	}
	if src["media_type"] != "image/png" {
		t.Errorf("source.media_type = %v", src["media_type"])
	}
	if src["data"] != "iVBOR" {
		t.Errorf("source.data = %v", src["data"])
	}
}

// ---------------------------------------------------------------------------
// Anthropic Client Response Encoding — thinking + tool_use
// ---------------------------------------------------------------------------

func TestEncodeAnthropicClientResponse_ThinkingAndToolUse(t *testing.T) {
	in64 := int64(10)
	out64 := int64(20)
	cacheCreate64 := int64(4)
	cacheRead64 := int64(6)
	resp := &canonical.CanonicalResponse{
		ID: "msg_123", Model: "claude-3-7-sonnet-20250219", StopReason: "tool_use",
		Output: []canonical.CanonicalMessage{
			{Role: "assistant", Content: []canonical.CanonicalContentBlock{
				{Type: "thinking", Thinking: &canonical.CanonicalThinkingBlock{Text: sp("Let me think...")}},
				{Type: "tool_call", ToolCall: &canonical.CanonicalToolCall{
					ID: "toolu_1", Name: "get_weather",
					Arguments: map[string]any{"location": "NYC"},
				}},
			}},
		},
		Usage: &canonical.CanonicalUsage{
			InputTokens: &in64, OutputTokens: &out64,
			CacheWriteTokens: &cacheCreate64, CacheReadTokens: &cacheRead64,
		},
	}

	body, err := EncodeAnthropicClientResponse(resp)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	if raw["type"] != "message" {
		t.Errorf("type = %v", raw["type"])
	}
	if raw["stop_reason"] != "tool_use" {
		t.Errorf("stop_reason = %v", raw["stop_reason"])
	}
	content := raw["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("content count = %d, want 2", len(content))
	}
	if content[0].(map[string]any)["type"] != "thinking" {
		t.Errorf("content[0].type = %v", content[0].(map[string]any)["type"])
	}
	if content[1].(map[string]any)["type"] != "tool_use" {
		t.Errorf("content[1].type = %v", content[1].(map[string]any)["type"])
	}
	usage := raw["usage"].(map[string]any)
	if usage["input_tokens"] != float64(10) {
		t.Errorf("input_tokens = %v", usage["input_tokens"])
	}
	if usage["cache_creation_input_tokens"] != float64(4) {
		t.Errorf("cache_creation_input_tokens = %v", usage["cache_creation_input_tokens"])
	}
	if usage["cache_read_input_tokens"] != float64(6) {
		t.Errorf("cache_read_input_tokens = %v", usage["cache_read_input_tokens"])
	}
}

// ---------------------------------------------------------------------------
// Anthropic Request Encoding — default max_tokens
// ---------------------------------------------------------------------------

func TestEncodeAnthropic_DefaultMaxTokens(t *testing.T) {
	req := &canonical.CanonicalRequest{
		Messages: []canonical.CanonicalMessage{
			{Role: "user", Content: []canonical.CanonicalContentBlock{
				{Type: "text", Text: sp("Hello")},
			}},
		},
		// No MaxTokens set — should default to 4096
	}

	body, err := EncodeAnthropicRequest(req, "claude-3-7-sonnet-20250219", false)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	if raw["max_tokens"] != float64(4096) {
		t.Errorf("max_tokens = %v, want 4096", raw["max_tokens"])
	}
}

// ---------------------------------------------------------------------------
// OpenAI Client Response Encoding — reasoning_content
// ---------------------------------------------------------------------------

func TestEncodeOpenAIClientResponse_ReasoningContent(t *testing.T) {
	resp := &canonical.CanonicalResponse{
		ID: "chatcmpl-123", Model: "gpt-4o", StopReason: "end_turn",
		Output: []canonical.CanonicalMessage{
			{Role: "assistant", Content: []canonical.CanonicalContentBlock{
				{Type: "thinking", Thinking: &canonical.CanonicalThinkingBlock{Text: sp("Step by step...")}},
				{Type: "text", Text: sp("The answer is 42.")},
			}},
		},
	}

	body, err := EncodeOpenAIClientResponse(resp)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	choices := raw["choices"].([]any)
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	if msg["reasoning_content"] != "Step by step..." {
		t.Errorf("reasoning_content = %v", msg["reasoning_content"])
	}
	if msg["content"] != "The answer is 42." {
		t.Errorf("content = %v", msg["content"])
	}
}
