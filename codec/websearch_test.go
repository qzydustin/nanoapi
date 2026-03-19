package codec

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/qzydustin/nanoapi/canonical"
)

func TestDecodeOpenAI_WebSearchTool(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4",
		"messages": [{"role": "user", "content": "search for latest news"}],
		"tools": [{"type": "web_search", "search_context_size": "high"}]
	}`)

	req, err := canonical.DecodeRequest(canonical.ProtocolOpenAIChat, body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(req.Tools) != 1 {
		t.Fatalf("tools = %d", len(req.Tools))
	}
	tool := req.Tools[0]
	if tool.Type != "web_search" {
		t.Errorf("type = %q, want web_search", tool.Type)
	}
	if tool.MaxUses == nil || *tool.MaxUses != 10 {
		t.Errorf("max_uses = %v, want 10", tool.MaxUses)
	}
}

func TestDecodeAnthropic_WebSearchTool(t *testing.T) {
	body := []byte(`{
		"model": "claude-opus-4",
		"max_tokens": 1024,
		"messages": [{"role": "user", "content": "search"}],
		"tools": [{"type": "web_search_20250305", "name": "web_search", "max_uses": 5}]
	}`)

	req, err := canonical.DecodeRequest(canonical.ProtocolAnthropicMessage, body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(req.Tools) != 1 {
		t.Fatalf("tools = %d", len(req.Tools))
	}
	tool := req.Tools[0]
	// Anthropic "web_search_20250305" is normalized to canonical "web_search"
	if tool.Type != "web_search" {
		t.Errorf("type = %q, want web_search", tool.Type)
	}
	if tool.MaxUses == nil || *tool.MaxUses != 5 {
		t.Errorf("max_uses = %v, want 5", tool.MaxUses)
	}
}

func TestEncodeAnthropic_WebSearchTool(t *testing.T) {
	maxUses := 10
	req := &canonical.CanonicalRequest{
		ClientProtocol: canonical.ProtocolOpenAIChat,
		ClientModel:    "gpt-4",
		Tools: []canonical.CanonicalTool{
			{Type: "web_search", MaxUses: &maxUses},
		},
		Messages: []canonical.CanonicalMessage{
			{Role: "user", Content: []canonical.CanonicalContentBlock{
				{Type: "text", Text: strPtr("search")},
			}},
		},
	}

	body, err := EncodeAnthropicRequest(req, "claude-opus-4", false, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var result map[string]any
	json.Unmarshal(body, &result)

	tools := result["tools"].([]any)
	tool := tools[0].(map[string]any)
	// Canonical "web_search" should become Anthropic "web_search_20250305"
	if tool["type"] != "web_search_20250305" {
		t.Errorf("type = %v, want web_search_20250305", tool["type"])
	}
	if tool["name"] != "web_search" {
		t.Errorf("name = %v, want web_search", tool["name"])
	}
	if tool["max_uses"] != float64(10) {
		t.Errorf("max_uses = %v, want 10", tool["max_uses"])
	}
}

func TestEncodeOpenAI_WebSearchTool(t *testing.T) {
	maxUses := 5
	req := &canonical.CanonicalRequest{
		ClientProtocol: canonical.ProtocolAnthropicMessage,
		ClientModel:    "claude-opus-4",
		Tools: []canonical.CanonicalTool{
			{Type: "web_search", MaxUses: &maxUses},
		},
		Messages: []canonical.CanonicalMessage{
			{Role: "user", Content: []canonical.CanonicalContentBlock{
				{Type: "text", Text: strPtr("search")},
			}},
		},
	}

	body, err := EncodeOpenAIRequest(req, "gpt-4", false, nil, "")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var result map[string]any
	json.Unmarshal(body, &result)

	tools := result["tools"].([]any)
	tool := tools[0].(map[string]any)
	if tool["type"] != "web_search" {
		t.Errorf("type = %v, want web_search", tool["type"])
	}
	if tool["search_context_size"] != "medium" {
		t.Errorf("search_context_size = %v, want medium", tool["search_context_size"])
	}
}

func TestEncodeOpenAI_WebSearchToolOpenWebUI(t *testing.T) {
	maxUses := 5
	req := &canonical.CanonicalRequest{
		ClientProtocol: canonical.ProtocolAnthropicMessage,
		ClientModel:    "claude-opus-4",
		Tools: []canonical.CanonicalTool{
			{Type: "web_search", MaxUses: &maxUses},
		},
		Messages: []canonical.CanonicalMessage{
			{Role: "user", Content: []canonical.CanonicalContentBlock{
				{Type: "text", Text: strPtr("search")},
			}},
		},
	}

	body, err := EncodeOpenAIRequest(req, "gpt-4", false, nil, "openwebui")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var result map[string]any
	json.Unmarshal(body, &result)

	if _, ok := result["tools"]; ok {
		t.Fatalf("tools should be omitted for openwebui, got %v", result["tools"])
	}
	features := result["features"].(map[string]any)
	if features["web_search"] != true {
		t.Fatalf("features.web_search = %v, want true", features["web_search"])
	}
}

func TestCrossProtocol_WebSearchOpenAIToAnthropic(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4",
		"messages": [{"role": "user", "content": "search"}],
		"tools": [{"type": "web_search", "search_context_size": "low"}]
	}`)

	req, err := canonical.DecodeRequest(canonical.ProtocolOpenAIChat, body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	out, err := EncodeAnthropicRequest(req, "claude-opus-4", false, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var result map[string]any
	json.Unmarshal(out, &result)

	tool := result["tools"].([]any)[0].(map[string]any)
	if tool["type"] != "web_search_20250305" {
		t.Errorf("type = %v, want web_search_20250305", tool["type"])
	}
	if tool["max_uses"] != float64(1) {
		t.Errorf("max_uses = %v, want 1", tool["max_uses"])
	}
}

func TestCrossProtocol_WebSearchAnthropicToOpenAI(t *testing.T) {
	body := []byte(`{
		"model": "claude-opus-4",
		"max_tokens": 1024,
		"messages": [{"role": "user", "content": "search"}],
		"tools": [{"type": "web_search_20250305", "name": "web_search", "max_uses": 10}]
	}`)

	req, err := canonical.DecodeRequest(canonical.ProtocolAnthropicMessage, body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	out, err := EncodeOpenAIRequest(req, "gpt-4", false, nil, "")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var result map[string]any
	json.Unmarshal(out, &result)

	tool := result["tools"].([]any)[0].(map[string]any)
	if tool["type"] != "web_search" {
		t.Errorf("type = %v, want web_search", tool["type"])
	}
	if tool["search_context_size"] != "high" {
		t.Errorf("search_context_size = %v, want high", tool["search_context_size"])
	}
}

// ---------------------------------------------------------------------------
// Response decode/encode tests for web search server tool blocks (raw passthrough)
// ---------------------------------------------------------------------------

func TestDecodeAnthropicResponse_WebSearch(t *testing.T) {
	body := []byte(`{
		"id": "msg_123",
		"type": "message",
		"model": "claude-opus-4-6",
		"role": "assistant",
		"content": [
			{"type": "server_tool_use", "id": "srvtoolu_abc", "name": "web_search", "input": {"query": "today date"}},
			{"type": "web_search_tool_result", "tool_use_id": "srvtoolu_abc", "content": [
				{"type": "web_search_result", "url": "https://example.com", "title": "Example", "encrypted_content": "enc123"}
			]},
			{"type": "text", "text": "Today is March 19."}
		],
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 100, "output_tokens": 50}
	}`)

	resp, err := DecodeAnthropicResponse(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Output) != 1 {
		t.Fatalf("output messages = %d, want 1", len(resp.Output))
	}
	blocks := resp.Output[0].Content
	if len(blocks) != 3 {
		t.Fatalf("blocks = %d, want 3", len(blocks))
	}

	// Block 0: server_tool_use preserved via RawJSON
	if blocks[0].Type != "server_tool_use" {
		t.Errorf("block[0].type = %q, want server_tool_use", blocks[0].Type)
	}
	if len(blocks[0].RawJSON) == 0 {
		t.Fatal("block[0].RawJSON is empty")
	}
	var b0 map[string]any
	json.Unmarshal(blocks[0].RawJSON, &b0)
	if b0["id"] != "srvtoolu_abc" {
		t.Errorf("block[0].id = %v, want srvtoolu_abc", b0["id"])
	}

	// Block 1: web_search_tool_result preserved via RawJSON
	if blocks[1].Type != "web_search_tool_result" {
		t.Errorf("block[1].type = %q, want web_search_tool_result", blocks[1].Type)
	}
	if len(blocks[1].RawJSON) == 0 {
		t.Fatal("block[1].RawJSON is empty")
	}
	var b1 map[string]any
	json.Unmarshal(blocks[1].RawJSON, &b1)
	if b1["tool_use_id"] != "srvtoolu_abc" {
		t.Errorf("block[1].tool_use_id = %v, want srvtoolu_abc", b1["tool_use_id"])
	}

	// Block 2: text
	if blocks[2].Type != "text" || *blocks[2].Text != "Today is March 19." {
		t.Errorf("block[2] = %q / %v", blocks[2].Type, blocks[2].Text)
	}
}

func TestEncodeAnthropicClientResponse_WebSearch(t *testing.T) {
	resp := &canonical.CanonicalResponse{
		ID:         "msg_123",
		Model:      "claude-opus-4-6",
		StopReason: "end_turn",
		Output: []canonical.CanonicalMessage{{
			Role: "assistant",
			Content: []canonical.CanonicalContentBlock{
				{
					Type:    "server_tool_use",
					RawJSON: json.RawMessage(`{"type":"server_tool_use","id":"srvtoolu_abc","name":"web_search","input":{"query":"today"}}`),
				},
				{
					Type:    "web_search_tool_result",
					RawJSON: json.RawMessage(`{"type":"web_search_tool_result","tool_use_id":"srvtoolu_abc","content":[{"type":"web_search_result","url":"https://example.com","title":"Ex"}]}`),
				},
				{Type: "text", Text: strPtr("Today is March 19.")},
			},
		}},
	}

	body, err := EncodeAnthropicClientResponse(resp)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var result map[string]any
	json.Unmarshal(body, &result)

	content := result["content"].([]any)
	if len(content) != 3 {
		t.Fatalf("content len = %d, want 3", len(content))
	}

	// Block 0: server_tool_use
	b0 := content[0].(map[string]any)
	if b0["type"] != "server_tool_use" {
		t.Errorf("content[0].type = %v, want server_tool_use", b0["type"])
	}
	if b0["id"] != "srvtoolu_abc" {
		t.Errorf("content[0].id = %v, want srvtoolu_abc", b0["id"])
	}

	// Block 1: web_search_tool_result
	b1 := content[1].(map[string]any)
	if b1["type"] != "web_search_tool_result" {
		t.Errorf("content[1].type = %v, want web_search_tool_result", b1["type"])
	}
	if b1["tool_use_id"] != "srvtoolu_abc" {
		t.Errorf("content[1].tool_use_id = %v, want srvtoolu_abc", b1["tool_use_id"])
	}
	resultContent := b1["content"].([]any)
	if len(resultContent) != 1 {
		t.Fatalf("content[1].content len = %d, want 1", len(resultContent))
	}

	// Block 2: text
	b2 := content[2].(map[string]any)
	if b2["type"] != "text" {
		t.Errorf("content[2].type = %v, want text", b2["type"])
	}
}

func TestWebSearchStream_DecodeEncode(t *testing.T) {
	decoder := NewAnthropicStreamDecoder()
	encoder := NewAnthropicStreamEncoder()

	// 1. message_start
	events, _ := decoder.DecodeLine("message_start", `{"message":{"id":"msg_1","model":"claude","usage":{"input_tokens":0,"output_tokens":0}}}`)
	for _, e := range events {
		encoder.Encode(e)
	}

	// 2. server_tool_use start → EventRawBlockStart
	events, _ = decoder.DecodeLine("content_block_start", `{"index":0,"content_block":{"type":"server_tool_use","id":"srvtoolu_1","name":"web_search","input":{}}}`)
	if len(events) != 1 || events[0].Type != canonical.EventRawBlockStart {
		t.Fatalf("expected EventRawBlockStart, got %v", events)
	}
	for _, e := range events {
		encoder.Encode(e)
	}

	// 3. input_json_delta for server_tool_use → EventRawBlockDelta
	events, _ = decoder.DecodeLine("content_block_delta", `{"index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"test\"}"}}`)
	if len(events) != 1 || events[0].Type != canonical.EventRawBlockDelta {
		t.Fatalf("expected EventRawBlockDelta, got %v", events)
	}
	for _, e := range events {
		encoder.Encode(e)
	}

	// 4. content_block_stop for server_tool_use → EventRawBlockStop
	events, _ = decoder.DecodeLine("content_block_stop", `{"index":0}`)
	if len(events) != 1 || events[0].Type != canonical.EventRawBlockStop {
		t.Fatalf("expected EventRawBlockStop, got %v", events)
	}
	for _, e := range events {
		encoder.Encode(e)
	}

	// 5. web_search_tool_result start → EventRawBlockStart
	events, _ = decoder.DecodeLine("content_block_start", `{"index":1,"content_block":{"type":"web_search_tool_result","tool_use_id":"srvtoolu_1","content":[{"type":"web_search_result","url":"https://ex.com","title":"Ex","encrypted_content":"enc"}]}}`)
	if len(events) != 1 || events[0].Type != canonical.EventRawBlockStart {
		t.Fatalf("expected EventRawBlockStart, got %v", events)
	}
	output := encoder.Encode(events[0])

	if !strings.Contains(output, "web_search_tool_result") {
		t.Errorf("encoded output missing web_search_tool_result: %s", output)
	}
	if !strings.Contains(output, "srvtoolu_1") {
		t.Errorf("encoded output missing tool_use_id: %s", output)
	}

	// 6. content_block_stop for web_search_tool_result
	events, _ = decoder.DecodeLine("content_block_stop", `{"index":1}`)
	for _, e := range events {
		encoder.Encode(e)
	}
}
