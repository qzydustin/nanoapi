package execute

// ExecutionMode describes how the gateway executes an upstream request.
type ExecutionMode string

const (
	ModePassthroughStream ExecutionMode = "passthrough_stream"
	ModeDirectNonStream   ExecutionMode = "direct_non_stream"
	ModeAggregateStream   ExecutionMode = "aggregate_stream"
)

// DecideMode determines the execution mode based on client streaming
// preference and provider force-stream policy.
//
// Rule: UpstreamStream = ClientStream || ForceStream
func DecideMode(clientStream bool, forceStream bool) ExecutionMode {
	if clientStream {
		return ModePassthroughStream
	}
	if forceStream {
		return ModeAggregateStream
	}
	return ModeDirectNonStream
}
