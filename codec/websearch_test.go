package codec

import (
	"encoding/json"
	"testing"
)

func TestDecodeAnthropic_WebSearchTool(t *testing.T) {
	body := []byte(`{
		"model": "claude-opus-4",
		"max_tokens": 1024,
		"messages": [{"role": "user", "content": "search"}],
		"tools": [{"type": "web_search_20250305", "name": "web_search", "max_uses": 5}]
	}`)

	req, err := DecodeAnthropicRequest(body)
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

func TestEncodeOpenAI_WebSearchTool(t *testing.T) {
	maxUses := 5
	req := &Request{
		ClientModel: "claude-opus-4",
		Tools: []Tool{
			{Type: "web_search", MaxUses: &maxUses},
		},
		Messages: []Message{
			{Role: "user", Content: []ContentBlock{
				{Type: "text", Text: strPtr("search")},
			}},
		},
	}

	body, err := EncodeOpenAIRequest(req, "gpt-4", false, nil, false)
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
	req := &Request{
		ClientModel: "claude-opus-4",
		Tools: []Tool{
			{Type: "web_search", MaxUses: &maxUses},
		},
		Messages: []Message{
			{Role: "user", Content: []ContentBlock{
				{Type: "text", Text: strPtr("search")},
			}},
		},
	}

	body, err := EncodeOpenAIRequest(req, "gpt-4", false, nil, true)
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

func TestCrossProtocol_WebSearchAnthropicToOpenAI(t *testing.T) {
	body := []byte(`{
		"model": "claude-opus-4",
		"max_tokens": 1024,
		"messages": [{"role": "user", "content": "search"}],
		"tools": [{"type": "web_search_20250305", "name": "web_search", "max_uses": 10}]
	}`)

	req, err := DecodeAnthropicRequest(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	out, err := EncodeOpenAIRequest(req, "gpt-4", false, nil, false)
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

func TestEncodeAnthropicClientResponse_WebSearch(t *testing.T) {
	resp := &Response{
		ID:         "msg_123",
		Model:      "claude-opus-4-6",
		StopReason: "end_turn",
		Output: []Message{{
			Role: "assistant",
			Content: []ContentBlock{
				{
					Type: "server_tool_use",
					ServerToolUse: &ServerToolUse{
						ID:    "srvtoolu_abc",
						Name:  "web_search",
						Input: map[string]any{"query": "today"},
					},
				},
				{
					Type: "web_search_tool_result",
					WebSearchToolResult: &WebSearchToolResult{
						ToolUseID: "srvtoolu_abc",
						Content: []WebSearchResult{
							{URL: "https://example.com", Title: "Ex"},
						},
					},
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
