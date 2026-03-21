package codec

import (
	"encoding/json"
	"fmt"

	"github.com/qzydustin/nanoapi/config"
)

type openAIOutRequest struct {
	Model           string             `json:"model"`
	Messages        []openAIOutMsg     `json:"messages"`
	Stream          bool               `json:"stream,omitempty"`
	MaxTokens       *int               `json:"max_completion_tokens,omitempty"`
	Temperature     *float64           `json:"temperature,omitempty"`
	TopP            *float64           `json:"top_p,omitempty"`
	Stop            []string           `json:"stop,omitempty"`
	Tools           *[]openAIOutTool   `json:"tools,omitempty"`
	ToolChoice      any                `json:"tool_choice,omitempty"`
	ResponseFormat  any                `json:"response_format,omitempty"`
	ReasoningEffort *string            `json:"reasoning_effort,omitempty"`
	StreamOptions   *streamOptions     `json:"stream_options,omitempty"`
	Features        *openAIOutFeatures `json:"features,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type openAIOutFeatures struct {
	WebSearch bool `json:"web_search,omitempty"`
}

type openAIOutMsg struct {
	Role       string        `json:"role"`
	Content    any           `json:"content,omitempty"`
	ToolCalls  []openAIOutTC `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type openAIOutContentPart struct {
	Type     string             `json:"type"`
	Text     string             `json:"text,omitempty"`
	ImageURL *openAIOutImageURL `json:"image_url,omitempty"`
}

type openAIOutImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail"`
}

type openAIOutTC struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function openAIOutTCFunc `json:"function"`
}

type openAIOutTCFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIOutTool struct {
	Type              string            `json:"type"`
	Function          openAIOutToolFunc `json:"function,omitempty"`
	SearchContextSize string            `json:"search_context_size,omitempty"`
}

type openAIOutToolFunc struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

