package canonical

import (
	"encoding/json"
	"testing"
)

// ---------------------------------------------------------------------------
// OpenAI decoding tests
// ---------------------------------------------------------------------------

func TestDecodeOpenAI_SimpleText(t *testing.T) {
	req, err := DecodeRequest(ProtocolOpenAIChat, fixtureBytes(t, "openai_simple_text.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.ClientModel != "gpt-4o" {
		t.Errorf("model = %q", req.ClientModel)
	}
	if req.Stream {
		t.Error("stream should be false")
	}
	if len(req.Messages) != 1 {
		t.Fatalf("messages count = %d", len(req.Messages))
	}
	m := req.Messages[0]
	if m.Role != "user" {
		t.Errorf("role = %q", m.Role)
	}
	if len(m.Content) != 1 || m.Content[0].Type != "text" || *m.Content[0].Text != "Hello" {
		t.Errorf("content = %+v", m.Content)
	}
}

func TestDecodeOpenAI_ArrayContent(t *testing.T) {
	req, err := DecodeRequest(ProtocolOpenAIChat, fixtureBytes(t, "openai_array_content.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(req.Messages[0].Content) != 2 {
		t.Fatalf("content count = %d", len(req.Messages[0].Content))
	}
	imgBlock := req.Messages[0].Content[1]
	if imgBlock.Type != "image" {
		t.Errorf("type = %q", imgBlock.Type)
	}
	if imgBlock.Image.SourceType != "url" {
		t.Errorf("source_type = %q", imgBlock.Image.SourceType)
	}
	if *imgBlock.Image.URL != "https://example.com/img.png" {
		t.Errorf("url = %q", *imgBlock.Image.URL)
	}
}

func TestDecodeOpenAI_DataURLImage(t *testing.T) {
	req, err := DecodeRequest(ProtocolOpenAIChat, fixtureBytes(t, "openai_data_url_image.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	img := req.Messages[0].Content[0].Image
	if img.SourceType != "data_url" {
		t.Errorf("source_type = %q", img.SourceType)
	}
	if *img.MediaType != "image/png" {
		t.Errorf("media_type = %q", *img.MediaType)
	}
	if *img.Data != "iVBOR" {
		t.Errorf("data = %q", *img.Data)
	}
}

func TestDecodeOpenAI_SystemAndDeveloper(t *testing.T) {
	req, err := DecodeRequest(ProtocolOpenAIChat, fixtureBytes(t, "openai_system_developer.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(req.System) != 2 {
		t.Fatalf("system blocks = %d, want 2", len(req.System))
	}
	if *req.System[0].Text != "You are helpful." {
		t.Errorf("system[0] = %q", *req.System[0].Text)
	}
	if *req.System[1].Text != "Be concise." {
		t.Errorf("system[1] = %q", *req.System[1].Text)
	}
	if len(req.Messages) != 1 {
		t.Errorf("messages count = %d, want 1 (user only)", len(req.Messages))
	}
}

func TestDecodeOpenAI_ToolCalls(t *testing.T) {
	req, err := DecodeRequest(ProtocolOpenAIChat, fixtureBytes(t, "openai_tool_calls.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(req.Messages) != 3 {
		t.Fatalf("messages = %d", len(req.Messages))
	}

	// Assistant message with tool call
	assistMsg := req.Messages[1]
	if len(assistMsg.Content) != 1 {
		t.Fatalf("assistant content count = %d", len(assistMsg.Content))
	}
	tc := assistMsg.Content[0]
	if tc.Type != "tool_call" {
		t.Errorf("type = %q", tc.Type)
	}
	if tc.ToolCall.ID != "call_123" {
		t.Errorf("tool_call id = %q", tc.ToolCall.ID)
	}
	if tc.ToolCall.Name != "get_weather" {
		t.Errorf("tool_call name = %q", tc.ToolCall.Name)
	}

	// Tool result message
	toolMsg := req.Messages[2]
	if toolMsg.Role != "tool" {
		t.Errorf("role = %q", toolMsg.Role)
	}
	tr := toolMsg.Content[0]
	if tr.Type != "tool_result" {
		t.Errorf("type = %q", tr.Type)
	}
	if tr.ToolResult.ToolCallID != "call_123" {
		t.Errorf("tool_call_id = %q", tr.ToolResult.ToolCallID)
	}
	if len(tr.ToolResult.Content) != 1 || *tr.ToolResult.Content[0].Text != "Sunny, 72°F" {
		t.Errorf("tool result content = %+v", tr.ToolResult.Content)
	}
}

func TestDecodeOpenAI_Streaming(t *testing.T) {
	req, err := DecodeRequest(ProtocolOpenAIChat, fixtureBytes(t, "openai_stream_true.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !req.Stream {
		t.Error("stream should be true")
	}
}

func TestDecodeOpenAI_Params(t *testing.T) {
	req, err := DecodeRequest(ProtocolOpenAIChat, fixtureBytes(t, "openai_params.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Params.MaxTokens == nil || *req.Params.MaxTokens != 1024 {
		t.Errorf("max_tokens = %v", req.Params.MaxTokens)
	}
	if req.Params.Temperature == nil || *req.Params.Temperature != 0.5 {
		t.Errorf("temperature = %v", req.Params.Temperature)
	}
	if req.Params.TopP == nil || *req.Params.TopP != 0.9 {
		t.Errorf("top_p = %v", req.Params.TopP)
	}
	if len(req.Params.Stop) != 2 || req.Params.Stop[0] != "END" {
		t.Errorf("stop = %v", req.Params.Stop)
	}
	if req.Params.Reasoning == nil || *req.Params.Reasoning.Effort != "medium" {
		t.Errorf("reasoning = %+v", req.Params.Reasoning)
	}
}

func TestDecodeOpenAI_StopSingleString(t *testing.T) {
	req, err := DecodeRequest(ProtocolOpenAIChat, fixtureBytes(t, "openai_stop_single.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(req.Params.Stop) != 1 || req.Params.Stop[0] != "END" {
		t.Errorf("stop = %v", req.Params.Stop)
	}
}

func TestDecodeOpenAI_Tools(t *testing.T) {
	req, err := DecodeRequest(ProtocolOpenAIChat, fixtureBytes(t, "openai_tools.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(req.Tools) != 1 {
		t.Fatalf("tools count = %d", len(req.Tools))
	}
	if req.Tools[0].Name != "get_weather" {
		t.Errorf("tool name = %q", req.Tools[0].Name)
	}
}

func TestDecodeOpenAI_MissingModel(t *testing.T) {
	_, err := DecodeRequest(ProtocolOpenAIChat, fixtureBytes(t, "openai_missing_model.json"))
	if err == nil {
		t.Fatal("expected error for missing model")
	}
}

// ---------------------------------------------------------------------------
// Anthropic decoding tests
// ---------------------------------------------------------------------------

func TestDecodeAnthropic_SimpleText(t *testing.T) {
	req, err := DecodeRequest(ProtocolAnthropicMessage, fixtureBytes(t, "anthropic_simple_text.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.ClientModel != "claude-3-7-sonnet-20250219" {
		t.Errorf("model = %q", req.ClientModel)
	}
	if len(req.Messages) != 1 || *req.Messages[0].Content[0].Text != "Hello" {
		t.Errorf("messages = %+v", req.Messages)
	}
}

func TestDecodeAnthropic_SystemString(t *testing.T) {
	req, err := DecodeRequest(ProtocolAnthropicMessage, fixtureBytes(t, "anthropic_system_string.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(req.System) != 1 || *req.System[0].Text != "You are helpful." {
		t.Errorf("system = %+v", req.System)
	}
}

func TestDecodeAnthropic_SystemArray(t *testing.T) {
	req, err := DecodeRequest(ProtocolAnthropicMessage, fixtureBytes(t, "anthropic_system_array.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(req.System) != 2 {
		t.Fatalf("system count = %d", len(req.System))
	}
	if *req.System[0].Text != "Be helpful." {
		t.Errorf("system[0] = %q", *req.System[0].Text)
	}
}

func TestDecodeAnthropic_Base64Image(t *testing.T) {
	req, err := DecodeRequest(ProtocolAnthropicMessage, fixtureBytes(t, "anthropic_base64_image.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	img := req.Messages[0].Content[0].Image
	if img.SourceType != "base64" {
		t.Errorf("source_type = %q", img.SourceType)
	}
	if *img.MediaType != "image/png" {
		t.Errorf("media_type = %q", *img.MediaType)
	}
	if *img.Data != "iVBOR" {
		t.Errorf("data = %q", *img.Data)
	}
}

func TestDecodeAnthropic_URLImage(t *testing.T) {
	req, err := DecodeRequest(ProtocolAnthropicMessage, fixtureBytes(t, "anthropic_url_image.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	img := req.Messages[0].Content[0].Image
	if img.SourceType != "url" {
		t.Errorf("source_type = %q", img.SourceType)
	}
	if *img.URL != "https://example.com/img.png" {
		t.Errorf("url = %q", *img.URL)
	}
}

func TestDecodeAnthropic_ToolUseAndResult(t *testing.T) {
	req, err := DecodeRequest(ProtocolAnthropicMessage, fixtureBytes(t, "anthropic_tool_use_result.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(req.Messages) != 3 {
		t.Fatalf("messages = %d", len(req.Messages))
	}

	// Tool use
	tc := req.Messages[1].Content[0]
	if tc.Type != "tool_call" {
		t.Errorf("type = %q", tc.Type)
	}
	if tc.ToolCall.ID != "toolu_123" {
		t.Errorf("id = %q", tc.ToolCall.ID)
	}

	// Tool result
	tr := req.Messages[2].Content[0]
	if tr.Type != "tool_result" {
		t.Errorf("type = %q", tr.Type)
	}
	if tr.ToolResult.ToolCallID != "toolu_123" {
		t.Errorf("tool_use_id = %q", tr.ToolResult.ToolCallID)
	}
}

func TestDecodeAnthropic_ThinkingConfig(t *testing.T) {
	req, err := DecodeRequest(ProtocolAnthropicMessage, fixtureBytes(t, "anthropic_thinking_config.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Params.Reasoning == nil {
		t.Fatal("reasoning should not be nil")
	}
	if req.Params.Reasoning.Mode != "enabled" {
		t.Errorf("mode = %q", req.Params.Reasoning.Mode)
	}
	if req.Params.Reasoning.BudgetTokens == nil || *req.Params.Reasoning.BudgetTokens != 10000 {
		t.Errorf("budget_tokens = %v", req.Params.Reasoning.BudgetTokens)
	}
}

func TestDecodeAnthropic_ThinkingBlock(t *testing.T) {
	req, err := DecodeRequest(ProtocolAnthropicMessage, fixtureBytes(t, "anthropic_thinking_block.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	blocks := req.Messages[0].Content
	if len(blocks) != 2 {
		t.Fatalf("blocks = %d", len(blocks))
	}
	if blocks[0].Type != "thinking" || *blocks[0].Thinking.Text != "Let me think about this..." {
		t.Errorf("thinking block = %+v", blocks[0])
	}
	if blocks[1].Type != "text" || *blocks[1].Text != "Here is my answer." {
		t.Errorf("text block = %+v", blocks[1])
	}
}

func TestDecodeAnthropic_MissingModel(t *testing.T) {
	_, err := DecodeRequest(ProtocolAnthropicMessage, fixtureBytes(t, "anthropic_missing_model.json"))
	if err == nil {
		t.Fatal("expected error for missing model")
	}
}

// ---------------------------------------------------------------------------
// Helper tests
// ---------------------------------------------------------------------------

func TestParseDataURL(t *testing.T) {
	tests := []struct {
		url       string
		wantMedia string
		wantData  string
		wantOK    bool
	}{
		{"data:image/png;base64,iVBOR", "image/png", "iVBOR", true},
		{"data:image/jpeg;base64,/9j/4", "image/jpeg", "/9j/4", true},
		{"https://example.com/img.png", "", "", false},
		{"data:text/plain;charset=utf-8,hello", "", "", false}, // no base64 marker
	}
	for _, tt := range tests {
		media, data, ok := parseDataURL(tt.url)
		if ok != tt.wantOK {
			t.Errorf("parseDataURL(%q) ok = %v, want %v", tt.url, ok, tt.wantOK)
			continue
		}
		if ok {
			if media != tt.wantMedia {
				t.Errorf("media = %q, want %q", media, tt.wantMedia)
			}
			if data != tt.wantData {
				t.Errorf("data = %q, want %q", data, tt.wantData)
			}
		}
	}
}

func TestDecodeUnsupportedProtocol(t *testing.T) {
	_, err := DecodeRequest("grpc", fixtureBytes(t, "unsupported_empty.json"))
	if err == nil {
		t.Fatal("expected error for unsupported protocol")
	}
}

// ---------------------------------------------------------------------------
// Round-trip verification: ensure JSON → canonical preserves semantics
// ---------------------------------------------------------------------------

func TestDecodeOpenAI_ToolArgsParsedAsJSON(t *testing.T) {
	req, err := DecodeRequest(ProtocolOpenAIChat, fixtureBytes(t, "openai_tool_args_json.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	tc := req.Messages[0].Content[0].ToolCall
	// Arguments should have been parsed to map/any, not kept as string.
	if _, ok := tc.Arguments.(map[string]any); !ok {
		b, _ := json.Marshal(tc.Arguments)
		t.Errorf("arguments should be map, got %T: %s", tc.Arguments, b)
	}
}
