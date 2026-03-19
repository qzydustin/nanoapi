package canonical

// CanonicalUsage holds token-accounting data in a protocol-neutral way.
type CanonicalUsage struct {
	InputTokens      *int64
	OutputTokens     *int64
	TotalTokens      *int64
	ReasoningTokens  *int64
	CacheReadTokens  *int64
	CacheWriteTokens *int64
}
