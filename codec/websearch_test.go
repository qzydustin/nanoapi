package codec

import (
	"encoding/json"
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
