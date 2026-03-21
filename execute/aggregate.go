package execute

import (
	"encoding/json"
	"strings"

	"github.com/qzydustin/nanoapi/codec"
)

// StreamAggregateState collects stream events into a final
// non-stream Response. Used for forced-stream aggregation.
type StreamAggregateState struct {
	ResponseID       string
	Model            string
	TextParts        []string
	ThinkingParts    []string
	ToolCalls        []*toolCallState
	WebSearchResults []codec.WebSearchResult
	StopReason       string
	Usage            *codec.Usage
}

// toolCallState tracks a single tool call being accumulated from streaming.
type toolCallState struct {
	ID   string
	Name string
	Args strings.Builder
}

// Apply processes a single stream event into the aggregate state.
func (s *StreamAggregateState) Apply(event codec.StreamEvent) {
	if event.ResponseID != "" && s.ResponseID == "" {
		s.ResponseID = event.ResponseID
	}
	if event.Model != "" && s.Model == "" {
		s.Model = event.Model
	}

	switch event.Type {
	case codec.EventTextDelta:
		if event.Text != "" {
			s.TextParts = append(s.TextParts, event.Text)
		}
	case codec.EventThinkingDelta:
		s.ThinkingParts = append(s.ThinkingParts, event.Text)
	case codec.EventToolCallStart:
		s.ToolCalls = append(s.ToolCalls, &toolCallState{
			ID:   event.ToolCallID,
			Name: event.ToolCallName,
		})
	case codec.EventToolCallDelta:
		if len(s.ToolCalls) > 0 {
			s.ToolCalls[len(s.ToolCalls)-1].Args.WriteString(event.ArgumentsDelta)
		}
	case codec.EventToolCallEnd:
		// Nothing to do; the tool call is already accumulated.
	case codec.EventMessageStop:
		s.StopReason = event.StopReason
	case codec.EventUsageFinal:
		if event.Usage != nil {
			if s.Usage == nil {
				s.Usage = &codec.Usage{}
			}
			// Merge: later usage events may have partial data.
			if event.Usage.InputTokens != nil {
				s.Usage.InputTokens = event.Usage.InputTokens
			}
			if event.Usage.OutputTokens != nil {
				s.Usage.OutputTokens = event.Usage.OutputTokens
			}
			if event.Usage.TotalTokens != nil {
				s.Usage.TotalTokens = event.Usage.TotalTokens
			}
			if event.Usage.ReasoningTokens != nil {
				s.Usage.ReasoningTokens = event.Usage.ReasoningTokens
			}
			if event.Usage.CacheReadTokens != nil {
				s.Usage.CacheReadTokens = event.Usage.CacheReadTokens
			}
			if event.Usage.CacheWriteTokens != nil {
				s.Usage.CacheWriteTokens = event.Usage.CacheWriteTokens
			}
		}
	}
}

// Finalize constructs a Response from the accumulated state.
func (s *StreamAggregateState) Finalize() *codec.Response {
	var blocks []codec.ContentBlock

	// Thinking output first.
	if len(s.ThinkingParts) > 0 {
		text := strings.Join(s.ThinkingParts, "")
		blocks = append(blocks, codec.ContentBlock{
			Type:     "thinking",
			Thinking: &codec.ThinkingBlock{Text: &text},
		})
	}

	// Text output.
	if len(s.TextParts) > 0 {
		text := strings.Join(s.TextParts, "")
		blocks = append(blocks, codec.ContentBlock{
			Type: "text",
			Text: &text,
		})
	}

	// Tool calls.
	for _, tc := range s.ToolCalls {
		var args any
		argsStr := tc.Args.String()
		if argsStr != "" {
			_ = json.Unmarshal([]byte(argsStr), &args)
		}
		blocks = append(blocks, codec.ContentBlock{
			Type: "tool_call",
			ToolCall: &codec.ToolCall{
				ID:        tc.ID,
				Name:      tc.Name,
				Arguments: args,
			},
		})
	}

	return &codec.Response{
		ID:         s.ResponseID,
		Model:      s.Model,
		StopReason: s.StopReason,
		Usage:      s.Usage,
		Output: []codec.Message{
			{Role: "assistant", Content: blocks},
		},
	}
}
