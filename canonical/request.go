package canonical

// Protocol identifies the wire protocol of a client or upstream provider.
type Protocol string

const (
	ProtocolOpenAIChat       Protocol = "openai_chat"
	ProtocolAnthropicMessage Protocol = "anthropic_messages"
)

// CanonicalRequest is the provider-neutral semantic representation of an
// inbound chat/messages request.  Every later module operates on this type
// rather than on raw protocol-specific JSON.
type CanonicalRequest struct {
	ClientProtocol Protocol
	ClientModel    string
	Stream         bool

	System   []CanonicalContentBlock
	Messages []CanonicalMessage
	Tools    []CanonicalTool
	Params   CanonicalParams
}

// CanonicalParams holds generation-level parameters in a protocol-neutral way.
type CanonicalParams struct {
	MaxTokens   *int
	Temperature *float64
	TopP        *float64
	Stop        []string

	Reasoning *CanonicalReasoning
}

// CanonicalReasoning represents the request-side reasoning / thinking config.
type CanonicalReasoning struct {
	Mode         string  // "disabled", "enabled", "adaptive"
	Effort       *string // "low", "medium", "high"
	BudgetTokens *int
}
