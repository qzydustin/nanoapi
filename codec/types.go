package codec

// Stream event type constants.
const (
	EventTextDelta         = "text_delta"
	EventThinkingDelta     = "thinking_delta"
	EventThinkingSignature = "thinking_signature"
	EventToolCallStart     = "tool_call_start"
	EventToolCallDelta     = "tool_call_delta"
	EventToolCallEnd       = "tool_call_end"
	EventServerToolUse     = "server_tool_use"
	EventWebSearchResult   = "web_search_tool_result"
	EventMessageStop       = "message_stop"
	EventUsageFinal        = "usage_final"
)

// Request is the provider-neutral semantic representation of an
// inbound chat/messages request.  Every later module operates on this type
// rather than on raw protocol-specific JSON.
type Request struct {
	ClientModel string
	Stream      bool

	System     []ContentBlock
	Messages   []Message
	Tools      []Tool
	ToolChoice *ToolChoice
	Params     Params
}

// Params holds generation-level parameters in a protocol-neutral way.
type Params struct {
	MaxTokens   *int
	Temperature *float64
	TopP        *float64
	Stop        []string

	Reasoning      *Reasoning
	ResponseFormat *ResponseFormat
}

// Clone returns a deep copy of Params so that ApplyOverride on the
// clone does not mutate the original (Reasoning is modified in-place).
func (p Params) Clone() Params {
	out := p
	if p.Reasoning != nil {
		r := *p.Reasoning
		out.Reasoning = &r
	}
	if p.ResponseFormat != nil {
		rf := *p.ResponseFormat
		out.ResponseFormat = &rf
	}
	return out
}

// Reasoning represents the request-side reasoning / thinking config.
type Reasoning struct {
	Mode         string  // "disabled", "enabled", "adaptive"
	Effort       *string // "low", "medium", "high"
	BudgetTokens *int
}

// Message is one turn in the conversation.
type Message struct {
	Role    string // "system", "user", "assistant", "tool"
	Content []ContentBlock
}

// ContentBlock is a single typed piece of content inside a message.
type ContentBlock struct {
	Type string // "text", "image", "tool_call", "tool_result", "thinking"

	Text                *string
	Image               *Image
	ToolCall            *ToolCall
	ToolResult          *ToolResult
	Thinking            *ThinkingBlock
	ServerToolUse       *ServerToolUse
	WebSearchToolResult *WebSearchToolResult
}

// Image stores image data in a protocol-neutral way.
type Image struct {
	SourceType string  // "data_url", "base64", "url"
	URL        *string // used when SourceType is "url" or "data_url"
	MediaType  *string // e.g. "image/png"
	Data       *string // raw base64 data
	Detail     string  // "low", "high", "auto"; empty means unset
}

// Tool describes a tool definition available for the model.
type Tool struct {
	Type        string // "" = function, "web_search" = built-in
	Name        string
	Description string
	InputSchema any
	MaxUses     *int
}

// ToolChoice is the provider-neutral representation of how tools may be used.
type ToolChoice struct {
	Type string // "auto", "required", "tool"
	Name string // required when Type == "tool"
}

// ToolCall represents a tool invocation produced by the model.
type ToolCall struct {
	ID        string
	Name      string
	Arguments any
}

// ToolResult is the result returned for a prior tool call.
type ToolResult struct {
	ToolCallID string
	Content    []ContentBlock
	IsError    bool
}

// ServerToolUse represents Anthropic's synthetic server-side web search usage block.
type ServerToolUse struct {
	ID    string
	Name  string
	Input any
}

// WebSearchToolResult represents Anthropic's synthetic web_search_tool_result block.
type WebSearchToolResult struct {
	ToolUseID string
	Content   []WebSearchResult
}

// ResponseFormat captures structured-output preferences that can be mapped to
// OpenAI-compatible backends.
type ResponseFormat struct {
	Type   string // "json_schema", "json_object"
	Name   string
	Schema any
	Strict *bool
}

// ThinkingBlock holds reasoning / chain-of-thought text produced by
// a model.  This must never be merged into visible answer text.
type ThinkingBlock struct {
	Text      *string
	Signature *string
}

// Response is the provider-neutral representation of an upstream
// completion response.
type Response struct {
	Model string
	ID    string

	Output     []Message
	StopReason string
	Usage      *Usage
}

// Usage holds token-accounting data in a protocol-neutral way.
type Usage struct {
	InputTokens      *int64
	OutputTokens     *int64
	TotalTokens      *int64
	ReasoningTokens  *int64
	CacheReadTokens  *int64
	CacheWriteTokens *int64
}

// StreamEvent is the minimal internal streaming abstraction for V1.
// Each upstream SSE chunk is decoded into one or more of these events.
type StreamEvent struct {
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

	// synthetic response-side blocks (for example web search)
	ServerToolUse       *ServerToolUse
	WebSearchToolResult *WebSearchToolResult

	// message_stop
	StopReason string

	// usage_final
	Usage *Usage

	// message-level metadata (typically from the first event)
	ResponseID string
	Model      string
}
