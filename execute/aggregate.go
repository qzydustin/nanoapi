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
	ToolCalls     []*toolCallState
	RawBlocks     []*rawBlockState
	StopReason    string
	Usage         *canonical.CanonicalUsage
}

// toolCallState tracks a single tool call being accumulated from streaming.
type toolCallState struct {
	ID   string
	Name string
	Args strings.Builder
}

// rawBlockState tracks a passthrough block accumulated from streaming.
type rawBlockState struct {
	ContentBlock json.RawMessage
	Deltas       []json.RawMessage
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
		s.ToolCalls = append(s.ToolCalls, &toolCallState{
			ID:   event.ToolCallID,
			Name: event.ToolCallName,
		})
	case canonical.EventToolCallDelta:
		if len(s.ToolCalls) > 0 {
			s.ToolCalls[len(s.ToolCalls)-1].Args.WriteString(event.ArgumentsDelta)
		}
	case canonical.EventToolCallEnd:
		// Nothing to do; the tool call is already accumulated.
	case canonical.EventRawBlockStart:
		var envelope struct {
			ContentBlock json.RawMessage `json:"content_block"`
		}
		json.Unmarshal(event.RawJSON, &envelope)
		s.RawBlocks = append(s.RawBlocks, &rawBlockState{
			ContentBlock: envelope.ContentBlock,
		})
	case canonical.EventRawBlockDelta:
		if len(s.RawBlocks) > 0 {
			s.RawBlocks[len(s.RawBlocks)-1].Deltas = append(
				s.RawBlocks[len(s.RawBlocks)-1].Deltas, event.RawJSON)
		}
	case canonical.EventRawBlockStop:
		// Nothing to do; the block is finalized in Finalize().
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

	// Raw passthrough blocks.
	for _, rb := range s.RawBlocks {
		finalBlock := finalizeRawBlock(rb)
		var header struct {
			Type string `json:"type"`
		}
		json.Unmarshal(finalBlock, &header)
		blocks = append(blocks, canonical.CanonicalContentBlock{
			Type:    header.Type,
			RawJSON: finalBlock,
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

// finalizeRawBlock merges input_json_delta partial_json values into the
// content_block's input field.
func finalizeRawBlock(rb *rawBlockState) json.RawMessage {
	if len(rb.Deltas) == 0 {
		return rb.ContentBlock
	}

	var inputBuf strings.Builder
	for _, d := range rb.Deltas {
		var delta struct {
			Delta struct {
				Type        string `json:"type"`
				PartialJSON string `json:"partial_json,omitempty"`
			} `json:"delta"`
		}
		if json.Unmarshal(d, &delta) == nil && delta.Delta.Type == "input_json_delta" {
			inputBuf.WriteString(delta.Delta.PartialJSON)
		}
	}

	if inputBuf.Len() == 0 {
		return rb.ContentBlock
	}

	// Merge accumulated input into the content_block.
	var input any
	if json.Unmarshal([]byte(inputBuf.String()), &input) != nil {
		return rb.ContentBlock
	}

	var block map[string]any
	if json.Unmarshal(rb.ContentBlock, &block) != nil {
		return rb.ContentBlock
	}
	block["input"] = input

	merged, err := json.Marshal(block)
	if err != nil {
		return rb.ContentBlock
	}
	return merged
}
