package execute

import (
	"encoding/json"
	"strings"

	"github.com/qzydustin/nanoapi/canonical"
)

// StreamAggregateState collects canonical stream events into a final
// non-stream CanonicalResponse. Used for forced-stream aggregation.
type StreamAggregateState struct {
	ResponseID    string
	Model         string
	TextParts     []string
	ThinkingParts []string
	ToolCalls     []*ToolCallState
	StopReason    string
	Usage         *canonical.CanonicalUsage
}

// ToolCallState tracks a single tool call being accumulated from streaming.
type ToolCallState struct {
	ID   string
	Name string
	Args strings.Builder
}

// NewStreamAggregateState creates a new aggregation state.
func NewStreamAggregateState() *StreamAggregateState {
	return &StreamAggregateState{}
}

// Apply processes a single canonical stream event into the aggregate state.
func (s *StreamAggregateState) Apply(event canonical.CanonicalStreamEvent) {
	if event.ResponseID != "" && s.ResponseID == "" {
		s.ResponseID = event.ResponseID
	}
	if event.Model != "" && s.Model == "" {
		s.Model = event.Model
	}

	switch event.Type {
	case canonical.EventTextDelta:
		if event.Text != "" {
			s.TextParts = append(s.TextParts, event.Text)
		}
	case canonical.EventThinkingDelta:
		s.ThinkingParts = append(s.ThinkingParts, event.Text)
	case canonical.EventToolCallStart:
		s.ToolCalls = append(s.ToolCalls, &ToolCallState{
			ID:   event.ToolCallID,
			Name: event.ToolCallName,
		})
	case canonical.EventToolCallDelta:
		if len(s.ToolCalls) > 0 {
			s.ToolCalls[len(s.ToolCalls)-1].Args.WriteString(event.ArgumentsDelta)
		}
	case canonical.EventToolCallEnd:
		// Nothing to do; the tool call is already accumulated.
	case canonical.EventMessageStop:
		s.StopReason = event.StopReason
	case canonical.EventUsageFinal:
		if event.Usage != nil {
			if s.Usage == nil {
				s.Usage = &canonical.CanonicalUsage{}
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

// Finalize constructs a CanonicalResponse from the accumulated state.
func (s *StreamAggregateState) Finalize() *canonical.CanonicalResponse {
	var blocks []canonical.CanonicalContentBlock

	// Thinking output first.
	if len(s.ThinkingParts) > 0 {
		text := strings.Join(s.ThinkingParts, "")
		blocks = append(blocks, canonical.CanonicalContentBlock{
			Type:     "thinking",
			Thinking: &canonical.CanonicalThinkingBlock{Text: &text},
		})
	}

	// Text output.
	if len(s.TextParts) > 0 {
		text := strings.Join(s.TextParts, "")
		blocks = append(blocks, canonical.CanonicalContentBlock{
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
		blocks = append(blocks, canonical.CanonicalContentBlock{
			Type: "tool_call",
			ToolCall: &canonical.CanonicalToolCall{
				ID:        tc.ID,
				Name:      tc.Name,
				Arguments: args,
			},
		})
	}

	return &canonical.CanonicalResponse{
		ID:         s.ResponseID,
		Model:      s.Model,
		StopReason: s.StopReason,
		Usage:      s.Usage,
		Output: []canonical.CanonicalMessage{
			{Role: "assistant", Content: blocks},
		},
	}
}
