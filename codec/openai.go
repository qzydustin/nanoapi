package codec

import (
	"encoding/json"
	"fmt"

	"github.com/qzydustin/nanoapi/canonical"
)

// ---------------------------------------------------------------------------
// OpenAI Request Encoding (canonical → OpenAI JSON)
// ---------------------------------------------------------------------------

type openAIOutRequest struct {
	Model           string          `json:"model"`
	Messages        []openAIOutMsg  `json:"messages"`
	Stream          bool            `json:"stream,omitempty"`
	MaxTokens       *int            `json:"max_completion_tokens,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	TopP            *float64        `json:"top_p,omitempty"`
	Stop            []string        `json:"stop,omitempty"`
	Tools           []openAIOutTool `json:"tools,omitempty"`
	ReasoningEffort *string         `json:"reasoning_effort,omitempty"`
	StreamOptions   *streamOptions  `json:"stream_options,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
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
	URL string `json:"url"`
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
	Type     string            `json:"type"`
	Function openAIOutToolFunc `json:"function"`
}

type openAIOutToolFunc struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

// EncodeOpenAIRequest converts a CanonicalRequest into an OpenAI Chat
// Completions JSON request body.
func EncodeOpenAIRequest(req *canonical.CanonicalRequest, upstreamModel string, stream bool) ([]byte, error) {
	out := openAIOutRequest{
		Model:       upstreamModel,
		Stream:      stream,
		MaxTokens:   req.Params.MaxTokens,
		Temperature: req.Params.Temperature,
		TopP:        req.Params.TopP,
		Stop:        req.Params.Stop,
	}

	if stream {
		out.StreamOptions = &streamOptions{IncludeUsage: true}
	}

	// Reasoning
	if r := req.Params.Reasoning; r != nil {
		if r.Effort != nil {
			out.ReasoningEffort = r.Effort
		} else if r.Mode != "" && r.Mode != "disabled" {
			effort := "medium"
			out.ReasoningEffort = &effort
		}
	}

	// Tools
	for _, t := range req.Tools {
		out.Tools = append(out.Tools, openAIOutTool{
			Type: "function",
			Function: openAIOutToolFunc{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}

	// System messages
	for _, sys := range req.System {
		if sys.Type == "text" && sys.Text != nil {
			out.Messages = append(out.Messages, openAIOutMsg{
				Role:    "system",
				Content: *sys.Text,
			})
		}
	}

	// Messages
	for _, m := range req.Messages {
		msg := encodeOpenAIMessage(m)
		out.Messages = append(out.Messages, msg...)
	}

	return json.Marshal(out)
}

func encodeOpenAIMessage(m canonical.CanonicalMessage) []openAIOutMsg {
	// Separate tool results into their own messages.
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
				contentParts = append(contentParts, openAIOutContentPart{
					Type:     "image_url",
					ImageURL: &openAIOutImageURL{URL: url},
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
				var content string
				for _, c := range b.ToolResult.Content {
					if c.Type == "text" && c.Text != nil {
						content += *c.Text
					}
				}
				toolResults = append(toolResults, openAIOutMsg{
					Role:       "tool",
					Content:    content,
					ToolCallID: b.ToolResult.ToolCallID,
				})
			}
		case "thinking":
			// OpenAI does not have a standard thinking field in requests.
			// Skip thinking blocks in outbound encoding.
		}
	}

	var msgs []openAIOutMsg

	if m.Role == "tool" {
		// The entire message is tool results.
		return toolResults
	}

	msg := openAIOutMsg{Role: m.Role}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}

	switch len(contentParts) {
	case 0:
		// assistant with only tool_calls — no content
	case 1:
		if contentParts[0].Type == "text" {
			msg.Content = contentParts[0].Text
		} else {
			msg.Content = contentParts
		}
	default:
		msg.Content = contentParts
	}

	msgs = append(msgs, msg)
	msgs = append(msgs, toolResults...)
	return msgs
}

func encodeOpenAIImageURL(img *canonical.CanonicalImage) string {
	switch img.SourceType {
	case "data_url":
		if img.MediaType != nil && img.Data != nil {
			return fmt.Sprintf("data:%s;base64,%s", *img.MediaType, *img.Data)
		}
	case "base64":
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

// ---------------------------------------------------------------------------
// OpenAI Response Decoding (upstream JSON → CanonicalResponse)
// ---------------------------------------------------------------------------

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
	Role             string         `json:"role"`
	Content          *string        `json:"content,omitempty"`
	ToolCalls        []openAIRespTC `json:"tool_calls,omitempty"`
	ReasoningContent *string        `json:"reasoning_content,omitempty"`
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
	PromptTokens            int                           `json:"prompt_tokens"`
	CompletionTokens        int                           `json:"completion_tokens"`
	TotalTokens             int                           `json:"total_tokens"`
	PromptTokensDetails     *openAIPromptTokensDetails    `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *openAICompletionTokensDetail `json:"completion_tokens_details,omitempty"`
}

type openAIPromptTokensDetails struct {
	CachedTokens *int `json:"cached_tokens,omitempty"`
}

type openAICompletionTokensDetail struct {
	ReasoningTokens          *int `json:"reasoning_tokens,omitempty"`
	AcceptedPredictionTokens *int `json:"accepted_prediction_tokens,omitempty"`
	RejectedPredictionTokens *int `json:"rejected_prediction_tokens,omitempty"`
}

// DecodeOpenAIResponse parses an OpenAI Chat Completions response into a
// CanonicalResponse.
func DecodeOpenAIResponse(body []byte) (*canonical.CanonicalResponse, error) {
	var raw openAIResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode openai response: %w", err)
	}

	resp := &canonical.CanonicalResponse{
		UpstreamProtocol: canonical.ProtocolOpenAIChat,
		Model:            raw.Model,
		ID:               raw.ID,
	}

	if len(raw.Choices) > 0 {
		c := raw.Choices[0]
		var blocks []canonical.CanonicalContentBlock

		if c.Message.ReasoningContent != nil && *c.Message.ReasoningContent != "" {
			blocks = append(blocks, canonical.CanonicalContentBlock{
				Type:     "thinking",
				Thinking: &canonical.CanonicalThinkingBlock{Text: c.Message.ReasoningContent},
			})
		}

		if c.Message.Content != nil && *c.Message.Content != "" {
			blocks = append(blocks, canonical.CanonicalContentBlock{
				Type: "text",
				Text: c.Message.Content,
			})
		}

		for _, tc := range c.Message.ToolCalls {
			var args any
			if tc.Function.Arguments != "" {
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			}
			blocks = append(blocks, canonical.CanonicalContentBlock{
				Type: "tool_call",
				ToolCall: &canonical.CanonicalToolCall{
					ID:        tc.ID,
					Name:      tc.Function.Name,
					Arguments: args,
				},
			})
		}

		resp.Output = []canonical.CanonicalMessage{
			{Role: c.Message.Role, Content: blocks},
		}

		if c.FinishReason != nil {
			resp.StopReason = normalizeOpenAIStopReason(*c.FinishReason)
		}
	}

	if raw.Usage != nil {
		in64 := int64(raw.Usage.PromptTokens)
		out64 := int64(raw.Usage.CompletionTokens)
		total64 := int64(raw.Usage.TotalTokens)
		resp.Usage = &canonical.CanonicalUsage{
			InputTokens:  &in64,
			OutputTokens: &out64,
			TotalTokens:  &total64,
		}
		if raw.Usage.CompletionTokensDetails != nil && raw.Usage.CompletionTokensDetails.ReasoningTokens != nil {
			reasoning64 := int64(*raw.Usage.CompletionTokensDetails.ReasoningTokens)
			resp.Usage.ReasoningTokens = &reasoning64
		}
		if raw.Usage.PromptTokensDetails != nil && raw.Usage.PromptTokensDetails.CachedTokens != nil {
			cacheRead64 := int64(*raw.Usage.PromptTokensDetails.CachedTokens)
			resp.Usage.CacheReadTokens = &cacheRead64
		}
	}

	return resp, nil
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

// ---------------------------------------------------------------------------
// OpenAI Client Response Encoding (CanonicalResponse → OpenAI JSON)
// ---------------------------------------------------------------------------

type openAIClientResp struct {
	ID      string               `json:"id"`
	Object  string               `json:"object"`
	Model   string               `json:"model"`
	Choices []openAIClientChoice `json:"choices"`
	Usage   *openAIClientUsage   `json:"usage,omitempty"`
}

type openAIClientChoice struct {
	Index        int             `json:"index"`
	Message      openAIClientMsg `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

type openAIClientMsg struct {
	Role             string        `json:"role"`
	Content          *string       `json:"content"`
	ToolCalls        []openAIOutTC `json:"tool_calls,omitempty"`
	ReasoningContent *string       `json:"reasoning_content,omitempty"`
}

type openAIClientUsage struct {
	PromptTokens            int                           `json:"prompt_tokens"`
	CompletionTokens        int                           `json:"completion_tokens"`
	TotalTokens             int                           `json:"total_tokens"`
	PromptTokensDetails     *openAIPromptTokensDetails    `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *openAICompletionTokensDetail `json:"completion_tokens_details,omitempty"`
}

// EncodeOpenAIClientResponse encodes a CanonicalResponse as an OpenAI Chat
// Completions JSON response for the client.
func EncodeOpenAIClientResponse(resp *canonical.CanonicalResponse) ([]byte, error) {
	out := openAIClientResp{
		ID:     resp.ID,
		Object: "chat.completion",
		Model:  resp.Model,
	}

	msg := openAIClientMsg{Role: "assistant"}
	finishReason := denormalizeOpenAIStopReason(resp.StopReason)

	if len(resp.Output) > 0 {
		for _, b := range resp.Output[0].Content {
			switch b.Type {
			case "text":
				msg.Content = b.Text
			case "thinking":
				if b.Thinking != nil {
					msg.ReasoningContent = b.Thinking.Text
				}
			case "tool_call":
				if b.ToolCall != nil {
					argsStr, _ := json.Marshal(b.ToolCall.Arguments)
					msg.ToolCalls = append(msg.ToolCalls, openAIOutTC{
						ID:   b.ToolCall.ID,
						Type: "function",
						Function: openAIOutTCFunc{
							Name:      b.ToolCall.Name,
							Arguments: string(argsStr),
						},
					})
				}
			}
		}
	}

	out.Choices = []openAIClientChoice{
		{Index: 0, Message: msg, FinishReason: finishReason},
	}

	if resp.Usage != nil {
		u := openAIClientUsage{}
		if resp.Usage.InputTokens != nil {
			u.PromptTokens = int(*resp.Usage.InputTokens)
		}
		if resp.Usage.OutputTokens != nil {
			u.CompletionTokens = int(*resp.Usage.OutputTokens)
		}
		if resp.Usage.TotalTokens != nil {
			u.TotalTokens = int(*resp.Usage.TotalTokens)
		} else {
			u.TotalTokens = u.PromptTokens + u.CompletionTokens
		}
		if resp.Usage.CacheReadTokens != nil {
			u.PromptTokensDetails = &openAIPromptTokensDetails{
				CachedTokens: intPtr(int(*resp.Usage.CacheReadTokens)),
			}
		}
		if resp.Usage.ReasoningTokens != nil {
			u.CompletionTokensDetails = &openAICompletionTokensDetail{
				ReasoningTokens: intPtr(int(*resp.Usage.ReasoningTokens)),
			}
		}
		out.Usage = &u
	}

	return json.Marshal(out)
}

func denormalizeOpenAIStopReason(reason string) string {
	switch reason {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		return reason
	}
}

func intPtr(v int) *int { return &v }
