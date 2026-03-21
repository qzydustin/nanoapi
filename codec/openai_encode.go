package codec

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

type openAIClientResp struct {
	ID      string               `json:"id"`
	Object  string               `json:"object"`
	Created int64                `json:"created"`
	Model   string               `json:"model"`
	Choices []openAIClientChoice `json:"choices"`
	Usage   *openAIUsage         `json:"usage,omitempty"`
}

type openAIClientChoice struct {
	Index        int             `json:"index"`
	Message      openAIClientMsg `json:"message"`
	FinishReason *string         `json:"finish_reason"`
}

type openAIClientMsg struct {
	Role             string           `json:"role"`
	Content          *string          `json:"content"`
	ReasoningContent *string          `json:"reasoning_content,omitempty"`
	ToolCalls        []openAIClientTC `json:"tool_calls,omitempty"`
}

type openAIClientTC struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIClientTCFunc `json:"function"`
}

type openAIClientTCFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func EncodeOpenAIClientResponse(resp *Response) ([]byte, error) {
	out := openAIClientResp{
		ID:      resp.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   resp.Model,
	}

	choice := openAIClientChoice{
		Index:   0,
		Message: openAIClientMsg{Role: "assistant"},
	}

	finishReason := denormalizeOpenAIStopReason(resp.StopReason)
	if finishReason != "" {
		choice.FinishReason = &finishReason
	}

	if len(resp.Output) > 0 {
		var textParts []string
		var thinkingParts []string
		var toolCalls []openAIClientTC

		for _, b := range resp.Output[0].Content {
			switch b.Type {
			case "text":
				if b.Text != nil {
					textParts = append(textParts, *b.Text)
				}
			case "thinking":
				if b.Thinking != nil && b.Thinking.Text != nil {
					thinkingParts = append(thinkingParts, *b.Thinking.Text)
				}
			case "tool_call":
				if b.ToolCall != nil {
					argsStr, _ := json.Marshal(b.ToolCall.Arguments)
					toolCalls = append(toolCalls, openAIClientTC{
						ID:   b.ToolCall.ID,
						Type: "function",
						Function: openAIClientTCFunc{
							Name:      b.ToolCall.Name,
							Arguments: string(argsStr),
						},
					})
				}
			}
		}

		if len(textParts) > 0 {
			joined := strings.Join(textParts, "")
			choice.Message.Content = &joined
		}
		if len(thinkingParts) > 0 {
			joined := strings.Join(thinkingParts, "")
			choice.Message.ReasoningContent = &joined
		}
		if len(toolCalls) > 0 {
			choice.Message.ToolCalls = toolCalls
		}
	}

	out.Choices = []openAIClientChoice{choice}

	if resp.Usage != nil {
		out.Usage = encodeOpenAIUsage(resp.Usage)
	}

	return json.Marshal(out)
}

func encodeOpenAIUsage(u *Usage) *openAIUsage {
	if u == nil {
		return nil
	}
	out := &openAIUsage{}
	if u.InputTokens != nil {
		out.PromptTokens = int(*u.InputTokens)
	}
	if u.OutputTokens != nil {
		out.CompletionTokens = int(*u.OutputTokens)
	}
	if u.TotalTokens != nil {
		out.TotalTokens = int(*u.TotalTokens)
	} else {
		out.TotalTokens = out.PromptTokens + out.CompletionTokens
	}
	if u.CacheWriteTokens != nil {
		v := int(*u.CacheWriteTokens)
		out.CacheCreationInputTokens = &v
	}
	if u.CacheReadTokens != nil {
		v := int(*u.CacheReadTokens)
		out.CacheReadInputTokens = &v
		out.PromptTokensDetails = &openAIPromptTokensDetails{CachedTokens: &v}
	}
	if u.ReasoningTokens != nil {
		v := int(*u.ReasoningTokens)
		out.CompletionTokensDetails = &openAICompletionTokensDetail{ReasoningTokens: &v}
	}
	return out
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

func NewOpenAIResponseID() string {
	return "chatcmpl-" + strings.ReplaceAll(uuid.NewString(), "-", "")
}
