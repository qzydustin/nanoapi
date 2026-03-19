package canonical

// CanonicalResponse is the provider-neutral representation of an upstream
// completion response.
type CanonicalResponse struct {
	UpstreamProtocol Protocol
	Model            string
	ID               string

	Output     []CanonicalMessage
	StopReason string
	Usage      *CanonicalUsage
}

// CanonicalUsage holds token-accounting data in a protocol-neutral way.
type CanonicalUsage struct {
	InputTokens      *int64
	OutputTokens     *int64
	TotalTokens      *int64
	ReasoningTokens  *int64
	CacheReadTokens  *int64
	CacheWriteTokens *int64
}
