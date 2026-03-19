package canonical

// CanonicalMessage is one turn in the conversation.
type CanonicalMessage struct {
	Role    string // "system", "user", "assistant", "tool"
	Content []CanonicalContentBlock
}

// CanonicalContentBlock is a single typed piece of content inside a message.
type CanonicalContentBlock struct {
	Type string // "text", "image", "tool_call", "tool_result", "thinking"

	Text       *string
	Image      *CanonicalImage
	ToolCall   *CanonicalToolCall
	ToolResult *CanonicalToolResult
	Thinking   *CanonicalThinkingBlock
}

// CanonicalImage stores image data in a protocol-neutral way.
type CanonicalImage struct {
	SourceType string  // "data_url", "base64", "url"
	URL        *string // used when SourceType is "url" or "data_url"
	MediaType  *string // e.g. "image/png"
	Data       *string // raw base64 data
	Detail     string  // "low", "high", "auto"; empty means unset
}

// CanonicalTool describes a tool definition available for the model.
type CanonicalTool struct {
	Name        string
	Description string
	InputSchema any
}

// CanonicalToolCall represents a tool invocation produced by the model.
type CanonicalToolCall struct {
	ID        string
	Name      string
	Arguments any
}

// CanonicalToolResult is the result returned for a prior tool call.
type CanonicalToolResult struct {
	ToolCallID string
	Content    []CanonicalContentBlock
}

// CanonicalThinkingBlock holds reasoning / chain-of-thought text produced by
// a model.  This must never be merged into visible answer text.
type CanonicalThinkingBlock struct {
	Text      *string
	Signature *string
}