// EncodeOpenAIRequest encodes a canonical request as an OpenAI chat request.
func EncodeOpenAIRequest(req *Request, upstreamModel string, stream bool, capability *config.ReasoningCapability, openwebUIWebSearch bool) ([]byte, error) {
	out := openAIOutRequest{
		Model:       upstreamModel,
		Stream:      stream,
		MaxTokens:   req.Params.MaxTokens,
		Temperature: req.Params.Temperature,
		TopP:        req.Params.TopP,
		Stop:        req.Params.Stop,
	}

	if req.ToolChoice != nil {
		out.ToolChoice = encodeOpenAIToolChoice(req.ToolChoice)
	}
	if req.Params.ResponseFormat != nil {
		out.ResponseFormat = encodeOpenAIResponseFormat(req.Params.ResponseFormat)
	}

	if stream {
		out.StreamOptions = &streamOptions{IncludeUsage: true}
	}

	if r := req.Params.Reasoning; r != nil {
		if effort, ok := buildOpenAIReasoning(r, capability); ok {
			out.ReasoningEffort = &effort
		}
	}

	var encodedTools []openAIOutTool
	enableOpenWebUISearch := false
	for _, t := range req.Tools {
		if t.Type == "web_search" {
			if openwebUIWebSearch {
				enableOpenWebUISearch = true
				continue
			}
			tool := openAIOutTool{Type: "web_search"}
			if t.MaxUses != nil {
				tool.SearchContextSize = maxUsesToSearchContextSize(*t.MaxUses)
			}
			encodedTools = append(encodedTools, tool)
			continue
		}

		encodedTools = append(encodedTools, openAIOutTool{
			Type: "function",
			Function: openAIOutToolFunc{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}
	if len(encodedTools) > 0 {
		out.Tools = &encodedTools
	}
	if enableOpenWebUISearch {
		out.Features = &openAIOutFeatures{WebSearch: true}
	}

	for _, sys := range req.System {
		if sys.Type == "text" && sys.Text != nil {
			out.Messages = append(out.Messages, openAIOutMsg{
				Role:    "system",
				Content: *sys.Text,
			})
		}
	}

	for _, m := range req.Messages {
		msg := encodeOpenAIMessage(m)
		out.Messages = append(out.Messages, msg...)
	}

	// Some OpenAI-compatible backends (e.g. LiteLLM/Bedrock) reject historical
	// tool_calls unless the request also includes a tools definition. When the
	// conversation contains prior tool calls but this turn has no tool defs,
	// reconstruct minimal tool definitions from the function names in history.
	if out.Tools == nil && !enableOpenWebUISearch {
		if inferred := inferToolsFromHistory(out.Messages); len(inferred) > 0 {
			out.Tools = &inferred
		}
	}

	return json.Marshal(out)
}

// inferToolsFromHistory extracts unique function names from historical
// tool_calls and builds minimal tool definitions so that backends that
// require a tools field will accept the request.
func inferToolsFromHistory(messages []openAIOutMsg) []openAIOutTool {
	seen := map[string]struct{}{}
	var tools []openAIOutTool
	for _, msg := range messages {
		for _, tc := range msg.ToolCalls {
			name := tc.Function.Name
			if _, ok := seen[name]; ok || name == "" {
				continue
			}
			seen[name] = struct{}{}
			tools = append(tools, openAIOutTool{
				Type: "function",
				Function: openAIOutToolFunc{
					Name:       name,
					Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
				},
			})
		}
	}
	return tools
}

func buildOpenAIReasoning(
	r *Reasoning,
	capability *config.ReasoningCapability,
) (string, bool) {
	if r == nil {
		return "", false
	}

	if r.Mode == "disabled" {
		return mapOpenAIDisabledEffort(capability)
	}
	if r.Effort != nil {
		return mapOpenAIEffort(*r.Effort, capability)
	}
	if r.BudgetTokens != nil {
		return mapOpenAIBudgetToEffort(*r.BudgetTokens, capability)
	}
	if r.Mode != "" {
		return mapOpenAIEffort("auto", capability)
	}

	return "", false
}

func encodeOpenAIMessage(m Message) []openAIOutMsg {
	var toolCalls []openAIOutTC
	var contentParts []openAIOutContentPart
	var toolResults []openAIOutMsg

	for _, b := range m.Content {
		switch b.Type {
		case "text":
			if b.Text != nil {
				contentParts = append(contentParts, openAIOutContentPart{
					Type: "text",
					Text: *b.Text,
				})
			}
		case "image":
			if b.Image != nil {
				url := encodeOpenAIImageURL(b.Image)
				if url == "" {
					continue
				}
				detail := b.Image.Detail
				if detail == "" {
					detail = "auto"
				}
				contentParts = append(contentParts, openAIOutContentPart{
					Type:     "image_url",
					ImageURL: &openAIOutImageURL{URL: url, Detail: detail},
				})
			}
		case "tool_call":
			if b.ToolCall != nil {
				argsStr, _ := json.Marshal(b.ToolCall.Arguments)
				toolCalls = append(toolCalls, openAIOutTC{
					ID:   b.ToolCall.ID,
					Type: "function",
					Function: openAIOutTCFunc{
						Name:      b.ToolCall.Name,
						Arguments: string(argsStr),
					},
				})
			}
		case "tool_result":
			if b.ToolResult != nil {
				toolResults = append(toolResults, openAIOutMsg{
					Role:       "tool",
					Content:    encodeOpenAIToolResultContent(b.ToolResult),
					ToolCallID: b.ToolResult.ToolCallID,
				})
			}
		case "thinking":
			// OpenAI requests do not carry canonical thinking blocks.
		}
	}

	var msgs []openAIOutMsg

	if m.Role == "tool" {
		return toolResults
	}

	if len(contentParts) == 0 && len(toolCalls) == 0 && len(toolResults) == 0 {
		return nil
	}

	msg := openAIOutMsg{Role: m.Role}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}

	switch len(contentParts) {
	case 0:
		// Some backends reject assistant messages without a content field.
		msg.Content = ""
	case 1:
		if contentParts[0].Type == "text" {
			msg.Content = contentParts[0].Text
		} else {
			msg.Content = contentParts
		}
	default:
		msg.Content = contentParts
	}

	// Emit tool results before residual user text so they stay adjacent to the
	// preceding tool call.
	if len(toolResults) > 0 && m.Role == "user" {
		msgs = append(msgs, toolResults...)
		if len(contentParts) > 0 {
			msgs = append(msgs, msg)
		}
		return msgs
	}

	msgs = append(msgs, msg)
	msgs = append(msgs, toolResults...)
	return msgs
}

func encodeOpenAIToolResultContent(result *ToolResult) any {
	var parts []openAIOutContentPart

	for _, b := range result.Content {
		switch b.Type {
		case "text":
			if b.Text != nil {
				parts = append(parts, openAIOutContentPart{
					Type: "text",
					Text: *b.Text,
				})
			}
		case "image":
			if b.Image != nil {
				url := encodeOpenAIImageURL(b.Image)
				if url == "" {
					continue
				}
				detail := b.Image.Detail
				if detail == "" {
					detail = "auto"
				}
				parts = append(parts, openAIOutContentPart{
					Type: "image_url",
					ImageURL: &openAIOutImageURL{
						URL:    url,
						Detail: detail,
					},
				})
			}
		}
	}

	if result.IsError {
		switch len(parts) {
		case 0:
			return "Error:"
		default:
			if parts[0].Type == "text" {
				parts[0].Text = "Error: " + parts[0].Text
			} else {
				parts = append([]openAIOutContentPart{{Type: "text", Text: "Error:"}}, parts...)
			}
		}
	}

	switch len(parts) {
	case 0:
		return ""
	case 1:
		if parts[0].Type == "text" {
			return parts[0].Text
		}
		return parts
	default:
		return parts
	}
}

func encodeOpenAIToolChoice(choice *ToolChoice) any {
	switch choice.Type {
	case "required":
		return "required"
	case "tool":
		if choice.Name == "" {
			return nil
		}
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": choice.Name,
			},
		}
	default:
		return "auto"
	}
}

