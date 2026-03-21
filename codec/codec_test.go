package codec

import (
	"encoding/json"
	"testing"

	"github.com/qzydustin/nanoapi/config"
)

func ip(i int) *int         { return &i }
func fp(f float64) *float64 { return &f }


func TestEncodeOpenAI_SimpleText(t *testing.T) {
	req := &Request{
		ClientModel: "gpt-4o",
		System:      []ContentBlock{{Type: "text", Text: strPtr("Be helpful.")}},
		Messages: []Message{
			{Role: "user", Content: []ContentBlock{
				{Type: "text", Text: strPtr("Hello")},
			}},
		},
		Params: Params{
			MaxTokens:   ip(1024),
			Temperature: fp(0.5),
		},
	}

	body, err := EncodeOpenAIRequest(req, "gpt-4o", false, nil)
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

func TestEncodeOpenAI_ImageBase64(t *testing.T) {
	req := &Request{
		Messages: []Message{
			{Role: "user", Content: []ContentBlock{
				{Type: "image", Image: &Image{
					MediaType: "image/png",
					Data:      "iVBOR",
				}},
			}},
		},
	}

	body, err := EncodeOpenAIRequest(req, "gpt-4o", false, nil)
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
	req := &Request{
		Messages: []Message{
			{Role: "assistant", Content: []ContentBlock{
				{Type: "tool_call", ToolCall: &ToolCall{
					ID: "call_1", Name: "get_weather",
					Arguments: map[string]any{"location": "NYC"},
				}},
			}},
		},
	}

	body, err := EncodeOpenAIRequest(req, "gpt-4o", false, nil)
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

func TestEncodeOpenAI_InfersDummyToolsFromHistory(t *testing.T) {
	req := &Request{
		Messages: []Message{
			{Role: "assistant", Content: []ContentBlock{
				{Type: "tool_call", ToolCall: &ToolCall{
					ID: "call_1", Name: "webfetch",
					Arguments: map[string]any{"url": "https://example.com"},
				}},
			}},
			{Role: "tool", Content: []ContentBlock{
				{Type: "tool_result", ToolResult: &ToolResult{
					ToolCallID: "call_1",
					Content:    []ContentBlock{{Type: "text", Text: strPtr("ok")}},
				}},
			}},
			{Role: "assistant", Content: []ContentBlock{
				{Type: "tool_call", ToolCall: &ToolCall{
					ID: "call_2", Name: "webfetch",
					Arguments: map[string]any{"url": "https://example.org"},
				}},
			}},
		},
	}

	body, err := EncodeOpenAIRequest(req, "gpt-4o", false, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	tools, ok := raw["tools"].([]any)
	if !ok {
		t.Fatalf("tools missing or wrong type: %T", raw["tools"])
	}
	if len(tools) != 1 {
		t.Fatalf("tools count = %d, want 1 (deduplicated)", len(tools))
	}
	tool := tools[0].(map[string]any)
	if tool["type"] != "function" {
		t.Errorf("tool.type = %v", tool["type"])
	}
	fn := tool["function"].(map[string]any)
	if fn["name"] != "webfetch" {
		t.Errorf("tool.function.name = %v", fn["name"])
	}
}

func TestEncodeOpenAI_NoToolsInferredWithoutHistory(t *testing.T) {
	req := &Request{
		Messages: []Message{
			{Role: "user", Content: []ContentBlock{
				{Type: "text", Text: strPtr("Hello")},
			}},
		},
	}

	body, err := EncodeOpenAIRequest(req, "gpt-4o", false, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	if _, ok := raw["tools"]; ok {
		t.Fatalf("tools should be omitted when no tool history, got %v", raw["tools"])
	}
}

func TestEncodeOpenAI_ExplicitToolsNotOverriddenByInference(t *testing.T) {
	req := &Request{
		Tools: []Tool{
			{Name: "calculator", Description: "Does math", InputSchema: map[string]any{"type": "object"}},
		},
		Messages: []Message{
			{Role: "assistant", Content: []ContentBlock{
				{Type: "tool_call", ToolCall: &ToolCall{
					ID: "call_1", Name: "calculator",
					Arguments: map[string]any{"expr": "1+1"},
				}},
			}},
		},
	}

	body, err := EncodeOpenAIRequest(req, "gpt-4o", false, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	tools := raw["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools count = %d, want 1", len(tools))
	}
	fn := tools[0].(map[string]any)["function"].(map[string]any)
	if fn["description"] != "Does math" {
		t.Errorf("should use explicit tool, not inferred; description = %v", fn["description"])
	}
}


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

func TestDecodeOpenAIResponse_UsageCacheFields(t *testing.T) {
	body := []byte(`{
  "id": "chatcmpl-cache",
  "model": "gpt-4o",
  "choices": [
    {
      "message": {
        "role": "assistant",
        "content": "OK"
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 15,
    "completion_tokens": 4,
    "total_tokens": 19,
    "cache_creation_input_tokens": 7,
    "cache_read_input_tokens": 9
  }
}`)

	resp, err := DecodeOpenAIResponse(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Usage == nil {
		t.Fatal("usage should not be nil")
	}
	if resp.Usage.CacheWriteTokens == nil || *resp.Usage.CacheWriteTokens != 7 {
		t.Fatalf("cache_write_tokens = %+v", resp.Usage)
	}
	if resp.Usage.CacheReadTokens == nil || *resp.Usage.CacheReadTokens != 9 {
		t.Fatalf("cache_read_tokens = %+v", resp.Usage)
	}
}

func TestDecodeAnthropic_ReasoningEffort(t *testing.T) {
	body := []byte(`{
		"model": "claude-opus-4-6",
		"max_tokens": 4096,
		"thinking": {"type": "adaptive"},
		"output_config": {"effort": "max"},
		"messages": [{"role": "user", "content": "hello"}]
	}`)

	req, err := DecodeAnthropicRequest(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Params.Reasoning == nil {
		t.Fatal("reasoning should not be nil")
	}
	if req.Params.Reasoning.Mode != "adaptive" {
		t.Fatalf("reasoning.mode = %q", req.Params.Reasoning.Mode)
	}
	if req.Params.Reasoning.Effort == nil || *req.Params.Reasoning.Effort != "max" {
		t.Fatalf("reasoning.effort = %+v", req.Params.Reasoning.Effort)
	}
}

func TestCrossProtocol_AnthropicToOpenAI(t *testing.T) {
	// Decode an Anthropic request
	req, err := DecodeAnthropicRequest(fixtureBytes(t, "cross_anthropic_request.json"))
	if err != nil {
		t.Fatalf("decode anthropic: %v", err)
	}

	// Encode as OpenAI request
	openaiBody, err := EncodeOpenAIRequest(req, "gpt-4o", false, nil)
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
	req, err := DecodeAnthropicRequest(fixtureBytes(t, "cross_anthropic_thinking_request.json"))
	if err != nil {
		t.Fatalf("decode anthropic: %v", err)
	}

	capability := &config.ReasoningCapability{AllowedEfforts: []string{"low", "medium", "high"}}
	openaiBody, err := EncodeOpenAIRequest(req, "gpt-4o", false, capability)
	if err != nil {
		t.Fatalf("encode openai: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(openaiBody, &raw)
	if raw["reasoning_effort"] != "medium" {
		t.Errorf("reasoning_effort = %v", raw["reasoning_effort"])
	}
}

func TestCrossProtocol_AnthropicToolChoiceToOpenAI(t *testing.T) {
	body := []byte(`{
		"model": "claude-opus-4-6",
		"tool_choice": {"type": "any"},
		"tools": [{
			"name": "heartbeat",
			"description": "Report status",
			"input_schema": {"type": "object"}
		}],
		"messages": [{"role": "user", "content": "hello"}]
	}`)

	req, err := DecodeAnthropicRequest(body)
	if err != nil {
		t.Fatalf("decode anthropic: %v", err)
	}

	openaiBody, err := EncodeOpenAIRequest(req, "gpt-4o", false, nil)
	if err != nil {
		t.Fatalf("encode openai: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(openaiBody, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if raw["tool_choice"] != "required" {
		t.Fatalf("tool_choice = %v, want required", raw["tool_choice"])
	}
}

func TestCrossProtocol_AnthropicStructuredOutputToOpenAIResponseFormat(t *testing.T) {
	body := []byte(`{
		"model": "claude-haiku-4-5",
		"messages": [{"role": "user", "content": "hello"}],
		"output_config": {
			"format": {
				"type": "json_schema",
				"schema": {
					"type": "object",
					"properties": {"title": {"type": "string"}},
					"required": ["title"],
					"additionalProperties": false
				}
			}
		}
	}`)

	req, err := DecodeAnthropicRequest(body)
	if err != nil {
		t.Fatalf("decode anthropic: %v", err)
	}
	if req.Params.ResponseFormat == nil {
		t.Fatal("response_format should not be nil")
	}

	openaiBody, err := EncodeOpenAIRequest(req, "gpt-4o", false, nil)
	if err != nil {
		t.Fatalf("encode openai: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(openaiBody, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	responseFormat, ok := raw["response_format"].(map[string]any)
	if !ok {
		t.Fatalf("response_format missing or wrong type: %T", raw["response_format"])
	}
	if responseFormat["type"] != "json_schema" {
		t.Fatalf("response_format.type = %v", responseFormat["type"])
	}
	jsonSchema, ok := responseFormat["json_schema"].(map[string]any)
	if !ok {
		t.Fatalf("response_format.json_schema missing or wrong type: %T", responseFormat["json_schema"])
	}
	if jsonSchema["name"] != "structured_output" {
		t.Fatalf("json_schema.name = %v", jsonSchema["name"])
	}
	schema, ok := jsonSchema["schema"].(map[string]any)
	if !ok {
		t.Fatalf("json_schema.schema missing or wrong type: %T", jsonSchema["schema"])
	}
	if schema["type"] != "object" {
		t.Fatalf("json_schema.schema.type = %v", schema["type"])
	}
}

func TestDecodeAnthropic_DropsUnknownRequestBlocks(t *testing.T) {
	body := []byte(`{
		"model": "claude-opus-4-6",
		"messages": [{
			"role": "assistant",
			"content": [{"type": "redacted_thinking", "data": "hidden"}]
		}]
	}`)

	req, err := DecodeAnthropicRequest(body)
	if err != nil {
		t.Fatalf("decode anthropic: %v", err)
	}
	if len(req.Messages) != 0 {
		t.Fatalf("messages count = %d, want 0", len(req.Messages))
	}
}


func TestEncodeOpenAI_ToolResult(t *testing.T) {
	req := &Request{
		Messages: []Message{
			{Role: "tool", Content: []ContentBlock{
				{Type: "tool_result", ToolResult: &ToolResult{
					ToolCallID: "call_1",
					Content:    []ContentBlock{{Type: "text", Text: strPtr("Sunny, 72°F")}},
				}},
			}},
		},
	}

	body, err := EncodeOpenAIRequest(req, "gpt-4o", false, nil)
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

func TestEncodeOpenAI_ToolResultErrorPrefix(t *testing.T) {
	req := &Request{
		Messages: []Message{
			{Role: "tool", Content: []ContentBlock{
				{Type: "tool_result", ToolResult: &ToolResult{
					ToolCallID: "call_1",
					IsError:    true,
					Content:    []ContentBlock{{Type: "text", Text: strPtr("File does not exist")}},
				}},
			}},
		},
	}

	body, err := EncodeOpenAIRequest(req, "gpt-4o", false, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	msgs := raw["messages"].([]any)
	msg := msgs[0].(map[string]any)
	if msg["content"] != "Error: File does not exist" {
		t.Fatalf("content = %v", msg["content"])
	}
}

func TestEncodeOpenAI_ToolResultPreservesImageContent(t *testing.T) {
	req := &Request{
		Messages: []Message{
			{Role: "tool", Content: []ContentBlock{
				{Type: "tool_result", ToolResult: &ToolResult{
					ToolCallID: "call_img",
					Content: []ContentBlock{
						{Type: "text", Text: strPtr("look at this")},
						{Type: "image", Image: &Image{
							MediaType: "image/png",
							Data:      "iVBORw0KGgoAAAANSUhEUg==",
						}},
					},
				}},
			}},
		},
	}

	body, err := EncodeOpenAIRequest(req, "gpt-4o", false, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	msgs := raw["messages"].([]any)
	msg := msgs[0].(map[string]any)
	content := msg["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("content count = %d, want 2", len(content))
	}
	img := content[1].(map[string]any)
	if img["type"] != "image_url" {
		t.Fatalf("content[1].type = %v", img["type"])
	}
	imageURL := img["image_url"].(map[string]any)
	if imageURL["url"] != "data:image/png;base64,iVBORw0KGgoAAAANSUhEUg==" {
		t.Fatalf("image_url.url = %v", imageURL["url"])
	}
}

func TestEncodeOpenAI_UserToolResultComesBeforeUserText(t *testing.T) {
	req := &Request{
		Messages: []Message{
			{Role: "user", Content: []ContentBlock{
				{Type: "tool_result", ToolResult: &ToolResult{
					ToolCallID: "call_1",
					Content:    []ContentBlock{{Type: "text", Text: strPtr("Sunny, 72°F")}},
				}},
				{Type: "text", Text: strPtr("What should I wear?")},
			}},
		},
	}

	body, err := EncodeOpenAIRequest(req, "gpt-4o", false, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	msgs := raw["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages count = %d, want 2", len(msgs))
	}

	toolMsg := msgs[0].(map[string]any)
	if toolMsg["role"] != "tool" {
		t.Fatalf("first role = %v, want tool", toolMsg["role"])
	}
	if toolMsg["tool_call_id"] != "call_1" {
		t.Fatalf("first tool_call_id = %v", toolMsg["tool_call_id"])
	}

	userMsg := msgs[1].(map[string]any)
	if userMsg["role"] != "user" {
		t.Fatalf("second role = %v, want user", userMsg["role"])
	}
	if userMsg["content"] != "What should I wear?" {
		t.Fatalf("second content = %v", userMsg["content"])
	}
}

func TestEncodeOpenAI_AssistantToolCallsIncludeEmptyContent(t *testing.T) {
	req := &Request{
		Messages: []Message{
			{Role: "assistant", Content: []ContentBlock{
				{Type: "tool_call", ToolCall: &ToolCall{
					ID: "call_1", Name: "get_weather", Arguments: map[string]any{"city": "Phoenix"},
				}},
			}},
		},
	}

	body, err := EncodeOpenAIRequest(req, "gpt-4o", false, nil)
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
	if msg["content"] != "" {
		t.Fatalf("content = %#v, want empty string", msg["content"])
	}
}

func TestEncodeOpenAI_SkipsAssistantThinkingOnlyMessage(t *testing.T) {
	req := &Request{
		Messages: []Message{
			{Role: "assistant", Content: []ContentBlock{
				{Type: "thinking", Thinking: &ThinkingBlock{Text: strPtr("internal only")}},
			}},
			{Role: "user", Content: []ContentBlock{
				{Type: "text", Text: strPtr("hello")},
			}},
		},
	}

	body, err := EncodeOpenAIRequest(req, "gpt-4o", false, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	msgs := raw["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages count = %d, want 1", len(msgs))
	}
	if msgs[0].(map[string]any)["role"] != "user" {
		t.Fatalf("role = %v, want user", msgs[0].(map[string]any)["role"])
	}
}


func TestEncodeOpenAI_ReasoningEffort(t *testing.T) {
	req := &Request{
		Messages: []Message{
			{Role: "user", Content: []ContentBlock{
				{Type: "text", Text: strPtr("Think hard")},
			}},
		},
		Params: Params{
			Reasoning: &Reasoning{
				Effort: strPtr("high"),
			},
		},
	}

	capability := &config.ReasoningCapability{AllowedEfforts: []string{"low", "medium", "high"}}
	body, err := EncodeOpenAIRequest(req, "gpt-4o", false, capability)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	if raw["reasoning_effort"] != "high" {
		t.Errorf("reasoning_effort = %v", raw["reasoning_effort"])
	}
}

func TestEncodeOpenAI_ReasoningEffortMaxMapsToHigh(t *testing.T) {
	req := &Request{
		Messages: []Message{
			{Role: "user", Content: []ContentBlock{
				{Type: "text", Text: strPtr("Think harder")},
			}},
		},
		Params: Params{
			Reasoning: &Reasoning{
				Effort: strPtr("max"),
			},
		},
	}

	capability := &config.ReasoningCapability{
		AllowedEfforts: []string{"low", "medium", "high"},
		EffortMap:      map[string]string{"max": "high"},
	}
	body, err := EncodeOpenAIRequest(req, "gpt-4o", false, capability)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	if raw["reasoning_effort"] != "high" {
		t.Errorf("reasoning_effort = %v, want high", raw["reasoning_effort"])
	}
}

func TestEncodeOpenAI_DisabledThinkingOmitsReasoningEffort(t *testing.T) {
	req := &Request{
		Messages: []Message{
			{Role: "user", Content: []ContentBlock{
				{Type: "text", Text: strPtr("No thinking")},
			}},
		},
		Params: Params{
			Reasoning: &Reasoning{
				Mode:   "disabled",
				Effort: strPtr("high"),
			},
		},
	}

	capability := &config.ReasoningCapability{AllowedEfforts: []string{"low", "medium", "high"}}
	body, err := EncodeOpenAIRequest(req, "gpt-4o", false, capability)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(body, &raw)
	if _, ok := raw["reasoning_effort"]; ok {
		t.Errorf("reasoning_effort should be omitted for disabled mode, got %v", raw["reasoning_effort"])
	}
}


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

func TestDecodeOpenAIResponse_ThinkingBlocks(t *testing.T) {
	body := []byte(`{
  "id": "chatcmpl-thinking",
  "model": "us.anthropic.claude-opus-4-6-v1",
  "choices": [
    {
      "message": {
        "role": "assistant",
        "content": "pong",
        "thinking_blocks": [
          {
            "type": "thinking",
            "thinking": "Let me think...",
            "signature": "sig_abc"
          }
        ]
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 10,
    "completion_tokens": 20,
    "total_tokens": 30
  }
}`)

	resp, err := DecodeOpenAIResponse(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	blocks := resp.Output[0].Content
	if len(blocks) != 2 {
		t.Fatalf("blocks count = %d, want 2", len(blocks))
	}
	if blocks[0].Type != "thinking" {
		t.Fatalf("blocks[0].type = %q", blocks[0].Type)
	}
	if blocks[0].Thinking == nil || blocks[0].Thinking.Text == nil || *blocks[0].Thinking.Text != "Let me think..." {
		t.Fatalf("thinking text = %+v", blocks[0].Thinking)
	}
	if blocks[0].Thinking.Signature == nil || *blocks[0].Thinking.Signature != "sig_abc" {
		t.Fatalf("thinking signature = %+v", blocks[0].Thinking)
	}
	if blocks[1].Type != "text" || blocks[1].Text == nil || *blocks[1].Text != "pong" {
		t.Fatalf("text block = %+v", blocks[1])
	}
}


func TestEncodeAnthropicClientResponse_ThinkingAndToolUse(t *testing.T) {
	in64 := int64(10)
	out64 := int64(20)
	cacheCreate64 := int64(4)
	cacheRead64 := int64(6)
	resp := &Response{
		ID: "msg_123", Model: "claude-3-7-sonnet-20250219", StopReason: "tool_use",
		Output: []Message{
			{Role: "assistant", Content: []ContentBlock{
				{Type: "thinking", Thinking: &ThinkingBlock{Text: strPtr("Let me think..."), Signature: strPtr("sig_123")}},
				{Type: "tool_call", ToolCall: &ToolCall{
					ID: "toolu_1", Name: "get_weather",
					Arguments: map[string]any{"location": "NYC"},
				}},
			}},
		},
		Usage: &Usage{
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
	if stopSequence, ok := raw["stop_sequence"]; !ok {
		t.Errorf("stop_sequence key missing")
	} else if stopSequence != nil {
		t.Errorf("stop_sequence = %v, want null", stopSequence)
	}
	content := raw["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("content count = %d, want 2", len(content))
	}
	if content[0].(map[string]any)["type"] != "thinking" {
		t.Errorf("content[0].type = %v", content[0].(map[string]any)["type"])
	}
	if content[0].(map[string]any)["signature"] != "sig_123" {
		t.Errorf("content[0].signature = %v", content[0].(map[string]any)["signature"])
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

func TestNormalizeResponse(t *testing.T) {
	resp := &Response{
		ID:    "chatcmpl-123",
		Model: "us.anthropic.claude-opus-4-6-v1",
	}

	NormalizeResponse(resp, "msg_test123", "claude-opus-4-6")

	if resp.ID != "msg_test123" {
		t.Fatalf("id = %q", resp.ID)
	}
	if resp.Model != "claude-opus-4-6" {
		t.Fatalf("model = %q", resp.Model)
	}
}
