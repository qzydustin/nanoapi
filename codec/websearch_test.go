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
		"tools": [{"type": "web_search_20250305", "name": "web_search"}]
	}`)

	req, err := DecodeAnthropicRequest(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(req.Tools) != 1 {
		t.Fatalf("tools = %d", len(req.Tools))
	}
	if req.Tools[0].Type != "web_search" {
		t.Errorf("type = %q, want web_search", req.Tools[0].Type)
	}
}

func TestEncodeOpenAI_WebSearchUsesFeatureFlag(t *testing.T) {
	req := &Request{
		ClientModel: "claude-opus-4",
		Tools: []Tool{
			{Type: "web_search"},
		},
		Messages: []Message{
			{Role: "user", Content: []ContentBlock{
				{Type: "text", Text: strPtr("search")},
			}},
		},
	}

	body, err := EncodeOpenAIRequest(req, "gpt-4", false, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var result map[string]any
	json.Unmarshal(body, &result)

	if _, ok := result["tools"]; ok {
		t.Fatalf("tools should be omitted, got %v", result["tools"])
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
		"tools": [{"type": "web_search_20250305", "name": "web_search"}]
	}`)

	req, err := DecodeAnthropicRequest(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	out, err := EncodeOpenAIRequest(req, "gpt-4", false, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var result map[string]any
	json.Unmarshal(out, &result)

	if _, ok := result["tools"]; ok {
		t.Fatalf("tools should be omitted, got %v", result["tools"])
	}
	features := result["features"].(map[string]any)
	if features["web_search"] != true {
		t.Fatalf("features.web_search = %v, want true", features["web_search"])
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
					Type:               "web_search",
					WebSearchToolUseID: "srvtoolu_abc",
					WebSearchResults: []WebSearchResult{
						{URL: "https://example.com", Title: "Ex"},
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

	b0 := content[0].(map[string]any)
	if b0["type"] != "server_tool_use" {
		t.Errorf("content[0].type = %v, want server_tool_use", b0["type"])
	}
	if b0["id"] != "srvtoolu_abc" {
		t.Errorf("content[0].id = %v, want srvtoolu_abc", b0["id"])
	}

	b1 := content[1].(map[string]any)
	if b1["type"] != "web_search_tool_result" {
		t.Errorf("content[1].type = %v, want web_search_tool_result", b1["type"])
	}

	b2 := content[2].(map[string]any)
	if b2["type"] != "text" {
		t.Errorf("content[2].type = %v, want text", b2["type"])
	}
}
