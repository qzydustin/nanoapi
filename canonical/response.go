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
