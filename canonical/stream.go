package canonical

import "encoding/json"

// Stream event type constants.
const (
	EventTextDelta         = "text_delta"
	EventThinkingDelta     = "thinking_delta"
	EventThinkingSignature = "thinking_signature"
	EventToolCallStart     = "tool_call_start"
	EventToolCallDelta     = "tool_call_delta"
	EventToolCallEnd       = "tool_call_end"
	EventRawBlockStart     = "raw_block_start"
	EventRawBlockDelta     = "raw_block_delta"
	EventRawBlockStop      = "raw_block_stop"
	EventMessageStop       = "message_stop"
	EventUsageFinal        = "usage_final"
)

// CanonicalStreamEvent is the minimal internal streaming abstraction for V1.
// Each upstream SSE chunk is decoded into one or more of these events.
type CanonicalStreamEvent struct {
	Type string

	// text_delta / thinking_delta
	Text string

	// thinking_signature
	Signature string

	// tool_call_start
	ToolCallID   string
	ToolCallName string

	// tool_call_delta
	ArgumentsDelta string

	// raw_block_start / raw_block_delta / raw_block_stop
	RawJSON json.RawMessage

	// message_stop
	StopReason string

	// usage_final
	Usage *CanonicalUsage

	// message-level metadata (typically from the first event)
	ResponseID string
	Model      string
}