func encodeOpenAIResponseFormat(format *ResponseFormat) any {
	switch format.Type {
	case "json_object":
		return map[string]any{"type": "json_object"}

	case "json_schema":
		if format.Schema == nil {
			return nil
		}
		name := format.Name
		if name == "" {
			name = "structured_output"
		}
		jsonSchema := map[string]any{
			"name":   name,
			"schema": format.Schema,
		}
		if format.Strict != nil {
			jsonSchema["strict"] = *format.Strict
		}
		return map[string]any{
			"type":        "json_schema",
			"json_schema": jsonSchema,
		}
	}

	return nil
}

func encodeOpenAIImageURL(img *Image) string {
	switch img.SourceType {
	case "data_url", "base64":
		if img.MediaType != nil && img.Data != nil {
			return fmt.Sprintf("data:%s;base64,%s", *img.MediaType, *img.Data)
		}
	case "url":
		if img.URL != nil {
			return *img.URL
		}
	}
	return ""
}

type openAIResponse struct {
	ID      string         `json:"id"`
	Model   string         `json:"model"`
	Choices []openAIChoice `json:"choices"`
	Usage   *openAIUsage   `json:"usage,omitempty"`
}

type openAIChoice struct {
	Message      openAIRespMsg `json:"message"`
	FinishReason *string       `json:"finish_reason,omitempty"`
}

type openAIRespMsg struct {
	Role             string                `json:"role"`
	Content          *string               `json:"content,omitempty"`
	ToolCalls        []openAIRespTC        `json:"tool_calls,omitempty"`
	ReasoningContent *string               `json:"reasoning_content,omitempty"`
	ThinkingBlocks   []openAIThinkingBlock `json:"thinking_blocks,omitempty"`
}

type openAIThinkingBlock struct {
	Type      string  `json:"type"`
	Thinking  *string `json:"thinking,omitempty"`
	Signature *string `json:"signature,omitempty"`
}

type openAIRespTC struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function openAIRespTCFunc `json:"function"`
}

type openAIRespTCFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIUsage struct {
	PromptTokens             int                           `json:"prompt_tokens"`
	CompletionTokens         int                           `json:"completion_tokens"`
	TotalTokens              int                           `json:"total_tokens"`
	CacheCreationInputTokens *int                          `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     *int                          `json:"cache_read_input_tokens,omitempty"`
	PromptTokensDetails      *openAIPromptTokensDetails    `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails  *openAICompletionTokensDetail `json:"completion_tokens_details,omitempty"`
}

