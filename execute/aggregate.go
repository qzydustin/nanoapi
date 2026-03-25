package execute

import (
	"encoding/json"
	"strings"

	"github.com/qzydustin/nanoapi/codec"
)

// StreamAggregateState collects stream events into a final
// non-stream Response. Used for forced-stream aggregation.
type StreamAggregateState struct {
	responseID       string
	model            string
	textParts        []string
	thinkingParts    []string
	toolCalls        []*toolCallState
	webSearchResults []codec.WebSearchResult
	stopReason       string
	usage            *codec.Usage
}

// toolCallState tracks a single tool call being accumulated from streaming.
type toolCallState struct {
	index int
	id    string
	name  string
	args  strings.Builder
}

// Apply processes a single stream event into the aggregate state.
func (s *StreamAggregateState) Apply(event codec.StreamEvent) {
	if event.ResponseID != "" && s.responseID == "" {
		s.responseID = event.ResponseID
	}
	if event.Model != "" && s.model == "" {
		s.model = event.Model
	}

	switch event.Type {
	case codec.EventTextDelta:
		if event.Text != "" {
			s.textParts = append(s.textParts, event.Text)
		}
	case codec.EventThinkingDelta:
		s.thinkingParts = append(s.thinkingParts, event.Text)
	case codec.EventToolCallStart:
		s.toolCalls = append(s.toolCalls, &toolCallState{
			index: event.ToolCallIndex,
			id:    event.ToolCallID,
			name:  event.ToolCallName,
		})
	case codec.EventToolCallDelta:
		for i := len(s.toolCalls) - 1; i >= 0; i-- {
			if s.toolCalls[i].index == event.ToolCallIndex {
				s.toolCalls[i].args.WriteString(event.ArgumentsDelta)
				break
			}
		}
	case codec.EventToolCallEnd:
	case codec.EventMessageStop:
		s.stopReason = event.StopReason
	case codec.EventUsageFinal:
		if event.Usage != nil {
			if s.usage == nil {
				s.usage = &codec.Usage{}
			}
			if event.Usage.InputTokens != nil {
				s.usage.InputTokens = event.Usage.InputTokens
			}
			if event.Usage.OutputTokens != nil {
				s.usage.OutputTokens = event.Usage.OutputTokens
			}
			if event.Usage.TotalTokens != nil {
				s.usage.TotalTokens = event.Usage.TotalTokens
			}
			if event.Usage.ReasoningTokens != nil {
				s.usage.ReasoningTokens = event.Usage.ReasoningTokens
			}
			if event.Usage.CacheReadTokens != nil {
				s.usage.CacheReadTokens = event.Usage.CacheReadTokens
			}
			if event.Usage.CacheWriteTokens != nil {
				s.usage.CacheWriteTokens = event.Usage.CacheWriteTokens
			}
		}
	}
}

// Finalize constructs a Response from the accumulated state.
func (s *StreamAggregateState) Finalize() *codec.Response {
	var blocks []codec.ContentBlock

	if len(s.thinkingParts) > 0 {
		text := strings.Join(s.thinkingParts, "")
		blocks = append(blocks, codec.ContentBlock{
			Type:     "thinking",
			Thinking: &codec.ThinkingBlock{Text: &text},
		})
	}

	if len(s.textParts) > 0 {
		text := strings.Join(s.textParts, "")
		blocks = append(blocks, codec.ContentBlock{
			Type: "text",
			Text: &text,
		})
	}

	for _, tc := range s.toolCalls {
		var args any
		argsStr := tc.args.String()
		if argsStr != "" {
			_ = json.Unmarshal([]byte(argsStr), &args)
		}
		blocks = append(blocks, codec.ContentBlock{
			Type: "tool_call",
			ToolCall: &codec.ToolCall{
				ID:        tc.id,
				Name:      tc.name,
				Arguments: args,
			},
		})
	}

	return &codec.Response{
		ID:         s.responseID,
		Model:      s.model,
		StopReason: s.stopReason,
		Usage:      s.usage,
		Output: []codec.Message{
			{Role: "assistant", Content: blocks},
		},
	}
}

// WebSearchResults returns the accumulated web search results.
func (s *StreamAggregateState) WebSearchResults() []codec.WebSearchResult {
	return s.webSearchResults
}

// SetWebSearchResults stores web search results parsed from OpenWebUI sources events.
func (s *StreamAggregateState) SetWebSearchResults(results []codec.WebSearchResult) {
	s.webSearchResults = results
}