type openAIPromptTokensDetails struct {
	CachedTokens *int `json:"cached_tokens,omitempty"`
}

type openAICompletionTokensDetail struct {
	ReasoningTokens          *int `json:"reasoning_tokens,omitempty"`
	AcceptedPredictionTokens *int `json:"accepted_prediction_tokens,omitempty"`
	RejectedPredictionTokens *int `json:"rejected_prediction_tokens,omitempty"`
}

// DecodeOpenAIResponse decodes an OpenAI chat response into canonical form.
func DecodeOpenAIResponse(body []byte) (*Response, error) {
	var raw openAIResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode openai response: %w", err)
	}

	resp := &Response{
		Model: raw.Model,
		ID:    raw.ID,
	}

	if len(raw.Choices) > 0 {
		c := raw.Choices[0]
		var blocks []ContentBlock

		if len(c.Message.ThinkingBlocks) > 0 {
			for _, tb := range c.Message.ThinkingBlocks {
				if tb.Type != "thinking" || tb.Thinking == nil || *tb.Thinking == "" {
					continue
				}
				blocks = append(blocks, ContentBlock{
					Type: "thinking",
					Thinking: &ThinkingBlock{
						Text:      tb.Thinking,
						Signature: tb.Signature,
					},
				})
			}
		} else if c.Message.ReasoningContent != nil && *c.Message.ReasoningContent != "" {
			blocks = append(blocks, ContentBlock{
				Type:     "thinking",
				Thinking: &ThinkingBlock{Text: c.Message.ReasoningContent},
			})
		}

		if c.Message.Content != nil && *c.Message.Content != "" {
			blocks = append(blocks, ContentBlock{
				Type: "text",
				Text: c.Message.Content,
			})
		}

		for _, tc := range c.Message.ToolCalls {
			var args any
			if tc.Function.Arguments != "" {
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			}
			blocks = append(blocks, ContentBlock{
				Type: "tool_call",
				ToolCall: &ToolCall{
					ID:        tc.ID,
					Name:      tc.Function.Name,
					Arguments: args,
				},
			})
		}

		resp.Output = []Message{
			{Role: c.Message.Role, Content: blocks},
		}

		if c.FinishReason != nil {
			resp.StopReason = normalizeOpenAIStopReason(*c.FinishReason)
		}
	}

	if raw.Usage != nil {
		resp.Usage = decodeOpenAIUsage(raw.Usage)
	}

	return resp, nil
}

func decodeOpenAIUsage(raw *openAIUsage) *Usage {
	if raw == nil {
		return nil
	}

	in64 := int64(raw.PromptTokens)
	out64 := int64(raw.CompletionTokens)
	total64 := int64(raw.TotalTokens)
	usage := &Usage{
		InputTokens:  &in64,
		OutputTokens: &out64,
		TotalTokens:  &total64,
	}

	if raw.CompletionTokensDetails != nil && raw.CompletionTokensDetails.ReasoningTokens != nil {
		reasoning64 := int64(*raw.CompletionTokensDetails.ReasoningTokens)
		usage.ReasoningTokens = &reasoning64
	}
	if raw.CacheCreationInputTokens != nil {
		cacheWrite64 := int64(*raw.CacheCreationInputTokens)
		usage.CacheWriteTokens = &cacheWrite64
	}
	if raw.PromptTokensDetails != nil && raw.PromptTokensDetails.CachedTokens != nil {
		cacheRead64 := int64(*raw.PromptTokensDetails.CachedTokens)
		usage.CacheReadTokens = &cacheRead64
	} else if raw.CacheReadInputTokens != nil {
		cacheRead64 := int64(*raw.CacheReadInputTokens)
		usage.CacheReadTokens = &cacheRead64
	}

	return usage
}

func normalizeOpenAIStopReason(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	default:
		return reason
	}
}

func maxUsesToSearchContextSize(maxUses int) string {
	switch maxUses {
	case 1:
		return "low"
	case 10:
		return "high"
	default:
		return "medium"
	}
}
